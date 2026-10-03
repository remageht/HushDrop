package transfer

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

var safeIDRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

var dangerousExtensions = map[string]bool{
	".exe": true,
	".bat": true,
	".cmd": true,
	".ps1": true,
	".sh":  true,
	".vbs": true,
	".msi": true,
	".com": true,
	".scr": true,
	".reg": true,
	".jar": true,
	".dll": true,
	".sys": true,
}

type FileMetadata struct {
	ID             string    `json:"id"`
	OriginalName   string    `json:"originalName"`
	CleanName      string    `json:"cleanName"`
	Size           int64     `json:"size"`
	MimeType       string    `json:"mimeType"`
	TotalChunks    int       `json:"totalChunks"`
	UploadedChunks int       `json:"uploadedChunks"`
	SHA256         string    `json:"sha256"`
	IsDangerous    bool      `json:"isDangerous"`
	IsCompleted    bool      `json:"isCompleted"`
	CreatedAt      time.Time `json:"createdAt"`
	FilePath       string    `json:"-"`
	TempFilePath   string    `json:"-"`
}

type Manager struct {
	mu           sync.RWMutex
	downloadsDir string
	tempDir      string
	maxChunkSize int64
	maxFileSize  int64
	files        map[string]*FileMetadata // fileID -> metadata
	chunkTracker map[string]map[int]bool  // fileID -> set of received chunks
}

func NewManager(downloadsDir, tempDir string, maxChunkSize, maxFileSize int64) *Manager {
	return &Manager{
		downloadsDir: filepath.Clean(downloadsDir),
		tempDir:      filepath.Clean(tempDir),
		maxChunkSize: maxChunkSize,
		maxFileSize:  maxFileSize,
		files:        make(map[string]*FileMetadata),
		chunkTracker: make(map[string]map[int]bool),
	}
}

// SanitizeFilename eliminates path traversals, URL-encodings, null bytes, and malicious characters
func SanitizeFilename(rawName string) string {
	if unescaped, err := url.QueryUnescape(rawName); err == nil && unescaped != "" {
		rawName = unescaped
	}

	// Normalize Windows backslashes to forward slashes for cross-platform safety
	rawName = strings.ReplaceAll(rawName, "\\", "/")

	clean := filepath.Base(rawName)
	clean = strings.ReplaceAll(clean, "\x00", "")
	clean = strings.ReplaceAll(clean, "..", "")
	clean = strings.ReplaceAll(clean, "/", "")
	clean = strings.ReplaceAll(clean, "\\", "")
	clean = strings.TrimSpace(clean)

	if clean == "" || clean == "." {
		clean = "unnamed_file"
	}
	return clean
}

// IsDangerousFile checks if the extension is considered executable or hazardous
func IsDangerousFile(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	return dangerousExtensions[ext]
}

// InitUpload initializes a file upload session
func (m *Manager) InitUpload(fileID, rawName string, totalSize int64, totalChunks int) (*FileMetadata, error) {
	if !safeIDRegex.MatchString(fileID) || len(fileID) < 4 || len(fileID) > 64 {
		return nil, errors.New("invalid file id: must be alphanumeric and between 4 and 64 characters")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if totalSize > m.maxFileSize {
		return nil, fmt.Errorf("file size %d bytes exceeds max allowed %d bytes", totalSize, m.maxFileSize)
	}

	cleanName := SanitizeFilename(rawName)
	isDangerous := IsDangerousFile(cleanName)

	prefix := fileID
	if len(prefix) > 8 {
		prefix = prefix[:8]
	}

	tempPath := filepath.Join(m.tempDir, fmt.Sprintf("%s.part", fileID))
	finalPath := filepath.Join(m.downloadsDir, fmt.Sprintf("%s_%s", prefix, cleanName))

	// Strict directory containment verification (prevent directory traversal)
	if !strings.HasPrefix(filepath.Clean(tempPath), m.tempDir) {
		return nil, errors.New("temp path escape detected")
	}
	if !strings.HasPrefix(filepath.Clean(finalPath), m.downloadsDir) {
		return nil, errors.New("destination path escape detected")
	}

	// Pre-create/truncate temp file
	f, err := os.OpenFile(tempPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file: %w", err)
	}
	_ = f.Close()

	meta := &FileMetadata{
		ID:             fileID,
		OriginalName:   rawName,
		CleanName:      cleanName,
		Size:           totalSize,
		TotalChunks:    totalChunks,
		UploadedChunks: 0,
		IsDangerous:    isDangerous,
		IsCompleted:    false,
		CreatedAt:      time.Now(),
		FilePath:       finalPath,
		TempFilePath:   tempPath,
	}

	m.files[fileID] = meta
	m.chunkTracker[fileID] = make(map[int]bool)

	return meta, nil
}

// WriteChunk streams a chunk directly to disk at the calculated offset without loading it into RAM
func (m *Manager) WriteChunk(fileID string, chunkIndex int, chunkSize int64, chunkReader io.Reader) (*FileMetadata, error) {
	if chunkSize > m.maxChunkSize {
		return nil, fmt.Errorf("chunk size %d exceeds limit of %d bytes", chunkSize, m.maxChunkSize)
	}

	m.mu.Lock()
	meta, exists := m.files[fileID]
	if !exists {
		m.mu.Unlock()
		return nil, errors.New("upload session not initialized")
	}
	tracker := m.chunkTracker[fileID]
	m.mu.Unlock()

	// Open temp file for writing at calculated offset
	f, err := os.OpenFile(meta.TempFilePath, os.O_WRONLY, 0600)
	if err != nil {
		return nil, fmt.Errorf("failed to open temp file for chunk: %w", err)
	}

	offset := int64(chunkIndex) * m.maxChunkSize
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("failed to seek offset in temp file: %w", err)
	}

	// Stream chunk directly with small 64KB buffer to guarantee < 200MB RAM
	buf := make([]byte, 64*1024)
	written, err := io.CopyBuffer(f, io.LimitReader(chunkReader, m.maxChunkSize), buf)
	_ = f.Close()
	if err != nil {
		return nil, fmt.Errorf("failed to write chunk to disk: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if !tracker[chunkIndex] {
		tracker[chunkIndex] = true
		meta.UploadedChunks++
	}

	// Verify if all chunks have completed
	if meta.UploadedChunks >= meta.TotalChunks {
		if err := m.finalizeUploadLocked(meta, tracker); err != nil {
			return nil, err
		}
	}

	_ = written
	return meta, nil
}

func (m *Manager) finalizeUploadLocked(meta *FileMetadata, tracker map[int]bool) error {
	// Integrity check: verify every chunk from 0 to totalChunks-1 was received
	if len(tracker) != meta.TotalChunks {
		return fmt.Errorf("incomplete upload: received %d chunks out of %d", len(tracker), meta.TotalChunks)
	}
	for i := 0; i < meta.TotalChunks; i++ {
		if !tracker[i] {
			return fmt.Errorf("missing chunk %d before finalization", i)
		}
	}

	// Truncate temp file to exact final size
	if err := os.Truncate(meta.TempFilePath, meta.Size); err != nil {
		return fmt.Errorf("failed to truncate temp file to exact size: %w", err)
	}

	// Calculate SHA256 checksum in stream
	f, err := os.Open(meta.TempFilePath)
	if err != nil {
		return err
	}
	hasher := sha256.New()
	buf := make([]byte, 64*1024)
	if _, err := io.CopyBuffer(hasher, f, buf); err != nil {
		_ = f.Close()
		return err
	}
	_ = f.Close()

	meta.SHA256 = hex.EncodeToString(hasher.Sum(nil))

	// Move from temp to downloads
	if err := os.Rename(meta.TempFilePath, meta.FilePath); err != nil {
		return fmt.Errorf("failed to move temp file to downloads: %w", err)
	}

	meta.IsCompleted = true
	return nil
}

// ListFiles returns a copy of all completed files
func (m *Manager) ListFiles() []*FileMetadata {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*FileMetadata
	for _, meta := range m.files {
		if meta.IsCompleted {
			copyMeta := *meta
			result = append(result, &copyMeta)
		}
	}
	return result
}

// GetFile retrieves metadata for a specific file
func (m *Manager) GetFile(fileID string) (*FileMetadata, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	meta, exists := m.files[fileID]
	if !exists || !meta.IsCompleted {
		return nil, errors.New("file not found or incomplete")
	}
	return meta, nil
}

// ClearAll deletes all stored files and temporary chunks from disk ("Забыть всё")
func (m *Manager) ClearAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, meta := range m.files {
		_ = os.Remove(meta.TempFilePath)
		_ = os.Remove(meta.FilePath)
	}

	m.files = make(map[string]*FileMetadata)
	m.chunkTracker = make(map[string]map[int]bool)

	// Clean downloads and temp directory completely
	_ = os.RemoveAll(m.tempDir)
	_ = os.RemoveAll(m.downloadsDir)
	_ = os.MkdirAll(m.tempDir, 0700)
	_ = os.MkdirAll(m.downloadsDir, 0700)
}

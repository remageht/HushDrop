package transfer

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"hushdrop/internal/crypto"
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
	Encrypted      bool      `json:"isEncrypted,omitempty"`
	IsCompleted    bool      `json:"isCompleted"`
	CreatedAt      time.Time `json:"createdAt"`
	FilePath       string    `json:"-"`
	TempFilePath   string    `json:"-"`
}

type UploadStatus struct {
	FileMetadata
	ReceivedChunks []int `json:"receivedChunks"`
}

// IsValidFileID verifies that a file ID is alphanumeric and within length bounds (4-64 chars)
func IsValidFileID(fileID string) bool {
	return safeIDRegex.MatchString(fileID) && len(fileID) >= 4 && len(fileID) <= 64
}

type Manager struct {
	mu           sync.RWMutex
	downloadsDir string
	tempDir      string
	maxChunkSize int64
	maxFileSize  int64
	files        map[string]*FileMetadata // fileID -> metadata
	chunkTracker map[string]map[int]bool  // fileID -> set of received chunks
	atRestKey    []byte                   // non-nil enables at-rest encryption of saved files
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

// NewManagerWithEncryption creates a manager that transparently encrypts every
// completed upload into the crypto at-rest container format. The key must be exactly
// crypto.AtRestKeySize bytes (see crypto.LoadOrCreateAtRestKey).
func NewManagerWithEncryption(downloadsDir, tempDir string, maxChunkSize, maxFileSize int64, atRestKey []byte) *Manager {
	m := NewManager(downloadsDir, tempDir, maxChunkSize, maxFileSize)
	if len(atRestKey) > 0 {
		m.atRestKey = bytes.Clone(atRestKey)
	}
	return m
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
	// Strip control characters (CR/LF/TAB etc.) to prevent header injection
	clean = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, clean)
	// ':' is invalid for filenames on NTFS (alternate data streams)
	clean = strings.ReplaceAll(clean, ":", "_")
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
	if !IsValidFileID(fileID) {
		return nil, errors.New("invalid file id: must be alphanumeric and between 4 and 64 characters")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	cleanName := SanitizeFilename(rawName)

	if existing, exists := m.files[fileID]; exists {
		if existing.IsCompleted {
			snapshot := *existing
			return &snapshot, nil
		}
		if existing.Size == totalSize && existing.CleanName == cleanName {
			snapshot := *existing
			return &snapshot, nil
		}
		return nil, fmt.Errorf("upload session exists with different metadata")
	}

	if totalSize <= 0 || totalSize > m.maxFileSize {
		return nil, fmt.Errorf("invalid declared file size %d: must be between 1 and %d bytes", totalSize, m.maxFileSize)
	}

	// Chunk count must correspond exactly to the declared size at max chunk granularity,
	// otherwise a client could pad the file far beyond the declared limits.
	expectedChunks := (totalSize + m.maxChunkSize - 1) / m.maxChunkSize
	if int64(totalChunks) != expectedChunks {
		return nil, fmt.Errorf("chunk count mismatch: declared %d bytes needs exactly %d chunks, got %d", totalSize, expectedChunks, totalChunks)
	}

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

	snapshot := *meta
	return &snapshot, nil
}

// WriteChunk streams a chunk directly to disk at the calculated offset without loading it into RAM
func (m *Manager) WriteChunk(fileID string, chunkIndex int, chunkSize int64, chunkReader io.Reader) (*FileMetadata, error) {
	if chunkSize <= 0 || chunkSize > m.maxChunkSize {
		return nil, fmt.Errorf("chunk size %d exceeds limit of %d bytes", chunkSize, m.maxChunkSize)
	}

	if chunkIndex < 0 {
		return nil, fmt.Errorf("invalid chunk index %d", chunkIndex)
	}

	m.mu.Lock()
	meta, exists := m.files[fileID]
	if !exists {
		m.mu.Unlock()
		return nil, errors.New("upload session not initialized")
	}
	if chunkIndex >= meta.TotalChunks {
		m.mu.Unlock()
		return nil, fmt.Errorf("chunk index %d out of range (total chunks %d)", chunkIndex, meta.TotalChunks)
	}
	offset := int64(chunkIndex) * m.maxChunkSize
	if offset < 0 || offset >= meta.Size {
		m.mu.Unlock()
		return nil, fmt.Errorf("chunk offset %d beyond declared file size %d", offset, meta.Size)
	}
	tracker := m.chunkTracker[fileID]
	if meta.IsCompleted {
		// Upload already finalized: acknowledge idempotently without rewriting the file
		snapshot := *meta
		m.mu.Unlock()
		return &snapshot, nil
	}
	m.mu.Unlock()

	// Open temp file for writing at calculated offset
	f, err := os.OpenFile(meta.TempFilePath, os.O_WRONLY, 0600)
	if err != nil {
		return nil, fmt.Errorf("failed to open temp file for chunk: %w", err)
	}

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
	if chunkSize >= 0 && written != chunkSize {
		return nil, fmt.Errorf("incomplete chunk write: %d of %d bytes received", written, chunkSize)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	currentMeta, exists := m.files[fileID]
	if !exists || currentMeta != meta {
		return nil, errors.New("upload session expired or revoked")
	}

	// Re-check completion under lock: a concurrent request may have finalized already
	if meta.IsCompleted {
		snapshot := *meta
		return &snapshot, nil
	}

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

	// Return a snapshot copy: callers must not read shared state outside the lock
	snapshot := *meta
	return &snapshot, nil
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

	// Store the file: either a plain rename (default) or a streamed at-rest encrypted
	// container. SHA256 above always refers to the plaintext.
	meta.Encrypted = m.atRestKey != nil
	if meta.Encrypted {
		if err := m.encryptToFinalLocked(meta); err != nil {
			return fmt.Errorf("at-rest encryption of %s failed: %w", meta.CleanName, err)
		}
	} else if err := os.Rename(meta.TempFilePath, meta.FilePath); err != nil {
		return fmt.Errorf("failed to move temp file to downloads: %w", err)
	}

	meta.IsCompleted = true
	return nil
}

// encryptToFinalLocked streams the plaintext temp file into an at-rest encrypted
// container at the final destination, verifying the container in the process.
func (m *Manager) encryptToFinalLocked(meta *FileMetadata) error {
	src, err := os.Open(meta.TempFilePath)
	if err != nil {
		return fmt.Errorf("open temp file: %w", err)
	}

	dst, err := os.OpenFile(meta.FilePath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		_ = src.Close()
		return fmt.Errorf("create final file: %w", err)
	}

	w, werr := crypto.NewAtRestWriter(dst, m.atRestKey)
	if werr == nil {
		buf := make([]byte, 64*1024)
		_, werr = io.CopyBuffer(w, src, buf)
		if werr == nil {
			werr = w.Close()
		}
	}
	if cerr := dst.Close(); werr == nil {
		werr = cerr
	}
	_ = src.Close()

	if werr != nil {
		// Never leave a partially written container behind.
		_ = os.Remove(meta.FilePath)
		return werr
	}

	// Cheap structural validation: the container must announce itself and outweigh
	// the plaintext by the framing overhead.
	out, err := os.Open(meta.FilePath)
	if err == nil {
		enc, verr := crypto.IsEncrypted(out)
		size, _ := out.Seek(0, io.SeekEnd)
		_ = out.Close()
		if verr != nil {
			return verr
		}
		if !enc {
			return errors.New("written container misses the at-rest magic header")
		}
		if size <= meta.Size {
			return fmt.Errorf("container size %d suspiciously small for %d plaintext bytes", size, meta.Size)
		}
	}

	_ = os.Remove(meta.TempFilePath)
	return nil
}

// OpenDownload opens meta's backing file for reading. Encrypted files are returned as
// a streaming decryptor over the at-rest container. The caller must close the file.
func (m *Manager) OpenDownload(meta *FileMetadata) (io.Reader, *os.File, bool, error) {
	file, err := os.Open(meta.FilePath)
	if err != nil {
		return nil, nil, false, err
	}
	if !meta.Encrypted {
		return file, file, false, nil
	}
	if m.atRestKey == nil {
		_ = file.Close()
		return nil, nil, false, errors.New("file is encrypted but the at-rest key is not available")
	}
	reader, err := crypto.NewAtRestReader(file, m.atRestKey)
	if err != nil {
		_ = file.Close()
		return nil, nil, false, err
	}
	return reader, file, true, nil
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
	copyMeta := *meta
	return &copyMeta, nil
}

// GetUploadStatus returns the upload progress and received chunks for a session
func (m *Manager) GetUploadStatus(fileID string) (*UploadStatus, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	meta, exists := m.files[fileID]
	if !exists {
		return nil, os.ErrNotExist
	}

	received := make([]int, 0, len(m.chunkTracker[fileID]))
	if tracker, ok := m.chunkTracker[fileID]; ok {
		for chunkIdx := range tracker {
			received = append(received, chunkIdx)
		}
		sort.Ints(received)
	}

	return &UploadStatus{
		FileMetadata:   *meta,
		ReceivedChunks: received,
	}, nil
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

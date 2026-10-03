package transfer

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
	"testing"
)

func TestSanitizeFilename(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"../../etc/passwd", "passwd"},
		{"..\\..\\Windows\\System32\\cmd.exe", "cmd.exe"},
		{"hello%20world.txt", "hello world.txt"},
		{"null\x00byte.pdf", "nullbyte.pdf"},
		{"../secret/archive.tar.gz", "archive.tar.gz"},
		{"", "unnamed_file"},
		{"...", "unnamed_file"},
	}

	for _, tc := range cases {
		out := SanitizeFilename(tc.input)
		if strings.Contains(out, "/") || strings.Contains(out, "\\") || strings.Contains(out, "..") || strings.Contains(out, "\x00") {
			t.Fatalf("sanitize failed for input %q, got unsafe output %q", tc.input, out)
		}
		if out != tc.expected {
			t.Errorf("SanitizeFilename(%q) = %q; want %q", tc.input, out, tc.expected)
		}
	}
}

func TestDangerousExtensions(t *testing.T) {
	dangerous := []string{"test.exe", "script.sh", "payload.ps1", "auto.bat", "setup.msi", "lib.dll"}
	for _, f := range dangerous {
		if !IsDangerousFile(f) {
			t.Errorf("expected %s to be flagged dangerous", f)
		}
	}

	safe := []string{"photo.jpg", "document.pdf", "notes.txt", "archive.zip"}
	for _, f := range safe {
		if IsDangerousFile(f) {
			t.Errorf("expected %s to be safe", f)
		}
	}
}

func TestTransferManager_ChunkUploadAndIntegrity(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "hushdrop_test_temp_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	downloadsDir, err := os.MkdirTemp("", "hushdrop_test_dl_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(downloadsDir)

	maxChunk := int64(1024)      // 1KB chunks for test
	maxFile := int64(1024 * 1024) // 1MB

	tm := NewManager(downloadsDir, tempDir, maxChunk, maxFile)

	// Test Path Traversal in File ID
	_, err = tm.InitUpload("../../badid", "safe.txt", 100, 1)
	if err == nil {
		t.Fatalf("expected InitUpload with traversal in fileID to fail")
	}

	// Legitimate 2-chunk upload
	data1 := bytes.Repeat([]byte("A"), 1024)
	data2 := bytes.Repeat([]byte("B"), 500)
	fullData := append(data1, data2...)
	fullHashBytes := sha256.Sum256(fullData)
	expectedHash := hex.EncodeToString(fullHashBytes[:])

	fileID := "file12345"
	meta, err := tm.InitUpload(fileID, "my_data.txt", int64(len(fullData)), 2)
	if err != nil {
		t.Fatalf("InitUpload failed: %v", err)
	}

	// Write chunk 0
	meta, err = tm.WriteChunk(fileID, 0, int64(len(data1)), bytes.NewReader(data1))
	if err != nil {
		t.Fatalf("WriteChunk 0 failed: %v", err)
	}
	if meta.IsCompleted {
		t.Fatalf("file should not be completed after chunk 0 of 2")
	}

	// Write chunk 1
	meta, err = tm.WriteChunk(fileID, 1, int64(len(data2)), bytes.NewReader(data2))
	if err != nil {
		t.Fatalf("WriteChunk 1 failed: %v", err)
	}
	if !meta.IsCompleted {
		t.Fatalf("file should be completed after chunk 1 of 2")
	}
	if meta.SHA256 != expectedHash {
		t.Fatalf("SHA256 mismatch: got %s, want %s", meta.SHA256, expectedHash)
	}

	// Verify file on disk
	fileMeta, err := tm.GetFile(fileID)
	if err != nil {
		t.Fatalf("GetFile failed: %v", err)
	}
	diskData, err := os.ReadFile(fileMeta.FilePath)
	if err != nil {
		t.Fatalf("failed to read finalized file on disk: %v", err)
	}
	if !bytes.Equal(diskData, fullData) {
		t.Fatalf("data on disk mismatch")
	}

	// Verify ClearAll deletes files
	tm.ClearAll()
	if _, err := os.Stat(fileMeta.FilePath); !os.IsNotExist(err) {
		t.Fatalf("expected file to be deleted after ClearAll")
	}
}

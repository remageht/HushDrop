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

func TestUploadResumption(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "hushdrop_test_resumption_temp_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	downloadsDir, err := os.MkdirTemp("", "hushdrop_test_resumption_dl_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(downloadsDir)

	maxChunk := int64(4 * 1024 * 1024)      // 4MB max chunk
	maxFile := int64(5 * 1024 * 1024 * 1024) // 5GB max file

	tm := NewManager(downloadsDir, tempDir, maxChunk, maxFile)

	// 1. InitUpload for a 2-chunk file (5MB total size, max chunk 4MB -> 2 chunks).
	fileID := "resumable-file-1234"
	fileName := "large_document.pdf"
	totalSize := int64(5 * 1024 * 1024) // 5MB
	totalChunks := 2

	meta, err := tm.InitUpload(fileID, fileName, totalSize, totalChunks)
	if err != nil {
		t.Fatalf("InitUpload failed: %v", err)
	}
	if meta.UploadedChunks != 0 || meta.IsCompleted {
		t.Fatalf("expected 0 uploaded chunks and incomplete, got %d, %v", meta.UploadedChunks, meta.IsCompleted)
	}

	// 2. Write chunk 0 (4MB)
	chunk0Data := bytes.Repeat([]byte("X"), int(maxChunk))
	meta, err = tm.WriteChunk(fileID, 0, int64(len(chunk0Data)), bytes.NewReader(chunk0Data))
	if err != nil {
		t.Fatalf("WriteChunk 0 failed: %v", err)
	}
	if meta.UploadedChunks != 1 {
		t.Fatalf("expected 1 uploaded chunk, got %d", meta.UploadedChunks)
	}

	// 3. Call GetUploadStatus -> verify uploadedChunks == 1, receivedChunks contains 0, isCompleted == false.
	status, err := tm.GetUploadStatus(fileID)
	if err != nil {
		t.Fatalf("GetUploadStatus failed: %v", err)
	}
	if status.UploadedChunks != 1 {
		t.Fatalf("expected status.UploadedChunks == 1, got %d", status.UploadedChunks)
	}
	if status.IsCompleted {
		t.Fatalf("expected status.IsCompleted == false")
	}
	if len(status.ReceivedChunks) != 1 || status.ReceivedChunks[0] != 0 {
		t.Fatalf("expected receivedChunks to contain [0], got %v", status.ReceivedChunks)
	}

	// 4. Call InitUpload again with same fileID -> verify it succeeds and retains progress.
	resumedMeta, err := tm.InitUpload(fileID, fileName, totalSize, totalChunks)
	if err != nil {
		t.Fatalf("resumed InitUpload failed: %v", err)
	}
	if resumedMeta.UploadedChunks != 1 {
		t.Fatalf("expected retained progress uploadedChunks == 1, got %d", resumedMeta.UploadedChunks)
	}

	// Verify status still retains chunk 0
	statusAfterResume, err := tm.GetUploadStatus(fileID)
	if err != nil {
		t.Fatalf("GetUploadStatus after resume failed: %v", err)
	}
	if len(statusAfterResume.ReceivedChunks) != 1 || statusAfterResume.ReceivedChunks[0] != 0 {
		t.Fatalf("expected receivedChunks after resume to be [0], got %v", statusAfterResume.ReceivedChunks)
	}

	// Test conflicting metadata on InitUpload while upload is in-flight
	if _, err := tm.InitUpload(fileID, "different_name.pdf", totalSize, totalChunks); err == nil {
		t.Fatalf("expected error when InitUpload has conflicting filename")
	}
	if _, err := tm.InitUpload(fileID, fileName, totalSize+100, totalChunks); err == nil {
		t.Fatalf("expected error when InitUpload has conflicting size")
	}

	// 5. Write chunk 1 -> verify upload completes (isCompleted == true) and final file exists with correct size.
	chunk1Size := totalSize - maxChunk // 1MB
	chunk1Data := bytes.Repeat([]byte("Y"), int(chunk1Size))
	meta, err = tm.WriteChunk(fileID, 1, chunk1Size, bytes.NewReader(chunk1Data))
	if err != nil {
		t.Fatalf("WriteChunk 1 failed: %v", err)
	}
	if !meta.IsCompleted {
		t.Fatalf("expected upload to complete after chunk 1")
	}
	if meta.UploadedChunks != 2 {
		t.Fatalf("expected 2 uploaded chunks, got %d", meta.UploadedChunks)
	}

	finalStatus, err := tm.GetUploadStatus(fileID)
	if err != nil {
		t.Fatalf("final GetUploadStatus failed: %v", err)
	}
	if !finalStatus.IsCompleted {
		t.Fatalf("expected finalStatus.IsCompleted == true")
	}
	if len(finalStatus.ReceivedChunks) != 2 || finalStatus.ReceivedChunks[0] != 0 || finalStatus.ReceivedChunks[1] != 1 {
		t.Fatalf("expected receivedChunks to contain [0, 1], got %v", finalStatus.ReceivedChunks)
	}

	fileInfo, err := os.Stat(meta.FilePath)
	if err != nil {
		t.Fatalf("final file not found on disk at %s: %v", meta.FilePath, err)
	}
	if fileInfo.Size() != totalSize {
		t.Fatalf("final file size mismatch: got %d, want %d", fileInfo.Size(), totalSize)
	}

	// Calling InitUpload after completion returns snapshot copy idempotently
	completedMeta, err := tm.InitUpload(fileID, fileName, totalSize, totalChunks)
	if err != nil || !completedMeta.IsCompleted {
		t.Fatalf("expected completed snapshot from InitUpload after completion, got %v, err: %v", completedMeta, err)
	}

	// Test non-existent file
	if _, err := tm.GetUploadStatus("non-existent-id"); err == nil {
		t.Fatalf("expected error for non-existent file ID in GetUploadStatus")
	}
}

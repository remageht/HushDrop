package crypto

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"errors"
	"io"
	"os"
	"testing"
)

func TestAtRest_EncryptDecrypt_Success(t *testing.T) {
	testSizes := []struct {
		name string
		size int
	}{
		{name: "100_Bytes", size: 100},
		{name: "2_MB", size: 2 * 1024 * 1024},
	}

	passphrase := "SecureHushDropP@ssphrase2026!"

	for _, tc := range testSizes {
		t.Run(tc.name, func(t *testing.T) {
			// Generate random test payload
			originalData := make([]byte, tc.size)
			if _, err := io.ReadFull(rand.Reader, originalData); err != nil {
				t.Fatalf("failed to generate random data: %v", err)
			}

			// 1. Encrypt stream
			src := bytes.NewReader(originalData)
			var encryptedBuf bytes.Buffer
			if err := EncryptStream(src, &encryptedBuf, passphrase); err != nil {
				t.Fatalf("EncryptStream failed: %v", err)
			}

			encryptedBytes := encryptedBuf.Bytes()

			// Expected minimum length: header(16) + salt(16) + nonce(12) + payload(tc.size) + tag(16) = tc.size + 60
			expectedLen := tc.size + 60
			if len(encryptedBytes) != expectedLen {
				t.Fatalf("expected encrypted size %d, got %d", expectedLen, len(encryptedBytes))
			}

			// 2. Test IsEncrypted across various reader types
			// A. Test with *bytes.Reader (io.ReadSeeker)
			bytesReader := bytes.NewReader(encryptedBytes)
			isEnc, err := IsEncrypted(bytesReader)
			if err != nil {
				t.Fatalf("IsEncrypted (*bytes.Reader) error: %v", err)
			}
			if !isEnc {
				t.Fatalf("expected IsEncrypted (*bytes.Reader) to be true")
			}
			// Verify read position was NOT altered
			pos, _ := bytesReader.Seek(0, io.SeekCurrent)
			if pos != 0 {
				t.Fatalf("expected reader position 0, got %d", pos)
			}

			// B. Test with *bufio.Reader
			bufReader := bufio.NewReader(bytes.NewReader(encryptedBytes))
			isEnc, err = IsEncrypted(bufReader)
			if err != nil {
				t.Fatalf("IsEncrypted (*bufio.Reader) error: %v", err)
			}
			if !isEnc {
				t.Fatalf("expected IsEncrypted (*bufio.Reader) to be true")
			}
			// Verify peek didn't consume bytes
			firstByte, err := bufReader.ReadByte()
			if err != nil || firstByte != 'H' {
				t.Fatalf("expected first byte 'H', got %v (err: %v)", firstByte, err)
			}

			// C. Test with *bytes.Buffer (byteProvider)
			bufCopy := bytes.NewBuffer(encryptedBytes)
			isEnc, err = IsEncrypted(bufCopy)
			if err != nil {
				t.Fatalf("IsEncrypted (*bytes.Buffer) error: %v", err)
			}
			if !isEnc {
				t.Fatalf("expected IsEncrypted (*bytes.Buffer) to be true")
			}
			if bufCopy.Len() != len(encryptedBytes) {
				t.Fatalf("expected buffer length unchanged, got %d", bufCopy.Len())
			}

			// D. Test with *os.File
			tmpFile, err := os.CreateTemp("", "hushdrop_atrest_test_*")
			if err != nil {
				t.Fatalf("failed to create temp file: %v", err)
			}
			defer os.Remove(tmpFile.Name())
			defer tmpFile.Close()

			if _, err := tmpFile.Write(encryptedBytes); err != nil {
				t.Fatalf("failed to write to temp file: %v", err)
			}
			if _, err := tmpFile.Seek(0, io.SeekStart); err != nil {
				t.Fatalf("failed to seek temp file: %v", err)
			}

			isEnc, err = IsEncrypted(tmpFile)
			if err != nil {
				t.Fatalf("IsEncrypted (*os.File) error: %v", err)
			}
			if !isEnc {
				t.Fatalf("expected IsEncrypted (*os.File) to be true")
			}
			filePos, _ := tmpFile.Seek(0, io.SeekCurrent)
			if filePos != 0 {
				t.Fatalf("expected file position 0, got %d", filePos)
			}

			// 3. Decrypt stream
			decSrc := bytes.NewReader(encryptedBytes)
			var decryptedBuf bytes.Buffer
			if err := DecryptStream(decSrc, &decryptedBuf, passphrase); err != nil {
				t.Fatalf("DecryptStream failed: %v", err)
			}

			// 4. Verify plaintext equality
			decryptedBytes := decryptedBuf.Bytes()
			if !bytes.Equal(originalData, decryptedBytes) {
				t.Fatalf("decrypted data does not match original data (len %d vs %d)", len(originalData), len(decryptedBytes))
			}
		})
	}
}

func TestAtRest_WrongPassphrase(t *testing.T) {
	data := []byte("Sensitive HushDrop message to be encrypted at rest")
	passphrase := "CorrectPassphrase#123"
	wrongPassphrase := "IncorrectPassphrase#999"

	var encryptedBuf bytes.Buffer
	if err := EncryptStream(bytes.NewReader(data), &encryptedBuf, passphrase); err != nil {
		t.Fatalf("EncryptStream failed: %v", err)
	}

	var decryptedBuf bytes.Buffer
	err := DecryptStream(bytes.NewReader(encryptedBuf.Bytes()), &decryptedBuf, wrongPassphrase)
	if err == nil {
		t.Fatalf("expected error when decrypting with wrong passphrase, got nil")
	}

	if !errors.Is(err, ErrInvalidPassphrase) {
		t.Fatalf("expected ErrInvalidPassphrase, got: %v", err)
	}

	if decryptedBuf.Len() > 0 {
		t.Fatalf("expected no output on decryption failure, got %d bytes", decryptedBuf.Len())
	}
}

func TestAtRest_CorruptedCiphertext(t *testing.T) {
	data := []byte("Sensitive HushDrop payload that will have bytes corrupted")
	passphrase := "TestPassphrase2026"

	var encryptedBuf bytes.Buffer
	if err := EncryptStream(bytes.NewReader(data), &encryptedBuf, passphrase); err != nil {
		t.Fatalf("EncryptStream failed: %v", err)
	}

	encryptedBytes := encryptedBuf.Bytes()

	t.Run("CorruptedPayloadByte", func(t *testing.T) {
		corrupted := make([]byte, len(encryptedBytes))
		copy(corrupted, encryptedBytes)

		// Corrupt a byte in the encrypted payload area (after header=16, salt=16, nonce=12 -> index >= 44)
		corrupted[44] ^= 0xFF

		var decryptedBuf bytes.Buffer
		err := DecryptStream(bytes.NewReader(corrupted), &decryptedBuf, passphrase)
		if err == nil {
			t.Fatalf("expected error on corrupted payload byte, got nil")
		}
		if !errors.Is(err, ErrInvalidPassphrase) {
			t.Fatalf("expected ErrInvalidPassphrase on corrupted payload, got: %v", err)
		}
		if decryptedBuf.Len() > 0 {
			t.Fatalf("expected no output on corrupted payload, got %d bytes", decryptedBuf.Len())
		}
	})

	t.Run("CorruptedAuthTagByte", func(t *testing.T) {
		corrupted := make([]byte, len(encryptedBytes))
		copy(corrupted, encryptedBytes)

		// Corrupt the very last byte (part of the 16-byte GCM authentication tag)
		corrupted[len(corrupted)-1] ^= 0xAA

		var decryptedBuf bytes.Buffer
		err := DecryptStream(bytes.NewReader(corrupted), &decryptedBuf, passphrase)
		if err == nil {
			t.Fatalf("expected error on corrupted GCM tag, got nil")
		}
		if !errors.Is(err, ErrInvalidPassphrase) {
			t.Fatalf("expected ErrInvalidPassphrase on corrupted tag, got: %v", err)
		}
	})

	t.Run("CorruptedNonce", func(t *testing.T) {
		corrupted := make([]byte, len(encryptedBytes))
		copy(corrupted, encryptedBytes)

		// Nonce starts at index 32 (header 16 + salt 16)
		corrupted[32] ^= 0x55

		var decryptedBuf bytes.Buffer
		err := DecryptStream(bytes.NewReader(corrupted), &decryptedBuf, passphrase)
		if err == nil {
			t.Fatalf("expected error on corrupted nonce, got nil")
		}
		if !errors.Is(err, ErrInvalidPassphrase) {
			t.Fatalf("expected ErrInvalidPassphrase on corrupted nonce, got: %v", err)
		}
	})

	t.Run("TruncatedCiphertext", func(t *testing.T) {
		// Truncate to less than tag size
		truncated := encryptedBytes[:44] // Only header, salt, nonce, no ciphertext/tag

		var decryptedBuf bytes.Buffer
		err := DecryptStream(bytes.NewReader(truncated), &decryptedBuf, passphrase)
		if err == nil {
			t.Fatalf("expected error on truncated stream, got nil")
		}
		if !errors.Is(err, ErrInvalidPassphrase) {
			t.Fatalf("expected ErrInvalidPassphrase on truncated stream, got: %v", err)
		}
	})
}

func TestAtRest_IsEncrypted_Unencrypted(t *testing.T) {
	plainData := []byte("This is plain unencrypted text that does not have hushdrop header.")

	isEnc, err := IsEncrypted(bytes.NewReader(plainData))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if isEnc {
		t.Fatalf("expected plainData to not be marked encrypted")
	}

	// Short data (< 16 bytes)
	shortData := []byte("short")
	isEnc, err = IsEncrypted(bytes.NewReader(shortData))
	if err != nil {
		t.Fatalf("unexpected error on short data: %v", err)
	}
	if isEnc {
		t.Fatalf("expected shortData to not be marked encrypted")
	}

	// Empty data
	isEnc, err = IsEncrypted(bytes.NewReader([]byte{}))
	if err != nil {
		t.Fatalf("unexpected error on empty data: %v", err)
	}
	if isEnc {
		t.Fatalf("expected empty data to not be marked encrypted")
	}
}

func TestAtRest_InvalidHeader(t *testing.T) {
	fakeData := []byte("SOME_RANDOM_HEAD16BYTES_OF_DATA_PLUS_MORE")
	var decryptedBuf bytes.Buffer
	err := DecryptStream(bytes.NewReader(fakeData), &decryptedBuf, "passphrase")
	if err == nil {
		t.Fatalf("expected error on invalid header, got nil")
	}
	if !errors.Is(err, ErrInvalidHeader) {
		t.Fatalf("expected ErrInvalidHeader, got: %v", err)
	}
}

func TestAtRest_EmptyPassphrase(t *testing.T) {
	data := []byte("test data")
	var buf bytes.Buffer
	if err := EncryptStream(bytes.NewReader(data), &buf, ""); err == nil {
		t.Fatalf("expected error on empty passphrase in EncryptStream, got nil")
	}

	if err := DecryptStream(bytes.NewReader(data), &buf, ""); err == nil {
		t.Fatalf("expected error on empty passphrase in DecryptStream, got nil")
	}
}

func TestAtRest_PeekHelper(t *testing.T) {
	data := []byte("Plain text test for peek helper")
	r := bytes.NewReader(data)

	isEnc, restoredReader, err := PeekIsEncrypted(r)
	if err != nil {
		t.Fatalf("PeekIsEncrypted error: %v", err)
	}
	if isEnc {
		t.Fatalf("expected isEnc false")
	}

	readAll, err := io.ReadAll(restoredReader)
	if err != nil {
		t.Fatalf("ReadAll error: %v", err)
	}
	if !bytes.Equal(readAll, data) {
		t.Fatalf("restoredReader did not return all original data")
	}
}

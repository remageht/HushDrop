package crypto

import (
	"bufio"
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/argon2"
)

const (
	// AtRestMagicHeader is the 16-byte magic identifier for HushDrop at-rest encrypted files.
	AtRestMagicHeader = "HUSHDROP_ENC_V1\n"

	// SaltSize is the length in bytes of the Argon2id salt.
	SaltSize = 16

	// NonceSize is the length in bytes of the standard AES-GCM nonce.
	NonceSize = 12

	// Argon2Time is the number of iterations for Argon2id key derivation.
	Argon2Time uint32 = 1

	// Argon2Memory is the memory cost in KiB for Argon2id (64 MB).
	Argon2Memory uint32 = 64 * 1024

	// Argon2Threads is the degree of parallelism for Argon2id.
	Argon2Threads uint8 = 4

	// KeyLength is the derived AES-256 key size in bytes.
	KeyLength uint32 = 32
)

var (
	// ErrInvalidPassphrase is returned when decryption fails due to an incorrect passphrase or corrupted data.
	ErrInvalidPassphrase = errors.New("invalid passphrase or corrupted data")

	// ErrInvalidHeader is returned when the stream does not begin with the expected magic header.
	ErrInvalidHeader = errors.New("invalid hushdrop encryption header")

	// ErrEmptyPassphrase is returned when an empty passphrase is provided.
	ErrEmptyPassphrase = errors.New("passphrase cannot be empty")
)

type peeker interface {
	Peek(n int) ([]byte, error)
}

type byteProvider interface {
	Bytes() []byte
}

// EncryptStream encrypts data from src using AES-256-GCM with an Argon2id-derived key
// and writes the encrypted payload with magic header, salt, and nonce to dst.
func EncryptStream(src io.Reader, dst io.Writer, passphrase string) error {
	if passphrase == "" {
		return ErrEmptyPassphrase
	}

	// 1. Generate cryptographically secure random salt (16 bytes)
	salt := make([]byte, SaltSize)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return fmt.Errorf("failed to generate random salt: %w", err)
	}

	// 2. Derive 32-byte key via Argon2id
	key := argon2.IDKey([]byte(passphrase), salt, Argon2Time, Argon2Memory, Argon2Threads, KeyLength)
	defer Zeroize(key)

	// 3. Initialize AES cipher and GCM mode
	block, err := aes.NewCipher(key)
	if err != nil {
		return fmt.Errorf("failed to initialize AES cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return fmt.Errorf("failed to initialize GCM cipher: %w", err)
	}

	// 4. Generate random nonce (12 bytes)
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return fmt.Errorf("failed to generate nonce: %w", err)
	}

	// 5. Read all plaintext from src
	plaintext, err := io.ReadAll(src)
	if err != nil {
		return fmt.Errorf("failed to read plaintext: %w", err)
	}

	// 6. Encrypt plaintext (appends 16-byte GCM authentication tag)
	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)

	// 7. Write magic header, salt, nonce, and ciphertext to dst
	if _, err := dst.Write([]byte(AtRestMagicHeader)); err != nil {
		return fmt.Errorf("failed to write magic header: %w", err)
	}
	if _, err := dst.Write(salt); err != nil {
		return fmt.Errorf("failed to write salt: %w", err)
	}
	if _, err := dst.Write(nonce); err != nil {
		return fmt.Errorf("failed to write nonce: %w", err)
	}
	if _, err := dst.Write(ciphertext); err != nil {
		return fmt.Errorf("failed to write ciphertext: %w", err)
	}

	return nil
}

// DecryptStream reads an at-rest encrypted stream from src, validates the magic header,
// derives the AES-256-GCM key via Argon2id, verifies the authentication tag, and writes the plaintext to dst.
func DecryptStream(src io.Reader, dst io.Writer, passphrase string) error {
	if passphrase == "" {
		return ErrInvalidPassphrase
	}

	// 1. Verify magic header (16 bytes)
	header := make([]byte, len(AtRestMagicHeader))
	if _, err := io.ReadFull(src, header); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return ErrInvalidHeader
		}
		return fmt.Errorf("failed to read magic header: %w", err)
	}
	if !bytes.Equal(header, []byte(AtRestMagicHeader)) {
		return ErrInvalidHeader
	}

	// 2. Read salt (16 bytes)
	salt := make([]byte, SaltSize)
	if _, err := io.ReadFull(src, salt); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return ErrInvalidPassphrase
		}
		return fmt.Errorf("failed to read salt: %w", err)
	}

	// 3. Read nonce (12 bytes)
	nonce := make([]byte, NonceSize)
	if _, err := io.ReadFull(src, nonce); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return ErrInvalidPassphrase
		}
		return fmt.Errorf("failed to read nonce: %w", err)
	}

	// 4. Derive key via Argon2id
	key := argon2.IDKey([]byte(passphrase), salt, Argon2Time, Argon2Memory, Argon2Threads, KeyLength)
	defer Zeroize(key)

	// 5. Initialize AES cipher and GCM mode
	block, err := aes.NewCipher(key)
	if err != nil {
		return fmt.Errorf("failed to initialize AES cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return fmt.Errorf("failed to initialize GCM cipher: %w", err)
	}

	// 6. Read remaining ciphertext + tag
	ciphertext, err := io.ReadAll(src)
	if err != nil {
		return fmt.Errorf("failed to read ciphertext: %w", err)
	}
	if len(ciphertext) < gcm.Overhead() {
		return ErrInvalidPassphrase
	}

	// 7. Decrypt and verify authentication tag
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return ErrInvalidPassphrase
	}

	// 8. Write authenticated plaintext to dst
	if _, err := dst.Write(plaintext); err != nil {
		return fmt.Errorf("failed to write decrypted plaintext: %w", err)
	}

	return nil
}

// IsEncrypted inspects the first 16 bytes of r to check for the HushDrop at-rest magic header
// without advancing the reader's position. It supports io.ReadSeeker, *bufio.Reader, and buffers.
func IsEncrypted(r io.Reader) (bool, error) {
	// 1. If reader supports Peeker (e.g. *bufio.Reader)
	if p, ok := r.(peeker); ok {
		header, err := p.Peek(len(AtRestMagicHeader))
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return false, nil
			}
			return false, err
		}
		return bytes.Equal(header, []byte(AtRestMagicHeader)), nil
	}

	// 2. If reader supports byteProvider (e.g. *bytes.Buffer)
	if bp, ok := r.(byteProvider); ok {
		b := bp.Bytes()
		if len(b) < len(AtRestMagicHeader) {
			return false, nil
		}
		return bytes.Equal(b[:len(AtRestMagicHeader)], []byte(AtRestMagicHeader)), nil
	}

	// 3. If reader supports io.ReadSeeker (e.g. *os.File, *bytes.Reader, *strings.Reader)
	if rs, ok := r.(io.ReadSeeker); ok {
		header := make([]byte, len(AtRestMagicHeader))
		n, err := io.ReadFull(rs, header)
		if n > 0 {
			if _, seekErr := rs.Seek(-int64(n), io.SeekCurrent); seekErr != nil {
				return false, fmt.Errorf("failed to rewind read seeker: %w", seekErr)
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return false, nil
			}
			return false, err
		}
		return bytes.Equal(header, []byte(AtRestMagicHeader)), nil
	}

	return false, fmt.Errorf("reader of type %T does not support seeking or peeking; wrap with bufio.NewReader or use io.ReadSeeker", r)
}

// PeekIsEncrypted checks whether an arbitrary io.Reader is encrypted by wrapping it
// in a *bufio.Reader if necessary. It returns the encryption status and a valid io.Reader.
func PeekIsEncrypted(r io.Reader) (bool, io.Reader, error) {
	br, ok := r.(*bufio.Reader)
	if !ok {
		br = bufio.NewReader(r)
	}
	enc, err := IsEncrypted(br)
	return enc, br, err
}

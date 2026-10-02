package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"

	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/hkdf"
)

// GenerateX25519KeyPair generates an ephemeral X25519 private and public key pair
func GenerateX25519KeyPair() (privKey [32]byte, pubKey [32]byte, err error) {
	if _, err := io.ReadFull(rand.Reader, privKey[:]); err != nil {
		return privKey, pubKey, fmt.Errorf("failed to generate random bytes for X25519: %w", err)
	}
	curve25519.ScalarBaseMult(&pubKey, &privKey)
	return privKey, pubKey, nil
}

// ComputeSharedSecret computes X25519 ECDH shared secret between our private key and peer's public key
func ComputeSharedSecret(privKey [32]byte, peerPubKey [32]byte) ([32]byte, error) {
	var sharedSecret [32]byte
	secret, err := curve25519.X25519(privKey[:], peerPubKey[:])
	if err != nil {
		return sharedSecret, fmt.Errorf("failed to compute X25519 shared secret: %w", err)
	}
	copy(sharedSecret[:], secret)
	return sharedSecret, nil
}

// DeriveAESGCMKey derives a 32-byte AES-256 key using HKDF-SHA256 from the shared secret
func DeriveAESGCMKey(sharedSecret [32]byte, salt []byte, info string) ([]byte, error) {
	hkdfReader := hkdf.New(sha256.New, sharedSecret[:], salt, []byte(info))
	key := make([]byte, 32)
	if _, err := io.ReadFull(hkdfReader, key); err != nil {
		return nil, fmt.Errorf("failed to derive key using HKDF: %w", err)
	}
	return key, nil
}

// EncryptAESGCM encrypts plaintext using AES-256-GCM with a random 12-byte nonce
func EncryptAESGCM(key []byte, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	// Nonce is prepended to ciphertext
	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	return ciphertext, nil
}

// DecryptAESGCM decrypts ciphertext using AES-256-GCM where the first 12 bytes is the nonce
func DecryptAESGCM(key []byte, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, errors.New("ciphertext too short")
	}
	nonce, actualCiphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	return gcm.Open(nil, nonce, actualCiphertext, nil)
}

// Zeroize zeroes out sensitive bytes from memory
func Zeroize(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// GenerateRandomHex generates secure random hex string of given byte length
func GenerateRandomHex(numBytes int) (string, error) {
	b := make([]byte, numBytes)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// ConstantTimeEquals compares two strings in constant time
func ConstantTimeEquals(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// ComputeFileSHA256 calculates SHA-256 hash of a file by streaming it in chunks (low memory)
func ComputeFileSHA256(filePath string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	hasher := sha256.New()
	buf := make([]byte, 64*1024) // 64KB buffer
	if _, err := io.CopyBuffer(hasher, f, buf); err != nil {
		return "", err
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
}

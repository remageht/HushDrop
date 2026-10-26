package crypto

import (
	"bufio"
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// At-rest container format (V2, streaming):
//
//	magic      16B  AtRestMagicHeader
//	baseNonce   8B  random per file; frame nonce = baseNonce || BE32(frameCounter), counter starts at 1
//	segLen      4B  BE u32, maximum plaintext bytes per frame (writer emits AtRestSegmentSize)
//	frames:
//	    data   BE u32 plainLen (1..segLen) || AES-256-GCM(key, nonce, plaintext, aad=magic) [16B tag]
//	    eof    BE u32 0                   || AES-256-GCM(key, nonce, <empty>,    aad=magic) [16B tag]
//
// The eof frame authenticates the end of the plaintext stream, so a truncated file fails
// with a missing-terminator error instead of silently returning a prefix. Trailing data
// after the eof frame is rejected. Every frame is bound to the magic header (AAD), so
// frames cannot be transplanted into files with a different format.

const (
	// AtRestMagicHeader is the 16-byte magic prefix of every V2 at-rest encrypted payload.
	AtRestMagicHeader = "HUSHDROP_ENC_V2\n"

	// AtRestBaseNonceSize is the per-file random prefix of every frame nonce.
	// The full 12-byte AES-GCM nonce is baseNonce || BE32(counter) with counter >= 1.
	AtRestBaseNonceSize = 8

	// AtRestSegmentSize is the maximum plaintext bytes per frame (4 MiB).
	AtRestSegmentSize = 4 * 1024 * 1024

	// AtRestFrameHeaderSize is the 4-byte big-endian plaintext-length prefix of each frame.
	AtRestFrameHeaderSize = 4

	// AtRestKeySize is the exact AES-256 key length accepted by this format.
	AtRestKeySize = 32

	// AtRestKeyFileName is the default key file name (stored inside the data directory).
	AtRestKeyFileName = "atrest.key"
)

type peeker interface {
	Peek(n int) ([]byte, error)
}

type byteProvider interface {
	Bytes() []byte
}

var (
	// ErrInvalidHeader is returned when the stream does not begin with the V2 magic header.
	ErrInvalidHeader = errors.New("invalid hushdrop at-rest encryption header")

	// ErrCorruptFrame is returned for an out-of-range frame length, a missing eof frame
	// (truncated stream) or trailing data after the eof frame.
	ErrCorruptFrame = errors.New("corrupted hushdrop at-rest stream")

	// ErrInvalidKey is returned when the key length is wrong or the AEAD authentication
	// of a frame fails (wrong key material or corrupted ciphertext).
	ErrInvalidKey = errors.New("invalid at-rest key or corrupted data")

	// ErrWriteAfterClose is returned by writes after Close.
	ErrWriteAfterClose = errors.New("write after close")
)

// LoadOrCreateAtRestKey returns the 32-byte master key stored at path, creating a fresh
// random key file (0600) on first use. Never log the returned bytes.
func LoadOrCreateAtRestKey(path string) ([]byte, error) {
	if path == "" {
		return nil, errors.New("at-rest key file path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, fmt.Errorf("at-rest key directory: %w", err)
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err == nil {
		defer f.Close()
		key := make([]byte, AtRestKeySize)
		if _, err := rand.Read(key); err != nil {
			return nil, fmt.Errorf("at-rest key generation: %w", err)
		}
		if _, err := f.Write(key); err != nil {
			_ = os.Remove(path)
			return nil, fmt.Errorf("at-rest key write: %w", err)
		}
		return key, nil
	}
	if !os.IsExist(err) {
		return nil, fmt.Errorf("at-rest key file: %w", err)
	}

	key, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("at-rest key read: %w", err)
	}
	if len(key) != AtRestKeySize {
		return nil, fmt.Errorf("at-rest key file %s has wrong size %d, expected %d bytes", path, len(key), AtRestKeySize)
	}
	return key, nil
}

// newAtRestAead builds the AES-256-GCM primitive for the given key and generates a fresh
// per-file base nonce when requested.
func newAtRestAead(key []byte) (cipher.AEAD, error) {
	if len(key) != AtRestKeySize {
		return nil, ErrInvalidKey
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("at-rest cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("at-rest gcm: %w", err)
	}
	return aead, nil
}

// NewAtRestWriter returns an io.WriteCloser that encrypts writes into the V2 container
// format on dst. Full frames (exactly AtRestSegmentSize plaintext bytes) are emitted as
// the buffer fills; Close flushes the final partial frame plus the authenticated eof
// frame. Close is idempotent.
func NewAtRestWriter(dst io.Writer, key []byte) (io.WriteCloser, error) {
	aead, err := newAtRestAead(key)
	if err != nil {
		return nil, err
	}
	w := &atRestWriter{
		dst:      dst,
		aead:     aead,
		aad:      []byte(AtRestMagicHeader),
		buf:      make([]byte, 0, AtRestSegmentSize),
		counter:  1,
		lenScr:   make([]byte, AtRestFrameHeaderSize),
		nonceScr: make([]byte, 12),
	}
	if _, err := rand.Read(w.base[:]); err != nil {
		return nil, fmt.Errorf("at-rest nonce: %w", err)
	}

	// Container header: magic || baseNonce || segLen, written eagerly so that every
	// encrypted output is self-describing from its very first byte.
	header := make([]byte, 0, len(AtRestMagicHeader)+AtRestBaseNonceSize+4)
	header = append(header, AtRestMagicHeader...)
	header = append(header, w.base[:]...)
	segScr := make([]byte, 4)
	binary.BigEndian.PutUint32(segScr, AtRestSegmentSize)
	header = append(header, segScr...)
	if _, err := dst.Write(header); err != nil {
		return nil, fmt.Errorf("at-rest header write: %w", err)
	}

	return w, nil
}

type atRestWriter struct {
	dst      io.Writer
	aead     cipher.AEAD
	base     [AtRestBaseNonceSize]byte
	counter  uint32
	buf      []byte
	err      error
	closed   bool
	mu       sync.Mutex
	aad      []byte
	lenScr   []byte
	nonceScr []byte
}

func (w *atRestWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.err != nil {
		return 0, w.err
	}
	if w.closed {
		w.err = ErrWriteAfterClose
		return 0, w.err
	}

	written := 0
	for len(p) > 0 {
		free := AtRestSegmentSize - len(w.buf)
		if free == 0 {
			w.flushFrameLocked(len(w.buf))
			if w.err != nil {
				return written, w.err
			}
			free = AtRestSegmentSize
		}
		n := len(p)
		if n > free {
			n = free
		}
		w.buf = append(w.buf, p[:n]...)
		p = p[n:]
		written += n
	}
	return written, w.err
}

func (w *atRestWriter) flushFrameLocked(n int) {
	if w.counter == 0xFFFFFFFF {
		w.err = errors.New("at-rest frame counter exhausted")
		return
	}
	copy(w.nonceScr, w.base[:])
	binary.BigEndian.PutUint32(w.nonceScr[AtRestBaseNonceSize:], w.counter)

	ct := w.aead.Seal(nil, w.nonceScr, w.buf[:n], w.aad)
	binary.BigEndian.PutUint32(w.lenScr, uint32(n))

	if _, err := w.dst.Write(w.lenScr); err != nil {
		w.err = fmt.Errorf("at-rest frame header: %w", err)
		return
	}
	if _, err := w.dst.Write(ct); err != nil {
		w.err = fmt.Errorf("at-rest frame body: %w", err)
		return
	}
	w.counter++
	w.buf = w.buf[:0]
}

func (w *atRestWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return w.err
	}
	w.closed = true
	if w.err != nil {
		return w.err
	}
	if len(w.buf) > 0 {
		w.flushFrameLocked(len(w.buf)) // final partial frame (skipped when exactly aligned)
		if w.err != nil {
			return w.err
		}
	}
	w.flushFrameLocked(0) // authenticated eof marker
	return w.err
}

// NewAtRestReader returns an io.Reader that transparently decrypts a V2 at-rest stream
// from src. Reads keep at most one frame of plaintext in memory. Frame authentication
// failures surface as ErrInvalidKey; abrupt truncation as ErrCorruptFrame.
func NewAtRestReader(src io.Reader, key []byte) (io.Reader, error) {
	header := make([]byte, len(AtRestMagicHeader)+AtRestBaseNonceSize+4)
	if _, err := io.ReadFull(src, header); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return nil, ErrInvalidHeader
		}
		return nil, fmt.Errorf("at-rest header read: %w", err)
	}
	if !bytes.Equal(header[:len(AtRestMagicHeader)], []byte(AtRestMagicHeader)) {
		return nil, ErrInvalidHeader
	}

	r := &atRestReader{
		src:      src,
		aad:      []byte(AtRestMagicHeader),
		counter:  1,
		lenScr:   make([]byte, AtRestFrameHeaderSize),
		nonceScr: make([]byte, 12),
	}
	copy(r.base[:], header[len(AtRestMagicHeader):len(AtRestMagicHeader)+AtRestBaseNonceSize])
	r.segLen = binary.BigEndian.Uint32(header[len(header)-4:])
	if r.segLen == 0 || r.segLen > AtRestSegmentSize {
		return nil, ErrCorruptFrame
	}

	aead, err := newAtRestAead(key)
	if err != nil {
		return nil, err
	}
	r.aead = aead
	return r, nil
}

type atRestReader struct {
	src      io.Reader
	aead     cipher.AEAD
	base     [AtRestBaseNonceSize]byte
	segLen   uint32
	counter  uint32
	plain    []byte
	off      int
	done     bool
	err      error
	aad      []byte
	lenScr   []byte
	nonceScr []byte
}

func (r *atRestReader) Read(p []byte) (int, error) {
	if r.err != nil {
		return 0, r.err
	}
	if r.done {
		return 0, io.EOF
	}
	for r.off >= len(r.plain) {
		if err := r.nextFrame(); err != nil {
			return 0, err
		}
		if r.done {
			return 0, io.EOF
		}
	}
	n := copy(p, r.plain[r.off:])
	r.off += n
	return n, nil
}

func (r *atRestReader) nextFrame() error {
	if _, err := io.ReadFull(r.src, r.lenScr); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return r.fail(ErrCorruptFrame)
		}
		return r.fail(fmt.Errorf("at-rest frame header: %w", err))
	}
	plainLen := binary.BigEndian.Uint32(r.lenScr)
	if plainLen > r.segLen {
		return r.fail(ErrCorruptFrame)
	}

	boxed := make([]byte, int(plainLen)+int(r.aead.Overhead()))
	if _, err := io.ReadFull(r.src, boxed); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return r.fail(ErrCorruptFrame)
		}
		return r.fail(fmt.Errorf("at-rest frame body: %w", err))
	}

	copy(r.nonceScr, r.base[:])
	binary.BigEndian.PutUint32(r.nonceScr[AtRestBaseNonceSize:], r.counter)
	r.counter++

	plain, err := r.aead.Open(nil, r.nonceScr, boxed, r.aad)
	if err != nil {
		return r.fail(ErrInvalidKey)
	}

	if len(plain) == 0 {
		// Authenticated end-of-stream marker: refuse trailing data.
		var probe [1]byte
		n, err := r.src.Read(probe[:])
		if n > 0 {
			return r.fail(ErrCorruptFrame)
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return r.fail(fmt.Errorf("at-rest trailer read: %w", err))
		}
		r.plain = nil
		r.off = 0
		r.done = true
		return nil
	}

	r.plain = plain
	r.off = 0
	return nil
}

func (r *atRestReader) fail(err error) error {
	r.err = err
	return err
}

// EncryptStream writes all of src encrypted into the V2 at-rest format to dst
// (streaming; memory stays bounded by one segment regardless of input size).
func EncryptStream(src io.Reader, dst io.Writer, key []byte) error {
	w, err := NewAtRestWriter(dst, key)
	if err != nil {
		return err
	}
	buf := make([]byte, 64*1024)
	if _, err := io.CopyBuffer(w, src, buf); err != nil {
		_ = w.Close()
		return fmt.Errorf("at-rest encrypt stream: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("at-rest encrypt close: %w", err)
	}
	return nil
}

// DecryptStream streams the plaintext of a V2 at-rest encrypted stream from src to dst.
func DecryptStream(src io.Reader, dst io.Writer, key []byte) error {
	r, err := NewAtRestReader(src, key)
	if err != nil {
		return err
	}
	buf := make([]byte, 64*1024)
	if _, err := io.CopyBuffer(dst, r, buf); err != nil {
		return fmt.Errorf("at-rest decrypt stream: %w", err)
	}
	return nil
}

// IsEncrypted inspects the first 16 bytes of r to check for the HushDrop at-rest magic
// header without advancing the reader's position. It supports io.ReadSeeker,
// *bufio.Reader and buffered byte providers.
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
		if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
			return false, err
		}
		if n < len(AtRestMagicHeader) {
			return false, nil
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

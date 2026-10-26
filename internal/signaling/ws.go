package signaling

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	wsGUID          = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
	maxPayloadBytes = 64 << 10 // 64 KB max payload to prevent DoS

	opContinuation = 0x0
	opText         = 0x1
	opBinary       = 0x2
	opClose        = 0x8
	opPing         = 0x9
	opPong         = 0xA
)

var (
	ErrPayloadTooLarge = errors.New("websocket: payload exceeds maximum allowed size (64KB)")
	ErrMaskRequired    = errors.New("websocket: client frames must be masked")
	ErrNotHijackable   = errors.New("websocket: response writer does not support hijacking")
)

// Conn represents an RFC 6455 WebSocket connection over a hijacked TCP/TLS socket
type Conn struct {
	netConn net.Conn
	bufrw   *bufio.ReadWriter
	writeMu sync.Mutex
	closed  bool
}

// Upgrade upgrades an HTTP GET request to a WebSocket connection
func Upgrade(w http.ResponseWriter, r *http.Request) (*Conn, error) {
	if r.Method != http.MethodGet {
		return nil, errors.New("websocket: method must be GET")
	}

	// Verify headers
	connectionHdr := strings.ToLower(r.Header.Get("Connection"))
	if !strings.Contains(connectionHdr, "upgrade") {
		return nil, errors.New("websocket: missing or invalid Connection header")
	}
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return nil, errors.New("websocket: missing or invalid Upgrade header")
	}

	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		return nil, errors.New("websocket: missing Sec-WebSocket-Key")
	}

	// Compute accept hash: base64(sha1(key + GUID))
	h := sha1.New()
	h.Write([]byte(key + wsGUID))
	accept := base64.StdEncoding.EncodeToString(h.Sum(nil))

	// Hijack the underlying connection
	hj, ok := w.(http.Hijacker)
	if !ok {
		return nil, ErrNotHijackable
	}
	netConn, bufrw, err := hj.Hijack()
	if err != nil {
		return nil, fmt.Errorf("websocket hijack: %w", err)
	}

	// Write 101 response
	resp := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + accept + "\r\n\r\n"

	if _, err := bufrw.WriteString(resp); err != nil {
		netConn.Close()
		return nil, fmt.Errorf("websocket write handshake: %w", err)
	}
	if err := bufrw.Flush(); err != nil {
		netConn.Close()
		return nil, fmt.Errorf("websocket flush handshake: %w", err)
	}

	return &Conn{
		netConn: netConn,
		bufrw:   bufrw,
	}, nil
}

// ReadTextMessage reads the next full text frame from the connection
func (c *Conn) ReadTextMessage() (string, error) {
	for {
		header := make([]byte, 2)
		if _, err := io.ReadFull(c.bufrw, header); err != nil {
			return "", err
		}

		fin := (header[0] & 0x80) != 0
		opcode := header[0] & 0x0F
		masked := (header[1] & 0x80) != 0
		length := uint64(header[1] & 0x7F)

		if !masked {
			return "", ErrMaskRequired
		}

		if length == 126 {
			extLen := make([]byte, 2)
			if _, err := io.ReadFull(c.bufrw, extLen); err != nil {
				return "", err
			}
			length = uint64(binary.BigEndian.Uint16(extLen))
		} else if length == 127 {
			extLen := make([]byte, 8)
			if _, err := io.ReadFull(c.bufrw, extLen); err != nil {
				return "", err
			}
			length = binary.BigEndian.Uint64(extLen)
		}

		if length > maxPayloadBytes {
			return "", ErrPayloadTooLarge
		}

		maskKey := make([]byte, 4)
		if _, err := io.ReadFull(c.bufrw, maskKey); err != nil {
			return "", err
		}

		payload := make([]byte, length)
		if _, err := io.ReadFull(c.bufrw, payload); err != nil {
			return "", err
		}

		// Unmask payload
		for i := uint64(0); i < length; i++ {
			payload[i] ^= maskKey[i%4]
		}

		switch opcode {
		case opClose:
			c.Close()
			return "", io.EOF
		case opPing:
			_ = c.writeFrame(opPong, payload)
			continue
		case opPong:
			continue
		case opText:
			if !fin {
				// Fragmented messages: we expect non-fragmented small signaling messages <= 64KB
				return string(payload), nil
			}
			return string(payload), nil
		case opBinary:
			return string(payload), nil
		default:
			// Ignore unsupported opcodes
			continue
		}
	}
}

// WriteTextMessage writes a text frame (unmasked from server to client per RFC 6455)
func (c *Conn) WriteTextMessage(msg string) error {
	return c.writeFrame(opText, []byte(msg))
}

// Ping sends a WebSocket ping frame
func (c *Conn) Ping() error {
	return c.writeFrame(opPing, []byte("ping"))
}

func (c *Conn) writeFrame(opcode byte, payload []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	if c.closed {
		return errors.New("websocket: connection is closed")
	}

	length := len(payload)
	var header []byte

	if length <= 125 {
		header = []byte{0x80 | opcode, byte(length)}
	} else if length <= 65535 {
		header = make([]byte, 4)
		header[0] = 0x80 | opcode
		header[1] = 126
		binary.BigEndian.PutUint16(header[2:], uint16(length))
	} else {
		header = make([]byte, 10)
		header[0] = 0x80 | opcode
		header[1] = 127
		binary.BigEndian.PutUint64(header[2:], uint64(length))
	}

	if err := c.netConn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return err
	}

	if _, err := c.bufrw.Write(header); err != nil {
		return err
	}
	if length > 0 {
		if _, err := c.bufrw.Write(payload); err != nil {
			return err
		}
	}
	return c.bufrw.Flush()
}

// Close closes the underlying network connection
func (c *Conn) Close() error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	// Try sending a standard close frame
	closeHeader := []byte{0x80 | opClose, 0x00}
	_, _ = c.bufrw.Write(closeHeader)
	_ = c.bufrw.Flush()
	return c.netConn.Close()
}

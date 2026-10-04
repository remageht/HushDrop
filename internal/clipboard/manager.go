package clipboard

import (
	"errors"
	"sync"
	"time"

	"hushdrop/internal/crypto"
)

const MaxClipboardSize = 1024 * 1024 // 1 MB max text in RAM

type Item struct {
	Text          string `json:"text"`
	BurnAfterRead bool   `json:"burnAfterRead"`
	UpdatedAt     int64  `json:"updatedAt"`
}

type Manager struct {
	mu            sync.RWMutex
	textBytes     []byte
	burnAfterRead bool
	updatedAt     time.Time
}

func NewManager() *Manager {
	return &Manager{}
}

// Set stores the text strictly in RAM and zeroizes any previous buffer
func (m *Manager) Set(text string, burnAfterRead bool) error {
	raw := []byte(text)
	if len(raw) > MaxClipboardSize {
		return errors.New("clipboard text exceeds 1MB limit")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Zeroize old RAM buffer
	if len(m.textBytes) > 0 {
		crypto.Zeroize(m.textBytes)
	}

	// Copy into new buffer
	newBuf := make([]byte, len(raw))
	copy(newBuf, raw)

	m.textBytes = newBuf
	m.burnAfterRead = burnAfterRead
	m.updatedAt = time.Now()

	return nil
}

// Get returns the text. If BurnAfterRead is enabled, it zeroizes memory immediately.
func (m *Manager) Get() (Item, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.textBytes) == 0 {
		return Item{}, false
	}

	res := Item{
		Text:          string(m.textBytes),
		BurnAfterRead: m.burnAfterRead,
		UpdatedAt:     m.updatedAt.Unix(),
	}

	// If burn after read was requested, zeroize and clear now
	if m.burnAfterRead {
		crypto.Zeroize(m.textBytes)
		m.textBytes = nil
		m.burnAfterRead = false
	}

	return res, true
}

// Clear zeroes the RAM buffer and resets state
func (m *Manager) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.textBytes) > 0 {
		crypto.Zeroize(m.textBytes)
		m.textBytes = nil
	}
	m.burnAfterRead = false
	m.updatedAt = time.Time{}
}

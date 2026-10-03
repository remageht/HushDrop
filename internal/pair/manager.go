package pair

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"sync"
	"time"

	"hushdrop/internal/crypto"
)

type Session struct {
	AccessToken    string
	RefreshToken   string
	ClientIP       string
	CreatedAt      time.Time
	AccessExpires  time.Time
	RefreshExpires time.Time
	LastActivity   time.Time
	SharedKey      []byte // AES-256 session key held strictly in RAM
}

type IPAttemptTracker struct {
	FailedAttempts int
	BannedUntil    time.Time
}

type Manager struct {
	mu                   sync.RWMutex
	currentPIN           string
	pinExpiresAt         time.Time
	oneTimeToken         string
	tokenExpiresAt       time.Time
	sessions             map[string]*Session        // accessToken -> Session
	refreshIndex         map[string]string          // refreshToken -> accessToken
	ipAttempts           map[string]*IPAttemptTracker // IP -> attempts
	pinTTL               time.Duration
	inactivityTTL        time.Duration
	accessTokenTTL       time.Duration
	refreshTokenTTL      time.Duration
}

func NewManager(pinTTL, inactivityTTL, accessTTL, refreshTTL time.Duration) *Manager {
	m := &Manager{
		sessions:        make(map[string]*Session),
		refreshIndex:    make(map[string]string),
		ipAttempts:      make(map[string]*IPAttemptTracker),
		pinTTL:          pinTTL,
		inactivityTTL:   inactivityTTL,
		accessTokenTTL:  accessTTL,
		refreshTokenTTL: refreshTTL,
	}
	m.GenerateNewPINAndToken()

	// Background sweeper for expired sessions and inactivity
	go m.startSweeper()

	return m
}

// GenerateNewPINAndToken creates a new 6-digit numeric PIN and one-time QR token with 10-minute TTL
func (m *Manager) GenerateNewPINAndToken() (string, string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 6 random digits
	n, _ := rand.Int(rand.Reader, big.NewInt(900000))
	m.currentPIN = fmt.Sprintf("%06d", n.Int64()+100000)
	m.pinExpiresAt = time.Now().Add(m.pinTTL)

	token, _ := crypto.GenerateRandomHex(24)
	m.oneTimeToken = token
	m.tokenExpiresAt = time.Now().Add(m.pinTTL)

	return m.currentPIN, m.oneTimeToken
}

func (m *Manager) GetActivePairingDetails() (pin string, token string, expiresAt time.Time) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.currentPIN, m.oneTimeToken, m.pinExpiresAt
}

// CheckIPBan checks if the given IP address is banned
func (m *Manager) CheckIPBan(ip string) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if tracker, exists := m.ipAttempts[ip]; exists {
		if time.Now().Before(tracker.BannedUntil) {
			remaining := time.Until(tracker.BannedUntil).Round(time.Second)
			return fmt.Errorf("ip banned due to repeated failed attempts, try again in %v", remaining)
		}
	}
	return nil
}

func (m *Manager) recordFailedAttemptLocked(ip string) {
	tracker, exists := m.ipAttempts[ip]
	if !exists {
		tracker = &IPAttemptTracker{}
		m.ipAttempts[ip] = tracker
	}

	tracker.FailedAttempts++
	if tracker.FailedAttempts >= 5 {
		tracker.BannedUntil = time.Now().Add(5 * time.Minute)
		tracker.FailedAttempts = 0
	}
}

// RecordFailedAttempt records a failed PIN submission, banning the IP after 5 attempts for 5 minutes
func (m *Manager) RecordFailedAttempt(ip string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.recordFailedAttemptLocked(ip)
}

// ClearFailedAttempts resets the attempt counter for an IP on successful pairing
func (m *Manager) ClearFailedAttempts(ip string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.ipAttempts, ip)
}

// VerifyPairing validates PIN and one-time token, performing X25519 key exchange if client public key is provided
func (m *Manager) VerifyPairing(ip string, submittedPIN string, submittedToken string, clientPubKeyHex string) (*Session, string, error) {
	if err := m.CheckIPBan(ip); err != nil {
		return nil, "", err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()

	// Check expiration
	if now.After(m.pinExpiresAt) || now.After(m.tokenExpiresAt) {
		return nil, "", errors.New("pairing PIN or QR token has expired")
	}

	// Constant time PIN and token comparison
	pinValid := crypto.ConstantTimeEquals(m.currentPIN, submittedPIN)
	tokenValid := submittedToken == "" || crypto.ConstantTimeEquals(m.oneTimeToken, submittedToken)

	if !pinValid || !tokenValid {
		m.recordFailedAttemptLocked(ip)
		return nil, "", errors.New("invalid PIN or token")
	}

	// Successful pairing: reset attempts
	delete(m.ipAttempts, ip)

	// Create tokens
	accessToken, err := crypto.GenerateRandomHex(32)
	if err != nil {
		return nil, "", err
	}
	refreshToken, err := crypto.GenerateRandomHex(32)
	if err != nil {
		return nil, "", err
	}

	var sharedKey []byte
	var serverPubKeyHex string

	if clientPubKeyHex != "" {
		if clientPubKeyBytes, err := hex.DecodeString(clientPubKeyHex); err == nil && len(clientPubKeyBytes) == 32 {
			var peerPubKey [32]byte
			copy(peerPubKey[:], clientPubKeyBytes)

			privKey, pubKey, err := crypto.GenerateX25519KeyPair()
			if err == nil {
				sharedSecret, err := crypto.ComputeSharedSecret(privKey, peerPubKey)
				if err == nil {
					key, err := crypto.DeriveAESGCMKey(sharedSecret, nil, "HushDrop-E2E-v1")
					if err == nil {
						sharedKey = key
						serverPubKeyHex = hex.EncodeToString(pubKey[:])
					}
					crypto.Zeroize(sharedSecret[:])
				}
				crypto.Zeroize(privKey[:])
			}
		}
	}

	if sharedKey == nil {
		if rndKey, err := crypto.GenerateRandomHex(32); err == nil {
			sharedKey, _ = hex.DecodeString(rndKey)
		}
	}

	session := &Session{
		AccessToken:    accessToken,
		RefreshToken:   refreshToken,
		ClientIP:       ip,
		CreatedAt:      now,
		AccessExpires:  now.Add(m.accessTokenTTL),
		RefreshExpires: now.Add(m.refreshTokenTTL),
		LastActivity:   now,
		SharedKey:      sharedKey,
	}

	m.sessions[accessToken] = session
	m.refreshIndex[refreshToken] = accessToken

	return session, serverPubKeyHex, nil
}

// RefreshSession issues a new access token and rotated refresh token
func (m *Manager) RefreshSession(oldRefreshToken string, clientIP string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	oldAccessToken, exists := m.refreshIndex[oldRefreshToken]
	if !exists {
		return nil, errors.New("invalid refresh token")
	}

	session, exists := m.sessions[oldAccessToken]
	if !exists || now.After(session.RefreshExpires) {
		delete(m.refreshIndex, oldRefreshToken)
		return nil, errors.New("session expired")
	}

	// Inactivity check
	if now.Sub(session.LastActivity) > m.inactivityTTL {
		delete(m.sessions, oldAccessToken)
		delete(m.refreshIndex, oldRefreshToken)
		return nil, errors.New("session expired due to inactivity")
	}

	// Clean up old references
	delete(m.sessions, oldAccessToken)
	delete(m.refreshIndex, oldRefreshToken)

	// Issue new token pair
	newAccessToken, err := crypto.GenerateRandomHex(32)
	if err != nil {
		return nil, err
	}
	newRefreshToken, err := crypto.GenerateRandomHex(32)
	if err != nil {
		return nil, err
	}

	newSession := &Session{
		AccessToken:    newAccessToken,
		RefreshToken:   newRefreshToken,
		ClientIP:       clientIP,
		CreatedAt:      session.CreatedAt,
		AccessExpires:  now.Add(m.accessTokenTTL),
		RefreshExpires: now.Add(m.refreshTokenTTL),
		LastActivity:   now,
		SharedKey:      session.SharedKey,
	}

	m.sessions[newAccessToken] = newSession
	m.refreshIndex[newRefreshToken] = newAccessToken

	return newSession, nil
}

// ValidateSession verifies if an access token is valid and active
func (m *Manager) ValidateSession(accessToken string) (*Session, bool) {
	m.mu.RLock()
	session, exists := m.sessions[accessToken]
	m.mu.RUnlock()

	if !exists {
		return nil, false
	}

	now := time.Now()
	if now.After(session.AccessExpires) {
		return nil, false
	}

	// Check inactivity
	if now.Sub(session.LastActivity) > m.inactivityTTL {
		m.mu.Lock()
		delete(m.sessions, accessToken)
		delete(m.refreshIndex, session.RefreshToken)
		m.mu.Unlock()
		return nil, false
	}

	// Touch last activity
	m.mu.Lock()
	session.LastActivity = now
	m.mu.Unlock()

	return session, true
}

// RevokeAll clears all active sessions, zeroizes in-memory keys, and resets PIN ("Забыть всё")
func (m *Manager) RevokeAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Zeroize all session keys in RAM
	for _, sess := range m.sessions {
		if len(sess.SharedKey) > 0 {
			crypto.Zeroize(sess.SharedKey)
		}
	}

	m.sessions = make(map[string]*Session)
	m.refreshIndex = make(map[string]string)
	m.ipAttempts = make(map[string]*IPAttemptTracker)

	// Generate fresh PIN and QR token
	n, _ := rand.Int(rand.Reader, big.NewInt(900000))
	m.currentPIN = fmt.Sprintf("%06d", n.Int64()+100000)
	m.pinExpiresAt = time.Now().Add(m.pinTTL)
	token, _ := crypto.GenerateRandomHex(24)
	m.oneTimeToken = token
	m.tokenExpiresAt = time.Now().Add(m.pinTTL)
}

func (m *Manager) startSweeper() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		m.mu.Lock()
		now := time.Now()

		for token, sess := range m.sessions {
			if now.After(sess.RefreshExpires) || now.Sub(sess.LastActivity) > m.inactivityTTL {
				if len(sess.SharedKey) > 0 {
					crypto.Zeroize(sess.SharedKey)
				}
				delete(m.sessions, token)
				delete(m.refreshIndex, sess.RefreshToken)
			}
		}

		// Clean up old bans
		for ip, tracker := range m.ipAttempts {
			if now.After(tracker.BannedUntil) && tracker.FailedAttempts == 0 {
				delete(m.ipAttempts, ip)
			}
		}

		m.mu.Unlock()
	}
}

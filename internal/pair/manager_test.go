package pair

import (
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"hushdrop/internal/crypto"
)

func TestPairManager_SuccessAndRevoke(t *testing.T) {
	pm := NewManager(10*time.Minute, 10*time.Minute, 5*time.Minute, 1*time.Hour)
	pin, token, _ := pm.GetActivePairingDetails()

	// Generate client X25519 keypair
	_, clientPub, err := crypto.GenerateX25519KeyPair()
	if err != nil {
		t.Fatalf("failed to generate client keypair: %v", err)
	}
	clientPubHex := hex.EncodeToString(clientPub[:])

	session, serverPubHex, err := pm.VerifyPairing("192.168.1.50", pin, token, clientPubHex)
	if err != nil {
		t.Fatalf("expected successful pairing, got error: %v", err)
	}

	if session.AccessToken == "" || session.RefreshToken == "" {
		t.Fatalf("expected non-empty tokens")
	}
	if serverPubHex == "" {
		t.Fatalf("expected server public key for X25519 exchange")
	}
	if len(session.SharedKey) != 32 {
		t.Fatalf("expected 32-byte shared key in RAM, got %d", len(session.SharedKey))
	}

	// Validate session
	validatedSession, valid := pm.ValidateSession(session.AccessToken)
	if !valid || validatedSession == nil {
		t.Fatalf("expected session to be valid")
	}

	// Refresh session
	refreshed, err := pm.RefreshSession(session.RefreshToken, "192.168.1.50")
	if err != nil {
		t.Fatalf("expected refresh to succeed: %v", err)
	}
	if refreshed.AccessToken == session.AccessToken {
		t.Fatalf("expected rotated access token")
	}

	// Revoke All
	pm.RevokeAll()

	// Old and refreshed sessions must be invalidated
	_, valid = pm.ValidateSession(session.AccessToken)
	if valid {
		t.Fatalf("expected session to be invalidated after RevokeAll")
	}
	_, valid = pm.ValidateSession(refreshed.AccessToken)
	if valid {
		t.Fatalf("expected refreshed session to be invalidated after RevokeAll")
	}
}

func TestPairManager_InvalidPinAndBruteForceBan(t *testing.T) {
	pm := NewManager(10*time.Minute, 10*time.Minute, 5*time.Minute, 1*time.Hour)
	_, token, _ := pm.GetActivePairingDetails()

	testIP := "192.168.1.100"

	// 4 failed attempts
	for i := 0; i < 4; i++ {
		_, _, err := pm.VerifyPairing(testIP, "000000", token, "")
		if err == nil {
			t.Fatalf("attempt %d: expected error for invalid PIN", i+1)
		}
	}

	// 5th failed attempt should trigger ban
	_, _, err := pm.VerifyPairing(testIP, "000000", token, "")
	if err == nil {
		t.Fatalf("attempt 5: expected error for invalid PIN")
	}

	// 6th attempt must be rejected with ban notice
	err = pm.CheckIPBan(testIP)
	if err == nil || !strings.Contains(err.Error(), "ip banned") {
		t.Fatalf("expected IP ban error on 6th attempt, got: %v", err)
	}
}

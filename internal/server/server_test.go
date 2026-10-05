package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"hushdrop/internal/config"
	"hushdrop/internal/crypto"
	"hushdrop/internal/pair"
	"hushdrop/internal/transfer"
)

func setupTestServer(t *testing.T) (*Server, *pair.Manager, *transfer.Manager, func()) {
	tempDir, err := os.MkdirTemp("", "hushdrop_srv_temp_*")
	if err != nil {
		t.Fatal(err)
	}
	dlDir, err := os.MkdirTemp("", "hushdrop_srv_dl_*")
	if err != nil {
		t.Fatal(err)
	}
	certsDir, err := os.MkdirTemp("", "hushdrop_srv_certs_*")
	if err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Port:                 8443,
		Host:                 "127.0.0.1",
		Portable:             true,
		DataDir:              tempDir,
		CertsDir:             certsDir,
		DownloadsDir:         dlDir,
		TempDir:              tempDir,
		MaxChunkSize:         4 * 1024 * 1024,      // 4MB
		MaxFileSize:          5 * 1024 * 1024 * 1024, // 5GB
		RateLimitPerMinute:   20,
		PinTTL:               10 * time.Minute,
		SessionInactivityTTL: 10 * time.Minute,
		AccessTokenTTL:       5 * time.Minute,
		RefreshTokenTTL:      1 * time.Hour,
	}

	tlsInfo, err := crypto.GetOrCreateTLSCert(certsDir, []net.IP{net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatalf("failed to create test TLS cert: %v", err)
	}

	pm := pair.NewManager(cfg.PinTTL, cfg.SessionInactivityTTL, cfg.AccessTokenTTL, cfg.RefreshTokenTTL)
	tm := transfer.NewManager(dlDir, tempDir, cfg.MaxChunkSize, cfg.MaxFileSize)

	srv := NewServer(cfg, tlsInfo, pm, tm)

	cleanup := func() {
		os.RemoveAll(tempDir)
		os.RemoveAll(dlDir)
		os.RemoveAll(certsDir)
	}

	return srv, pm, tm, cleanup
}

func TestServer_HealthAndAuth(t *testing.T) {
	srv, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	// 1. GET /health -> 200
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.RemoteAddr = "127.0.0.1:54321"
	rec := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected /health 200, got %d", rec.Code)
	}

	// 2. GET /api/files without auth -> 401
	req = httptest.NewRequest(http.MethodGet, "/api/files", nil)
	req.RemoteAddr = "127.0.0.1:54321"
	rec = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected /api/files without auth to return 401, got %d", rec.Code)
	}
}

func TestServer_PairingAndBruteForceBan(t *testing.T) {
	srv, pm, _, cleanup := setupTestServer(t)
	defer cleanup()

	pin, token, _ := pm.GetActivePairingDetails()
	clientIP := "192.168.1.150:12345"

	// 1. Wrong PIN -> 403
	pairBody, _ := json.Marshal(PairRequest{PIN: "000000", Token: token})
	req := httptest.NewRequest(http.MethodPost, "/api/pair", bytes.NewReader(pairBody))
	req.RemoteAddr = clientIP
	rec := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected wrong PIN 403, got %d", rec.Code)
	}

	// 2. 4 more wrong attempts -> total 5 -> ban
	for i := 0; i < 4; i++ {
		req = httptest.NewRequest(http.MethodPost, "/api/pair", bytes.NewReader(pairBody))
		req.RemoteAddr = clientIP
		rec = httptest.NewRecorder()
		srv.httpServer.Handler.ServeHTTP(rec, req)
	}

	// 6th attempt should return 403 with ban message
	req = httptest.NewRequest(http.MethodPost, "/api/pair", bytes.NewReader(pairBody))
	req.RemoteAddr = clientIP
	rec = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "ip banned") {
		t.Fatalf("expected 403 with ban message, got code %d body %s", rec.Code, rec.Body.String())
	}

	// 3. Different unbanned IP pairs successfully
	freshIP := "192.168.1.151:12345"
	validBody, _ := json.Marshal(PairRequest{PIN: pin, Token: token})
	req = httptest.NewRequest(http.MethodPost, "/api/pair", bytes.NewReader(validBody))
	req.RemoteAddr = freshIP
	rec = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected valid pair 200, got %d body %s", rec.Code, rec.Body.String())
	}
}

func TestServer_ChunkSizeLimit413(t *testing.T) {
	srv, pm, _, cleanup := setupTestServer(t)
	defer cleanup()

	pin, token, _ := pm.GetActivePairingDetails()
	clientIP := "192.168.1.160:12345"

	// Pair first to get token
	validBody, _ := json.Marshal(PairRequest{PIN: pin, Token: token})
	req := httptest.NewRequest(http.MethodPost, "/api/pair", bytes.NewReader(validBody))
	req.RemoteAddr = clientIP
	rec := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(rec, req)

	var pairResp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &pairResp)
	accessToken := pairResp["accessToken"].(string)

	// Send chunk exceeding 4MB (Content-Length: 5MB)
	oversizedLen := int64(5 * 1024 * 1024)
	req = httptest.NewRequest(http.MethodPost, "/api/upload", bytes.NewReader([]byte("dummy")))
	req.RemoteAddr = clientIP
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("X-File-Id", "testfile413")
	req.Header.Set("X-Chunk-Index", "0")
	req.Header.Set("X-Total-Chunks", "1")
	req.Header.Set("X-File-Name", "large.iso")
	req.Header.Set("X-File-Size", fmt.Sprintf("%d", oversizedLen))
	req.ContentLength = oversizedLen

	rec = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 Payload Too Large for 5MB chunk, got %d body %s", rec.Code, rec.Body.String())
	}
}

func TestServer_RateLimit429(t *testing.T) {
	srv, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	clientIP := "192.168.1.170:12345"

	// Unmetered endpoints (/health, /api/pair/info) must bypass rate limiting without consuming tokens
	for i := 0; i < 25; i++ {
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		req.RemoteAddr = clientIP
		rec := httptest.NewRecorder()
		srv.httpServer.Handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("unmetered request %d: expected 200, got %d", i+1, rec.Code)
		}
	}

	// Metered endpoints (/api/pair) are limited to 20 requests per minute
	for i := 0; i < 20; i++ {
		body := strings.NewReader(`{"pin":"000000","token":"invalid"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/pair", body)
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = clientIP
		rec := httptest.NewRecorder()
		srv.httpServer.Handler.ServeHTTP(rec, req)
		if rec.Code == http.StatusTooManyRequests {
			t.Fatalf("metered request %d: triggered 429 prematurely", i+1)
		}
	}

	// 21st metered request must trigger 429
	body := strings.NewReader(`{"pin":"000000","token":"invalid"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/pair", body)
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = clientIP
	rec := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 Too Many Requests on flood, got %d", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Fatalf("expected Retry-After header on 429 response")
	}
}

func TestServer_RangeDownloadAndRevoke(t *testing.T) {
	srv, pm, _, cleanup := setupTestServer(t)
	defer cleanup()

	pin, token, _ := pm.GetActivePairingDetails()
	clientIP := "192.168.1.180:12345"

	// Pair
	validBody, _ := json.Marshal(PairRequest{PIN: pin, Token: token})
	req := httptest.NewRequest(http.MethodPost, "/api/pair", bytes.NewReader(validBody))
	req.RemoteAddr = clientIP
	rec := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(rec, req)

	var pairResp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &pairResp)
	accessToken := pairResp["accessToken"].(string)

	// Upload complete file
	filePayload := []byte("0123456789ABCDEF") // 16 bytes
	req = httptest.NewRequest(http.MethodPost, "/api/upload", bytes.NewReader(filePayload))
	req.RemoteAddr = clientIP
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("X-File-Id", "samplefile1")
	req.Header.Set("X-Chunk-Index", "0")
	req.Header.Set("X-Total-Chunks", "1")
	req.Header.Set("X-File-Name", "sample.txt")
	req.Header.Set("X-File-Size", fmt.Sprintf("%d", len(filePayload)))
	req.ContentLength = int64(len(filePayload))

	rec = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected upload 200, got %d body %s", rec.Code, rec.Body.String())
	}

	// Download with Range: bytes=0-4 (first 5 bytes -> "01234")
	req = httptest.NewRequest(http.MethodGet, "/api/download?id=samplefile1", nil)
	req.RemoteAddr = clientIP
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Range", "bytes=0-4")

	rec = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusPartialContent {
		t.Fatalf("expected 206 Partial Content, got %d", rec.Code)
	}
	if rec.Body.String() != "01234" {
		t.Fatalf("expected range body '01234', got %q", rec.Body.String())
	}
	if !strings.Contains(rec.Header().Get("Content-Range"), "bytes 0-4/16") {
		t.Fatalf("expected Content-Range header 'bytes 0-4/16', got %q", rec.Header().Get("Content-Range"))
	}

	// Revoke all
	req = httptest.NewRequest(http.MethodPost, "/api/revoke", nil)
	req.RemoteAddr = clientIP
	req.Header.Set("Authorization", "Bearer "+accessToken)
	rec = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected revoke 200, got %d", rec.Code)
	}

	// Subsequent /api/files should return 401
	req = httptest.NewRequest(http.MethodGet, "/api/files", nil)
	req.RemoteAddr = clientIP
	req.Header.Set("Authorization", "Bearer "+accessToken)
	rec = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized after revoke, got %d", rec.Code)
	}
}

func TestServer_Clipboard(t *testing.T) {
	srv, pm, _, cleanup := setupTestServer(t)
	defer cleanup()

	clientIP := "192.168.1.100:45678"
	pin, token, _ := pm.GetActivePairingDetails()

	// Pair client
	pairBody, _ := json.Marshal(map[string]string{"pin": pin, "token": token})
	req := httptest.NewRequest(http.MethodPost, "/api/pair", bytes.NewReader(pairBody))
	req.RemoteAddr = clientIP
	rec := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(rec, req)

	var pairResp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &pairResp)
	accessToken := pairResp["accessToken"].(string)

	// 1. Initial GET /api/clipboard -> empty
	req = httptest.NewRequest(http.MethodGet, "/api/clipboard", nil)
	req.RemoteAddr = clientIP
	req.Header.Set("Authorization", "Bearer "+accessToken)
	rec = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}
	var getResp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &getResp)
	if getResp["isEmpty"] != true {
		t.Fatalf("expected isEmpty true, got %+v", getResp)
	}

	// 2. POST /api/clipboard -> set text
	clipPayload, _ := json.Marshal(map[string]interface{}{
		"text":          "Secret WiFi password: HushDrop2026!",
		"burnAfterRead": true,
	})
	req = httptest.NewRequest(http.MethodPost, "/api/clipboard", bytes.NewReader(clipPayload))
	req.RemoteAddr = clientIP
	req.Header.Set("Authorization", "Bearer "+accessToken)
	rec = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}

	// 3. GET /api/clipboard -> returns text and burns
	req = httptest.NewRequest(http.MethodGet, "/api/clipboard", nil)
	req.RemoteAddr = clientIP
	req.Header.Set("Authorization", "Bearer "+accessToken)
	rec = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &getResp)
	if getResp["text"] != "Secret WiFi password: HushDrop2026!" {
		t.Fatalf("unexpected text: %v", getResp["text"])
	}
	if getResp["burnAfterRead"] != true {
		t.Fatalf("expected burnAfterRead true")
	}

	// 4. Second GET -> must be burned and empty
	req = httptest.NewRequest(http.MethodGet, "/api/clipboard", nil)
	req.RemoteAddr = clientIP
	req.Header.Set("Authorization", "Bearer "+accessToken)
	rec = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(rec, req)

	_ = json.Unmarshal(rec.Body.Bytes(), &getResp)
	if getResp["isEmpty"] != true {
		t.Fatalf("expected isEmpty true after burn-after-read, got %+v", getResp)
	}
}


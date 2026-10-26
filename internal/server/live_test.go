package server

import (
	"bytes"
	"crypto/rand"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"hushdrop/internal/config"
	"hushdrop/internal/crypto"
	"hushdrop/internal/discovery"
	"hushdrop/internal/pair"
	"hushdrop/internal/transfer"
)

func createTestNode(t *testing.T, port int) (*Server, *pair.Manager, func()) {
	tempDir, err := os.MkdirTemp("", fmt.Sprintf("hushdrop_live_temp_%d_*", port))
	if err != nil {
		t.Fatal(err)
	}
	dlDir, err := os.MkdirTemp("", fmt.Sprintf("hushdrop_live_dl_%d_*", port))
	if err != nil {
		t.Fatal(err)
	}
	certsDir, err := os.MkdirTemp("", fmt.Sprintf("hushdrop_live_certs_%d_*", port))
	if err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Port:                 port,
		Host:                 "127.0.0.1",
		Portable:             true,
		DataDir:              tempDir,
		CertsDir:             certsDir,
		DownloadsDir:         dlDir,
		TempDir:              tempDir,
		MaxChunkSize:         4 * 1024 * 1024,
		MaxFileSize:          5 * 1024 * 1024 * 1024,
		RateLimitPerMinute:   60,
		PinTTL:               10 * time.Minute,
		SessionInactivityTTL: 10 * time.Minute,
		AccessTokenTTL:       5 * time.Minute,
		RefreshTokenTTL:      1 * time.Hour,
	}

	tlsInfo, err := crypto.GetOrCreateTLSCert(certsDir, []net.IP{net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatalf("failed to create TLS cert: %v", err)
	}

	pm := pair.NewManager(cfg.PinTTL, cfg.SessionInactivityTTL, cfg.AccessTokenTTL, cfg.RefreshTokenTTL)
	tm := transfer.NewManager(dlDir, tempDir, cfg.MaxChunkSize, cfg.MaxFileSize)

	srv := NewServer(cfg, tlsInfo, pm, tm)

	ln, err := net.Listen("tcp", cfg.Addr())
	if err != nil {
		t.Fatalf("failed to listen on %s: %v", cfg.Addr(), err)
	}
	tlsListener := tls.NewListener(ln, srv.httpServer.TLSConfig)
	go func() {
		_ = srv.httpServer.Serve(tlsListener)
	}()

	cleanup := func() {
		_ = tlsListener.Close()
		os.RemoveAll(tempDir)
		os.RemoveAll(dlDir)
		os.RemoveAll(certsDir)
	}

	return srv, pm, cleanup
}

func TestLiveTwoNodesDiscovery(t *testing.T) {
	// 1. Setup Node 1 on port 8443
	srv1, pm1, cleanup1 := createTestNode(t, 8443)
	defer cleanup1()

	// 2. Setup Node 2 on port 8444
	srv2, pm2, cleanup2 := createTestNode(t, 8444)
	defer cleanup2()

	// 3. Start Foreign HTTP service on port 8445 (returns Apache / non-HushDrop service)
	foreignListener, err := net.Listen("tcp", "127.0.0.1:8445")
	if err != nil {
		t.Fatalf("failed to listen on 8445: %v", err)
	}
	defer foreignListener.Close()

	foreignServer := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"service": "Apache",
			"version": "2.4.52",
			"status":  "ok",
		})
	}))
	foreignServer.Listener = foreignListener
	foreignServer.StartTLS()
	defer foreignServer.Close()

	// Pair client with Node 1
	pin1, tok1, _ := pm1.GetActivePairingDetails()
	session1, _, err := pm1.VerifyPairing("127.0.0.1", pin1, tok1, "")
	if err != nil {
		t.Fatalf("pairing with Node 1 failed: %v", err)
	}

	// Pair client with Node 2
	pin2, tok2, _ := pm2.GetActivePairingDetails()
	session2, _, err := pm2.VerifyPairing("127.0.0.1", pin2, tok2, "")
	if err != nil {
		t.Fatalf("pairing with Node 2 failed: %v", err)
	}

	clientIP := "127.0.0.1:55555"

	// --- A. Node 1 Scans LAN (should discover Node 2, exclude self & foreign service) ---
	req1 := httptest.NewRequest(http.MethodPost, "/api/nearby/scan", bytes.NewReader([]byte("{}")))
	req1.RemoteAddr = clientIP
	req1.Header.Set("Authorization", "Bearer "+session1.AccessToken)
	rec1 := httptest.NewRecorder()
	srv1.httpServer.Handler.ServeHTTP(rec1, req1)

	if rec1.Code != http.StatusOK {
		t.Fatalf("Node 1 scan failed with code %d: %s", rec1.Code, rec1.Body.String())
	}

	var discoveredBy1 []discovery.DiscoveredNode
	if err := json.Unmarshal(rec1.Body.Bytes(), &discoveredBy1); err != nil {
		t.Fatalf("failed to parse Node 1 scan JSON: %v", err)
	}

	// Verify Node 2 is found
	foundNode2 := false
	for _, n := range discoveredBy1 {
		if n.Port == 8444 {
			foundNode2 = true
			if n.Version != "0.5.0" {
				t.Errorf("expected Node 2 version 0.5.0, got %s", n.Version)
			}
			if n.LatencyMs <= 0 {
				t.Errorf("expected positive latency, got %d", n.LatencyMs)
			}
		}
		if n.Port == 8443 {
			t.Errorf("Node 1 should not discover itself on port 8443")
		}
		if n.Port == 8445 {
			t.Errorf("Foreign service on port 8445 must NOT be included in results")
		}
	}
	if !foundNode2 {
		t.Errorf("Node 1 failed to discover Node 2 (port 8444). Discovered: %+v", discoveredBy1)
	}

	// Verify fingerprint is strictly absent from JSON body
	var rawObjects []map[string]interface{}
	_ = json.Unmarshal(rec1.Body.Bytes(), &rawObjects)
	for _, obj := range rawObjects {
		if _, hasFp := obj["fingerprint"]; hasFp {
			t.Errorf("Security violation: fingerprint returned in scan response: %+v", obj)
		}
	}

	// --- B. Immediate repeated scan on Node 1 (<15s) must return 429 ---
	req1Repeat := httptest.NewRequest(http.MethodPost, "/api/nearby/scan", bytes.NewReader([]byte("{}")))
	req1Repeat.RemoteAddr = clientIP
	req1Repeat.Header.Set("Authorization", "Bearer "+session1.AccessToken)
	rec1Repeat := httptest.NewRecorder()
	srv1.httpServer.Handler.ServeHTTP(rec1Repeat, req1Repeat)

	if rec1Repeat.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429 Too Many Requests on repeat scan, got %d", rec1Repeat.Code)
	}
	retryAfter := rec1Repeat.Header().Get("Retry-After")
	if retryAfter == "" {
		t.Errorf("expected Retry-After header on 429 response")
	}

	// --- C. Node 2 Scans LAN (should discover Node 1 on port 8443, exclude self & foreign service) ---
	req2 := httptest.NewRequest(http.MethodPost, "/api/nearby/scan", bytes.NewReader([]byte("{}")))
	req2.RemoteAddr = clientIP
	req2.Header.Set("Authorization", "Bearer "+session2.AccessToken)
	rec2 := httptest.NewRecorder()
	srv2.httpServer.Handler.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Fatalf("Node 2 scan failed with code %d: %s", rec2.Code, rec2.Body.String())
	}

	var discoveredBy2 []discovery.DiscoveredNode
	if err := json.Unmarshal(rec2.Body.Bytes(), &discoveredBy2); err != nil {
		t.Fatalf("failed to parse Node 2 scan JSON: %v", err)
	}

	foundNode1 := false
	for _, n := range discoveredBy2 {
		if n.Port == 8443 {
			foundNode1 = true
			if n.Version != "0.5.0" {
				t.Errorf("expected Node 1 version 0.5.0, got %s", n.Version)
			}
		}
		if n.Port == 8444 {
			t.Errorf("Node 2 should not discover itself on port 8444")
		}
		if n.Port == 8445 {
			t.Errorf("Foreign service on port 8445 must NOT be included in results")
		}
	}
	if !foundNode1 {
		t.Errorf("Node 2 failed to discover Node 1 (port 8443). Discovered: %+v", discoveredBy2)
	}

	// --- D. Verify foreign subnet scanning is rejected ---
	_, foreignSubnet, _ := net.ParseCIDR("8.8.8.0/24")
	_, err = discovery.GenerateSubnetIPs(foreignSubnet, 256)
	if err == nil {
		t.Errorf("Foreign subnet 8.8.8.0/24 must be rejected by GenerateSubnetIPs")
	}
}

// --- At-rest encryption E2E -------------------------------------------------

func createEncryptedTestNode(t *testing.T, port int, key []byte) (*Server, *pair.Manager, string, func()) {
	tempDir, err := os.MkdirTemp("", fmt.Sprintf("hushdrop_live_enc_temp_%d_*", port))
	if err != nil {
		t.Fatal(err)
	}
	dlDir, err := os.MkdirTemp("", fmt.Sprintf("hushdrop_live_enc_dl_%d_*", port))
	if err != nil {
		t.Fatal(err)
	}
	certsDir, err := os.MkdirTemp("", fmt.Sprintf("hushdrop_live_enc_certs_%d_*", port))
	if err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Port:                 port,
		Host:                 "127.0.0.1",
		Portable:             true,
		DataDir:              tempDir,
		CertsDir:             certsDir,
		DownloadsDir:         dlDir,
		TempDir:              tempDir,
		MaxChunkSize:         4 * 1024 * 1024,
		MaxFileSize:          5 * 1024 * 1024 * 1024,
		RateLimitPerMinute:   60,
		PinTTL:               10 * time.Minute,
		SessionInactivityTTL: 10 * time.Minute,
		AccessTokenTTL:       5 * time.Minute,
		RefreshTokenTTL:      1 * time.Hour,
	}

	tlsInfo, err := crypto.GetOrCreateTLSCert(certsDir, []net.IP{net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatalf("failed to create TLS cert: %v", err)
	}

	pm := pair.NewManager(cfg.PinTTL, cfg.SessionInactivityTTL, cfg.AccessTokenTTL, cfg.RefreshTokenTTL)
	tm := transfer.NewManagerWithEncryption(dlDir, tempDir, cfg.MaxChunkSize, cfg.MaxFileSize, key)

	srv := NewServer(cfg, tlsInfo, pm, tm)

	localTestServer := httptest.NewServer(srv.httpServer.Handler)
	cleanup := func() {
		localTestServer.Close()
		os.RemoveAll(tempDir)
		os.RemoveAll(dlDir)
		os.RemoveAll(certsDir)
	}
	return srv, pm, dlDir, cleanup
}

// TestLiveEncryptedUploadDownload proves the full at-rest loop on a live handler set:
// the stored container is ciphertext on disk while the HTTP download is plaintext.
func TestLiveEncryptedUploadDownload(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}

	srv, pm, dlDir, cleanup := createEncryptedTestNode(t, 8446, key)
	defer cleanup()

	pin, tok, _ := pm.GetActivePairingDetails()
	session, _, err := pm.VerifyPairing("127.0.0.1", pin, tok, "")
	if err != nil {
		t.Fatalf("pairing failed: %v", err)
	}

	// Build a 300KB payload with recognizable plaintext patterns
	payload := make([]byte, 300*1024)
	for i := range payload {
		payload[i] = byte(i % 251)
	}

	fileID := "encdl0001"
	req := httptest.NewRequest(http.MethodPost, "/api/upload", bytes.NewReader(payload))
	req.RemoteAddr = "127.0.0.1:55600"
	req.Header.Set("Authorization", "Bearer "+session.AccessToken)
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("X-File-Id", fileID)
	req.Header.Set("X-Chunk-Index", "0")
	req.Header.Set("X-Total-Chunks", "1")
	req.Header.Set("X-File-Name", "secret_report.bin")
	req.Header.Set("X-File-Size", strconv.Itoa(len(payload)))
	rec := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("upload failed with code %d: %s", rec.Code, rec.Body.String())
	}

	var uploadResp struct {
		IsCompleted bool `json:"isCompleted"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &uploadResp); err != nil {
		t.Fatalf("upload response parse: %v", err)
	}
	if !uploadResp.IsCompleted {
		t.Fatalf("expected upload to complete immediately (single chunk)")
	}

	// --- Files list must flag the file as encrypted at rest ---
	listReq := httptest.NewRequest(http.MethodGet, "/api/files", nil)
	listReq.RemoteAddr = "127.0.0.1:55600"
	listReq.Header.Set("Authorization", "Bearer "+session.AccessToken)
	listRec := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("files list failed: %d: %s", listRec.Code, listRec.Body.String())
	}

	var listResp struct {
		Files []map[string]interface{} `json:"files"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("files list parse: %v", err)
	}
	foundEncrypted := false
	for _, f := range listResp.Files {
		if f["id"] == fileID {
			if enc, ok := f["isEncrypted"].(bool); ok && enc {
				foundEncrypted = true
			}
		}
	}
	if !foundEncrypted {
		t.Fatalf("expected isEncrypted:true for %s in files list: %+v", fileID, listResp.Files)
	}

	// --- On disk the stored file must be a ciphertext container ---
	entries, _ := os.ReadDir(dlDir)
	if len(entries) != 1 {
		t.Fatalf("expected exactly one stored file, got %d", len(entries))
	}
	container, err := os.ReadFile(filepath.Join(dlDir, entries[0].Name()))
	if err != nil {
		t.Fatalf("read container: %v", err)
	}
	if !bytes.HasPrefix(container, []byte("HUSHDROP_ENC_V2\n")) {
		t.Fatalf("stored file misses the V2 magic header")
	}
	// The 16-byte pattern cycle must NOT appear anywhere in the ciphertext.
	pattern := make([]byte, 0, 32)
	for i := 0; i < 32; i++ {
		pattern = append(pattern, byte(16+i%251))
	}
	if bytes.Index(container, pattern) != -1 {
		t.Fatalf("plaintext pattern leaked into the stored container")
	}
	if int64(len(container)) <= int64(len(payload)) {
		t.Fatalf("container must be larger than plaintext (framing overhead)")
	}

	// --- HTTP download path streams the plaintext back ---
	dlReq := httptest.NewRequest(http.MethodGet, "/api/download?id="+fileID, nil)
	dlReq.RemoteAddr = "127.0.0.1:55600"
	dlReq.Header.Set("Authorization", "Bearer "+session.AccessToken)
	dlRec := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(dlRec, dlReq)

	if dlRec.Code != http.StatusOK {
		t.Fatalf("download failed: %d: %s", dlRec.Code, dlRec.Body.String())
	}
	if !bytes.Equal(dlRec.Body.Bytes(), payload) {
		t.Fatalf("downloaded plaintext mismatch (got %d bytes, want %d)", dlRec.Body.Len(), len(payload))
	}
	if got := dlRec.Header().Get("Content-Length"); got != strconv.Itoa(len(payload)) {
		t.Fatalf("expected Content-Length %d, got %q", len(payload), got)
	}
	if ra := dlRec.Header().Get("Accept-Ranges"); ra != "" {
		t.Fatalf("Accept-Ranges must not be offered on encrypted downloads")
	}

	// --- Status endpoint still reports the same plaintext metadata ---
	statusReq := httptest.NewRequest(http.MethodGet, "/api/transfer/status?id="+fileID, nil)
	statusReq.RemoteAddr = "127.0.0.1:55600"
	statusReq.Header.Set("Authorization", "Bearer "+session.AccessToken)
	statusRec := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(statusRec, statusReq)
	if statusRec.Code != http.StatusOK {
		t.Fatalf("status failed: %d: %s", statusRec.Code, statusRec.Body.String())
	}
}

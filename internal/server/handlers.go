package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"hushdrop/internal/clipboard"
)

type PairRequest struct {
	PIN          string `json:"pin"`
	Token        string `json:"token"`
	ClientPubKey string `json:"clientPubKey,omitempty"`
}

type RefreshRequest struct {
	RefreshToken string `json:"refreshToken"`
}

type ClipboardRequest struct {
	Text          string `json:"text"`
	BurnAfterRead bool   `json:"burnAfterRead"`
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"status":    "ok",
		"service":   "HushDrop",
		"version":   "0.3.3",
		"timestamp": time.Now().Unix(),
	})
}

func (s *Server) handlePairInfo(w http.ResponseWriter, r *http.Request) {
	_, token, expiresAt := s.pairManager.GetActivePairingDetails()
	remaining := int(time.Until(expiresAt).Seconds())
	if remaining < 0 {
		remaining = 0
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"fingerprint": s.tlsInfo.Fingerprint,
		"token":       token,
		"expiresIn":   remaining,
	})
}

func (s *Server) handlePair(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	clientIP := extractIP(r)

	// Check IP ban
	if err := s.pairManager.CheckIPBan(clientIP); err != nil {
		respondJSON(w, http.StatusForbidden, map[string]string{
			"error": err.Error(),
		})
		return
	}

	var req PairRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Invalid request body",
		})
		return
	}

	session, serverPubKey, err := s.pairManager.VerifyPairing(clientIP, req.PIN, req.Token, req.ClientPubKey)
	if err != nil {
		log.Printf("[PAIR_FAILED] Failed PIN pairing attempt from masked IP %s", MaskIP(clientIP))
		respondJSON(w, http.StatusForbidden, map[string]string{
			"error": err.Error(),
		})
		return
	}

	log.Printf("[PAIR_SUCCESS] Paired device from masked IP %s", MaskIP(clientIP))

	resp := map[string]interface{}{
		"accessToken":      session.AccessToken,
		"refreshToken":     session.RefreshToken,
		"accessExpiresIn":  int(s.config.AccessTokenTTL.Seconds()),
		"refreshExpiresIn": int(s.config.RefreshTokenTTL.Seconds()),
		"fingerprint":      s.tlsInfo.Fingerprint,
	}
	if serverPubKey != "" {
		resp["serverPubKey"] = serverPubKey
	}

	respondJSON(w, http.StatusOK, resp)
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req RefreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Invalid request body",
		})
		return
	}

	clientIP := extractIP(r)
	newSession, err := s.pairManager.RefreshSession(req.RefreshToken, clientIP)
	if err != nil {
		respondJSON(w, http.StatusUnauthorized, map[string]string{
			"error": err.Error(),
		})
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"accessToken":      newSession.AccessToken,
		"refreshToken":     newSession.RefreshToken,
		"accessExpiresIn":  int(s.config.AccessTokenTTL.Seconds()),
		"refreshExpiresIn": int(s.config.RefreshTokenTTL.Seconds()),
	})
}

func (s *Server) handleRevoke(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	clientIP := extractIP(r)
	log.Printf("[REVOKE_ALL] 'Forget Everything' triggered from masked IP %s", MaskIP(clientIP))

	// Invalidate pairing sessions, zeroize RAM keys
	s.pairManager.RevokeAll()

	// Wipe stored files and temporary chunks
	s.transferManager.ClearAll()

	// Clear and zeroize in-memory clipboard
	s.clipboardManager.Clear()

	respondJSON(w, http.StatusOK, map[string]string{
		"message": "All sessions revoked, keys zeroized, and files cleared",
	})
}

func (s *Server) handleFilesList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	files := s.transferManager.ListFiles()
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"files": files,
	})
}

func (s *Server) handleClipboard(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		item, exists := s.clipboardManager.Get()
		if !exists {
			respondJSON(w, http.StatusOK, map[string]interface{}{
				"text":          "",
				"isEmpty":       true,
				"burnAfterRead": false,
				"updatedAt":     0,
			})
			return
		}
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"text":          item.Text,
			"burnAfterRead": item.BurnAfterRead,
			"updatedAt":     item.UpdatedAt,
			"isEmpty":       false,
		})

	case http.MethodPost:
		if r.ContentLength > clipboard.MaxClipboardSize {
			respondJSON(w, http.StatusRequestEntityTooLarge, map[string]string{
				"error": "Clipboard text exceeds 1MB limit",
			})
			return
		}

		var req ClipboardRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondJSON(w, http.StatusBadRequest, map[string]string{
				"error": "Invalid request body",
			})
			return
		}

		if err := s.clipboardManager.Set(req.Text, req.BurnAfterRead); err != nil {
			respondJSON(w, http.StatusRequestEntityTooLarge, map[string]string{
				"error": err.Error(),
			})
			return
		}

		respondJSON(w, http.StatusOK, map[string]interface{}{
			"success":   true,
			"updatedAt": time.Now().Unix(),
		})

	case http.MethodDelete:
		s.clipboardManager.Clear()
		respondJSON(w, http.StatusOK, map[string]string{
			"message": "Clipboard cleared and zeroized",
		})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleUploadChunk(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	fileID := r.Header.Get("X-File-Id")
	chunkIdxStr := r.Header.Get("X-Chunk-Index")
	totalChunksStr := r.Header.Get("X-Total-Chunks")
	fileName := r.Header.Get("X-File-Name")
	fileSizeStr := r.Header.Get("X-File-Size")

	if fileID == "" || chunkIdxStr == "" || totalChunksStr == "" {
		respondJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Missing required chunk headers (X-File-Id, X-Chunk-Index, X-Total-Chunks)",
		})
		return
	}

	chunkIdx, err := strconv.Atoi(chunkIdxStr)
	if err != nil || chunkIdx < 0 {
		respondJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Invalid X-Chunk-Index",
		})
		return
	}

	totalChunks, err := strconv.Atoi(totalChunksStr)
	if err != nil || totalChunks <= 0 {
		respondJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Invalid X-Total-Chunks",
		})
		return
	}

	// Content-Length check for chunk limit (<= 4MB)
	chunkSize := r.ContentLength
	if chunkSize > s.config.MaxChunkSize {
		respondJSON(w, http.StatusRequestEntityTooLarge, map[string]string{
			"error": fmt.Sprintf("Chunk size %d exceeds 4MB limit", chunkSize),
		})
		return
	}

	// Initialize upload on chunk 0 or first encounter
	fileSize, _ := strconv.ParseInt(fileSizeStr, 10, 64)
	if fileSize > s.config.MaxFileSize {
		respondJSON(w, http.StatusRequestEntityTooLarge, map[string]string{
			"error": fmt.Sprintf("Total file size %d exceeds 5GB limit", fileSize),
		})
		return
	}

	if chunkIdx == 0 {
		_, err := s.transferManager.InitUpload(fileID, fileName, fileSize, totalChunks)
		if err != nil {
			log.Printf("[UPLOAD_INIT_ERR] File %s: %v", MaskFilename(fileName), err)
			respondJSON(w, http.StatusBadRequest, map[string]string{
				"error": "Failed to initialize upload session: " + err.Error(),
			})
			return
		}
		prefix := fileID
		if len(prefix) > 8 {
			prefix = prefix[:8]
		}
		log.Printf("[UPLOAD_START] New transfer ID %s for masked file %s (%d bytes)",
			prefix, MaskFilename(fileName), fileSize)
	}

	// Stream chunk directly to disk
	meta, err := s.transferManager.WriteChunk(fileID, chunkIdx, chunkSize, r.Body)
	if err != nil {
		log.Printf("[UPLOAD_CHUNK_ERR] FileID %s chunk %d: %v", fileID, chunkIdx, err)
		respondJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "Failed to write chunk or finalize file",
		})
		return
	}

	if meta.IsCompleted {
		log.Printf("[UPLOAD_DONE] Completed file %s, SHA256: %s",
			MaskFilename(meta.CleanName), meta.SHA256[:12]+"...")
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"success":        true,
		"chunkIndex":     chunkIdx,
		"uploadedChunks": meta.UploadedChunks,
		"totalChunks":    meta.TotalChunks,
		"isCompleted":    meta.IsCompleted,
		"file":           meta,
	})
}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	fileID := r.URL.Query().Get("id")
	if fileID == "" {
		respondJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Missing file id parameter",
		})
		return
	}

	meta, err := s.transferManager.GetFile(fileID)
	if err != nil {
		respondJSON(w, http.StatusNotFound, map[string]string{
			"error": "File not found or transfer incomplete",
		})
		return
	}

	file, err := os.Open(meta.FilePath)
	if err != nil {
		log.Printf("[DOWNLOAD_OPEN_ERR] %v", err)
		respondJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "Failed to open requested file",
		})
		return
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		log.Printf("[DOWNLOAD_STAT_ERR] %v", err)
		respondJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "Failed to stat requested file",
		})
		return
	}

	// Security Headers
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if meta.IsDangerous {
		w.Header().Set("X-Security-Warning", "Potentially executable file")
	}

	// Content Disposition
	safeFilename := strings.ReplaceAll(meta.CleanName, `"`, `_`)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, safeFilename))
	w.Header().Set("Accept-Ranges", "bytes")

	// Support HTTP Range headers for resuming & fast parallel download
	http.ServeContent(w, r, meta.CleanName, stat.ModTime(), file)
}

func (s *Server) handleCertDownload(w http.ResponseWriter, r *http.Request) {
	if s.tlsInfo == nil || s.tlsInfo.CertFile == "" {
		http.Error(w, "Certificate not available", http.StatusNotFound)
		return
	}

	certData, err := os.ReadFile(s.tlsInfo.CertFile)
	if err != nil {
		http.Error(w, "Failed to read certificate", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/x-x509-ca-cert")
	w.Header().Set("Content-Disposition", "attachment; filename=\"hushdrop.crt\"")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(certData)
}

func (s *Server) handleHTTPHelperLanding(w http.ResponseWriter, r *http.Request) {
	host := r.Host
	if h, _, err := net.SplitHostPort(r.Host); err == nil {
		host = h
	}
	httpsURL := fmt.Sprintf("https://%s:%d/", host, s.config.Port)

	html := fmt.Sprintf(`<!DOCTYPE html>
<html lang="ru">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>HushDrop — Сертификат и вход</title>
<style>
body { background: #0b0f19; color: #e2e8f0; font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; display: flex; align-items: center; justify-content: center; min-height: 100vh; margin: 0; padding: 20px; box-sizing: border-box; }
.card { background: #161e2e; border: 1px solid #334155; border-radius: 16px; padding: 28px; max-width: 480px; width: 100%%; box-shadow: 0 10px 25px rgba(0,0,0,0.5); }
h1 { font-size: 20px; margin-top: 0; color: #38bdf8; display: flex; align-items: center; gap: 8px; }
p { font-size: 14px; line-height: 1.6; color: #94a3b8; }
.fp-box { background: #0f172a; border: 1px solid #1e293b; border-radius: 8px; padding: 12px; font-family: monospace; font-size: 11px; word-break: break-all; color: #22c55e; margin: 16px 0; }
.btn { display: block; text-align: center; padding: 12px 18px; border-radius: 10px; font-weight: 600; font-size: 14px; text-decoration: none; margin-bottom: 12px; transition: opacity 0.2s; }
.btn-primary { background: #0284c7; color: #ffffff; }
.btn-secondary { background: #334155; color: #f1f5f9; }
.btn:hover { opacity: 0.9; }
.note { font-size: 12px; color: #64748b; margin-top: 16px; }
</style>
</head>
<body>
<div class="card">
  <h1>🔒 HushDrop Local Helper</h1>
  <p>HushDrop работает в защищенном режиме по <b>HTTPS (TLS 1.3)</b> с локальным самоподписанным сертификатом.</p>
  <div class="fp-box">SHA-256 Fingerprint:<br>%s</div>
  <a class="btn btn-primary" href="%s">Перейти в HushDrop Web App (HTTPS)</a>
  <a class="btn btn-secondary" href="/cert">Скачать сертификат (hushdrop.crt)</a>
  <p class="note"><b>Подсказка:</b> Если браузер показывает предупреждение о сертификате, нажмите «Дополнительно» (Advanced) &rarr; «Перейти на сайт» (Proceed). Отпечаток сертификата гарантирует отсутствие перехвата в локальной сети.</p>
</div>
</body>
</html>`, s.tlsInfo.Fingerprint, httpsURL)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(html))
}

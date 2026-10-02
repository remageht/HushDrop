package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

type PairRequest struct {
	PIN   string `json:"pin"`
	Token string `json:"token"`
}

type RefreshRequest struct {
	RefreshToken string `json:"refreshToken"`
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"status":    "ok",
		"service":   "HushDrop",
		"version":   "1.0.0",
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

	session, err := s.pairManager.VerifyPairing(clientIP, req.PIN, req.Token)
	if err != nil {
		log.Printf("[PAIR_FAILED] Failed PIN pairing attempt from masked IP %s", MaskIP(clientIP))
		respondJSON(w, http.StatusForbidden, map[string]string{
			"error": err.Error(),
		})
		return
	}

	log.Printf("[PAIR_SUCCESS] Paired device from masked IP %s", MaskIP(clientIP))

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"accessToken":      session.AccessToken,
		"refreshToken":     session.RefreshToken,
		"accessExpiresIn":  int(s.config.AccessTokenTTL.Seconds()),
		"refreshExpiresIn": int(s.config.RefreshTokenTTL.Seconds()),
		"fingerprint":      s.tlsInfo.Fingerprint,
	})
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
			respondJSON(w, http.StatusBadRequest, map[string]string{
				"error": err.Error(),
			})
			return
		}
		log.Printf("[UPLOAD_START] New transfer ID %s for masked file %s (%d bytes)",
			fileID[:8], MaskFilename(fileName), fileSize)
	}

	// Stream chunk directly to disk
	meta, err := s.transferManager.WriteChunk(fileID, chunkIdx, chunkSize, r.Body)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{
			"error": err.Error(),
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
		respondJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "Failed to read file on disk",
		})
		return
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "Failed to stat file",
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

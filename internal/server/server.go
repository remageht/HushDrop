package server

import (
	"context"
	"crypto/tls"
	"log"
	"net/http"
	"time"

	"hushdrop/internal/config"
	"hushdrop/internal/crypto"
	"hushdrop/internal/pair"
	"hushdrop/internal/transfer"
)

type Server struct {
	config          *config.Config
	tlsInfo         *crypto.TLSInfo
	pairManager     *pair.Manager
	transferManager *transfer.Manager
	limiter         *ipRateLimiter
	httpServer      *http.Server
}

func NewServer(cfg *config.Config, tlsInfo *crypto.TLSInfo, pm *pair.Manager, tm *transfer.Manager) *Server {
	s := &Server{
		config:          cfg,
		tlsInfo:         tlsInfo,
		pairManager:     pm,
		transferManager: tm,
		limiter:         newRateLimiter(cfg.RateLimitPerMinute, time.Minute),
	}

	mux := http.NewServeMux()

	// API Endpoints
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/api/pair/info", s.handlePairInfo)
	mux.HandleFunc("/api/pair", s.handlePair)
	mux.HandleFunc("/api/pair/refresh", s.handleRefresh)
	mux.HandleFunc("/api/revoke", s.handleRevoke)
	mux.HandleFunc("/api/files", s.handleFilesList)
	mux.HandleFunc("/api/upload", s.handleUploadChunk)
	mux.HandleFunc("/api/download", s.handleDownload)

	// Embedded Static Frontend & SPA Fallback
	staticHandler := &spaHandler{staticFS: getFileSystem()}
	mux.Handle("/", staticHandler)

	// Wrap middleware chain: Recovery -> LAN Filter -> Rate Limit -> Auth -> Mux
	handler := s.recoveryMiddleware(
		s.lanFilterMiddleware(
			s.rateLimitMiddleware(
				s.authMiddleware(mux),
			),
		),
	)

	// Strict TLS 1.3 configuration
	tlsConfig := &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{tlsInfo.Certificate},
	}

	s.httpServer = &http.Server{
		Addr:         cfg.Addr(),
		Handler:      handler,
		TLSConfig:    tlsConfig,
		ReadTimeout:  15 * time.Minute, // Allow large chunked streams
		WriteTimeout: 15 * time.Minute,
		IdleTimeout:  60 * time.Second,
	}

	return s
}

func (s *Server) Start() error {
	log.Printf("[SERVER] Starting HushDrop secure TLS 1.3 server on %s", s.httpServer.Addr)
	return s.httpServer.ListenAndServeTLS("", "")
}

func (s *Server) Shutdown(ctx context.Context) error {
	log.Printf("[SERVER] Gracefully stopping server...")
	return s.httpServer.Shutdown(ctx)
}

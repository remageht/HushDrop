package server

import (
	"context"
	"crypto/tls"
	"log"
	"net/http"
	"os"
	"strings"
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
	certHTTPServer  *http.Server
}

// tlsErrorFilterWriter suppresses expected browser noise like EOF, bad/unknown cert, and aborts
type tlsErrorFilterWriter struct{}

func (w *tlsErrorFilterWriter) Write(p []byte) (n int, err error) {
	msg := string(p)
	if strings.Contains(msg, "TLS handshake error") {
		if strings.Contains(msg, "EOF") ||
			strings.Contains(msg, "connection reset by peer") ||
			strings.Contains(msg, "broken pipe") ||
			strings.Contains(msg, "i/o timeout") ||
			strings.Contains(msg, "remote error: tls: unknown certificate") ||
			strings.Contains(msg, "remote error: tls: bad certificate") ||
			strings.Contains(msg, "use of closed network connection") {
			return len(p), nil
		}
	}
	return os.Stderr.Write(p)
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
	mux.HandleFunc("/cert", s.handleCertDownload)
	mux.HandleFunc("/api/cert", s.handleCertDownload)

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
		ErrorLog:     log.New(&tlsErrorFilterWriter{}, "", 0),
		ReadTimeout:  15 * time.Minute, // Allow large chunked streams
		WriteTimeout: 15 * time.Minute,
		IdleTimeout:  60 * time.Second,
	}

	// Plain HTTP helper server for certificate installation and friendly redirect
	if cfg.HTTPPort > 0 {
		certMux := http.NewServeMux()
		certMux.HandleFunc("/cert", s.handleCertDownload)
		certMux.HandleFunc("/hushdrop.crt", s.handleCertDownload)
		certMux.HandleFunc("/server.crt", s.handleCertDownload)
		certMux.HandleFunc("/", s.handleHTTPHelperLanding)

		certHandler := s.recoveryMiddleware(
			s.lanFilterMiddleware(certMux),
		)

		s.certHTTPServer = &http.Server{
			Addr:         cfg.HTTPAddr(),
			Handler:      certHandler,
			ReadTimeout:  10 * time.Second,
			WriteTimeout: 10 * time.Second,
			IdleTimeout:  30 * time.Second,
		}
	}

	return s
}

func (s *Server) Start() error {
	if s.certHTTPServer != nil {
		go func() {
			log.Printf("[SERVER] Starting HushDrop HTTP cert/helper service on %s", s.certHTTPServer.Addr)
			if err := s.certHTTPServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Printf("[SERVER] HTTP cert service error: %v", err)
			}
		}()
	}

	log.Printf("[SERVER] Starting HushDrop secure TLS 1.3 server on %s", s.httpServer.Addr)
	return s.httpServer.ListenAndServeTLS("", "")
}

func (s *Server) Shutdown(ctx context.Context) error {
	log.Printf("[SERVER] Gracefully stopping server...")
	if s.certHTTPServer != nil {
		_ = s.certHTTPServer.Shutdown(ctx)
	}
	return s.httpServer.Shutdown(ctx)
}

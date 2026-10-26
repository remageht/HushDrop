package server

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"hushdrop/internal/clipboard"
	"hushdrop/internal/config"
	"hushdrop/internal/crypto"
	"hushdrop/internal/discovery"
	"hushdrop/internal/pair"
	"hushdrop/internal/signaling"
	"hushdrop/internal/transfer"
)

type Server struct {
	config           *config.Config
	tlsInfo          *crypto.TLSInfo
	pairManager      *pair.Manager
	transferManager  *transfer.Manager
	clipboardManager *clipboard.Manager
	signalingHub     *signaling.Hub
	limiter          *ipRateLimiter
	nearbyScanner    *discovery.Scanner
	scanMu           sync.Mutex
	scanCooldowns    map[string]time.Time
	scanActive       bool
	httpServer       *http.Server
	certHTTPServer   *http.Server
	// Pre-bound sockets (see Bind). When set, Start() serves on them
	// instead of binding lazily inside ListenAndServe.
	tlsListener  net.Listener
	httpListener net.Listener
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
		config:           cfg,
		tlsInfo:          tlsInfo,
		pairManager:      pm,
		transferManager:  tm,
		clipboardManager: clipboard.NewManager(),
		signalingHub:     signaling.NewHub(),
		limiter:          newRateLimiter(cfg.RateLimitPerMinute, time.Minute),
		nearbyScanner:    discovery.NewScanner(32, 800*time.Millisecond),
		scanCooldowns:    make(map[string]time.Time),
	}

	mux := http.NewServeMux()

	// API Endpoints
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/api/pair/info", s.handlePairInfo)
	mux.HandleFunc("/api/pair", s.handlePair)
	mux.HandleFunc("/api/pair/refresh", s.handleRefresh)
	mux.HandleFunc("/api/revoke", s.handleRevoke)
	mux.HandleFunc("/api/files", s.handleFilesList)
	mux.HandleFunc("/api/clipboard", s.handleClipboard)
	mux.HandleFunc("/api/nearby/scan", s.handleNearbyScan)
	mux.HandleFunc("/api/upload", s.handleUploadChunk)
	mux.HandleFunc("/api/transfer/status", s.handleTransferStatus)
	mux.HandleFunc("/api/download", s.handleDownload)
	mux.HandleFunc("/api/signal", s.handleSignalWebSocket)
	mux.HandleFunc("/cert", s.handleCertDownload)
	mux.HandleFunc("/api/cert", s.handleCertDownload)

	// Embedded Static Frontend & SPA Fallback
	fsys := getFileSystem()
	staticHandler := &spaHandler{staticFS: fsys, fileServer: http.FileServer(fsys)}
	mux.Handle("/", staticHandler)

	// Wrap middleware chain: Recovery -> Security Headers -> LAN Filter -> Rate Limit -> Auth -> Mux
	handler := s.recoveryMiddleware(
		s.securityHeadersMiddleware(
			s.lanFilterMiddleware(
				s.rateLimitMiddleware(
					s.authMiddleware(mux),
				),
			),
		),
	)

	// Strict TLS 1.3 configuration
	tlsConfig := &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{tlsInfo.Certificate},
	}

	s.httpServer = &http.Server{
		Addr:              cfg.Addr(),
		Handler:           handler,
		TLSConfig:         tlsConfig,
		ErrorLog:          log.New(&tlsErrorFilterWriter{}, "", 0),
		ReadHeaderTimeout: 10 * time.Second, // Slowloris protection
		MaxHeaderBytes:    64 << 10,
		ReadTimeout:       15 * time.Minute, // Allow large chunked streams
		WriteTimeout:      15 * time.Minute,
		IdleTimeout:       60 * time.Second,
	}

	// Plain HTTP helper server for certificate installation and friendly redirect
	if cfg.HTTPPort > 0 {
		certMux := http.NewServeMux()
		certMux.HandleFunc("/cert", s.handleCertDownload)
		certMux.HandleFunc("/hushdrop.crt", s.handleCertDownload)
		certMux.HandleFunc("/server.crt", s.handleCertDownload)
		certMux.HandleFunc("/", s.handleHTTPHelperLanding)

		certHandler := s.recoveryMiddleware(
			s.securityHeadersMiddleware(
				s.lanFilterMiddleware(certMux),
			),
		)

		s.certHTTPServer = &http.Server{
			Addr:              cfg.HTTPAddr(),
			Handler:           certHandler,
			ReadHeaderTimeout: 5 * time.Second,
			MaxHeaderBytes:    32 << 10,
			ReadTimeout:       10 * time.Second,
			WriteTimeout:      10 * time.Second,
			IdleTimeout:       30 * time.Second,
		}
	}

	return s
}

// Bind pre-opens both TCP listeners (TLS main + plain HTTP helper) so that
// a port conflict is reported synchronously. Without this, ListenAndServe
// would only fail inside a background goroutine — after the banner with
// PIN/QR has already been printed, misleading the user into scanning a
// dead instance (see device-debug session of 2026-10-26).
func (s *Server) Bind() error {
	tlsLn, err := net.Listen("tcp", s.httpServer.Addr)
	if err != nil {
		return fmt.Errorf("не удалось занять TLS-порт %s: %w", s.httpServer.Addr, err)
	}
	s.tlsListener = tlsLn
	if s.certHTTPServer != nil {
		httpLn, err := net.Listen("tcp", s.certHTTPServer.Addr)
		if err != nil {
			tlsLn.Close()
			return fmt.Errorf("не удалось занять HTTP-порт %s: %w", s.certHTTPServer.Addr, err)
		}
		s.httpListener = httpLn
	}
	return nil
}

func (s *Server) Start() error {
	if s.certHTTPServer != nil {
		go func() {
			log.Printf("[SERVER] Starting HushDrop HTTP cert/helper service on %s", s.certHTTPServer.Addr)
			var serveErr error
			if s.httpListener != nil {
				serveErr = s.certHTTPServer.Serve(s.httpListener)
			} else {
				serveErr = s.certHTTPServer.ListenAndServe()
			}
			if serveErr != nil && serveErr != http.ErrServerClosed {
				log.Printf("[SERVER] HTTP cert service error: %v", serveErr)
			}
		}()
	}

	log.Printf("[SERVER] Starting HushDrop secure TLS 1.3 server on %s", s.httpServer.Addr)
	if s.tlsListener != nil {
		return s.httpServer.ServeTLS(s.tlsListener, "", "")
	}
	return s.httpServer.ListenAndServeTLS("", "")
}

func (s *Server) Shutdown(ctx context.Context) error {
	log.Printf("[SERVER] Gracefully stopping server...")
	if s.certHTTPServer != nil {
		_ = s.certHTTPServer.Shutdown(ctx)
	}
	return s.httpServer.Shutdown(ctx)
}

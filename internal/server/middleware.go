package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"hushdrop/internal/discovery"
)

type ipRateLimiter struct {
	mu       sync.Mutex
	requests map[string][]time.Time
	limit    int
	window   time.Duration
}

func newRateLimiter(limit int, window time.Duration) *ipRateLimiter {
	rl := &ipRateLimiter{
		requests: make(map[string][]time.Time),
		limit:    limit,
		window:   window,
	}

	// Periodic cleanup of old timestamps
	go func() {
		ticker := time.NewTicker(time.Minute)
		for range ticker.C {
			rl.mu.Lock()
			now := time.Now()
			for ip, times := range rl.requests {
				var valid []time.Time
				for _, t := range times {
					if now.Sub(t) < rl.window {
						valid = append(valid, t)
					}
				}
				if len(valid) == 0 {
					delete(rl.requests, ip)
				} else {
					rl.requests[ip] = valid
				}
			}
			rl.mu.Unlock()
		}
	}()

	return rl
}

func (rl *ipRateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-rl.window)

	var valid []time.Time
	for _, t := range rl.requests[ip] {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}

	if len(valid) >= rl.limit {
		rl.requests[ip] = valid
		return false
	}

	rl.requests[ip] = append(valid, now)
	return true
}

// MaskIP masks sensitive parts of an IP for safe logging (e.g. 192.168.1.15 -> 192.168.***.***)
func MaskIP(ipStr string) string {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return "***"
	}
	ip4 := ip.To4()
	if ip4 != nil {
		return fmt.Sprintf("%d.%d.***.***", ip4[0], ip4[1])
	}
	// IPv6
	return "fe80:***:***"
}

// MaskFilename masks filenames for safe logging (e.g. passport.pdf -> pa***.pdf)
func MaskFilename(filename string) string {
	if len(filename) <= 4 {
		return "***"
	}
	return filename[:2] + "***" + filename[len(filename)-4:]
}

func extractIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func (s *Server) lanFilterMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clientIPStr := extractIP(r)
		clientIP := net.ParseIP(clientIPStr)

		// Strictly reject external non-private IPs
		if !discovery.IsPrivateIP(clientIP) {
			log.Printf("[SECURITY] Dropped non-LAN request from masked IP %s: %s %s", MaskIP(clientIPStr), r.Method, r.URL.Path)
			respondJSON(w, http.StatusForbidden, map[string]string{
				"error": "Forbidden: LAN access only",
			})
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (s *Server) rateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// Do not count static frontend assets, health checks, or pairing discovery against the rate limit
		if path == "/" || path == "/health" || path == "/api/pair/info" || !strings.HasPrefix(path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}

		clientIP := extractIP(r)

		if !s.limiter.allow(clientIP) {
			log.Printf("[RATE_LIMIT] Throttled masked IP %s on %s", MaskIP(clientIP), r.URL.Path)
			w.Header().Set("Retry-After", "60")
			respondJSON(w, http.StatusTooManyRequests, map[string]string{
				"error": "Too many requests. Please slow down.",
			})
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// Public unauthenticated endpoints
		if !strings.HasPrefix(path, "/api/") ||
			path == "/api/pair" ||
			path == "/api/pair/refresh" ||
			path == "/api/pair/info" ||
			path == "/health" {
			next.ServeHTTP(w, r)
			return
		}

		// Check Bearer Token
		authHeader := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			respondJSON(w, http.StatusUnauthorized, map[string]string{
				"error": "Unauthorized: missing bearer token",
			})
			return
		}

		token := strings.TrimPrefix(authHeader, "Bearer ")
		session, valid := s.pairManager.ValidateSession(token)
		if !valid || session == nil {
			respondJSON(w, http.StatusUnauthorized, map[string]string{
				"error": "Unauthorized: invalid or expired session",
			})
			return
		}

		// Valid session: continue
		next.ServeHTTP(w, r)
	})
}

func (s *Server) recoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				clientIP := extractIP(r)
				log.Printf("[PANIC_RECOVERED] Masked IP %s: %v", MaskIP(clientIP), rec)
				// Return generic 500 without stack traces
				respondJSON(w, http.StatusInternalServerError, map[string]string{
					"error": "Internal Server Error",
				})
			}
		}()
		next.ServeHTTP(w, r)
	})
}

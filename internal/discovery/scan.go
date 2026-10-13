package discovery

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"
)

// DiscoveredNode represents an active HushDrop peer found in the LAN.
// Fingerprint is strictly omitted to preserve the zero-trust manual PIN pairing model.
type DiscoveredNode struct {
	IP        string `json:"ip"`
	Port      int    `json:"port"`
	Version   string `json:"version"`
	LatencyMs int64  `json:"latencyMs"`
}

// Scanner handles concurrent probing of LAN addresses.
type Scanner struct {
	client      *http.Client
	concurrency int
	timeout     time.Duration
}

// NewScanner creates a Scanner configured with concurrency 32 and 800ms probe timeout.
func NewScanner(concurrency int, timeout time.Duration) *Scanner {
	if concurrency <= 0 {
		concurrency = 32
	}
	if timeout <= 0 {
		timeout = 800 * time.Millisecond
	}

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true, // Self-signed certs expected for LAN discovery
		},
		DisableKeepAlives: true,
		DialContext: (&net.Dialer{
			Timeout: timeout,
		}).DialContext,
		TLSHandshakeTimeout: timeout,
		ResponseHeaderTimeout: timeout,
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   timeout,
	}

	return &Scanner{
		client:      client,
		concurrency: concurrency,
		timeout:     timeout,
	}
}

type scanTarget struct {
	ip   string
	port int
}

// ProbeNode queries GET /health on https://<ip>:<port> and validates that service is "HushDrop".
func (s *Scanner) ProbeNode(ip string, port int) (*DiscoveredNode, error) {
	url := fmt.Sprintf("https://%s/health", net.JoinHostPort(ip, strconv.Itoa(port)))
	start := time.Now()

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// Limit response size to 4KB
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return nil, err
	}

	var payload struct {
		Service string `json:"service"`
		Version string `json:"version"`
		Status  string `json:"status"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}

	if payload.Service != "HushDrop" {
		return nil, fmt.Errorf("foreign service: %s", payload.Service)
	}

	latency := time.Since(start).Milliseconds()
	if latency < 1 {
		latency = 1
	}

	version := payload.Version
	if version == "" {
		version = "unknown"
	}

	return &DiscoveredNode{
		IP:        ip,
		Port:      port,
		Version:   version,
		LatencyMs: latency,
	}, nil
}

// Scan probes candidate IPs on given candidate ports, filtering out the host's own instance.
func (s *Scanner) Scan(candidateIPs []net.IP, customPorts []int, selfPort int) []DiscoveredNode {
	// Discover local IPs to avoid probing self
	localIPMap := make(map[string]bool)
	if lanIPs, err := GetLANIPs(); err == nil {
		for _, lip := range lanIPs {
			localIPMap[lip.String()] = true
		}
	}
	localIPMap["127.0.0.1"] = true
	localIPMap["::1"] = true

	// Validate or set default ports
	var portsToScan []int
	for _, p := range customPorts {
		if p > 0 && p <= 65535 {
			portsToScan = append(portsToScan, p)
		}
	}
	if len(portsToScan) > 4 {
		portsToScan = portsToScan[:4] // Cap custom ports to prevent flooding
	}

	hasCustomPorts := len(portsToScan) > 0
	if !hasCustomPorts {
		portsToScan = []int{8443}
	}

	// Build target list
	var targets []scanTarget
	for _, ip := range candidateIPs {
		ipStr := ip.String()
		isLocal := localIPMap[ipStr]

		for _, p := range portsToScan {
			// Skip self
			if isLocal && p == selfPort {
				continue
			}
			targets = append(targets, scanTarget{ip: ipStr, port: p})
		}

		// On local IP, also probe 8444 if no custom ports were passed, to find co-located test nodes
		if isLocal && !hasCustomPorts && selfPort != 8444 {
			targets = append(targets, scanTarget{ip: ipStr, port: 8444})
		}
	}

	// Also check loopback for co-located second node on different port
	if !hasCustomPorts && selfPort == 8443 {
		targets = append(targets, scanTarget{ip: "127.0.0.1", port: 8444})
	} else if !hasCustomPorts && selfPort == 8444 {
		targets = append(targets, scanTarget{ip: "127.0.0.1", port: 8443})
	}

	if len(targets) == 0 {
		return []DiscoveredNode{}
	}

	targetCh := make(chan scanTarget, len(targets))
	for _, t := range targets {
		targetCh <- t
	}
	close(targetCh)

	concurrency := s.concurrency
	if concurrency > len(targets) {
		concurrency = len(targets)
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	var results []DiscoveredNode
	seen := make(map[string]bool)

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for target := range targetCh {
				node, err := s.ProbeNode(target.ip, target.port)
				if err == nil && node != nil {
					key := fmt.Sprintf("%s:%d", node.IP, node.Port)
					mu.Lock()
					if !seen[key] {
						seen[key] = true
						results = append(results, *node)
					}
					mu.Unlock()
				}
			}
		}()
	}

	wg.Wait()

	// Sort results by latency ascending
	sort.Slice(results, func(i, j int) bool {
		return results[i].LatencyMs < results[j].LatencyMs
	})

	if results == nil {
		results = []DiscoveredNode{}
	}

	return results
}

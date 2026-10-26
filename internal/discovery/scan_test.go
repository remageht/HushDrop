package discovery

import (
	"crypto/tls"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func TestIsRFC1918(t *testing.T) {
	tests := []struct {
		ip       string
		expected bool
	}{
		{"10.0.0.1", true},
		{"10.255.255.254", true},
		{"172.16.0.1", true},
		{"172.31.255.254", true},
		{"172.32.0.1", false},
		{"192.168.0.1", true},
		{"192.168.1.100", true},
		{"127.0.0.1", false},      // Loopback
		{"169.254.1.1", false},    // Link-local
		{"8.8.8.8", false},        // Public
		{"1.1.1.1", false},        // Public
		{"::1", false},            // IPv6
	}

	for _, tc := range tests {
		ip := net.ParseIP(tc.ip)
		actual := IsRFC1918(ip)
		if actual != tc.expected {
			t.Errorf("IsRFC1918(%s) = %v; want %v", tc.ip, actual, tc.expected)
		}
	}
}

func TestGenerateSubnetIPs_ParsingAndCapping(t *testing.T) {
	// 1. Standard /24 subnet: should produce exactly 254 host IPs
	_, subnet24, err := net.ParseCIDR("192.168.1.50/24")
	if err != nil {
		t.Fatalf("ParseCIDR failed: %v", err)
	}
	ips24, err := GenerateSubnetIPs(subnet24, 256)
	if err != nil {
		t.Fatalf("GenerateSubnetIPs(/24) failed: %v", err)
	}
	if len(ips24) != 254 {
		t.Errorf("expected 254 IPs for /24, got %d", len(ips24))
	}
	if ips24[0].String() != "192.168.1.1" {
		t.Errorf("expected first IP to be 192.168.1.1, got %s", ips24[0].String())
	}
	if ips24[len(ips24)-1].String() != "192.168.1.254" {
		t.Errorf("expected last IP to be 192.168.1.254, got %s", ips24[len(ips24)-1].String())
	}

	// 2. /16 network: should be clamped to /24 (254 IPs, capped at 256)
	subnet16 := &net.IPNet{
		IP:   net.ParseIP("172.16.5.10"),
		Mask: net.CIDRMask(16, 32),
	}
	ips16, err := GenerateSubnetIPs(subnet16, 256)
	if err != nil {
		t.Fatalf("GenerateSubnetIPs(/16) failed: %v", err)
	}
	if len(ips16) != 254 {
		t.Errorf("expected 254 IPs for clamped /16, got %d", len(ips16))
	}
	if ips16[0].String() != "172.16.5.1" {
		t.Errorf("expected first IP to be 172.16.5.1, got %s", ips16[0].String())
	}

	// 3. /8 network: should also be clamped to /24 (254 IPs)
	subnet8 := &net.IPNet{
		IP:   net.ParseIP("10.5.20.1"),
		Mask: net.CIDRMask(8, 32),
	}
	ips8, err := GenerateSubnetIPs(subnet8, 256)
	if err != nil {
		t.Fatalf("GenerateSubnetIPs(/8) failed: %v", err)
	}
	if len(ips8) != 254 {
		t.Errorf("expected 254 IPs for clamped /8, got %d", len(ips8))
	}
	if ips8[0].String() != "10.5.20.1" {
		t.Errorf("expected first IP to be 10.5.20.1, got %s", ips8[0].String())
	}

	// 4. Smaller /28 network: 14 hosts
	_, subnet28, _ := net.ParseCIDR("192.168.1.10/28")
	ips28, err := GenerateSubnetIPs(subnet28, 256)
	if err != nil {
		t.Fatalf("GenerateSubnetIPs(/28) failed: %v", err)
	}
	if len(ips28) != 14 {
		t.Errorf("expected 14 IPs for /28, got %d", len(ips28))
	}

	// 5. Foreign / Non-RFC1918 subnet: must be rejected with error
	_, publicSubnet, _ := net.ParseCIDR("8.8.8.0/24")
	_, err = GenerateSubnetIPs(publicSubnet, 256)
	if err == nil {
		t.Errorf("expected error for non-RFC1918 subnet 8.8.8.0/24, got nil")
	}

	// 6. Link-local subnet: must be rejected
	_, linkLocalSubnet, _ := net.ParseCIDR("169.254.1.0/16")
	_, err = GenerateSubnetIPs(linkLocalSubnet, 256)
	if err == nil {
		t.Errorf("expected error for link-local subnet, got nil")
	}
}

func TestScanner_PeerFilteringByService(t *testing.T) {
	// Server 1: Genuine HushDrop peer
	hushdropServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status":  "ok",
				"service": "HushDrop",
				"version": "0.4.0",
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer hushdropServer.Close()

	// Server 2: Foreign HTTP service (e.g. Printer, Router, or other tool)
	foreignServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status":  "ok",
				"service": "Apache",
				"version": "2.4.51",
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer foreignServer.Close()

	// Server 3: Random HTTP service returning 404
	notFoundServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer notFoundServer.Close()

	// Extract host and ports
	hdHost, hdPortStr, _ := net.SplitHostPort(hushdropServer.Listener.Addr().String())
	hdPort, _ := strconv.Atoi(hdPortStr)

	fgHost, fgPortStr, _ := net.SplitHostPort(foreignServer.Listener.Addr().String())
	fgPort, _ := strconv.Atoi(fgPortStr)

	nfHost, nfPortStr, _ := net.SplitHostPort(notFoundServer.Listener.Addr().String())
	nfPort, _ := strconv.Atoi(nfPortStr)

	scanner := NewScanner(32, 500*time.Millisecond)
	// Enable TLS verification skip for test server certificates
	scanner.client.Transport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}

	// 1. Probe HushDrop server: must succeed and extract metadata
	node, err := scanner.ProbeNode(hdHost, hdPort)
	if err != nil {
		t.Fatalf("ProbeNode(HushDrop) failed: %v", err)
	}
	if node.Version != "0.4.0" {
		t.Errorf("expected version 0.4.0, got %s", node.Version)
	}
	if node.Port != hdPort {
		t.Errorf("expected port %d, got %d", hdPort, node.Port)
	}
	if node.LatencyMs < 0 {
		t.Errorf("expected positive latency, got %d", node.LatencyMs)
	}

	// 2. Probe foreign server: must be rejected because service != "HushDrop"
	_, err = scanner.ProbeNode(fgHost, fgPort)
	if err == nil {
		t.Errorf("expected foreign server to be rejected, but probe succeeded")
	}

	// 3. Probe not-found server: must be rejected
	_, err = scanner.ProbeNode(nfHost, nfPort)
	if err == nil {
		t.Errorf("expected 404 server to be rejected, but probe succeeded")
	}
}

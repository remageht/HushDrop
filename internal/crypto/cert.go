package crypto

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type TLSInfo struct {
	CertFile    string
	KeyFile     string
	Certificate tls.Certificate
	Fingerprint string // Formatted SHA-256 fingerprint (e.g. AA:BB:CC...)
}

func GetOrCreateTLSCert(certsDir string, lanIPs []net.IP) (*TLSInfo, error) {
	certFile := filepath.Join(certsDir, "server.crt")
	keyFile := filepath.Join(certsDir, "server.key")

	// Try loading existing cert
	if _, err := os.Stat(certFile); err == nil {
		if _, err := os.Stat(keyFile); err == nil {
			cert, err := tls.LoadX509KeyPair(certFile, keyFile)
			if err == nil && len(cert.Certificate) > 0 {
				parsed, err := x509.ParseCertificate(cert.Certificate[0])
				if err == nil && time.Now().Before(parsed.NotAfter) {
					fp := computeFingerprint(cert.Certificate[0])
					return &TLSInfo{
						CertFile:    certFile,
						KeyFile:     keyFile,
						Certificate: cert,
						Fingerprint: fp,
					}, nil
				}
			}
		}
	}

	// Generate new self-signed ECDSA certificate
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate private key: %w", err)
	}

	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return nil, fmt.Errorf("failed to generate serial number: %w", err)
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"HushDrop Secure Transfer"},
			CommonName:   "HushDrop Local Server",
		},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           append([]net.IP{net.ParseIP("127.0.0.1"), net.IPv6loopback}, lanIPs...),
		DNSNames:              []string{"localhost", "hushdrop.local"},
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return nil, fmt.Errorf("failed to create certificate: %w", err)
	}

	// Write cert PEM
	certPem := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	if err := os.WriteFile(certFile, certPem, 0600); err != nil {
		return nil, fmt.Errorf("failed to write cert file: %w", err)
	}

	// Write key PEM
	keyBytes, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal private key: %w", err)
	}
	keyPem := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})
	if err := os.WriteFile(keyFile, keyPem, 0600); err != nil {
		return nil, fmt.Errorf("failed to write key file: %w", err)
	}

	cert, err := tls.X509KeyPair(certPem, keyPem)
	if err != nil {
		return nil, fmt.Errorf("failed to load generated key pair: %w", err)
	}

	fp := computeFingerprint(derBytes)
	return &TLSInfo{
		CertFile:    certFile,
		KeyFile:     keyFile,
		Certificate: cert,
		Fingerprint: fp,
	}, nil
}

func computeFingerprint(certDER []byte) string {
	hash := sha256.Sum256(certDER)
	hexStr := hex.EncodeToString(hash[:])
	var chunks []string
	for i := 0; i < len(hexStr); i += 2 {
		chunks = append(chunks, strings.ToUpper(hexStr[i:i+2]))
	}
	return strings.Join(chunks, ":")
}

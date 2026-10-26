package config

import (
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"hushdrop/internal/crypto"
)

type Config struct {
	Port                 int
	HTTPPort             int
	Host                 string
	Portable             bool
	EncryptStorage       bool
	EncryptionKeyFile    string
	DataDir              string
	CertsDir             string
	DownloadsDir         string
	TempDir              string
	MaxChunkSize         int64
	MaxFileSize          int64
	RateLimitPerMinute   int
	PinTTL               time.Duration
	SessionInactivityTTL time.Duration
	AccessTokenTTL       time.Duration
	RefreshTokenTTL      time.Duration
	OneTimeToken         string
	CurrentPin           string
}

func LoadConfig() (*Config, error) {
	portFlag := flag.Int("port", 8443, "Port to listen on (HTTPS)")
	httpPortFlag := flag.Int("http-port", 8080, "Port for plain HTTP helper (certificate download / redirect)")
	hostFlag := flag.String("host", "0.0.0.0", "Host/interface to bind to")
	portableFlag := flag.Bool("portable", true, "Portable mode: store all state in ./data alongside the binary")
	dataDirFlag := flag.String("data", "", "Custom data directory path")
	encryptStorageFlag := flag.Bool("encrypt-storage", false, "Encrypt saved files on disk (at-rest encryption; a random key is auto-generated into the data dir)")
	flag.Parse()

	cfg := &Config{
		Port:                 *portFlag,
		HTTPPort:             *httpPortFlag,
		Host:                 *hostFlag,
		Portable:             *portableFlag,
		EncryptStorage:       *encryptStorageFlag,
		MaxChunkSize:         4 * 1024 * 1024,      // 4MB max chunk size
		MaxFileSize:          5 * 1024 * 1024 * 1024, // 5GB max file size
		RateLimitPerMinute:   60,
		PinTTL:               10 * time.Minute,
		SessionInactivityTTL: 10 * time.Minute,
		AccessTokenTTL:       5 * time.Minute,
		RefreshTokenTTL:      1 * time.Hour,
	}

	// Environment overrides
	if envPort := os.Getenv("PORT"); envPort != "" {
		if p, err := strconv.Atoi(envPort); err == nil {
			cfg.Port = p
		}
	}
	if envHTTPPort := os.Getenv("HTTP_PORT"); envHTTPPort != "" {
		if p, err := strconv.Atoi(envHTTPPort); err == nil {
			cfg.HTTPPort = p
		}
	}
	if envHost := os.Getenv("HOST"); envHost != "" {
		cfg.Host = envHost
	}
	if envEncryptStorage := os.Getenv("ENCRYPT_STORAGE"); envEncryptStorage != "" {
		if b, err := strconv.ParseBool(envEncryptStorage); err == nil {
			cfg.EncryptStorage = b
		}
	}

	// Base executable directory for portable resolution
	exePath, err := os.Executable()
	baseDir := "."
	if err == nil {
		baseDir = filepath.Dir(exePath)
	}

	if *dataDirFlag != "" {
		cfg.DataDir = *dataDirFlag
	} else if cfg.Portable {
		// Strictly in ./data adjacent to the binary or working dir
		cfg.DataDir = filepath.Join(baseDir, "data")
	} else {
		// Fallback local data dir
		cfg.DataDir = filepath.Join(baseDir, "data")
	}

	cfg.CertsDir = filepath.Join(cfg.DataDir, "certs")
	cfg.DownloadsDir = filepath.Join(cfg.DataDir, "downloads")
	cfg.TempDir = filepath.Join(cfg.DataDir, "temp")
	cfg.EncryptionKeyFile = filepath.Join(cfg.DataDir, crypto.AtRestKeyFileName)

	// Ensure directories exist
	for _, dir := range []string{cfg.DataDir, cfg.CertsDir, cfg.DownloadsDir, cfg.TempDir} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return nil, fmt.Errorf("failed to initialize directory %s: %w", dir, err)
		}
	}

	return cfg, nil
}

func (c *Config) Addr() string {
	return net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
}

func (c *Config) HTTPAddr() string {
	return net.JoinHostPort(c.Host, strconv.Itoa(c.HTTPPort))
}

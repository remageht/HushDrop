package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"hushdrop/internal/config"
	"hushdrop/internal/crypto"
	"hushdrop/internal/discovery"
	"hushdrop/internal/pair"
	"hushdrop/internal/server"
	"hushdrop/internal/transfer"
)

var version = "0.3.5"

// openBrowserDefault is overridden per build via ldflags:
// console build -> "false", GUI (windowsgui) build -> "true".
// Tauri sidecar always passes --open-browser=false explicitly.
var openBrowserDefault = "false"

// writePairingFile saves the current pairing details next to the data dir
// so GUI-subsystem builds (no console window) still expose PIN/fingerprint.
func writePairingFile(dataDir, url, pin, fingerprint string) {
	content := fmt.Sprintf("HushDrop pairing (valid ~10 min from %s)\nURL: %s\nPIN: %s\nTLS fingerprint (SHA256): %s\n",
		time.Now().Format("2006-01-02 15:04:05"), url, pin, fingerprint)
	_ = os.WriteFile(filepath.Join(dataDir, "pairing.txt"), []byte(content), 0600)
}

// openBrowser tries to open the URL in the default browser (best effort, no CGO).
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}

func main() {
	openBrowserFlag := flag.Bool("open-browser", openBrowserDefault == "true", "Open the local PWA in the default browser on startup and write data/pairing.txt")
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Ошибка конфигурации: %v", err)
	}

	// Discover LAN IP addresses
	lanIPs, err := discovery.GetLANIPs()
	if err != nil {
		log.Printf("[DISCOVERY] Предупреждение: не удалось получить список интерфейсов: %v", err)
	}
	primaryIP := discovery.GetPrimaryLANIP()

	// Initialize TLS 1.3 self-signed certificate
	tlsInfo, err := crypto.GetOrCreateTLSCert(cfg.CertsDir, append(lanIPs, primaryIP))
	if err != nil {
		log.Fatalf("Ошибка генерации TLS-сертификата: %v", err)
	}

	// Initialize Pairing Manager
	pairManager := pair.NewManager(cfg.PinTTL, cfg.SessionInactivityTTL, cfg.AccessTokenTTL, cfg.RefreshTokenTTL)
	pin, token, _ := pairManager.GetActivePairingDetails()

	// Initialize Transfer Manager
	transferManager := transfer.NewManager(cfg.DownloadsDir, cfg.TempDir, cfg.MaxChunkSize, cfg.MaxFileSize)

	// Format Pairing URL
	portStr := fmt.Sprintf("%d", cfg.Port)
	primaryURL := fmt.Sprintf("https://%s/?token=%s&fp=%s", net.JoinHostPort(primaryIP.String(), portStr), token, tlsInfo.Fingerprint)

	// Display Banner & Security Details
	fmt.Println("=================================================================")
	fmt.Println(" HushDrop — Твои файлы. Твоя сеть. Никого лишнего.")
	fmt.Printf(" Версия: %s | Режим: ", version)
	if cfg.Portable {
		fmt.Printf("Портативный (данные в %s)\n", cfg.DataDir)
	} else {
		fmt.Println("Стандартный")
	}
	fmt.Println("=================================================================")
	fmt.Printf("🔒 TLS 1.3 Fingerprint (SHA256): %s\n", tlsInfo.Fingerprint)
	fmt.Printf("🔑 PIN для сопряжения:          %s (действителен 10 минут)\n", pin)
	fmt.Printf("🌐 Адрес подключения:            %s\n", primaryURL)
	if cfg.HTTPPort > 0 {
		fmt.Printf("📜 Скачать сертификат (HTTP):   http://%s/cert\n", net.JoinHostPort(primaryIP.String(), fmt.Sprintf("%d", cfg.HTTPPort)))
	}
	if len(lanIPs) > 1 {
		fmt.Println("📡 Дополнительные LAN IP:")
		for _, ip := range lanIPs {
			if !ip.Equal(primaryIP) {
				fmt.Printf("   -> https://%s/?token=%s\n", net.JoinHostPort(ip.String(), portStr), token)
			}
		}
	}
	fmt.Println("-----------------------------------------------------------------")
	fmt.Println("Отсканируйте QR-код телефоном в той же сети Wi-Fi:")
	discovery.PrintTerminalQR(primaryURL)
	fmt.Println("=================================================================")

	if *openBrowserFlag {
		writePairingFile(cfg.DataDir, primaryURL, pin, tlsInfo.Fingerprint)
		openBrowser(fmt.Sprintf("https://127.0.0.1:%s/", portStr))
	}

	// Create and start server
	srv := server.NewServer(cfg, tlsInfo, pairManager, transferManager)

	// Graceful shutdown channel
	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		if err := srv.Start(); err != nil {
			log.Printf("[SERVER] Остановка: %v", err)
		}
	}()

	<-stopChan
	fmt.Println("\nЗавершение работы HushDrop...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Zeroize memory and clean active states
	pairManager.RevokeAll()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("Ошибка при остановке сервера: %v", err)
	}
	fmt.Println("Безопасное завершение выполнено. Ключи в RAM уничтожены.")
}

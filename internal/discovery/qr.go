package discovery

import (
	"fmt"

	qrcode "github.com/skip2/go-qrcode"
)

// PrintTerminalQR prints an ASCII QR code to the console
func PrintTerminalQR(url string) {
	qr, err := qrcode.New(url, qrcode.Medium)
	if err != nil {
		fmt.Printf("Не удалось сгенерировать QR-код: %v\n", err)
		return
	}
	// Small ASCII string
	fmt.Println(qr.ToSmallString(false))
}

// GenerateQRPNG generates PNG bytes for the given URL string
func GenerateQRPNG(url string, size int) ([]byte, error) {
	return qrcode.Encode(url, qrcode.Medium, size)
}

# HushDrop

> **Your files. Your network. Nobody else.**

[Русская версия](README.md) | **English**

Private phone ↔ PC file transfer over the local network (Wi-Fi) — no internet, no messengers, no third-party servers, no cloud storage. Phone ↔ phone works via the shared hub on the PC: one uploads via "Send", the other picks up via "Receive".

[![Release](https://img.shields.io/badge/release-v0.3.2-emerald?style=flat)](https://github.com/remageht/HushDrop/releases)
[![Status](https://img.shields.io/badge/status-alpha-orange?style=flat)](#security-disclaimer)
[![Go Version](https://img.shields.io/badge/Go-1.22-00ADD8?style=flat&logo=go)](https://go.dev)
[![React](https://img.shields.io/badge/React-18-61DAFB?style=flat&logo=react)](https://react.dev)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

> [!WARNING]
> ### Security disclaimer (Alpha)
> The project is in active alpha (`v0.3.2`). Intended strictly for local networks (home Wi-Fi, office LAN, mobile hotspot). Do not expose the service port to the public internet without a VPN. Always verify the TLS fingerprint before uploading files.

---

## ⚡ Quick start

### Windows (portable)
1. Download `HushDrop.exe` (or `HushDrop-Desktop.exe` with tray) from [Releases](https://github.com/remageht/HushDrop/releases).
2. Run by double-click or from the terminal:
   ```cmd
   HushDrop.exe --portable
   ```
3. Scan the QR code shown in the console with your phone camera (both devices must be on the same Wi-Fi).
4. In the opened PWA, enter the 6-digit PIN and verify the TLS fingerprint.
5. Done! Drag and drop files into the browser window to transfer them.

> **100% portable**: in `--portable` mode all certificates, temp chunks and downloaded files stay strictly in `./data` next to the binary. Nothing is written to the Windows registry or `%APPDATA%`. Runs straight from a USB stick without admin rights.

### Android
Download `HushDrop.apk` from [Releases](https://github.com/remageht/HushDrop/releases) and install via sideload. Same flow: menu → connect via QR + PIN → Send / Receive.

### Docker (dev environment)
```bash
docker compose up --build
```
The UI will be available at `https://<lan-ip>:8443`.

---

## 🔍 How to verify the Fingerprint (TLS 1.3)

Fingerprint verification rules out man-in-the-middle (MITM) attacks on untrusted Wi-Fi:

1. **On PC at startup** the console prints a line like:
   ```
   🔒 TLS 1.3 Fingerprint (SHA256): ED:5B:3F:AD:D2:...
   ```
2. **When scanning the QR code** the fingerprint travels inside the link (`&fp=...`).
3. **In the phone web UI**, the PIN screen shows the server fingerprint:
   - If the hash matches the QR link, a green badge lights up: **"Verified with QR"**.
   - Compare the first 4 and last 4 characters with the PC screen. A match guarantees the connection goes straight to your computer.

---

## 🔒 Security & Architecture

- **Strictly LAN-Only (RFC 1918)**: any incoming connection outside private ranges (`10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`, `127.0.0.1`, `::1`) is instantly rejected with `403 Forbidden`.
- **TLS 1.3 with Fingerprint check**: a self-signed ECDSA P-256 certificate is generated on first run. The SHA-256 fingerprint is shown in the console and verified by the client when connecting via QR.
- **E2E over TLS (PFS)**: ephemeral X25519 keys and AES-256-GCM session keys live **exclusively in RAM**. Restarting the server instantly invalidates all sessions.
- **PIN brute-force protection**: the 6-digit PIN lives for 10 minutes. 5 wrong attempts in a row ban the client IP for 5 minutes.
- **Rate limiting**: 20 requests per minute per IP. Exceeding it returns `429 Too Many Requests`.
- **Memory safety (< 200MB RAM)**: 4MB chunked transfer with streaming writes to disk via a 64KB buffer. Transferring files up to 5GB never exhausts RAM.
- **Dangerous-extension confirmation**: executables and scripts (`.exe`, `.bat`, `.ps1`, `.sh`, `.msi`) get a safety flag and require explicit user consent before download.
- **"Forget everything" button (`POST /api/revoke`)**: instantly zeroes active keys in RAM, invalidates sessions and wipes transferred temp files.
- **Log masking**: IPs and filenames are masked (`192.168.***.***`, `fi***.zip`), preventing metadata leaks. Generic HTTP 500 without stacktrace.
- **No telemetry, no external CDNs**: the app is fully autonomous and works on an isolated network with no internet access.

---

## 📡 API reference

All private endpoints require the `Authorization: Bearer <access_token>` header (except `/api/pair`, `/api/pair/info`, `/health` and static files).

| Method | Endpoint | Description |
|---|---|---|
| `GET` | `/health` | Service availability check |
| `GET` | `/api/pair/info` | Get TLS fingerprint and remaining pairing time |
| `POST` | `/api/pair` | Pair via PIN and X25519: returns `{accessToken, refreshToken, serverPubKey}` |
| `POST` | `/api/pair/refresh` | Refresh the access token via refresh token |
| `GET` | `/api/files` | List received completed files |
| `POST` | `/api/upload` | Upload a 4MB chunk (`X-File-Id`, `X-Chunk-Index`, `X-Total-Chunks`) |
| `GET` | `/api/download?id=...` | Stream-download a file with HTTP Range support (`bytes=...`) |
| `POST` | `/api/revoke` | Invalidate all sessions, zeroize keys and delete files |

---

## 🛠 Build from source

Requires **Go 1.22+** and **Node.js 18+**.

```bash
# Build with automatic frontend bundling into ./dist:
# Windows:
powershell -ExecutionPolicy Bypass -File .\scripts\build.ps1

# Linux / macOS:
chmod +x ./scripts/build.sh
./scripts/build.sh
```

---

## 📄 License & Security

- License: [MIT](LICENSE)
- Vulnerability disclosure policy: [SECURITY.md](SECURITY.md)

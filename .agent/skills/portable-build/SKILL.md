---
name: portable-build
description: "Zero-registry, zero-admin portable compilation and distribution patterns for HushDrop across Windows, Linux, and macOS."
---

# Portable Distribution & Zero-Install Architecture

HushDrop is designed for true plug-and-play portability. It can run directly from a USB flash drive without administrator privileges, system installation, or Windows Registry writes.

## 1. Single Binary Self-Containment
- **Embedded Frontend**: The production React PWA is built into `cmd/hushdrop/web` or `internal/server/dist` and embedded directly into the Go executable via standard `//go:embed`.
- **Zero External Runtime Dependencies**: Compiles with `CGO_ENABLED=0` to create static Go binaries. No MSVC runtimes, glibc dependencies, or dynamic linkers are strictly required.

## 2. `--portable` Mode & Data Isolation
- When invoked with `--portable` (or when running alongside a `./data` directory or `.portable` marker file):
  - All configurations, generated TLS certificates, temporary chunks, and received files are strictly scoped to `./data/` adjacent to the executable:
    - `./data/certs/` -> Ephemeral/cached TLS certificates.
    - `./data/downloads/` -> Received files.
    - `./data/temp/` -> In-flight transfer chunks.
  - No writes to `%APPDATA%`, `HKEY_CURRENT_USER\Software`, `/etc`, or `~/.config`.
  - Safely ejectable: deleting the directory or pulling the USB leaves zero traces on host system.

## 3. Cross-Platform Targets
- **Windows**: `HushDrop.exe` (x86_64 and ARM64).
- **Linux**: Static binary and AppImage packaging.
- **macOS**: Standalone portable `.app` bundle or universal Mach-O binary.
- **Development Docker**: Lightweight alpine-based Go container (`Dockerfile.backend` + `docker-compose.yml`) for local testing only.

## 4. Build Commands & Flags
```bash
# Windows portable release
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags="-s -w -X main.version=1.0.0" -o HushDrop.exe ./cmd/hushdrop

# Linux static release
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w -X main.version=1.0.0" -o hushdrop-linux ./cmd/hushdrop

# macOS universal release
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags="-s -w -X main.version=1.0.0" -o hushdrop-darwin-arm64 ./cmd/hushdrop
```

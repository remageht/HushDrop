---
name: portable-build
description: "Single binary CGO_ENABLED=0, strictly ./data adjacent, zero registry/appdata/admin rights, AppImage+zip+apk, internal/server/web untracked and built in CI/scripts for HushDrop."
---

# Portable Distribution & Zero-Install Architecture

HushDrop is built for plug-and-play operation directly from removable media (USB drives) with zero footprints left on the operating system.

## 1. Single Binary Self-Containment
- **Embedded Frontend**: Built from React PWA sources via `npm run build` and copied to `internal/server/web/`.
- **Git Hygiene**: `internal/server/web/*` is **NOT committed** to git tracking; it is populated during CI and build script execution. A `.gitkeep` placeholder ensures initial checkouts compile without error.
- **Zero External Runtime Dependencies**: Compiles with `CGO_ENABLED=0` to create static Go binaries. No MSVC runtimes or glibc dependencies required.

## 2. `--portable` Mode & Isolation Rules
- When invoked with `--portable`:
  - All configurations, generated TLS certificates, temporary chunks, and received files are strictly scoped to `./data/` adjacent to the executable:
    - `./data/certs/` -> Ephemeral/cached TLS certificates.
    - `./data/downloads/` -> Received files.
    - `./data/temp/` -> In-flight transfer chunks.
  - Zero writes to Windows Registry (`HKCU`/`HKLM`), `%APPDATA%`, `%LOCALAPPDATA%`, `/etc`, or `~/.config`.
  - Safely ejectable: deleting the application folder leaves zero residual files or settings on the host machine.

## 3. Cross-Platform Packaging Targets
- **Windows**: Single portable `.zip` containing `HushDrop.exe` (x86_64 and ARM64).
- **Linux**: Static binary and portable AppImage.
- **macOS**: Standalone portable `.tar.gz` Mach-O universal binary.
- **Mobile Sideload (Capacitor)**: Android APK built via `@capacitor/android` pointing `webDir` to `../frontend/dist`. Native directories `mobile/android/` and `mobile/ios/` are strictly generated on-the-fly and excluded from git tracking.
- **Web/iOS Fallback**: iOS 17+ home screen PWA installation without Google Play Services or App Store dependencies.

## 4. Production Build Commands
```bash
# Frontend Compilation
cd frontend && npm ci && npm run build && cd ..

# Asset Synchronization
cp -r frontend/dist/* internal/server/web/

# Binary Compilation
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags="-s -w -X main.version=0.1.0" -o dist/HushDrop.exe ./cmd/hushdrop
```

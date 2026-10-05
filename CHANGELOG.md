# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.3.1] - 2026-10-05

### Added
- Auto-open `PairingModal` when opening the Web UI or PWA with a `?token=` query parameter from scanned QR codes.
- UI clipboard copy feedback indicator for PIN and Fingerprint details.

### Fixed
- **Android Host TLS Certificate Generation**: Removed provider constraint from `JcaX509CertificateConverter` to prevent `CertificateException` under Conscrypt / Android OpenSSL runtime.
- **Android Foreground Service Execution**: Declared `HostForegroundService` with `android:exported="true"` in `AndroidManifest.xml` for reliable service startup across MIUI and Android 14+.

### Changed
- Unified version bump across all packages (Go core, Android host, Capacitor mobile wrapper, Desktop Tauri wrapper, and Frontend) to `v0.3.1`.

---

## [0.3.0] - 2026-10-05

### Added
- **Android Phone Host Mode**: Embedded Kotlin HTTPS server with 1:1 API parity with the Go server, enabling direct phone-to-phone transfers without a PC.
- **Host Foreground Service**: Android background execution resilience with active notification and `WifiLock`.
- **Host UI**: Dedicated `HostPage` displaying real-time pairing QR code, 6-digit PIN, SHA-256 fingerprint, and active shared files list.
- **Ephemeral RAM-Only Clipboard**: Local P2P text synchronization with optional Burn After Read.
- **Roadmap**: Documentation for v0.4-v0.6 architectural milestones (WebRTC direct mesh, mDNS zero-conf discovery, and relay modes).

---

## [0.2.0] - 2026-10-04

### Added
- **Desktop Tauri 2 Wrapper**: Portable desktop application packaging the Go binary as a sidecar process.
- **System Tray Integration**: Show/Hide, QR Pairing viewer, direct downloads folder shortcut, and instant session revocation.
- **Drag-and-Drop**: Support for dragging files directly into the desktop window for instant upload.
- **Mobile Capacitor Setup**: Android sideload wrapper with camera scanning and custom URL scheme (`hushdrop://pair`).
- **Tab Navigation**: Clean Menu / Send / Receive interface tabs and custom HushDrop adaptive launcher icons.

---

## [0.1.0] - 2026-10-03

### Added
- Initial alpha release of HushDrop.
- **Core Go Server**: TLS 1.3 self-signed ECDSA P-256 certificates with SHA-256 fingerprint verification.
- **Security & Pairing**: 6-digit numeric PIN pairing with 10-minute TTL and rate limiting (5 failed attempts = 5-minute lockout).
- **Chunked Transfer**: Memory-bounded streaming with 4MB chunks and SHA-256 integrity verification.
- **Resumable Downloads**: HTTP Range requests with `206 Partial Content`.
- **100% Portable**: Local-only `./data` storage with zero AppData/Registry modification and zero external cloud telemetry.
- **PWA Frontend**: React + Vite + TypeScript interface with centralized API client.

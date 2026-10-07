# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.3.4] - 2026-10-06

### Fixed
- **Rate Limit Exhuastion**: Increased metered API budget to 60 req/min and reduced background polling frequency (8s for Host, 10s for Receive) with `visibilitychange` suspension to prevent 429 errors when multiple tabs are open.
- **Android Background Stop Path**: Corrected `HostForegroundService` shutdown sequence on Android 8+ by starting foreground with a stopping notification inside the catch block before `stopSelf()`, averting `ForegroundServiceDidNotStartInTimeException`.
- **System Share Queue Deduplication**: Removed redundant UI share event emission to prevent duplicate chunk uploads, and automatically flush the pending share queue into active transfers upon successful pairing.

### Changed
- Bumped unified project version to `0.3.4` (Android `versionCode 34`).

---

## [0.3.3] - 2026-10-05

### Added
- **Animated SVG Intro Splash**: Interactive 1.6-second brand intro animation on cold start featuring circle path drawing, smooth arrow entry, base line animation, and emerald shimmer highlight.
- **Graceful Loading & Fallback**: Responsive progress bar for loads exceeding 3 seconds, timeout protection (reload prompt after 8 seconds), and full `prefers-reduced-motion` accessibility support.
- **Tauri Desktop Splashscreen**: Centered 400x400 borderless splashscreen window matching the animated logo until Go backend service is fully ready.
- **Zero-Flicker Native Transition**: Synchronized native splash window background (`#0b0f19`) and 300ms fade-out duration across Capacitor Android and Tauri wrappers.

### Changed
- Bumped unified project version to `0.3.3` (Android `versionCode 33`).

---

## [0.3.2] - 2026-10-05

### Security & Hardening
- **Android Host Service Isolation**: Declared `HostForegroundService` with `android:exported="false"`, enforcing strict internal UI/plugin control and preventing unauthorized intent invocation.
- **Permission Minimization**: Completely removed broad storage and media permissions (`READ_MEDIA_IMAGES`, `READ_MEDIA_VIDEO`, `READ_MEDIA_AUDIO`, `READ_EXTERNAL_STORAGE`). System share targets now stream directly via `ContentResolver` delegate `grant-uri-permission`.
- **Cleartext Boundary Restriction**: Introduced `networkSecurityConfig` restricting cleartext communication strictly to localhost loopback helper endpoints.
- **Log Privacy**: Eliminated 6-digit PIN and raw IP addresses from Android `Log.i` logcat output (IPs are masked, PIN is shown exclusively in user-facing notifications).
- **Distribution Data Isolation**: Build scripts (`scripts/build.ps1`) verify zero `.key`, `.pem`, `.crt`, `.log`, or `.env` files exist in release staging directories; `./data/` runtime directories are isolated.

### Fixed
- **Self-DoS & Throttling Fix**: Unmetered public endpoints (`GET /`, `/health`, `/api/pair/info`) so health checks and pairing discovery never consume rate-limit quotas on Go core and Android embedded servers.
- **Frontend Polling Guards**: `HostPage` and `ReceivePage` now strictly guard file listing polling behind `isAuthenticated()`, preventing unauthenticated clients from burning IP rate limits.
- **Android 8+ Service Lifecycle**: Handled `stopHost` via `startForegroundService` on Android O+ to prevent background `IllegalStateException`.
- **Zero Raw Fetch Calls**: Replaced raw `fetch()` calls in `HostPage` and `SendPage` with centralized `apiClient.listFiles()` and `apiClient.fetchLocalBlob()`.
- **Sharing Bridge Consolidation**: Unified system share target handling inside `mobile/src/share.ts` without inline duplication in `SendPage`.

### Changed
- Unified version bump to `v0.3.2` across Go core, Desktop Tauri (`0.3.2`), Android APK (`versionCode 32`, `versionName 0.3.2`), and Web PWA.

---

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

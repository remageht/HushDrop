---
name: secure-transfer
description: "LAN-only TLS+E2E PIN+QR zero-cloud secure transfer architecture guidelines and protocols for HushDrop."
---

# Secure Transfer Architecture & Protocols

HushDrop implements zero-cloud, peer-to-peer, localized file transmission with defense-in-depth security principles.

## 1. Network Boundary: LAN-Only Enforcement
- **Strict IP Whitelisting**: Every incoming connection is verified against RFC 1918 private IPv4 spaces and RFC 4193 / link-local IPv6:
  - `10.0.0.0/8`
  - `172.16.0.0/12`
  - `192.168.0.0/16`
  - `127.0.0.0/8` (Loopback)
  - `::1` (IPv6 Loopback), `fe80::/10` (Link-local), `fc00::/7` (Unique Local)
- **Immediate Rejection**: Any packet originating outside private IP ranges is dropped immediately with HTTP 403 Forbidden without exposing server details.

## 2. Transport Layer Security (TLS 1.3)
- **Self-signed Ephemeral / Local Certificate**: Auto-generated on initial launch with ECDSA P-256 or Ed25519.
- **TLS Version**: Strict TLS 1.3 minimum configuration (`tls.VersionTLS13`).
- **Fingerprint Verification**: SHA-256 certificate fingerprint is calculated and displayed in terminal / host UI. The client compares this fingerprint against the host before establishing encrypted sessions to prevent Man-in-the-Middle (MITM) attacks.

## 3. End-to-End Cryptography (E2E Layer)
- **Ephemeral Key Exchange**: X25519 ECDH key exchange is performed per pairing session.
- **Session Keys**: AES-256-GCM symmetric cipher key derived via HKDF-SHA256 from the shared secret.
- **RAM-Only Policy**: Cryptographic session keys exist strictly in volatile memory (RAM) with `crypto/subtle` constant-time comparisons. Keys are zeroized upon session termination or process exit.
- **Perfect Forward Secrecy (PFS)**: Every server reboot invalidates prior keys and session tokens.

## 4. Pairing & Authentication Lifecycle
- **QR Code Flow**: Host displays QR code containing `https://<lan-ip>:8443/?token=<one-time-token>&fp=<sha256-fingerprint>`.
- **PIN Verification**: 6-digit numeric PIN with a strict 10-minute Time-To-Live (TTL).
- **Brute-Force Shield**: 5 consecutive incorrect PIN attempts result in a 5-minute IP ban.
- **Rate Limiting**: 20 requests/minute per client IP across all API routes.
- **Token Invalidation**:
  - Access Token TTL: 5 minutes.
  - Refresh Token TTL: 1 hour (rotated on refresh).
  - Inactivity Timeout: 10 minutes without transfer activity revokes the session.
  - One-click "Forget Everything" (`/api/revoke`) instantaneously wipes all active tokens, memory keys, and transfer records.

## 5. File Validation & Streaming Safety
- **Filename Sanitization**: Strip directory traversal paths (`../`, `..\\`), null bytes (`\x00`), and control characters. Files are saved into dedicated safe subfolders using UUID/hashed internal IDs while preserving sanitized display names.
- **Chunk & Size Caps**:
  - Maximum chunk size: 4MB (`4,194,304` bytes). Exceeding requests return HTTP 413 Payload Too Large.
  - Maximum file size: 5GB (`5,368,709,120` bytes).
- **Direct Disk Streaming**: Uploaded chunks are appended directly to temporary disk files, keeping application memory footprint under 200MB even during multi-gigabyte transfers.
- **Executable File Safeguard**: Uploads with executable extensions (`.exe`, `.bat`, `.cmd`, `.ps1`, `.sh`, `.vbs`, `.msi`) are flagged with a `dangerous` attribute, requiring explicit recipient acknowledgment before execution or downloading.

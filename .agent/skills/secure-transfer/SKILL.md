---
name: secure-transfer
description: "LAN-only RFC1918, TLS1.3 ECDSA+P256 fingerprint SHA256, X25519+AES-GCM RAM-only PFS, PIN 6 digits TTL10min 5 attempts=ban5min, Bearer access5min, rate 20/min/IP 429, sanitize ../ null-byte, chunk<=4MB file<=5GB disk stream 64KB buffer RAM<200MB, .exe/.ps1/.sh confirmation, PII mask, 500 generic without stacktrace, no telemetry/CDN for HushDrop."
---

# Secure Transfer Architecture & Protocols

HushDrop enforces a zero-trust, zero-cloud localized transport architecture designed to preserve absolute confidentiality.

## 1. Network Boundary: LAN-Only Enforcement (RFC 1918 & Local IPv6)
- **Whitelisted Subnets**:
  - `10.0.0.0/8`
  - `172.16.0.0/12`
  - `192.168.0.0/16`
  - `127.0.0.0/8` (Loopback)
  - `::1` (IPv6 Loopback), `fe80::/10` (Link-local), `fc00::/7` (Unique Local)
- **Immediate Rejection**: Any packet originating outside private IP ranges is dropped immediately with HTTP 403 Forbidden. `X-Forwarded-For` is never trusted for remote IP resolution.

## 2. Transport Layer Security (TLS 1.3 Minimum)
- **Self-Signed Ephemeral Certificate**: Auto-generated on initial launch with ECDSA P-256 (`crypto/ecdsa`, `elliptic.P256`).
- **TLS Version**: Strict TLS 1.3 (`tls.VersionTLS13`). Older TLS versions are refused by cipher suite config.
- **Fingerprint Verification**: SHA-256 certificate fingerprint (`AA:BB:CC:...`) is output to terminal and compared by client during QR pairing to guarantee immunity against active MITM interception.

## 3. End-to-End Cryptography (E2E Layer in RAM)
- **X25519 ECDH Handshake**: Ephemeral keypair generated per pairing session.
- **Session Keys**: AES-256-GCM symmetric cipher key derived via HKDF-SHA256 from the shared secret.
- **RAM-Only Policy**: Keys reside strictly in volatile memory. Keys and secrets are zeroized via `crypto.Zeroize` upon token revocation, inactivity timeout, or process termination (Perfect Forward Secrecy).
- **Restart Invalidation**: Server restart discards memory keys and regenerates PIN + QR token.

## 4. Pairing & Authentication Lifecycle
- **QR Code Flow**: Host displays QR code containing `https://<lan-ip>:8443/?token=<one-time-token>&fp=<sha256-fingerprint>`.
- **PIN Verification**: 6-digit numeric PIN with strict 10-minute TTL.
- **Brute-Force Shield**: 5 consecutive incorrect PIN attempts result in an automatic 5-minute IP ban.
- **Rate Limiting**: 20 requests/minute per client IP across all API routes. Exceeding requests receive HTTP 429 Too Many Requests (`Retry-After: 60`).
- **Token Invalidation**:
  - Access Token TTL: 5 minutes.
  - Refresh Token TTL: 1 hour (rotated on refresh).
  - Inactivity Timeout: 10 minutes without transfer activity revokes session.
  - One-click "Forget Everything" (`/api/revoke`) instantaneously wipes all active tokens, memory keys, and transfer records.

## 5. File Validation & Streaming Safety
- **Path Traversal Shield**: Filenames decoded (`url.QueryUnescape`), sanitized from `../`, `..\`, null bytes (`\x00`), and control characters. Target paths verified with `filepath.Clean` and strict directory containment check (`strings.HasPrefix`).
- **Chunk & Size Caps**:
  - Maximum chunk size: 4MB (`4,194,304` bytes). Exceeding requests return HTTP 413 Payload Too Large.
  - Maximum file size: 5GB (`5,368,709,120` bytes).
- **Direct Disk Streaming**: Chunks streamed directly to disk via a 64KB buffer, ensuring total process RAM remains < 200MB even when processing multi-gigabyte transfers.
- **Executable File Safeguard**: Files with executable or script extensions (`.exe`, `.bat`, `.cmd`, `.ps1`, `.sh`, `.vbs`, `.msi`) are flagged with `isDangerous: true` and require recipient confirmation prior to download.
- **PII Masking & Generic Errors**: Mask IP addresses (`192.168.***.***`) and filenames (`se***.txt`) in logs. Client-facing 500 errors are generic without exposing internal stack traces or filesystem paths. No telemetry, no external CDN dependencies.

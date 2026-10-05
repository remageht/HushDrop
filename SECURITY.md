# Security Policy & Vulnerability Disclosure

## Threat Model & Security Architecture

HushDrop is designed for **strictly localized, zero-cloud peer-to-peer file transfers** across trusted or semi-trusted Local Area Networks (LAN / Wi-Fi).

### Defense-in-Depth Principles
1. **Local Boundary (RFC 1918 / RFC 4193)**:
   - Only connections originating from private address spaces (`10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`, `127.0.0.1`, `::1`) are accepted.
   - Public IP traffic is rejected immediately with HTTP 403 Forbidden.
2. **Transport Security (TLS 1.3 Minimum)**:
   - Uses an ephemeral self-signed ECDSA P-256 certificate auto-generated on first launch.
   - The SHA-256 certificate fingerprint is displayed upon startup and matched against the QR code URL payload (`&fp=...`) to prevent Active Man-in-the-Middle (MITM) attacks.
3. **Application Layer PFS (Perfect Forward Secrecy)**:
   - X25519 ECDH key exchange is performed upon PIN pairing.
   - Session keys (AES-256-GCM) are held **strictly in volatile memory (RAM)** and zeroized upon session termination or server shutdown.
   - Server reboots immediately invalidate all prior QR codes and sessions.
4. **Brute-Force & Abuse Mitigation**:
   - 6-digit numeric PIN with a strict 10-minute TTL.
   - 5 consecutive failed PIN attempts trigger an automatic 5-minute IP ban.
   - Global rate limit of 20 requests per minute per IP address.
5. **Memory Safety & Streaming Integrity**:
   - File uploads are capped at 4MB per chunk and streamed directly to disk through a 64KB buffer.
   - Application memory footprint remains under 200MB even when transferring 5GB files.
   - File paths are sanitized against directory traversal (`../`, null bytes, URL encoding) and verified to remain strictly within `./data/downloads/`.
6. **Execution Safeguards**:
   - Executable files (`.exe`, `.bat`, `.cmd`, `.ps1`, `.sh`, `.msi`) are flagged with a security badge and require explicit recipient confirmation before download.
7. **Android Permission Minimization & System Sharing**:
   - Broad storage permissions (`READ_MEDIA_IMAGES`, `READ_MEDIA_VIDEO`, `READ_MEDIA_AUDIO`, `READ_EXTERNAL_STORAGE`) are entirely removed.
   - Incoming shared files from system gallery/file manager intents are received solely via delegate `grant-uri-permission` flags and streamed through `ContentResolver` into app-private cache storage.
8. **Network Security Config & Cleartext Boundary**:
   - Android `networkSecurityConfig` restricts cleartext traffic by default (`cleartextTrafficPermitted="false"`).
   - Cleartext is permitted strictly for loopback/local developer helper port (`127.0.0.1`, `localhost`) to download initial certificates if necessary.
   - All actual peer transfer and pairing communication occurs strictly over TLS 1.3 (`:8443`).
9. **Distribution Security & Data Isolation**:
   - The runtime `./data/` folder containing generated TLS keys (`cert.key`), certificates (`cert.pem`), and transfer logs is strictly untracked and never packaged in release distributions.
   - Packaging automation (`scripts/build.ps1`) validates that release staging directories contain zero `.key`, `.pem`, `.crt`, `.log`, or `.env` files.

---

## Supported Versions

| Version | Supported          |
| ------- | ------------------ |
| 0.1.x (Alpha) | :white_check_mark: |

---

## Reporting a Vulnerability

If you discover a security vulnerability in HushDrop, please do **NOT** open a public issue.

Please report vulnerabilities via:
- **GitHub Private Vulnerability Reporting**: [Security Advisories](https://github.com/remageht/HushDrop/security/advisories)
- **Direct Email**: `security@hushdrop.local` (or contact the maintainer at https://github.com/remageht)

Please include:
- A description of the issue and potential impact.
- Steps to reproduce or a proof of concept (PoC).
- Any proposed remediation or patches.

We will acknowledge receipt within 48 hours and work with you to coordinate a responsible public disclosure after a fix is published.

# HushDrop — Phone Host v0.3 Roadmap

> Generated: 2026-10-03. Commit: c5090c7

---

## Completed (v0.1–v0.3)

| Version | Feature |
|---------|---------|
| v0.1 | Go core: TLS1.3 ECDSA, PIN 6-digit TTL10m, chunks 4MB, portable `./data` |
| v0.2 | React PWA frontend, Capacitor Android sideload, Tauri desktop wrapper |
| v0.3 | **Phone-host**: Android embedded HTTPS server (`android-host` module), 1:1 Go API parity, `HostPage.tsx` with QR/PIN/fingerprint, 4-tab navigation |

---

## v0.4 — WebRTC DataChannel + iOS PWA (Q4 2026)

**Goal:** Direct browser-to-browser file transfer without server intermediary, and iOS 17+ support as a client.

### Tasks
- [ ] `internal/signaling/` — lightweight WebSocket signaling relay (Go) for SDP/ICE exchange only (no data in relay)
- [ ] `frontend/src/pages/PeerPage.tsx` — WebRTC DataChannel UI: offer/answer flow, progress bar, cancel
- [ ] iOS PWA manifest tweaks: `apple-touch-icon`, `apple-mobile-web-app-capable`, splash screens
- [ ] `frontend/src/api/peer.ts` — WebRTC abstraction (`createOffer`, `createAnswer`, `sendFile`, `receiveFile`)
- [ ] Chunk size negotiation (DataChannel buffer pressure → adaptive chunk size 64KB-1MB)
- [ ] Fallback: if WebRTC unavailable, offer existing HTTPS transfer

### Security
- All SDP/ICE data passes only over the LAN signaling relay (no STUN/TURN servers — LAN-only)
- Fingerprint of local peer cert shown and verified in pairing modal before DataChannel opens

---

## v0.5 — mDNS Auto-Discovery + AES-GCM At-Rest Encryption (Q1 2027)

**Goal:** Zero-config peer discovery (no QR scan needed on same LAN), and optional encrypted storage for hosted files.

### Tasks
- [ ] `internal/discovery/mdns_advertise.go` — advertise `_hushdrop._tcp.` with fingerprint TXT record (currently only client-side NSD browse)
- [ ] `frontend/src/api/discovery.ts` — mDNS browse via Capacitor `@capacitor-community/mdns` or custom plugin; auto-populate server URL
- [ ] `mobile/android-host/NsdHelper.kt` — already done (browse side to add)
- [x] **At-rest encryption** (done in [Unreleased]): optional AES-256-GCM streaming container applied on upload finalization; design changed from passphrase+Argon2id to an auto-generated device-local key file (`data/atrest.key`, 32 random bytes, 0600)
  - Go: `internal/crypto/atrest.go` — V2 wire format (4 MiB authenticated frames, authenticated EOF marker, trailing-junk rejection) + `internal/transfer` wiring (`NewManagerWithEncryption`, streamed decrypt on `/api/download` without Range support) + `--encrypt-storage` flag wired in `cmd/hushdrop/main.go`
  - Android host: `AtRest.kt` mirrors the same wire format via `javax.crypto` (pinned by a cross-language decrypt-vector test), `TransferManager.withEncryption()` for uploads and share-intent hosted files
  - Frontend passphrase modal — dropped by design: no passphrase UI needed; `isEncrypted` flag exposed instead

### Security note
- At-rest master key is device-local (`data/atrest.key`; Android: app-private filesDir); no passphrase exists to leak
- At-rest encryption is opt-in (`--encrypt-storage`), disabled by default; without the key file stored content is unrecoverable

---

## v0.6 — Relay Mode + Background Transfer Queue (Q2 2027)

**Goal:** Transfer files even when devices are on different subnets (VPN, mobile data fallback), with a resumable background queue.

### Tasks
- [ ] **Self-hosted relay**: optional Go relay server (`cmd/hushdrop-relay/`) — encrypted end-to-end (X25519+AES-GCM), relay never sees plaintext
  - Relay is self-hosted only; no public HushDrop relay
  - Client authenticates to relay via same PIN+fingerprint flow
- [ ] **Background transfer queue** (Android):
  - `WorkManager`-based `TransferWorker.kt` for large files that survive app backgrounding
  - Queue state persisted to `data/hushdrop_host/queue.json` (file IDs only, no file content)
  - Frontend: `QueuePage.tsx` — queued transfers, retry, cancel
- [ ] **Resume support** (chunked uploads already have chunk tracker; add HTTP `Content-Range` on upload resumption)
- [ ] **Desktop notification** (Tauri) on transfer complete with file name + size

### Security note
- Relay mode requires explicit user opt-in with relay URL configured in `.env`
- All relay traffic is end-to-end encrypted before leaving LAN

---

## Architecture Diagram

```mermaid
flowchart LR
    subgraph v0.3["v0.3 — Current"]
        PC["HushDrop.exe\n(Go HTTPS :8443)"]
        PhoneHost["Android Host\n(Kotlin HTTPS :8443)"]
        Client["PWA Client\n(React)"]

        PC -->|"QR + PIN"| Client
        PhoneHost -->|"QR + PIN"| Client
    end

    subgraph v0.4["v0.4 — WebRTC"]
        Sig["Go Signaling\n(:8444 WS"]
        Peer1["Browser A\n(DataChannel)"]
        Peer2["Browser B\n(DataChannel)"]
        Peer1 -->|SDP/ICE| Sig
        Sig -->|SDP/ICE| Peer2
        Peer1 <-->|"P2P DataChannel\n(LAN direct)"| Peer2
    end

    subgraph v0.6["v0.6 — Relay"]
        Relay["Self-hosted Relay\n(E2E encrypted)"]
        DevA["Device A"] -->|"X25519 encrypted"| Relay
        Relay -->|"X25519 encrypted"| DevB["Device B"]
    end
```

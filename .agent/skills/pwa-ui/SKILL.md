---
name: pwa-ui
description: "Strict frontend/src/api/client.ts layer, zero component fetch calls, explicit loading/error/empty/progress state machines, TLS fingerprint verification in UI for HushDrop."
---

# PWA UI & Frontend Design Patterns

HushDrop frontend is built with React 18, TypeScript, and Tailwind CSS, optimized for mobile devices and desktop browsers with offline caching.

## 1. Single Centralized API Client (`frontend/src/api/client.ts`)
- **Strict Prohibition**: UI components MUST NEVER call raw `fetch()` or `axios`. All communications route through `client.ts`.
- **Automatic Token Ingestion & Rotation**:
  - Automatically captures bearer tokens upon PIN pairing.
  - Transparently intercepts HTTP 401 and attempts silent token refresh.
  - Inactivity and revoke events cleanly reset stored tokens.
- **Explicit UI States**:
  - `loading`: visual indicators during handshakes and list queries.
  - `error`: localized error descriptions without technical stack traces.
  - `empty`: informative zero-state illustrations when no transfers exist.
  - `progress`: chunk-level percentage indicators, transfer speed (MB/s), and estimated time remaining (ETA).

## 2. In-App TLS Fingerprint Verification
- **Visual Verification**:
  - When opened via QR code (`/?token=...&fp=...`), the PWA automatically extracts the host's SHA-256 fingerprint.
  - Queries `GET /api/pair/info` to verify certificate parameters.
  - Displays a green "Сверен с QR" confirmation badge and full fingerprint string for user verification before PIN entry.

## 3. Progressive Web App (PWA) Standards
- **Manifest (`manifest.webmanifest`)**:
  - Standalone display mode with dark theme background `#090d16`.
  - Responsive app icons (`logo.svg`).
- **Service Worker (`sw.js`)**:
  - Caches app shell, fonts, and assets for offline presentation.
  - Explicitly bypasses caching for `/api/*` and chunk streams to ensure real-time transmission.

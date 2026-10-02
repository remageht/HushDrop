---
name: pwa-ui
description: "Single client.ts API layer, responsive Russian dark-mode PWA architecture, offline caching, and real-time chunk progress for HushDrop."
---

# PWA UI & Frontend Design Patterns

HushDrop frontend is built with React 18 / 19, TypeScript, and Tailwind CSS, engineered specifically for fast mobile and desktop browsers with offline capabilities.

## 1. Single Centralized API Client (`client.ts`)
- **Strict Prohibition**: UI components MUST NEVER call raw `fetch()` or `axios`.
- **Typed Methods**: All communication with the Go backend passes strictly through `frontend/src/api/client.ts`.
- **Automatic Token Ingestion & Rotation**:
  - Automatically captures bearer tokens upon PIN pairing.
  - Transparently intercepts HTTP 401 and attempts silent token refresh.
  - Automatically redirects to pairing modal upon session revocation.
- **Explicit UI States**:
  - `loading`: spinners and skeleton loaders during handshakes.
  - `error`: user-friendly localized messages without leaking technical traces.
  - `empty`: intuitive zero-state illustrations when no files are queued.
  - `progress`: granular byte-level and chunk-level percentage indicators, transfer speed (MB/s), and estimated time remaining (ETA).

## 2. Progressive Web App (PWA) Standards
- **Manifest (`manifest.webmanifest`)**:
  - Standalone display mode with dark theme background `#0f172a`.
  - Responsive app icons (192x192 and 512x512).
  - Web share target integration where supported.
- **Service Worker (`sw.js`)**:
  - Caches app shell, fonts, and assets for instant loading even during transient network dropouts.
  - Bypasses caching for `/api/*` and chunk streams to ensure real-time transmission.

## 3. UI/UX Principles
- **Theme**: Deep dark palette (Tailwind `slate-900` / `zinc-950` with emerald and violet accents).
- **Language**: Native Russian (`ru-RU`), clear terminology ("Отправить", "Получить", "Подтвердить PIN", "Забыть всё").
- **Chunked File Transfer UX**:
  - Drag-and-drop zone with folder hierarchy support (`webkitdirectory`).
  - Active transfers list with Pause/Resume/Cancel controls.
  - Security warning dialog for suspicious file formats (`.exe`, `.ps1`, `.sh`, `.bat`).

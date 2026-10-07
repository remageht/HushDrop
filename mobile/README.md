# HushDrop Mobile — Capacitor Wrapper (Android Sideload)

Standalone Capacitor container for the HushDrop React PWA. Delivers zero-cloud, direct peer-to-peer file transfers over LAN directly to Android devices without Google Play Services or external relay servers.

---

## 1. Architecture & Security Model

- **Zero-Cloud / LAN-Only**: Enforces strict RFC 1918 subnets (`10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`, `127.0.0.0/8`).
- **RAM-Only Ephemeral Tokens**: Secrets, PINs, and AES-256-GCM session keys reside strictly in memory via `frontend/src/api/client.ts`. `@capacitor/preferences` stores ONLY the host and verified SHA-256 fingerprint for pairing UX.
- **TLS 1.3 + Fingerprint**: Self-signed host certificate is verified via the QR fingerprint string (`&fp=...`) or installed to device trust store via `http://<lan-ip>:8080/cert`.
- **Packaging Integrity**: The native directories (`mobile/android/`, `mobile/ios/`) are treated as build targets and are excluded from git repository tracking.

---

## 2. Android native: generated, правишь patches + скрипт, не файлы

Директория `mobile/android/` является полностью генерируемым артефактом Capacitor (`cap add` / `cap sync`) и **полностью исключена из Git-трекинга**.

Никакие файлы в `mobile/android/` **не редактируются вручную** и не добавляются через `git add -f`. Все кастомизации платформы хранятся в виде эталонов и фрагментов в папке `mobile/android-patches/`:
- `android-patches/network_security_config.xml` — конфигурация безопасности TLS / localhost loopback
- `android-patches/MainActivity.java` — эталонный класс с интеграцией `HushDropHostPlugin` и обработкой `onNewIntent`
- `android-patches/manifest-snippet.xml` — фрагмент манифеста (deep-links, SEND/SEND_MULTIPLE share targets, изоляция сервиса `HostForegroundService`, FileProvider)
- `android-patches/build.gradle.patch` — фрагмент конфигурации Gradle с вычислением `versionName` и `versionCode` из `package.json`

Кастомизации накладываются автоматически при каждом вызове:
```bash
npm run cap:sync
```
или через скрипты напрямую:
```bash
# Linux / macOS:
./scripts/apply-android-custom.sh

# Windows PowerShell:
.\scripts\apply-android-custom.ps1
```
Скрипт полностью идемпотентен: повторные запуски не вызывают повторных правок или дублирования строк.

---

## 3. Build & Run Instructions

### Step 1: Install Dependencies
```bash
cd mobile
npm install
```

### Step 2: Build Frontend
```bash
cd ../frontend
npm run build
cd ../mobile
```

### Step 3: Add & Sync Android Project
```bash
npx cap add android
npm run cap:sync
```
*(Скрипт `apply-android-custom` выполнится автоматически после синхронизации).*

### Step 4: Assemble Debug APK
```bash
cd android
./gradlew assembleDebug
cd ..
```
The compiled APK will be located at:
`mobile/android/app/build/outputs/apk/debug/app-debug.apk`

### Step 5: Sideload onto Device via USB
```bash
adb install -r android/app/build/outputs/apk/debug/app-debug.apk
```

---

## 4. Connection & Pairing Flow

1. **Launch Host**: Run `HushDrop.exe --portable` on PC.
2. **Scan / Open**:
   - Tap link from QR code or open `app.hushdrop`.
   - Optional: download and install cert via `http://<lan-ip>:8080/cert`.
3. **Verify Fingerprint & PIN**: Compare SHA-256 fingerprint in UI, enter 6-digit PIN.
4. **Transfer**: Drag-and-drop or select files via system file picker / share sheet.

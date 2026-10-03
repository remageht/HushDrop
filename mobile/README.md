# HushDrop Mobile — Capacitor Wrapper (Android Sideload)

Standalone Capacitor container for the HushDrop React PWA. Delivers zero-cloud, direct peer-to-peer file transfers over LAN directly to Android devices without Google Play Services or external relay servers.

---

## 1. Architecture & Security Model

- **Zero-Cloud / LAN-Only**: Enforces strict RFC 1918 subnets (`10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`, `127.0.0.0/8`).
- **RAM-Only Ephemeral Tokens**: Secrets, PINs, and AES-256-GCM session keys reside strictly in memory via `frontend/src/api/client.ts`. `@capacitor/preferences` stores ONLY the host and verified SHA-256 fingerprint for pairing UX.
- **TLS 1.3 + Fingerprint**: Self-signed host certificate is verified via the QR fingerprint string (`&fp=...`) or installed to device trust store via `http://<lan-ip>:8080/cert`.
- **Packaging Integrity**: The native directories (`mobile/android/`, `mobile/ios/`) are treated as build targets and are excluded from git repository tracking.

---

## 2. AndroidManifest.xml Configuration Fragment

When `npx cap add android` or `npx cap sync` is executed, the following configuration is applied to `mobile/android/app/src/main/AndroidManifest.xml`:

```xml
<?xml version="1.0" encoding="utf-8"?>
<manifest xmlns:android="http://schemas.android.com/apk/res/android"
    package="app.hushdrop">

    <!-- Network Permissions (LAN P2P) -->
    <uses-permission android:name="android.permission.INTERNET" />
    <uses-permission android:name="android.permission.ACCESS_NETWORK_STATE" />
    <uses-permission android:name="android.permission.ACCESS_WIFI_STATE" />

    <!-- Storage Permissions for Sharing Files -->
    <uses-permission android:name="android.permission.READ_MEDIA_IMAGES" />
    <uses-permission android:name="android.permission.READ_MEDIA_VIDEO" />
    <uses-permission android:name="android.permission.READ_MEDIA_AUDIO" />
    <uses-permission android:name="android.permission.READ_EXTERNAL_STORAGE" android:maxSdkVersion="32" />

    <application
        android:allowBackup="false"
        android:icon="@mipmap/ic_launcher"
        android:label="HushDrop"
        android:roundIcon="@mipmap/ic_launcher_round"
        android:supportsRtl="true"
        android:theme="@style/AppTheme"
        android:usesCleartextTraffic="true"> <!-- Cleartext allowed solely for local :8080/cert download -->

        <activity
            android:configChanges="orientation|keyboardHidden|keyboard|screenSize|locale|smallestScreenSize|screenLayout|uiMode"
            android:name=".MainActivity"
            android:label="HushDrop"
            android:theme="@style/AppTheme.NoActionBarLaunch"
            android:launchMode="singleTask"
            android:exported="true">

            <intent-filter>
                <action android:name="android.intent.action.MAIN" />
                <category android:name="android.intent.category.LAUNCHER" />
            </intent-filter>

            <!-- Custom Scheme Deep Linking: hushdrop://pair?host=...&token=...&fp=... -->
            <intent-filter android:autoVerify="false">
                <action android:name="android.intent.action.VIEW" />
                <category android:name="android.intent.category.DEFAULT" />
                <category android:name="android.intent.category.BROWSABLE" />
                <data android:scheme="hushdrop" android:host="pair" />
            </intent-filter>

            <!-- Standard HTTPS Deep Linking from QR Code: https://<lan-ip>:8443/?token=... -->
            <intent-filter>
                <action android:name="android.intent.action.VIEW" />
                <category android:name="android.intent.category.DEFAULT" />
                <category android:name="android.intent.category.BROWSABLE" />
                <data android:scheme="https" android:port="8443" />
            </intent-filter>

            <!-- System Share Target (Single File): Send to HushDrop -->
            <intent-filter>
                <action android:name="android.intent.action.SEND" />
                <category android:name="android.intent.category.DEFAULT" />
                <data android:mimeType="*/*" />
            </intent-filter>

            <!-- System Share Target (Multiple Files): Send to HushDrop -->
            <intent-filter>
                <action android:name="android.intent.action.SEND_MULTIPLE" />
                <category android:name="android.intent.category.DEFAULT" />
                <data android:mimeType="*/*" />
            </intent-filter>

        </activity>
    </application>
</manifest>
```

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
npx cap sync android
```

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

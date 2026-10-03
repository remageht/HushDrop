---
name: debug-usb
description: USB debugging and reverse tunneling procedures for mobile-to-host testing in HushDrop.
---

# USB Debugging & Reverse Tunneling Skill for HushDrop

## Purpose
Guide for connecting Android test devices via USB, managing port forwarding via `adb reverse`, capturing browser/system logs via `adb logcat`, inspecting PWA webviews via `chrome://inspect`, and sanitizing diagnostic artifacts.

## Prerequisites
- Android SDK platform-tools (`adb.exe`) located in `C:\Android\sdk\platform-tools\adb.exe` or `%LOCALAPPDATA%\Android\Sdk\platform-tools\`.
- Device connected with USB Debugging enabled.

## Core Commands

### 1. Device Discovery
Verify device status:
```powershell
& "C:\Android\sdk\platform-tools\adb.exe" devices
```
If device is unauthorized, accept the RSA prompt on the phone screen.

### 2. Reverse Port Forwarding
To bypass Wi-Fi AP isolation or test localhost loops directly from the phone:
```powershell
& "C:\Android\sdk\platform-tools\adb.exe" reverse tcp:8443 tcp:8443
```
Now `https://localhost:8443` on the phone routes directly to port 8443 on the host PC.

### 3. Logcat Capture & Filtering
Capture SSL/TLS, Chromium, and networking events while filtering noise:
```powershell
& "C:\Android\sdk\platform-tools\adb.exe" logcat -d | Select-String -Pattern "ssl|cert|hushdrop|chromium|Conscrypt"
```
Or stream to a temporary local debug directory:
```powershell
& "C:\Android\sdk\platform-tools\adb.exe" logcat -d > dist/debug/logcat.log
```

### 4. Remote Web Inspector
- Open Chrome on the host machine: `chrome://inspect/#devices`
- Inspect active tabs, inspect service workers, console logs, network requests, and SSL errors directly.

## Security & Sanitization Rules
- **Never commit debug logs**: All debug output, logcat dumps, and screenshots must be stored strictly in `dist/debug/` (covered by root `.gitignore`).
- **Mask PII & Sensitive Data**:
  - Mask real IP addresses (e.g. `192.168.31.xxx` or `192.168.1.xxx`).
  - Mask user home directory names or machine names.
  - Mask one-time tokens, session tokens, and PIN codes in documentation or public issues.

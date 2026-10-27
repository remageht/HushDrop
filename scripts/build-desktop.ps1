# Desktop (Tauri 2) release artifact build for HushDrop.
# The raw exe from `cargo build --release` ships as HushDrop-Desktop.exe
# (tauri-bundler installers are not used in the release flow).
#
# Includes the issue #1 workaround: tauri-build does not set rerun-if-changed
# on bundle icons, so cargo keeps the package "up-to-date" even when the
# embedded Windows resource (icon.ico / version) changed — the package
# artifacts are deleted by hand, forcing a real recompile and re-embed.
#
# Usage:
#   pwsh -File scripts/build-desktop.ps1            # clean + build + verify
#   pwsh -File scripts/build-desktop.ps1 -Publish   # also copy dist-desktop\HushDrop-Desktop.exe
param(
    [switch]$Publish
)

$ErrorActionPreference = "Stop"

$rootDir = Resolve-Path "$PSScriptRoot\.."
$target = "$rootDir\desktop\src-tauri\target\release"
$exe = "$target\hushdrop-desktop.exe"

Write-Host "===> 1. Cleaning stale desktop package artifacts (issue #1 workaround)..." -ForegroundColor Cyan
Remove-Item -Force $exe -ErrorAction SilentlyContinue
Remove-Item -Force "$target\deps\hushdrop_desktop.exe" -ErrorAction SilentlyContinue
Remove-Item -Recurse -Force "$target\.fingerprint\hushdrop-desktop-*" -ErrorAction SilentlyContinue
Remove-Item -Recurse -Force "$target\build\hushdrop-desktop-*" -ErrorAction SilentlyContinue
Write-Host "   -> package artifacts removed; cargo will recompile hushdrop-desktop" -ForegroundColor Green

Write-Host "===> 2. Compiling hushdrop-desktop (release)..." -ForegroundColor Cyan
cargo build --release --manifest-path "$rootDir\desktop\src-tauri\Cargo.toml"
if ($LASTEXITCODE -ne 0) {
    Write-Host "cargo build failed (exit $LASTEXITCODE)" -ForegroundColor Red
    exit 1
}

Write-Host "===> 3. Verifying embedded version resource..." -ForegroundColor Cyan
$conf = Get-Content "$rootDir\desktop\src-tauri\tauri.conf.json" -Raw | ConvertFrom-Json
$vi = (Get-Item $exe).VersionInfo
Write-Host "   ProductVersion=$($vi.ProductVersion) FileVersion=$($vi.FileVersion) (tauri.conf.json: $($conf.version))"
if ($vi.ProductVersion -ne $conf.version -or $vi.FileVersion -ne $conf.version) {
    Write-Host "   FAIL: exe version does not match tauri.conf.json ($($conf.version))" -ForegroundColor Red
    exit 1
}
Write-Host "   -> version resource matches tauri.conf.json" -ForegroundColor Green

if ($Publish) {
    Write-Host "===> 4. Publishing artifact..." -ForegroundColor Cyan
    $stage = "$rootDir\dist-desktop"
    if (!(Test-Path $stage)) { New-Item -ItemType Directory $stage -Force | Out-Null }
    Copy-Item $exe "$stage\HushDrop-Desktop.exe" -Force
    Write-Host "   -> dist-desktop\HushDrop-Desktop.exe ($('{0:N0}' -f (Get-Item "$stage\HushDrop-Desktop.exe").Length) bytes)" -ForegroundColor Green
}

Write-Host "===> Desktop build finished: $exe" -ForegroundColor Green

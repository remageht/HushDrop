# Cross-platform release compilation script for HushDrop (PowerShell)
$ErrorActionPreference = "Stop"

$rootDir = Resolve-Path "$PSScriptRoot\.."
Set-Location -Path $rootDir

Write-Host "===> 1. Building React PWA frontend..." -ForegroundColor Cyan
Set-Location -Path "$rootDir\frontend"
cmd.exe /c "npm run build"
Set-Location -Path $rootDir

Write-Host "===> 2. Syncing embedded assets into internal/server/web..." -ForegroundColor Cyan
if (!(Test-Path "$rootDir\internal\server\web")) {
    New-Item -ItemType Directory -Path "$rootDir\internal\server\web" -Force | Out-Null
}
Copy-Item -Path "$rootDir\frontend\dist\*" -Destination "$rootDir\internal\server\web\" -Recurse -Force

Write-Host "===> 3. Compiling Go binaries into ./dist/..." -ForegroundColor Cyan
$distDir = "$rootDir\dist"
if (!(Test-Path $distDir)) {
    New-Item -ItemType Directory -Path $distDir -Force | Out-Null
}

$env:CGO_ENABLED = "0"
$version = "0.3.5"
$ldflags = "-s -w -X main.version=$version -X main.openBrowserDefault=false"

# Windows amd64 (console + GUI builds from the same code)
Write-Host "   -> Building dist/HushDrop-console.exe (Windows amd64, console)..." -ForegroundColor Yellow
$env:GOOS = "windows"
$env:GOARCH = "amd64"
go build -ldflags $ldflags -o "$distDir\HushDrop-console.exe" .\cmd\hushdrop
Copy-Item "$distDir\HushDrop-console.exe" "$rootDir\HushDrop-console.exe" -Force

Write-Host "   -> Building dist/HushDrop.exe (Windows amd64, GUI)..." -ForegroundColor Yellow
$ldflagsNoConsole = "-s -w -X main.version=$version -X main.openBrowserDefault=true -H=windowsgui"
go build -ldflags $ldflagsNoConsole -o "$distDir\HushDrop.exe" .\cmd\hushdrop
Copy-Item "$distDir\HushDrop.exe" "$rootDir\HushDrop.exe" -Force

# Linux amd64
Write-Host "   -> Building dist/hushdrop-linux-amd64 (Linux amd64)..." -ForegroundColor Yellow
$env:GOOS = "linux"
$env:GOARCH = "amd64"
go build -ldflags $ldflags -o "$distDir\hushdrop-linux-amd64" .\cmd\hushdrop

# macOS arm64
Write-Host "   -> Building dist/hushdrop-darwin-arm64 (macOS arm64)..." -ForegroundColor Yellow
$env:GOOS = "darwin"
$env:GOARCH = "arm64"
go build -ldflags $ldflags -o "$distDir\hushdrop-darwin-arm64" .\cmd\hushdrop

# macOS amd64
Write-Host "   -> Building dist/hushdrop-darwin-amd64 (macOS amd64)..." -ForegroundColor Yellow
$env:GOOS = "darwin"
$env:GOARCH = "amd64"
go build -ldflags $ldflags -o "$distDir\hushdrop-darwin-amd64" .\cmd\hushdrop

Write-Host "===> 4. Calculating SHA-256 checksums..." -ForegroundColor Cyan
$checksumFile = "$distDir\checksums.txt"
if (Test-Path $checksumFile) { Remove-Item $checksumFile -Force }

Get-ChildItem -Path $distDir -File | Where-Object { $_.Name -ne "checksums.txt" } | ForEach-Object {
    $hash = (Get-FileHash -Path $_.FullName -Algorithm SHA256).Hash.ToLower()
    "$hash  $($_.Name)" | Out-File -FilePath $checksumFile -Append -Encoding ascii
}

Write-Host "===> 5. Verifying artifact security (no keys / certs / logs / env)..." -ForegroundColor Cyan
$leakedFiles = Get-ChildItem -Path $distDir -Recurse -File -Include "*.key", "*.pem", "*.crt", "*.log", ".env*"
if ($leakedFiles.Count -gt 0) {
    Write-Error "CRITICAL: Private keys/logs detected in dist: $($leakedFiles -join ', ')"
    exit 1
}
Write-Host "   -> Artifact security check passed: no secrets or logs found." -ForegroundColor Green

Write-Host "===> Build finished successfully in ./dist/:" -ForegroundColor Green
Get-Content $checksumFile

# Cross-platform compilation script for HushDrop (PowerShell)
$ErrorActionPreference = "Stop"

Write-Host "===> 1. Сборка фронтенда React PWA..." -ForegroundColor Cyan
Set-Location -Path "$PSScriptRoot\..\frontend"
cmd.exe /c "npm run build"
Set-Location -Path "$PSScriptRoot\.."

Write-Host "===> 2. Синхронизация встроенных ресурсов..." -ForegroundColor Cyan
Copy-Item -Path "frontend\dist\*" -Destination "internal\server\web\" -Recurse -Force

Write-Host "===> 3. Компиляция статических бинарных файлов Go..." -ForegroundColor Cyan
$env:CGO_ENABLED = "0"
$version = "1.0.0"
$ldflags = "-s -w -X main.version=$version"

if (!(Test-Path "bin")) { New-Item -ItemType Directory -Path "bin" -Force }

# Windows x64
Write-Host "   -> Сборка HushDrop.exe (Windows amd64)..." -ForegroundColor Yellow
$env:GOOS = "windows"
$env:GOARCH = "amd64"
go build -ldflags $ldflags -o "bin\HushDrop.exe" .\cmd\hushdrop
Copy-Item "bin\HushDrop.exe" "HushDrop.exe" -Force

# Linux x64
Write-Host "   -> Сборка hushdrop-linux (Linux amd64)..." -ForegroundColor Yellow
$env:GOOS = "linux"
$env:GOARCH = "amd64"
go build -ldflags $ldflags -o "bin\hushdrop-linux-amd64" .\cmd\hushdrop

# macOS ARM64 & amd64
Write-Host "   -> Сборка hushdrop-darwin (macOS arm64)..." -ForegroundColor Yellow
$env:GOOS = "darwin"
$env:GOARCH = "arm64"
go build -ldflags $ldflags -o "bin\hushdrop-darwin-arm64" .\cmd\hushdrop

Write-Host "===> Сборка успешно завершена в ./bin/ и ./HushDrop.exe" -ForegroundColor Green

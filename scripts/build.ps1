# Cross-platform release compilation script for HushDrop (PowerShell)
$ErrorActionPreference = "Stop"

$rootDir = Resolve-Path "$PSScriptRoot\.."
Set-Location -Path $rootDir

Write-Host "===> 1. Сборка фронтенда React PWA..." -ForegroundColor Cyan
Set-Location -Path "$rootDir\frontend"
cmd.exe /c "npm install && npm run build"
Set-Location -Path $rootDir

Write-Host "===> 2. Синхронизация встроенных ресурсов в internal/server/web..." -ForegroundColor Cyan
if (!(Test-Path "$rootDir\internal\server\web")) {
    New-Item -ItemType Directory -Path "$rootDir\internal\server\web" -Force | Out-Null
}
Copy-Item -Path "$rootDir\frontend\dist\*" -Destination "$rootDir\internal\server\web\" -Recurse -Force

Write-Host "===> 3. Компиляция бинарных файлов Go в ./dist/..." -ForegroundColor Cyan
$distDir = "$rootDir\dist"
if (!(Test-Path $distDir)) {
    New-Item -ItemType Directory -Path $distDir -Force | Out-Null
}

$env:CGO_ENABLED = "0"
$version = "0.3.1"
$ldflags = "-s -w -X main.version=$version"

# Windows amd64
Write-Host "   -> Сборка dist/HushDrop.exe (Windows amd64)..." -ForegroundColor Yellow
$env:GOOS = "windows"
$env:GOARCH = "amd64"
go build -ldflags $ldflags -o "$distDir\HushDrop.exe" .\cmd\hushdrop
Copy-Item "$distDir\HushDrop.exe" "$rootDir\HushDrop.exe" -Force

# Linux amd64
Write-Host "   -> Сборка dist/hushdrop-linux-amd64 (Linux amd64)..." -ForegroundColor Yellow
$env:GOOS = "linux"
$env:GOARCH = "amd64"
go build -ldflags $ldflags -o "$distDir\hushdrop-linux-amd64" .\cmd\hushdrop

# macOS arm64
Write-Host "   -> Сборка dist/hushdrop-darwin-arm64 (macOS arm64)..." -ForegroundColor Yellow
$env:GOOS = "darwin"
$env:GOARCH = "arm64"
go build -ldflags $ldflags -o "$distDir\hushdrop-darwin-arm64" .\cmd\hushdrop

# macOS amd64
Write-Host "   -> Сборка dist/hushdrop-darwin-amd64 (macOS amd64)..." -ForegroundColor Yellow
$env:GOOS = "darwin"
$env:GOARCH = "amd64"
go build -ldflags $ldflags -o "$distDir\hushdrop-darwin-amd64" .\cmd\hushdrop

Write-Host "===> 4. Вычисление контрольных сумм SHA-256..." -ForegroundColor Cyan
$checksumFile = "$distDir\checksums.txt"
if (Test-Path $checksumFile) { Remove-Item $checksumFile -Force }

Get-ChildItem -Path $distDir -File | Where-Object { $_.Name -ne "checksums.txt" } | ForEach-Object {
    $hash = (Get-FileHash -Path $_.FullName -Algorithm SHA256).Hash.ToLower()
    "$hash  $($_.Name)" | Out-File -FilePath $checksumFile -Append -Encoding ascii
}

Write-Host "===> Сборка успешно завершена в ./dist/:" -ForegroundColor Green
Get-Content $checksumFile

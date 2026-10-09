#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

echo "===> 1. Сборка фронтенда React PWA..."
cd "$ROOT_DIR/frontend"
npm install
npm run build
cd "$ROOT_DIR"

echo "===> 2. Синхронизация встроенных ресурсов в internal/server/web..."
mkdir -p "$ROOT_DIR/internal/server/web"
cp -r "$ROOT_DIR/frontend/dist/"* "$ROOT_DIR/internal/server/web/"

echo "===> 3. Компиляция бинарных файлов Go в ./dist/..."
DIST_DIR="$ROOT_DIR/dist"
mkdir -p "$DIST_DIR"

export CGO_ENABLED=0
VERSION="0.3.4"
LDFLAGS="-s -w -X main.version=${VERSION}"

# Linux amd64
echo "   -> Сборка dist/hushdrop-linux-amd64..."
GOOS=linux GOARCH=amd64 go build -ldflags="${LDFLAGS}" -o "$DIST_DIR/hushdrop-linux-amd64" ./cmd/hushdrop

# Windows amd64 (console + GUI builds from the same code)
echo "   -> Сборка dist/HushDrop-console.exe (console)..."
GOOS=windows GOARCH=amd64 go build -ldflags="${LDFLAGS}" -o "$DIST_DIR/HushDrop-console.exe" ./cmd/hushdrop
echo "   -> Сборка dist/HushDrop.exe (GUI)..."
GOOS=windows GOARCH=amd64 go build -ldflags="${LDFLAGS} -H=windowsgui" -o "$DIST_DIR/HushDrop.exe" ./cmd/hushdrop

# macOS arm64
echo "   -> Сборка dist/hushdrop-darwin-arm64..."
GOOS=darwin GOARCH=arm64 go build -ldflags="${LDFLAGS}" -o "$DIST_DIR/hushdrop-darwin-arm64" ./cmd/hushdrop

# macOS amd64
echo "   -> Сборка dist/hushdrop-darwin-amd64..."
GOOS=darwin GOARCH=amd64 go build -ldflags="${LDFLAGS}" -o "$DIST_DIR/hushdrop-darwin-amd64" ./cmd/hushdrop

echo "===> 4. Вычисление контрольных сумм SHA-256..."
cd "$DIST_DIR"
if command -v sha256sum >/dev/null 2>&1; then
    sha256sum HushDrop.exe HushDrop-console.exe hushdrop-linux-amd64 hushdrop-darwin-arm64 hushdrop-darwin-amd64 > checksums.txt
elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 HushDrop.exe HushDrop-console.exe hushdrop-linux-amd64 hushdrop-darwin-arm64 hushdrop-darwin-amd64 > checksums.txt
fi

echo "===> Сборка успешно завершена в ./dist/!"
cat checksums.txt

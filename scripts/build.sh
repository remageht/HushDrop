#!/usr/bin/env bash
set -euo pipefail

echo "===> 1. Сборка фронтенда React PWA..."
cd "$(dirname "$0")/../frontend"
npm run build
cd ..

echo "===> 2. Синхронизация встроенных ресурсов..."
cp -r frontend/dist/* internal/server/web/

echo "===> 3. Компиляция Go..."
export CGO_ENABLED=0
VERSION="1.0.0"
LDFLAGS="-s -w -X main.version=${VERSION}"

mkdir -p bin

# Linux
echo "   -> Сборка для Linux..."
GOOS=linux GOARCH=amd64 go build -ldflags="${LDFLAGS}" -o bin/hushdrop-linux-amd64 ./cmd/hushdrop

# Windows
echo "   -> Сборка для Windows..."
GOOS=windows GOARCH=amd64 go build -ldflags="${LDFLAGS}" -o bin/HushDrop.exe ./cmd/hushdrop

# macOS
echo "   -> Сборка для macOS..."
GOOS=darwin GOARCH=arm64 go build -ldflags="${LDFLAGS}" -o bin/hushdrop-darwin-arm64 ./cmd/hushdrop

echo "===> Готово!"

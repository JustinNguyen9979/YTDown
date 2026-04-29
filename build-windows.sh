#!/bin/bash
set -e

APP=$(cat wails.json | python3 -c "import sys, json; print(json.load(sys.stdin)['name'])")

# Nhận VERSION từ CI env; fallback về get-version.sh khi chạy local
if [[ -z "$VERSION" || ! "$VERSION" =~ ^[0-9] ]]; then
  VERSION=$(bash "$(dirname "$0")/get-version.sh")
fi

echo "🔨 Building $APP $VERSION for Windows..."
mkdir -p dist
wails build -platform windows/amd64 -nsis -ldflags "-X main.Version=$VERSION"
cp "build/bin/$APP-amd64-installer.exe" "dist/$APP-$VERSION-Windows-Setup.exe"
echo "✅ dist/$APP-$VERSION-Windows-Setup.exe"
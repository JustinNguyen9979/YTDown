#!/bin/bash
set -e
APP=$(cat wails.json | python3 -c "import sys, json; print(json.load(sys.stdin)['name'])")

if [[ -z "$VERSION" || ! "$VERSION" =~ ^[0-9] ]]; then
  VERSION=$(bash "$(dirname "$0")/get-version.sh")
fi

echo "🔨 Building $APP $VERSION for Windows..."
mkdir -p dist

wails build -platform windows/amd64 -nsis -ldflags "-s -w -X main.Version=$VERSION"

EXE_NAME="$APP-$VERSION-Windows-Setup.exe"
ZIP_NAME="$APP-$VERSION-Windows-Setup.zip"

cp "build/bin/${APP}-amd64-installer.exe" "dist/$EXE_NAME"

# Dùng PowerShell thay vì zip (Windows runner không có zip command)
powershell -Command "Compress-Archive -Path 'dist\\$EXE_NAME' -DestinationPath 'dist\\$ZIP_NAME' -Force"

echo "✅ dist/$EXE_NAME"
echo "✅ dist/$ZIP_NAME"
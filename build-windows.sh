#!/bin/bash
set -e

APP=$(cat wails.json | python3 -c "import sys, json; print(json.load(sys.stdin)['name'])")
VERSION=$(git describe --tags --abbrev=0 2>/dev/null || echo "$(date +%Y.%-m.%-d)")

echo "🔨 Building $APP $VERSION for Windows..."

mkdir -p dist

wails build -platform windows/amd64 -nsis -ldflags "-X main.Version=$VERSION"

cp build/bin/$APP-amd64-installer.exe dist/$APP-$VERSION-Windows-Setup.exe

echo "✅ dist/$APP-$VERSION-Windows-Setup.exe"
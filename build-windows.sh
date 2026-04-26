#!/usr/bin/env bash
set -e
VERSION=${VERSION:-$(date +%Y.%-m.%-d)}
mkdir -p dist
wails build -platform windows/amd64 -nsis \
  -ldflags "-s -w -X main.version=${VERSION}"
  
cp build/bin/YTDown-amd64-installer.exe dist/YTDown-$VERSION-Windows-Setup.exe

echo "✅ dist/YTDown-$VERSION-Windows-Setup.exe"

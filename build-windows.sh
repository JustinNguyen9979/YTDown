#!/bin/bash
set -e
APP=$(cat wails.json | python3 -c "import sys, json; print(json.load(sys.stdin)['name'])")

if [[ -z "$VERSION" || ! "$VERSION" =~ ^[0-9] ]]; then
  VERSION=$(bash "$(dirname "$0")/get-version.sh")
fi

echo "🔨 Building $APP $VERSION for Windows..."
mkdir -p dist

wails build -platform windows/amd64 -nsis -ldflags "-X main.Version=$VERSION"

EXE_NAME="$APP-$VERSION-Windows-Setup.exe"
ZIP_NAME="$APP-$VERSION-Windows-Setup.zip"

cp "build/bin/$APP-amd64-installer.exe" "dist/$EXE_NAME"

# Đóng gói .exe vào .zip cùng tên
cd dist
zip "$ZIP_NAME" "$EXE_NAME"
cd ..

echo "✅ dist/$EXE_NAME"
echo "✅ dist/$ZIP_NAME"
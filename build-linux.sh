#!/bin/bash
set -e

APP=$(cat wails.json | python3 -c "import sys, json; print(json.load(sys.stdin)['name'])")
VERSION=$(git describe --tags --abbrev=0 2>/dev/null || echo "$(date +%Y.%-m.%-d)")

echo "🔨 Building $APP $VERSION for Linux..."

mkdir -p dist

wails build -platform linux/amd64 -ldflags "-X main.Version=$VERSION"

echo "📦 Packaging AppImage..."

cp -r build/appimage/AppDir .
cp build/bin/$APP AppDir/usr/bin/$APP

sed -i "s/APP_NAME/$APP/g" AppDir/$APP.desktop
cp AppDir/usr/share/icons/hicolor/256x256/apps/$APP.png AppDir/$APP.png

wget -q "https://github.com/AppImage/appimagetool/releases/download/continuous/appimagetool-x86_64.AppImage" -O appimagetool
chmod +x appimagetool

ARCH=x86_64 ./appimagetool --appimage-extract-and-run AppDir dist/$APP-$VERSION-Linux.AppImage

rm -rf AppDir appimagetool

echo "✅ dist/$APP-$VERSION-Linux.AppImage"
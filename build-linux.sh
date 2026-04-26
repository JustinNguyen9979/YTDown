#!/usr/bin/env bash
set -e

# ✅ FIX 1: Lấy version từ git tag (bỏ ${VERSION:-...})
VERSION=$(git describe --tags --abbrev=0 2>/dev/null || echo "$(date +%Y.%-m.%-d)")
APP=YTDown

mkdir -p dist

# ✅ FIX 2: -X main.Version (V hoa)
wails build -platform linux/amd64 \
  -ldflags "-s -w -X main.Version=${VERSION}"

# Tạo AppDir layout (GIỮ NGUYÊN gốc)
rm -rf AppDir
mkdir -p AppDir/usr/bin AppDir/usr/share/icons/hicolor/256x256/apps

cp build/bin/$APP AppDir/usr/bin/$APP

[ -f build/linux/icon.png ] && \
  cp build/linux/icon.png AppDir/usr/share/icons/hicolor/256x256/apps/$APP.png && \
  cp build/linux/icon.png AppDir/$APP.png

cat > AppDir/$APP.desktop << EOF
[Desktop Entry]
Name=$APP
Exec=$APP
Icon=$APP
Type=Application
Categories=AudioVideo;Network;
EOF

cat > AppDir/AppRun << 'EOF'
#!/bin/sh
HERE="$(dirname "$(readlink -f "$0")")"
exec "$HERE/usr/bin/YTDown" "$@"
EOF

chmod +x AppDir/AppRun

# Tải appimagetool và đóng gói (GIỮ NGUYÊN URL gốc)
wget -q -O appimagetool \
  https://github.com/AppImage/AppImageKit/releases/download/continuous/appimagetool-x86_64.AppImage

chmod +x appimagetool

# ✅ FIX 3: Thêm $VERSION vào tên file
ARCH=x86_64 ./appimagetool --appimage-extract-and-run AppDir dist/$APP-$VERSION-Linux.AppImage

echo "✅ dist/$APP-$VERSION-Linux.AppImage"
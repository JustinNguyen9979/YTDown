#!/usr/bin/env bash
set -e

VERSION=${VERSION:-$(git describe --tags --abbrev=0 2>/dev/null || echo "$(date +%Y.%-m.%-d)")}
APP=YTDown
PKG=ytdown

mkdir -p dist

# Detect kiến trúc của runner tự động
HOST_ARCH=$(uname -m)
case "$HOST_ARCH" in
  x86_64)  WAILS_ARCH="linux/amd64" ; DEB_ARCH="amd64" ;;
  aarch64) WAILS_ARCH="linux/arm64" ; DEB_ARCH="arm64" ;;
  arm64)   WAILS_ARCH="linux/arm64" ; DEB_ARCH="arm64" ;;
  *)
    echo "⚠️  Kiến trúc không xác định: $HOST_ARCH — mặc định amd64"
    WAILS_ARCH="linux/amd64"
    DEB_ARCH="amd64"
    ;;
esac

echo "══════════════════════════════════════════"
echo " Building $WAILS_ARCH → ${PKG}_${VERSION}_${DEB_ARCH}.deb"
echo "══════════════════════════════════════════"

# ── Build binary ──────────────────────────────────────────────
wails build -platform "$WAILS_ARCH" \
  -tags webkit2_41 \
  -ldflags "-s -w -X main.Version=${VERSION}"

# ── Tạo cấu trúc thư mục .deb ────────────────────────────────
DEB_DIR="dist/${PKG}_${VERSION}_${DEB_ARCH}"
rm -rf "$DEB_DIR"
mkdir -p \
  "$DEB_DIR/DEBIAN" \
  "$DEB_DIR/usr/bin" \
  "$DEB_DIR/usr/share/applications" \
  "$DEB_DIR/usr/share/icons/hicolor/256x256/apps"

# Copy binary
cp "build/bin/$APP" "$DEB_DIR/usr/bin/$PKG"
chmod +x "$DEB_DIR/usr/bin/$PKG"

# Copy icon (nếu có)
[ -f build/linux/icon.png ] && \
  cp build/linux/icon.png \
     "$DEB_DIR/usr/share/icons/hicolor/256x256/apps/${PKG}.png"

# ── File .desktop ─────────────────────────────────────────────
cat > "$DEB_DIR/usr/share/applications/${PKG}.desktop" << DESKTOP
[Desktop Entry]
Name=YTDown
Comment=Tải video từ YouTube, TikTok, Facebook và hàng trăm nền tảng khác
Exec=$PKG
Icon=$PKG
Type=Application
Categories=AudioVideo;Network;
Terminal=false
DESKTOP

# ── DEBIAN/control ────────────────────────────────────────────
# Khai báo Depends → apt tự động cài ffmpeg, yt-dlp, gallery-dl
cat > "$DEB_DIR/DEBIAN/control" << CONTROL
Package: $PKG
Version: $VERSION
Architecture: $DEB_ARCH
Maintainer: Justin Nguyen <justinnguyen9979@github.com>
Homepage: https://github.com/JustinNguyen9979/YTDown
Description: YTDown - Video & Media Downloader
 Tải video từ YouTube, TikTok, Facebook và hàng trăm nền tảng khác.
 Hỗ trợ batch download, cookie, playlist.
Depends: ffmpeg, yt-dlp, gallery-dl, libwebkit2gtk-4.1-0, libgtk-3-0
CONTROL

# ── DEBIAN/postinst ───────────────────────────────────────────
cat > "$DEB_DIR/DEBIAN/postinst" << 'POSTINST'
#!/bin/sh
set -e
echo ""
echo "✅ YTDown đã được cài đặt thành công!"
echo "   Các module (ffmpeg, yt-dlp, gallery-dl) đã được apt cài tự động."
echo "   Chạy: ytdown"
echo ""
POSTINST

chmod 755 "$DEB_DIR/DEBIAN/postinst"

# ── Build .deb ────────────────────────────────────────────────
dpkg-deb --build --root-owner-group \
  "$DEB_DIR" \
  "dist/${PKG}_${VERSION}_${DEB_ARCH}.deb"

echo "✅ dist/${PKG}_${VERSION}_${DEB_ARCH}.deb"
rm -rf "$DEB_DIR"
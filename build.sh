#!/bin/bash

# Build and package YTDown for distribution

set -e

VERSION="1.0.0"
OUTPUT_DIR="dist"
APP_NAME="YTDown"
APP_BUNDLE="build/bin/$APP_NAME.app"
RESOURCES_DIR="$APP_BUNDLE/Contents/Resources"
DMG_STAGING_DIR="$OUTPUT_DIR/dmg-staging"

echo "🏗️  Building $APP_NAME v$VERSION"
echo "=================================="

# Clean previous builds
rm -rf "$APP_BUNDLE" dist/

# Build for macOS (universal binary)
echo "📦 Building universal binary (Apple Silicon + Intel)..."
wails build -platform darwin -tags universal \
    -o "$APP_NAME" \
    -nsis=false

echo "📦 Bundling ffmpeg..."
bash scripts/bundle-binaries.sh "$APP_BUNDLE"

echo "🔏 Re-signing app bundle..."
codesign --force --deep --sign - "$APP_BUNDLE"

# Create distribution directory
mkdir -p "$OUTPUT_DIR"

# Copy app to dist
echo "📋 Organizing files..."
cp -r "$APP_BUNDLE" "$OUTPUT_DIR/"

# Create DMG (optional - requires hdiutil)
if command -v hdiutil &> /dev/null; then
    echo "💾 Creating DMG..."
    rm -rf "$DMG_STAGING_DIR"
    mkdir -p "$DMG_STAGING_DIR"
    cp -R "$OUTPUT_DIR/$APP_NAME.app" "$DMG_STAGING_DIR/"
    ln -s /Applications "$DMG_STAGING_DIR/Applications"
    hdiutil create -volname "$APP_NAME" \
        -srcfolder "$DMG_STAGING_DIR" \
        -ov -format UDZO \
        "dist/$APP_NAME-$VERSION.dmg"
    rm -rf "$DMG_STAGING_DIR"
fi

echo ""
echo "✅ Build complete!"
echo "   App: $OUTPUT_DIR/$APP_NAME.app"
if [ -f "dist/$APP_NAME-$VERSION.dmg" ]; then
    echo "   DMG: dist/$APP_NAME-$VERSION.dmg"
fi

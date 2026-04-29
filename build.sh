#!/bin/bash

# Build and package YTDown for distribution

set -e

if [[ -z "$VERSION" || ! "$VERSION" =~ ^[0-9] ]]; then
  VERSION=$(bash "$(dirname "$0")/get-version.sh")
fi
YEAR=$(date +"%Y")

OUTPUT_DIR="dist"
APP_NAME="YTDown"
APP_BUNDLE="build/bin/$APP_NAME.app"
RESOURCES_DIR="$APP_BUNDLE/Contents/Resources"
DMG_STAGING_DIR="$OUTPUT_DIR/dmg-staging"

echo "🏗️  Building $APP_NAME v$VERSION"
echo "=================================="

# Update wails.json with the new version and year
if command -v jq &> /dev/null; then
    echo "📝 Updating wails.json version to $VERSION..."
    # Update productversion and copyright year
    tmp=$(mktemp)
    jq ".info.productversion = \"$VERSION\" | .info.copyright = \"Copyright © $YEAR\"" wails.json > "$tmp" && mv "$tmp" wails.json
else
    echo "⚠️  jq not found, skipping wails.json auto-update."
fi

# Clean previous builds
rm -rf "$APP_BUNDLE" dist/

# Build for macOS (universal binary)
echo "📦 Building universal binary (Apple Silicon + Intel)..."
wails build -platform darwin/universal \
    -ldflags "-s -w -X main.Version=${VERSION}" \
    -o "$APP_NAME" \
    -nsis=false

echo "⚠️  Note: Dependencies (ffmpeg, yt-dlp, gallery-dl) should be installed via Homebrew"
echo "   Users will be prompted to install them on first launch."

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

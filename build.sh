#!/bin/bash

# Build and package YTDown for distribution

set -e

VERSION="1.0.0"
OUTPUT_DIR="dist"
APP_NAME="YTDown"

echo "🏗️  Building $APP_NAME v$VERSION"
echo "=================================="

# Clean previous builds
rm -rf build/bin/$APP_NAME.app dist/

# Build for macOS (universal binary)
echo "📦 Building universal binary (Apple Silicon + Intel)..."
wails build -platform darwin -tags universal \
    -o "$APP_NAME" \
    -nsis=false

# Create distribution directory
mkdir -p $OUTPUT_DIR

# Copy app to dist
echo "📋 Organizing files..."
cp -r "build/bin/$APP_NAME.app" "$OUTPUT_DIR/"

# Create DMG (optional - requires hdiutil)
if command -v hdiutil &> /dev/null; then
    echo "💾 Creating DMG..."
    hdiutil create -volname "$APP_NAME" \
        -srcfolder "$OUTPUT_DIR" \
        -ov -format UDZO \
        "dist/$APP_NAME-$VERSION.dmg"
fi

echo ""
echo "✅ Build complete!"
echo "   App: $OUTPUT_DIR/$APP_NAME.app"
if [ -f "dist/$APP_NAME-$VERSION.dmg" ]; then
    echo "   DMG: dist/$APP_NAME-$VERSION.dmg"
fi

#!/bin/bash

set -euo pipefail

APP_BUNDLE="${1:-build/bin/YTDown.app}"
RESOURCES_DIR="$APP_BUNDLE/Contents/Resources"

if [ ! -d "$APP_BUNDLE" ]; then
    echo "❌ App bundle not found: $APP_BUNDLE"
    exit 1
fi

mkdir -p "$RESOURCES_DIR"

resolve_binary() {
    local env_name="$1"
    local binary_name="$2"
    local configured_path="${!env_name:-}"

    if [ -n "$configured_path" ] && [ -f "$configured_path" ]; then
        echo "$configured_path"
        return 0
    fi

    if command -v "$binary_name" >/dev/null 2>&1; then
        command -v "$binary_name"
        return 0
    fi

    return 1
}

copy_binary() {
    local source_path="$1"
    local target_name="$2"
    local target_path="$RESOURCES_DIR/$target_name"

    cp "$source_path" "$target_path"
    chmod 755 "$target_path"
    echo "   Added $target_name from $source_path"
}

echo "Preparing bundled binaries in $RESOURCES_DIR"

FFMPEG_PATH="$(resolve_binary FFMPEG_PATH ffmpeg || true)"

if [ -z "$FFMPEG_PATH" ]; then
    echo "❌ ffmpeg not found. Install it or set FFMPEG_PATH."
    exit 1
fi

copy_binary "$FFMPEG_PATH" "ffmpeg"

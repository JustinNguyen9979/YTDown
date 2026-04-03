#!/bin/bash

# YTDown Setup Script
# This script prepares the development environment

set -e

echo "🚀 YTDown Development Setup"
echo "=============================="

# Check Go installation
if ! command -v go &> /dev/null; then
    echo "❌ Go is not installed. Please install Go 1.21 or higher."
    exit 1
fi
echo "✅ Go $(go version | awk '{print $3}')"

# Check Wails installation
if ! command -v wails &> /dev/null; then
    echo "📦 Installing Wails..."
    go install github.com/wailsapp/wails/v2/cmd/wails@latest
fi
echo "✅ Wails installed"

# Download Go dependencies
echo "📦 Downloading Go modules..."
go mod download
go mod tidy

# Check for yt-dlp
if command -v yt-dlp &> /dev/null; then
    echo "✅ yt-dlp found: $(which yt-dlp)"
else
    echo "⚠️  yt-dlp not found"
    echo "   Install via: brew install yt-dlp"
    echo "   Or download from: https://github.com/yt-dlp/yt-dlp/releases"
fi

# Check for ffmpeg
if command -v ffmpeg &> /dev/null; then
    echo "✅ ffmpeg found: $(which ffmpeg)"
else
    echo "⚠️  ffmpeg not found"
    echo "   Install via: brew install ffmpeg"
fi

echo ""
echo "════════════════════════════════════════"
echo "✅ Setup complete!"
echo ""
echo "To start development:"
echo "  wails dev"
echo ""
echo "To build production app:"
echo "  wails build -platform darwin"
echo ""
echo "For more information, see README.md"

# Development Guide for YTDown

## Quick Start

### 1. First-Time Setup

```bash
# Clone or navigate to the project
cd YTDown

# Run setup script
bash setup.sh

# Or using make
make setup
```

### 2. Install Dependencies

```bash
# Download Go modules
go mod download
go mod tidy

# Or using make
make install
```

### 3. Development Workflow

```bash
# Start Wails dev server (hot reload on file changes)
wails dev
# Or: make dev

# This opens the app and watches for changes
# - Go changes require restart
# - Frontend changes auto-reload
```

### 4. Build for Production

```bash
# Build native .app bundle
wails build -platform darwin

# Build universal binary (Apple Silicon + Intel)
wails build -platform darwin -tags universal

# Or using make
make build
```

## Project Architecture

### Backend Structure (Go)

| File | Purpose |
|------|---------|
| `main.go` | Wails entry point, window configuration |
| `app.go` | Application struct, lifecycle hooks, public API exposed to JS |
| `downloader.go` | Core download logic, progress parsing, binary detection |

### Frontend Structure

| File | Purpose |
|------|---------|
| `index.html` | UI markup with tabs, inputs, progress areas |
| `style.css` | Glassmorphism design, dark/light mode support |
| `main.js` | UI logic, event listeners, Wails bindings to Go |

## Key Development Concepts

### Wails Event System

Go can emit events to JavaScript:

```go
// In Go backend
runtime.EventsEmit(ctx, "event-name", data)

// In JavaScript
window.runtime.EventsOn("event-name", (data) => {
    // Handle event
});
```

### Go-to-JavaScript Bindings

Methods on the `App` struct are automatically exposed to JS:

```go
// In Go (app.go)
func (a *App) MyFunction(param string) string {
    return "result: " + param
}

// In JavaScript (main.js)
const result = await window.app.MyFunction("test");
```

## Common Development Tasks

### Adding a New Feature

1. **Add Go function** in `app.go` or create new file
   ```go
   func (a *App) NewFeature(params ...) ReturnType {
       // Implementation
   }
   ```

2. **Emit progress/results** via `runtime.EventsEmit`
   ```go
   runtime.EventsEmit(ctx, "feature-update", data)
   ```

3. **Update frontend** to call the function
   ```javascript
   window.runtime.Call.NewFeature(params).then(result => {
       // Handle result
   });
   ```

4. **Listen for events**
   ```javascript
   window.runtime.EventsOn("feature-update", (data) => {
       // Update UI
   });
   ```

### Styling Updates

All CSS uses CSS variables for theming:

```css
/* Dark mode (default) */
:root {
    --bg-primary: #1b1b1b;
    --text-primary: #ffffff;
    /* ... more vars */
}

/* Light mode */
@media (prefers-color-scheme: light) {
    :root {
        --bg-primary: #f5f5f7;
        --text-primary: #1d1d1d;
    }
}
```

Edit `frontend/style.css` to modify colors and appearance.

### Adding New Formats

Edit `downloader.go` `buildDownloadArgs()`:

```go
case "WEBM":
    args = append(args, 
        "-f", "best[ext=webm]",
        "--merge-output-format", "webm")
```

Then add option to HTML dropdowns.

## Troubleshooting

### Issue: "yt-dlp not found" error

**Solution:**
```bash
# Option 1: Install via Homebrew
brew install yt-dlp

# Option 2: Download binary manually
# https://github.com/yt-dlp/yt-dlp/releases
# Place in: resources/yt-dlp
```

### Issue: Hot reload not working

**Solution:**
- Make sure you're running `wails dev`
- Check **JavaScript** changes reload automatically
- Go changes require restarting `wails dev`

### Issue: Build fails with "no such file"

**Solution:**
```bash
make clean
go mod tidy
wails dev
```

### Issue: On M1/M2 Mac, can't run downloaded app

**Solution:**
```bash
# Build universal binary
wails build -platform darwin -tags universal

# Or explicitly for arm64
wails build -platform darwin
```

## Configuration Files Guide

### `wails.json`
- Main Wails configuration
- Window size, app metadata, build settings
- Modify `build.mac` for macOS-specific settings

### `build/darwin/Info.plist`
- macOS app metadata
- Bundle ID, icon, version info
- Identifies app to the system

### `build/darwin/entitlements.plist`
- macOS security & capability settings
- Disabled sandbox (needed for subprocess)
- Allows file/network access

### `.gitignore`
- Prevents build artifacts from being committed
- Ignore `build/bin/`, `dist/`, `node_modules/`, `.app` files

## Performance Optimization

### Download Speed
- Uses `--concurrent-fragments` set to CPU count
- Parallelizes segment downloads automatically

### Memory Usage
- Streams output instead of buffering
- Progress updates via events (not polling)

### UI Responsiveness
- Background workers for downloads (goroutines)
- Non-blocking event emissions

## Testing Locally

### Simulation Test Videos:
```
https://www.youtube.com/watch?v=dQw4w9WgXcQ (short)
https://www.youtube.com/watch?v=9bZkp7q19f0 (music video)
```

### Playlist Test:
```
https://www.youtube.com/playlist?list=PLrAXtmErZgOeiKm4sgNOknGvNjby9efdf
```

## Build & Distribution

### Create DMG for Distribution:

```bash
make build-dmg
# Creates: dist/YTDown-1.0.0.dmg
```

### Codesign for Release (optional):

```bash
codesign -s - dist/YTDown.app
```

## Dependencies

**Runtime:**
- yt-dlp (bundled)
- ffmpeg (bundled or from Homebrew)

**Development:**
- Go 1.21+
- Wails v2.5+
- Node.js (if you want frontend build tools)

## Further Reading

- [Wails Documentation](https://wails.io/docs/)
- [Go Documentation](https://golang.org/doc/)
- [yt-dlp Usage](https://github.com/yt-dlp/yt-dlp)
- [macOS App Distribution](https://developer.apple.com/distribution/)

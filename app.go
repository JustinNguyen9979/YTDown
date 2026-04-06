package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App struct
type App struct {
	ctx          context.Context
	config       *Config
	batchMu      sync.Mutex
	currentBatch *BatchDownloadState
}

type BatchDownloadState struct {
	URLs               []string
	Format             string
	Quality            string
	SavePath           string
	RestrictedFailures map[int]RestrictedFailure
}

type RestrictedFailure struct {
	URL       string
	LastError string
}

// BinaryVersion struct
type BinaryVersion struct {
	Name       string `json:"name"`
	Current    string `json:"current"`
	Latest     string `json:"latest"`
	CanUpgrade bool   `json:"canUpgrade"`
	UpdatePath string `json:"updatePath"`
}

// Config struct for storing settings
type Config struct {
	SavePath string `json:"savePath"`
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{}
}

// GetVersionStatus returns version info for yt-dlp and ffmpeg
func (a *App) GetVersionStatus() []BinaryVersion {
	var versions []BinaryVersion

	// Check yt-dlp
	ytdlpPath := getResourcePath("yt-dlp")
	if ytdlpPath != "" {
		current := ""
		cmd := exec.Command(ytdlpPath, "--version")
		if out, err := cmd.Output(); err == nil {
			current = strings.TrimSpace(string(out))
		}

		latest := current
		// Fetch latest from GitHub
		client := &http.Client{Timeout: 5 * time.Second}
		if resp, err := client.Get("https://api.github.com/repos/yt-dlp/yt-dlp/releases/latest"); err == nil {
			defer resp.Body.Close()
			var data struct {
				TagName string `json:"tag_name"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&data); err == nil {
				latest = data.TagName
			}
		}

		versions = append(versions, BinaryVersion{
			Name:       "yt-dlp",
			Current:    current,
			Latest:     latest,
			CanUpgrade: current != "" && latest != "" && current != latest,
			UpdatePath: "https://github.com/yt-dlp/yt-dlp/releases/latest",
		})
	}

	// Check ffmpeg
	ffmpegPath := getResourcePath("ffmpeg")
	if ffmpegPath != "" {
		current := ""
		cmd := exec.Command(ffmpegPath, "-version")
		if out, err := cmd.Output(); err == nil {
			lines := strings.Split(string(out), "\n")
			if len(lines) > 0 {
				// Parse "ffmpeg version 6.0 Copyright..."
				parts := strings.Fields(lines[0])
				if len(parts) >= 3 && parts[0] == "ffmpeg" && parts[1] == "version" {
					current = parts[2]
				} else {
					current = lines[0]
				}
			}
		}

		versions = append(versions, BinaryVersion{
			Name:       "ffmpeg",
			Current:    current,
			Latest:     current, // ffmpeg doesn't have a simple latest check here
			CanUpgrade: false,
		})
	}

	return versions
}

// UpgradeBinary attempts to upgrade a binary
func (a *App) UpgradeBinary(name string) error {
	if name != "yt-dlp" {
		return fmt.Errorf("upgrade not supported for %s", name)
	}

	ytdlpPath := getResourcePath("yt-dlp")
	if ytdlpPath == "" {
		return fmt.Errorf("yt-dlp not found")
	}

	runtime.EventsEmit(a.ctx, "upgrade-status", "Upgrading yt-dlp via self-update...")

	// Try self-update first
	cmd := exec.Command(ytdlpPath, "-U")
	if output, err := cmd.CombinedOutput(); err == nil {
		runtime.EventsEmit(a.ctx, "upgrade-status", "yt-dlp upgraded successfully.")
		return nil
	} else {
		fmt.Printf("yt-dlp -U failed: %v\nOutput: %s\n", err, string(output))
	}

	// Fallback to brew
	brewPath, _ := exec.LookPath("brew")
	if brewPath == "" {
		// Try common paths as fallback
		for _, p := range []string{"/opt/homebrew/bin/brew", "/usr/local/bin/brew"} {
			if _, err := os.Stat(p); err == nil {
				brewPath = p
				break
			}
		}
	}

	if brewPath != "" {
		runtime.EventsEmit(a.ctx, "upgrade-status", "Self-update failed. Trying Homebrew...")
		cmd = exec.Command(brewPath, "upgrade", "yt-dlp")
		if output, err := cmd.CombinedOutput(); err == nil {
			runtime.EventsEmit(a.ctx, "upgrade-status", "yt-dlp upgraded via Homebrew.")
			return nil
		} else {
			return fmt.Errorf("failed to upgrade yt-dlp: %s", string(output))
		}
	}

	return fmt.Errorf("failed to upgrade yt-dlp and Homebrew not found")
}

// LaunchSetupTerminal creates and runs a setup script in a new Terminal window
func (a *App) LaunchSetupTerminal() error {
	usr, _ := user.Current()
	setupScriptPath := filepath.Join(usr.HomeDir, ".config", "ytdown", "setup_env.sh")
	os.MkdirAll(filepath.Dir(setupScriptPath), 0755)

	scriptContent := `#!/bin/bash
set -e
echo "=========================================="
echo "   YTDown Environment Setup"
echo "=========================================="

# Check Homebrew
if ! command -v brew &> /dev/null && [ ! -f "/opt/homebrew/bin/brew" ] && [ ! -f "/usr/local/bin/brew" ]; then
    echo "📦 Homebrew not found. Installing..."
    /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
fi

# Setup Homebrew PATH for current session
if [ -f "/opt/homebrew/bin/brew" ]; then
    eval "$(/opt/homebrew/bin/brew shellenv)"
    BREW_PATH="/opt/homebrew/bin/brew"
elif [ -f "/usr/local/bin/brew" ]; then
    eval "$(/usr/local/bin/brew shellenv)"
    BREW_PATH="/usr/local/bin/brew"
else
    echo "❌ Error: Homebrew installation failed or not found."
    read -p "Press Enter to exit..."
    exit 1
fi

# Function to setup shell profile
setup_shell() {
    local profile=$1
    local cmd=$2
    if [ -f "$profile" ] || [ "$profile" == "$HOME/.zprofile" ]; then
        if ! grep -qs "homebrew shellenv" "$profile"; then
            echo "" >> "$profile"
            echo "$cmd" >> "$profile"
            echo "✅ Added Homebrew to $profile"
        fi
    fi
}

if [[ $(uname -m) == "arm64" ]]; then
    LINE='eval "$(/opt/homebrew/bin/brew shellenv)"'
else
    LINE='eval "$(/usr/local/bin/brew shellenv)"'
fi

setup_shell "$HOME/.zprofile" "$LINE"
setup_shell "$HOME/.zshrc" "$LINE"
setup_shell "$HOME/.bash_profile" "$LINE"

echo "📦 Installing/Updating yt-dlp and ffmpeg..."
$BREW_PATH install yt-dlp ffmpeg

echo ""
echo "✅ SETUP COMPLETE!"
echo "------------------------------------------"
echo "1. yt-dlp and ffmpeg are now installed."
echo "2. Homebrew PATH has been added to your shell profiles."
echo ""
echo "👉 THIS WINDOW WILL CLOSE IN 5 SECONDS."
echo "------------------------------------------"
sleep 5
exit
`

	err := os.WriteFile(setupScriptPath, []byte(scriptContent), 0755)
	if err != nil {
		return err
	}

	// Use osascript to open Terminal, run the script, and then close the window
	appleScript := fmt.Sprintf("tell application \"Terminal\" to do script \"/bin/bash %s; exit\"", setupScriptPath)
	cmd := exec.Command("osascript", "-e", appleScript)
	return cmd.Run()
}

// startup is called at application startup
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.loadConfig()

	// Check binaries after a short delay
	go func() {
		time.Sleep(1 * time.Second)
		status := a.CheckBinaries()
		if !status["ytdlp"].(bool) || !status["ffmpeg"].(bool) {
			// Emit event to frontend - the frontend should show a setup button/modal
			runtime.EventsEmit(ctx, "binary-warning", "yt-dlp or ffmpeg is missing.")
		}
	}()
}

// CheckBinaries checks if yt-dlp and ffmpeg are installed
func (a *App) CheckBinaries() map[string]interface{} {
	ytdlpPath := getResourcePath("yt-dlp")
	ffmpegPath := getResourcePath("ffmpeg")

	return map[string]interface{}{
		"ytdlp":  ytdlpPath != "",
		"ffmpeg": ffmpegPath != "",
	}
}

// shutdown is called at application termination
func (a *App) shutdown(ctx context.Context) {
	clearTemporaryYouTubeCookie()
	a.saveConfig()
}

// loadConfig loads configuration from file
func (a *App) loadConfig() {
	usr, _ := user.Current()
	configDir := filepath.Join(usr.HomeDir, ".config", "ytdown")
	configPath := filepath.Join(configDir, "config.json")

	a.config = &Config{
		SavePath: filepath.Join(usr.HomeDir, "Downloads"),
	}

	if data, err := ioutil.ReadFile(configPath); err == nil {
		json.Unmarshal(data, a.config)
	}
}

// saveConfig saves configuration to file
func (a *App) saveConfig() {
	usr, _ := user.Current()
	configDir := filepath.Join(usr.HomeDir, ".config", "ytdown")
	configPath := filepath.Join(configDir, "config.json")

	os.MkdirAll(configDir, 0755)
	if data, err := json.MarshalIndent(a.config, "", "  "); err == nil {
		ioutil.WriteFile(configPath, data, 0644)
	}
}

// OpenFolderDialog opens native folder picker
func (a *App) OpenFolderDialog() string {
	println("[DEBUG] OpenFolderDialog called")
	dir, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select Save Folder",
	})
	if err != nil {
		println("[ERROR] OpenDirectoryDialog:", err.Error())
		return a.config.SavePath
	}
	println("[DEBUG] Folder selected:", dir)
	a.config.SavePath = dir
	return dir
}

// GetVideoTitle fetches video title using yt-dlp
func (a *App) GetVideoTitle(url string) string {
	title, err := GetVideoMetadata(url)
	if err != nil {
		return ""
	}
	return title
}

// StartDownload starts downloading a single video
func (a *App) StartDownload(url, format, quality, savePath string) string {
	if strings.TrimSpace(url) == "" {
		return "Error: URL is empty"
	}

	println("[DEBUG] StartDownload called:", url, format, quality, savePath)

	go func() {
		println("[DEBUG] Download goroutine started")
		err := DownloadVideo(a.ctx, -1, url, format, quality, savePath)
		if err != nil {
			println("[ERROR]", err.Error())
			runtime.EventsEmit(a.ctx, "download-error", err.Error())
		} else {
			println("[SUCCESS] Download complete")
			runtime.EventsEmit(a.ctx, "download-complete", savePath)
		}
	}()

	return "Download started"
}

// StartBatchDownload starts batch downloading in parallel
func (a *App) StartBatchDownload(urls []string, format, quality, savePath string) string {
	if len(urls) == 0 {
		return "Error: No URLs provided"
	}

	a.batchMu.Lock()
	a.currentBatch = &BatchDownloadState{
		URLs:               append([]string(nil), urls...),
		Format:             format,
		Quality:            quality,
		SavePath:           savePath,
		RestrictedFailures: make(map[int]RestrictedFailure),
	}
	a.batchMu.Unlock()

	go func() {
		results := make(map[string]bool)
		var mu sync.Mutex
		var wg sync.WaitGroup
		sem := make(chan struct{}, 3) // Giới hạn 3 video tải cùng lúc (Parallel)

		for i, url := range urls {
			url = strings.TrimSpace(url)
			if url == "" {
				continue
			}

			wg.Add(1)
			go func(i int, url string) {
				defer wg.Done()
				sem <- struct{}{}        // Chiếm chỗ (Acquire semaphore)
				defer func() { <-sem }() // Nhả chỗ sau khi xong (Release semaphore)

				runtime.EventsEmit(a.ctx, "batch-status", map[string]interface{}{
					"index":  i,
					"status": "downloading",
				})

				err := DownloadVideo(a.ctx, i, url, format, quality, savePath)

				mu.Lock()
				results[url] = err == nil
				mu.Unlock()

				if err != nil {
					failure := classifyDownloadFailure(err, hasTemporaryCookie())
					if failure.RequiresCookie {
						a.trackRestrictedFailure(i, url, err.Error())
					}

					runtime.EventsEmit(a.ctx, "batch-error", map[string]interface{}{
						"index":          i,
						"error":          err.Error(),
						"displayMessage": failure.DisplayMessage,
						"details":        failure.Details,
						"requiresCookie": failure.RequiresCookie,
					})
				} else {
					a.clearRestrictedFailure(i)
					runtime.EventsEmit(a.ctx, "batch-status", map[string]interface{}{
						"index":  i,
						"status": "done",
					})
				}
			}(i, url)
		}
		wg.Wait()
		runtime.EventsEmit(a.ctx, "batch-complete", results)
	}()

	return "Batch download started in parallel"
}

// RetryDownload retries downloading a failed video
func (a *App) RetryDownload(url, format, quality, savePath string) string {
	return a.StartDownload(url, format, quality, savePath)
}

func (a *App) SetTemporaryYouTubeCookie(raw string) error {
	if err := setTemporaryYouTubeCookie(raw); err != nil {
		return err
	}

	go a.retryRestrictedBatchDownloads()
	return nil
}

// ValidateURL checks if URL is a valid YouTube link
func (a *App) ValidateURL(url string) bool {
	url = strings.TrimSpace(url)
	return strings.Contains(url, "youtube.com") || strings.Contains(url, "youtu.be") ||
		strings.Contains(url, "youtube.") || strings.Contains(url, "youtu.")
}

// CheckPlaylist checks if URL is a playlist and returns video count
func (a *App) CheckPlaylist(url string) map[string]interface{} {
	result := map[string]interface{}{
		"isPlaylist": false,
		"videoCount": 0,
		"urls":       []string{},
	}

	if !strings.Contains(url, "list=") {
		return result
	}

	// Extract playlist videos
	videos, err := GetPlaylistVideos(url)
	if err == nil && len(videos) > 0 {
		result["isPlaylist"] = true
		result["videoCount"] = len(videos)
		result["urls"] = videos
	}

	return result
}

// GetDefaultSavePath returns default download folder
func (a *App) GetDefaultSavePath() string {
	usr, err := user.Current()
	if err != nil {
		return "/Users/" + os.Getenv("USER") + "/Downloads"
	}
	return filepath.Join(usr.HomeDir, "Downloads")
}

func (a *App) trackRestrictedFailure(index int, url, errMsg string) {
	a.batchMu.Lock()
	defer a.batchMu.Unlock()

	if a.currentBatch == nil {
		return
	}

	a.currentBatch.RestrictedFailures[index] = RestrictedFailure{
		URL:       url,
		LastError: errMsg,
	}
}

func (a *App) clearRestrictedFailure(index int) {
	a.batchMu.Lock()
	defer a.batchMu.Unlock()

	if a.currentBatch == nil {
		return
	}

	delete(a.currentBatch.RestrictedFailures, index)
}

func (a *App) retryRestrictedBatchDownloads() {
	a.batchMu.Lock()
	if a.currentBatch == nil || len(a.currentBatch.RestrictedFailures) == 0 {
		a.batchMu.Unlock()
		return
	}

	type retryItem struct {
		index int
		url   string
	}

	format := a.currentBatch.Format
	quality := a.currentBatch.Quality
	savePath := a.currentBatch.SavePath
	items := make([]retryItem, 0, len(a.currentBatch.RestrictedFailures))

	for index, failure := range a.currentBatch.RestrictedFailures {
		items = append(items, retryItem{
			index: index,
			url:   failure.URL,
		})
	}
	a.batchMu.Unlock()

	sem := make(chan struct{}, 3)
	var wg sync.WaitGroup

	for _, item := range items {
		wg.Add(1)
		go func(item retryItem) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			runtime.EventsEmit(a.ctx, "batch-status", map[string]interface{}{
				"index":  item.index,
				"status": "retrying",
			})

			err := DownloadVideo(a.ctx, item.index, item.url, format, quality, savePath)
			if err != nil {
				failure := classifyDownloadFailure(err, true)
				a.trackRestrictedFailure(item.index, item.url, err.Error())
				runtime.EventsEmit(a.ctx, "batch-error", map[string]interface{}{
					"index":          item.index,
					"error":          err.Error(),
					"displayMessage": failure.DisplayMessage,
					"details":        failure.Details,
					"requiresCookie": failure.RequiresCookie,
				})
				return
			}

			a.clearRestrictedFailure(item.index)
			runtime.EventsEmit(a.ctx, "batch-status", map[string]interface{}{
				"index":  item.index,
				"status": "done",
			})
		}(item)
	}

	wg.Wait()
}

// SelectFiles opens native file picker for multiple files
func (a *App) SelectFiles(fileType string) []string {
	pattern := "*.*"
	if fileType == "video" {
		pattern = "*.mp4;*.mkv;*.avi;*.mov;*.wmv;*.flv;*.webm"
	} else if fileType == "image" {
		pattern = "*.jpg;*.jpeg;*.png;*.webp;*.bmp;*.gif;*.heic;*.avif"
	}

	// Use OpenMultipleFilesDialog to allow selecting more than one file
	multipleFiles, err := runtime.OpenMultipleFilesDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select Files to Compress",
		Filters: []runtime.FileFilter{
			{
				DisplayName: fileType + " Files",
				Pattern:     pattern,
			},
		},
	})
	if err != nil {
		return []string{}
	}

	return multipleFiles
}

// SelectFolder opens native folder picker and scans for files
func (a *App) SelectFolder(fileType string) []string {
	dir, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select Folder to Scan",
	})
	if err != nil || dir == "" {
		return []string{}
	}

	var extensions []string
	if fileType == "video" {
		extensions = []string{".mp4", ".mkv", ".avi", ".mov", ".wmv", ".flv", ".webm"}
	} else if fileType == "image" {
		extensions = []string{".jpg", ".jpeg", ".png", ".webp", ".bmp", ".gif", ".heic", ".avif"}
	}

	var files []string
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []string{}
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		for _, e := range extensions {
			if ext == e {
				files = append(files, filepath.Join(dir, entry.Name()))
				break
			}
		}
	}

	return files
}

// StartCompression starts compressing a list of files
func (a *App) StartCompression(files []string, options CompressionOptions) string {
	if len(files) == 0 {
		return "Error: No files selected"
	}

	go func() {
		for i, file := range files {
			runtime.EventsEmit(a.ctx, "compression-status", map[string]interface{}{
				"index":  i,
				"status": "processing",
			})

			err := CompressFile(a.ctx, file, options, i)

			if err != nil {
				runtime.EventsEmit(a.ctx, "compression-error", map[string]interface{}{
					"index": i,
					"error": err.Error(),
				})
			} else {
				runtime.EventsEmit(a.ctx, "compression-status", map[string]interface{}{
					"index":  i,
					"status": "done",
				})
			}
		}
		runtime.EventsEmit(a.ctx, "compression-complete", "All files processed")
	}()

	return "Compression started"
}

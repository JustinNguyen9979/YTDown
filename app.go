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
	ctx            context.Context
	config         *Config
	batchMu        sync.Mutex
	currentBatch   *BatchDownloadState
	galleryMu      sync.Mutex
	currentGallery *GalleryBatchState
}

type GalleryBatchState struct {
	URLs          []string
	Options       GalleryDownloadOptions
	ItemStates    map[int]string
	ActiveCancels map[int]context.CancelFunc
	Status        string
	SessionID     int64
}

type GalleryDownloadOptions struct {
	SavePath     string   `json:"savePath"`
	Threads      int      `json:"threads"`
	Browser      string   `json:"browser"`
	UgoiraToWebm bool     `json:"ugoiraToWebm"`
	Formats      []string `json:"formats"`
	Archive      bool     `json:"archive"`
	ExtraArgs    string   `json:"extraArgs"`
}

type BatchDownloadState struct {
	URLs               []string
	Format             string
	Quality            string
	SavePath           string
	MaxConcurrent      int
	RestrictedFailures map[int]RestrictedFailure
	ItemStates         map[int]string
	ActiveCancels      map[int]context.CancelFunc
	Status             string
	SessionID          int64
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

// GetVersionStatus returns version info for yt-dlp, ffmpeg and gallery-dl
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

	// Check gallery-dl
	gallerydlPath := getResourcePath("gallery-dl")
	if gallerydlPath != "" {
		current := ""
		cmd := exec.Command(gallerydlPath, "--version")
		if out, err := cmd.Output(); err == nil {
			current = strings.TrimSpace(string(out))
			// Handle "gallery-dl 1.28.1" format
			if parts := strings.Fields(current); len(parts) >= 2 {
				current = parts[1]
			}
		}

		latest := current
		// Fetch latest from GitHub
		client := &http.Client{Timeout: 5 * time.Second}
		if resp, err := client.Get("https://api.github.com/repos/mikf/gallery-dl/releases/latest"); err == nil {
			defer resp.Body.Close()
			var data struct {
				TagName string `json:"tag_name"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&data); err == nil {
				latest = data.TagName
				// Normalize tag name (e.g., "v1.28.1" to "1.28.1")
				latest = strings.TrimPrefix(latest, "v")
			}
		}

		versions = append(versions, BinaryVersion{
			Name:       "gallery-dl",
			Current:    current,
			Latest:     latest,
			CanUpgrade: current != "" && latest != "" && current != latest,
			UpdatePath: "https://github.com/mikf/gallery-dl/releases/latest",
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

// AppInfo struct for app information
type AppInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Author  string `json:"author"`
}

// GetAppInfo returns application information
func (a *App) GetAppInfo() AppInfo {
	return AppInfo{
		Name:    "YTDown",
		Version: Version,
		Author:  "Justin Nguyen",
	}
}

// UpgradeBinary attempts to upgrade a binary
func (a *App) UpgradeBinary(name string) error {
	if name != "yt-dlp" && name != "gallery-dl" {
		return fmt.Errorf("upgrade not supported for %s", name)
	}

	binaryPath := getResourcePath(name)
	if binaryPath == "" {
		return fmt.Errorf("%s not found", name)
	}

	runtime.EventsEmit(a.ctx, "upgrade-status", fmt.Sprintf("Upgrading %s via self-update...", name))

	// Try self-update first
	var cmd *exec.Cmd
	if name == "yt-dlp" {
		cmd = exec.Command(binaryPath, "-U")
	} else {
		cmd = exec.Command(binaryPath, "--update")
	}

	if output, err := cmd.CombinedOutput(); err == nil {
		runtime.EventsEmit(a.ctx, "upgrade-status", fmt.Sprintf("%s upgraded successfully.", name))
		return nil
	} else {
		fmt.Printf("%s update failed: %v\nOutput: %s\n", name, err, string(output))
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
		cmd = exec.Command(brewPath, "upgrade", name)
		if output, err := cmd.CombinedOutput(); err == nil {
			runtime.EventsEmit(a.ctx, "upgrade-status", fmt.Sprintf("%s upgraded via Homebrew.", name))
			return nil
		} else {
			return fmt.Errorf("failed to upgrade %s: %s", name, string(output))
		}
	}

	return fmt.Errorf("failed to upgrade %s and Homebrew not found", name)
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
    echo "👉 NOTE: Please enter your Mac password when prompted."
    echo "   (Characters will NOT show while you type, just type it and press Enter)"
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

echo "📦 Installing/Updating dependencies..."
$BREW_PATH update
$BREW_PATH install yt-dlp ffmpeg gallery-dl || $BREW_PATH upgrade yt-dlp ffmpeg gallery-dl

echo ""
echo "✅ SETUP COMPLETE!"
echo "------------------------------------------"
echo "1. yt-dlp, ffmpeg, and gallery-dl are now installed."
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
	manager.LoadConfig()

	// Check binaries after a short delay
	go func() {
		time.Sleep(1 * time.Second)
		status := a.CheckBinaries()
		if !status["ytdlp"].(bool) || !status["ffmpeg"].(bool) || !status["gallerydl"].(bool) {
			// Emit event to frontend - the frontend should show a setup button/modal
			runtime.EventsEmit(ctx, "binary-warning", "One or more dependencies are missing.")
		}
	}()
}

// CheckBinaries checks if yt-dlp, ffmpeg and gallery-dl are installed
func (a *App) CheckBinaries() map[string]interface{} {
	ytdlpPath := getResourcePath("yt-dlp")
	ffmpegPath := getResourcePath("ffmpeg")
	gallerydlPath := getResourcePath("gallery-dl")

	return map[string]interface{}{
		"ytdlp":     ytdlpPath != "",
		"ffmpeg":    ffmpegPath != "",
		"gallerydl": gallerydlPath != "",
	}
}

// shutdown is called at application termination
func (a *App) shutdown(ctx context.Context) {
	clearTemporaryYouTubeCookie()
	manager.SaveConfig()
	a.saveConfig()
}

// GetAvailableBrowsers returns a list of installed browsers for cookie extraction
func (a *App) GetAvailableBrowsers() []string {
	return GetInstalledBrowsers()
}

// GetCookieConfig returns the current cookie configuration
func (a *App) GetCookieConfig() CookieConfig {
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	return manager.config
}

// UpdateCookieConfig updates the cookie configuration
func (a *App) UpdateCookieConfig(mode string, browser string) error {
	manager.mu.Lock()
	manager.config.Mode = CookieMode(mode)
	manager.config.SelectedBrowser = browser
	manager.mu.Unlock()
	manager.SaveConfig()
	return nil
}

// ClearCookieConfig resets cookie configuration to default (none)
func (a *App) ClearCookieConfig() error {
	manager.mu.Lock()
	manager.config.Mode = CookieModeNone
	manager.config.SelectedBrowser = ""
	manager.config.ManualHeader = ""
	manager.mu.Unlock()

	manager.state.mu.Lock()
	manager.state.header = ""
	if manager.state.tempFile != "" {
		_ = os.RemoveAll(filepath.Dir(manager.state.tempFile))
		manager.state.tempFile = ""
	}
	manager.state.mu.Unlock()

	manager.SaveConfig()
	LogInfo("[Cookie] Configuration cleared by user")
	return nil
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
	LogDebug("OpenFolderDialog called")
	dir, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select Save Folder",
	})
	if err != nil {
		LogError("OpenDirectoryDialog: %v", err)
		return a.config.SavePath
	}
	LogDebug("Folder selected: %s", dir)
	a.config.SavePath = dir
	return dir
}

// OpenSaveFolder opens the specified folder path in Finder/File Explorer
func (a *App) OpenSaveFolder(savePath string) {
	if savePath == "" {
		savePath = a.config.SavePath
	}
	if savePath == "" {
		return
	}

	// On macOS, 'open' handles directories correctly.
	exec.Command("open", savePath).Run()
}

// OpenFile opens the specified file path in the system's default application
func (a *App) OpenFile(filePath string) {
	if filePath == "" {
		return
	}
	// On macOS, 'open' handles files correctly.
	exec.Command("open", filePath).Run()
}

// GetVideoInfo fetches video metadata using yt-dlp
func (a *App) GetVideoInfo(url string) *VideoInfo {
	info, err := GetVideoMetadata(a.ctx, url)
	if err != nil {
		return nil
	}
	return info
}

// StartDownload starts downloading a single video
func (a *App) StartDownload(url, format, quality, savePath string) string {
	if strings.TrimSpace(url) == "" {
		return "Error: URL is empty"
	}

	LogDebug("StartDownload called: %s %s %s %s", url, format, quality, savePath)

	go func() {
		LogDebug("Download goroutine started")
		err := DownloadVideo(a.ctx, -1, url, format, quality, savePath)
		if err != nil {
			LogError("Download error: %v", err)
			runtime.EventsEmit(a.ctx, "download-error", err.Error())
		} else {
			LogInfo("Download complete")
			runtime.EventsEmit(a.ctx, "download-complete", savePath)
		}
	}()

	return "Download started"
}

func normalizeBatchConcurrency(value int) int {
	if value < 1 {
		return 3
	}
	if value > 10 {
		return 10
	}
	return value
}

func isTerminalBatchStatus(status string) bool {
	return status == "done" || status == "error" || status == "canceled"
}

func cloneCancelFuncs(src map[int]context.CancelFunc) map[int]context.CancelFunc {
	dst := make(map[int]context.CancelFunc, len(src))
	for index, cancel := range src {
		dst[index] = cancel
	}
	return dst
}

func (a *App) emitBatchStatuses(statuses map[int]string) {
	for index, status := range statuses {
		runtime.EventsEmit(a.ctx, "batch-status", map[string]interface{}{
			"index":  index,
			"status": status,
		})
	}
}

func (a *App) finalizeBatchRun(sessionID int64) {
	a.batchMu.Lock()
	if a.currentBatch == nil || a.currentBatch.SessionID != sessionID || a.currentBatch.Status != "running" {
		a.batchMu.Unlock()
		return
	}

	for _, status := range a.currentBatch.ItemStates {
		if !isTerminalBatchStatus(status) {
			a.batchMu.Unlock()
			return
		}
	}

	a.currentBatch.Status = "completed"
	a.batchMu.Unlock()

	runtime.EventsEmit(a.ctx, "batch-complete", map[string]interface{}{})
}

func (a *App) runBatchSession(sessionID int64) {
	a.batchMu.Lock()
	if a.currentBatch == nil || a.currentBatch.SessionID != sessionID || a.currentBatch.Status != "running" {
		a.batchMu.Unlock()
		return
	}

	pendingIndices := make([]int, 0)
	for index, status := range a.currentBatch.ItemStates {
		if status == "waiting" || status == "paused" {
			pendingIndices = append(pendingIndices, index)
		}
	}

	format := a.currentBatch.Format
	quality := a.currentBatch.Quality
	savePath := a.currentBatch.SavePath
	maxConcurrent := a.currentBatch.MaxConcurrent
	urls := append([]string(nil), a.currentBatch.URLs...)
	a.batchMu.Unlock()

	if len(pendingIndices) == 0 {
		a.finalizeBatchRun(sessionID)
		return
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, maxConcurrent)

	for _, index := range pendingIndices {
		url := strings.TrimSpace(urls[index])
		if url == "" {
			continue
		}

		wg.Add(1)
		go func(index int, url string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			a.batchMu.Lock()
			if a.currentBatch == nil || a.currentBatch.SessionID != sessionID || a.currentBatch.Status != "running" {
				a.batchMu.Unlock()
				return
			}
			if isTerminalBatchStatus(a.currentBatch.ItemStates[index]) {
				a.batchMu.Unlock()
				return
			}

			itemCtx, cancel := context.WithCancel(a.ctx)
			a.currentBatch.ItemStates[index] = "downloading"
			a.currentBatch.ActiveCancels[index] = cancel
			a.batchMu.Unlock()

			runtime.EventsEmit(a.ctx, "batch-status", map[string]interface{}{
				"index":  index,
				"status": "downloading",
			})

			err := DownloadVideo(itemCtx, index, url, format, quality, savePath)

			a.batchMu.Lock()
			if a.currentBatch != nil {
				delete(a.currentBatch.ActiveCancels, index)
			}
			if a.currentBatch == nil || a.currentBatch.SessionID != sessionID {
				a.batchMu.Unlock()
				return
			}
			batchStatus := a.currentBatch.Status

			if err == nil {
				delete(a.currentBatch.RestrictedFailures, index)
				a.currentBatch.ItemStates[index] = "done"
				a.batchMu.Unlock()
				runtime.EventsEmit(a.ctx, "batch-status", map[string]interface{}{
					"index":  index,
					"status": "done",
				})
				return
			}

			if err == context.Canceled || strings.Contains(err.Error(), context.Canceled.Error()) {
				if batchStatus == "canceled" {
					a.currentBatch.ItemStates[index] = "canceled"
					a.batchMu.Unlock()
					runtime.EventsEmit(a.ctx, "batch-status", map[string]interface{}{
						"index":  index,
						"status": "canceled",
					})
					return
				}

				a.currentBatch.ItemStates[index] = "paused"
				a.batchMu.Unlock()
				runtime.EventsEmit(a.ctx, "batch-status", map[string]interface{}{
					"index":  index,
					"status": "paused",
				})
				return
			}

			a.currentBatch.ItemStates[index] = "error"
			a.batchMu.Unlock()

			failure := classifyDownloadFailure(err, hasTemporaryCookie())
			if failure.RequiresCookie {
				a.trackRestrictedFailure(index, url, err.Error())
			}

			runtime.EventsEmit(a.ctx, "batch-error", map[string]interface{}{
				"index":          index,
				"error":          err.Error(),
				"displayMessage": failure.DisplayMessage,
				"details":        failure.Details,
				"requiresCookie": failure.RequiresCookie,
			})
		}(index, url)
	}

	wg.Wait()
	a.finalizeBatchRun(sessionID)
}

// StartBatchDownload starts batch downloading in parallel
func (a *App) StartBatchDownload(urls []string, format, quality, savePath string, maxConcurrent int) string {
	if len(urls) == 0 {
		return "Error: No URLs provided"
	}

	maxConcurrent = normalizeBatchConcurrency(maxConcurrent)

	a.batchMu.Lock()
	if a.currentBatch != nil && (a.currentBatch.Status == "running" || a.currentBatch.Status == "paused") {
		a.batchMu.Unlock()
		return "Error: Batch session is already active"
	}

	itemStates := make(map[int]string, len(urls))
	for index := range urls {
		itemStates[index] = "waiting"
	}

	sessionID := time.Now().UnixNano()
	a.currentBatch = &BatchDownloadState{
		URLs:               append([]string(nil), urls...),
		Format:             format,
		Quality:            quality,
		SavePath:           savePath,
		MaxConcurrent:      maxConcurrent,
		RestrictedFailures: make(map[int]RestrictedFailure),
		ItemStates:         itemStates,
		ActiveCancels:      make(map[int]context.CancelFunc),
		Status:             "running",
		SessionID:          sessionID,
	}
	a.batchMu.Unlock()

	go a.runBatchSession(sessionID)

	return fmt.Sprintf("Batch download started with %d threads", maxConcurrent)
}

func (a *App) PauseBatchDownload() error {
	a.batchMu.Lock()
	if a.currentBatch == nil || a.currentBatch.Status != "running" {
		a.batchMu.Unlock()
		return fmt.Errorf("no running batch session")
	}

	a.currentBatch.Status = "paused"
	updatedStatuses := make(map[int]string)
	for index, status := range a.currentBatch.ItemStates {
		if status == "waiting" || status == "downloading" {
			a.currentBatch.ItemStates[index] = "paused"
			updatedStatuses[index] = "paused"
		}
	}

	cancels := cloneCancelFuncs(a.currentBatch.ActiveCancels)
	a.batchMu.Unlock()

	a.emitBatchStatuses(updatedStatuses)
	for _, cancel := range cancels {
		cancel()
	}

	runtime.EventsEmit(a.ctx, "batch-paused", map[string]interface{}{})
	return nil
}

func (a *App) ResumeBatchDownload(format, quality, savePath string, maxConcurrent int) string {
	maxConcurrent = normalizeBatchConcurrency(maxConcurrent)

	a.batchMu.Lock()
	if a.currentBatch == nil || a.currentBatch.Status != "paused" {
		a.batchMu.Unlock()
		return "Error: No paused batch session"
	}

	a.currentBatch.Format = format
	a.currentBatch.Quality = quality
	a.currentBatch.SavePath = savePath
	a.currentBatch.MaxConcurrent = maxConcurrent
	a.currentBatch.Status = "running"
	a.currentBatch.SessionID = time.Now().UnixNano()
	sessionID := a.currentBatch.SessionID

	waitingStatuses := make(map[int]string)
	for index, status := range a.currentBatch.ItemStates {
		if status == "paused" {
			a.currentBatch.ItemStates[index] = "waiting"
			waitingStatuses[index] = "waiting"
		}
	}
	a.batchMu.Unlock()

	a.emitBatchStatuses(waitingStatuses)
	runtime.EventsEmit(a.ctx, "batch-resumed", map[string]interface{}{})
	go a.runBatchSession(sessionID)

	return fmt.Sprintf("Batch download resumed with %d threads", maxConcurrent)
}

func (a *App) CancelBatchDownload() error {
	a.batchMu.Lock()
	if a.currentBatch == nil || (a.currentBatch.Status != "running" && a.currentBatch.Status != "paused") {
		a.batchMu.Unlock()
		return fmt.Errorf("no active batch session")
	}

	a.currentBatch.Status = "canceled"
	updatedStatuses := make(map[int]string)
	for index, status := range a.currentBatch.ItemStates {
		if !isTerminalBatchStatus(status) {
			a.currentBatch.ItemStates[index] = "canceled"
			updatedStatuses[index] = "canceled"
		}
	}

	cancels := cloneCancelFuncs(a.currentBatch.ActiveCancels)
	a.batchMu.Unlock()

	a.emitBatchStatuses(updatedStatuses)
	for _, cancel := range cancels {
		cancel()
	}

	runtime.EventsEmit(a.ctx, "batch-canceled", map[string]interface{}{})
	return nil
}

// RetryDownload retries downloading a failed video
func (a *App) RetryDownload(url, format, quality, savePath string) string {
	return a.StartDownload(url, format, quality, savePath)
}

func (a *App) SetManualCookie(raw string) error {
	if err := setManualCookie(raw); err != nil {
		return err
	}

	go a.retryRestrictedBatchDownloads()
	return nil
}

func (a *App) SetTemporaryYouTubeCookie(raw string) error {
	return a.SetManualCookie(raw)
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
	videos, err := GetPlaylistVideos(a.ctx, url)
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
	if a.currentBatch == nil || a.currentBatch.Status != "running" || len(a.currentBatch.RestrictedFailures) == 0 {
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

func (a *App) finalizeGalleryBatchRun(sessionID int64) {
	a.galleryMu.Lock()
	if a.currentGallery == nil || a.currentGallery.SessionID != sessionID || a.currentGallery.Status != "running" {
		a.galleryMu.Unlock()
		return
	}

	for _, status := range a.currentGallery.ItemStates {
		if !isTerminalBatchStatus(status) {
			a.galleryMu.Unlock()
			return
		}
	}

	a.currentGallery.Status = "completed"
	a.galleryMu.Unlock()

	runtime.EventsEmit(a.ctx, "gallery-batch-complete", map[string]interface{}{})
}

func (a *App) runGalleryBatchSession(sessionID int64) {
	a.galleryMu.Lock()
	if a.currentGallery == nil || a.currentGallery.SessionID != sessionID || a.currentGallery.Status != "running" {
		a.galleryMu.Unlock()
		return
	}

	pendingIndices := make([]int, 0)
	for index, status := range a.currentGallery.ItemStates {
		if status == "waiting" {
			pendingIndices = append(pendingIndices, index)
		}
	}

	options := a.currentGallery.Options
	urls := append([]string(nil), a.currentGallery.URLs...)
	maxConcurrent := options.Threads
	if maxConcurrent < 1 {
		maxConcurrent = 1
	}
	a.galleryMu.Unlock()

	if len(pendingIndices) == 0 {
		a.finalizeGalleryBatchRun(sessionID)
		return
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, maxConcurrent)

	for _, index := range pendingIndices {
		url := strings.TrimSpace(urls[index])
		if url == "" {
			continue
		}

		wg.Add(1)
		go func(index int, url string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			a.galleryMu.Lock()
			if a.currentGallery == nil || a.currentGallery.SessionID != sessionID || a.currentGallery.Status != "running" {
				a.galleryMu.Unlock()
				return
			}
			if isTerminalBatchStatus(a.currentGallery.ItemStates[index]) {
				a.galleryMu.Unlock()
				return
			}

			itemCtx, cancel := context.WithCancel(a.ctx)
			a.currentGallery.ItemStates[index] = "downloading"
			a.currentGallery.ActiveCancels[index] = cancel
			a.galleryMu.Unlock()

			runtime.EventsEmit(a.ctx, "gallery-status", map[string]interface{}{
				"index":  index,
				"status": "downloading",
			})

			// Convert options to separate arguments if needed
			err := DownloadGalleryWithOpts(itemCtx, index, url, options)

			a.galleryMu.Lock()
			if a.currentGallery != nil {
				delete(a.currentGallery.ActiveCancels, index)
			}
			if a.currentGallery == nil || a.currentGallery.SessionID != sessionID {
				a.galleryMu.Unlock()
				return
			}

			if err == nil {
				a.currentGallery.ItemStates[index] = "done"
				a.galleryMu.Unlock()
				runtime.EventsEmit(a.ctx, "gallery-status", map[string]interface{}{
					"index":  index,
					"status": "done",
				})
				return
			}

			if err == context.Canceled || strings.Contains(err.Error(), context.Canceled.Error()) {
				a.currentGallery.ItemStates[index] = "canceled"
				a.galleryMu.Unlock()
				runtime.EventsEmit(a.ctx, "gallery-status", map[string]interface{}{
					"index":  index,
					"status": "canceled",
				})
				return
			}

			a.currentGallery.ItemStates[index] = "error"
			a.galleryMu.Unlock()

			runtime.EventsEmit(a.ctx, "gallery-status", map[string]interface{}{
				"index":   index,
				"status":  "error",
				"message": err.Error(),
			})
		}(index, url)
	}

	wg.Wait()
	a.finalizeGalleryBatchRun(sessionID)
}

// StartGalleryBatchDownload starts downloading multiple galleries
func (a *App) StartGalleryBatchDownload(urls []string, options GalleryDownloadOptions) string {
	if len(urls) == 0 {
		return "Error: No URLs provided"
	}

	a.galleryMu.Lock()
	if a.currentGallery != nil && a.currentGallery.Status == "running" {
		a.galleryMu.Unlock()
		return "Error: A gallery download is already in progress"
	}

	itemStates := make(map[int]string, len(urls))
	for index := range urls {
		itemStates[index] = "waiting"
	}

	sessionID := time.Now().UnixNano()
	a.currentGallery = &GalleryBatchState{
		URLs:          append([]string(nil), urls...),
		Options:       options,
		ItemStates:    itemStates,
		ActiveCancels: make(map[int]context.CancelFunc),
		Status:        "running",
		SessionID:     sessionID,
	}
	a.galleryMu.Unlock()

	go a.runGalleryBatchSession(sessionID)

	return "Gallery batch download started"
}

// CancelGalleryDownload cancels all active gallery downloads
func (a *App) CancelGalleryDownload() error {
	a.galleryMu.Lock()
	if a.currentGallery == nil || a.currentGallery.Status != "running" {
		a.galleryMu.Unlock()
		return fmt.Errorf("no active gallery download")
	}

	a.currentGallery.Status = "canceled"
	updatedStatuses := make(map[int]string)
	for index, status := range a.currentGallery.ItemStates {
		if !isTerminalBatchStatus(status) {
			a.currentGallery.ItemStates[index] = "canceled"
			updatedStatuses[index] = "canceled"
		}
	}

	cancels := make(map[int]context.CancelFunc, len(a.currentGallery.ActiveCancels))
	for i, c := range a.currentGallery.ActiveCancels {
		cancels[i] = c
	}
	a.galleryMu.Unlock()

	for index, status := range updatedStatuses {
		runtime.EventsEmit(a.ctx, "gallery-status", map[string]interface{}{
			"index":  index,
			"status": status,
		})
	}

	for _, cancel := range cancels {
		cancel()
	}

	runtime.EventsEmit(a.ctx, "gallery-batch-complete", map[string]interface{}{})
	return nil
}

// SetGalleryCookie sets a temporary cookie for gallery-dl
func (a *App) SetGalleryCookie(raw string) error {
	return setGalleryCookie(raw)
}

// StartGalleryDownload starts downloading images from a gallery URL (Legacy)
func (a *App) StartGalleryDownload(url, savePath string) string {
	if strings.TrimSpace(url) == "" {
		return "Error: URL is empty"
	}

	LogDebug("StartGalleryDownload called: %s %s", url, savePath)

	go func() {
		LogDebug("Gallery download goroutine started")
		// Using index -2 to distinguish from video batch indices if needed,
		// or just use 0 if it's single URL for now.
		// The frontend will handle the UI row.
		err := DownloadGallery(a.ctx, 0, url, savePath)
		if err != nil {
			LogError("Gallery download error: %v", err)
			runtime.EventsEmit(a.ctx, "gallery-error", err.Error())
		} else {
			LogInfo("Gallery download complete")
			// download-complete is already emitted inside DownloadGallery
		}
	}()

	return "Gallery download started"
}

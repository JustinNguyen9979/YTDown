package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	platform "ytdown/flatform"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App struct
type App struct {
	ctx            context.Context
	pm             platform.Manager
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
	UgoiraToWebm bool     `json:"ugoiraToWebm"`
	Formats      []string `json:"formats"`
	AllFormats   bool     `json:"allFormats"`
	Archive      bool     `json:"archive"`
	ExtraArgs    string   `json:"extraArgs"`
}

type BatchDownloadState struct {
	URLs                []string
	Format              string
	Quality             string
	SavePath            string
	MaxConcurrent       int
	NoPlaylist          bool
	ConcurrentFragments int
	RestrictedFailures  map[int]RestrictedFailure
	ItemStates          map[int]string
	ActiveCancels       map[int]context.CancelFunc
	Status              string
	SessionID           int64
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
	return NewAppWithManager(platform.NewManager())
}

// NewAppWithManager creates a new App with an existing manager
func NewAppWithManager(pm platform.Manager) *App {
	return &App{
		pm: pm,
	}
}

func getBrewLatestVersion(name string) string {
	brewPath := ""
	for _, p := range []string{"/opt/homebrew/bin/brew", "/usr/local/bin/brew"} {
		if _, err := os.Stat(p); err == nil {
			brewPath = p
			break
		}
	}
	if brewPath == "" {
		return ""
	}

	cmd := platform.Command(brewPath, "info", "--json=v1", name)
	cmd.Env = append(os.Environ(),
		"PATH=/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin",
	)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}

	var result []struct {
		Versions struct {
			Stable string `json:"stable"`
		} `json:"versions"`
	}
	if err := json.Unmarshal(out, &result); err != nil || len(result) == 0 {
		return ""
	}
	return result[0].Versions.Stable
}

// GetVersionStatus returns version info for yt-dlp, ffmpeg and gallery-dl
func (a *App) GetVersionStatus() []BinaryVersion {
	var versions []BinaryVersion

	// Check yt-dlp
	ytdlpPath := getResourcePath("yt-dlp")
	if ytdlpPath != "" {
		current := ""
		if out, err := platform.Command(ytdlpPath, "--version").Output(); err == nil {
			current = strings.TrimSpace(string(out))
		}

		// ✅ Dùng platform manager — mỗi OS tự xử lý (brew/GitHub API/pip)
		current = normalizeVersion(current)
		latest := current
		if v := a.pm.GetLatestVersion("yt-dlp"); v != "" {
			latest = normalizeVersion(v)
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
		if out, err := platform.Command(gallerydlPath, "--version").Output(); err == nil {
			current = strings.TrimSpace(string(out))
			if parts := strings.Fields(current); len(parts) >= 2 {
				current = parts[1]
			}
		}

		// ✅ Dùng platform manager — không hardcode brew
		current = normalizeVersion(current)
		latest := current
		if v := a.pm.GetLatestVersion("gallery-dl"); v != "" {
			latest = normalizeVersion(v)
		}

		versions = append(versions, BinaryVersion{
			Name:       "gallery-dl",
			Current:    current,
			Latest:     latest,
			CanUpgrade: current != "" && latest != "" && current != latest,
			UpdatePath: "https://github.com/mikf/gallery-dl/releases/latest",
		})
	}

	// Check ffmpeg (giữ nguyên)
	ffmpegPath := getResourcePath("ffmpeg")
	if ffmpegPath != "" {
		current := ""
		if out, err := platform.Command(ffmpegPath, "-version").Output(); err == nil {
			lines := strings.Split(string(out), "\n")
			if len(lines) > 0 {
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
			Latest:     current,
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

// UpgradeBinary attempts to upgrade the specified binary (yt-dlp or gallery-dl)
func (a *App) UpgradeBinary(name string) error {
	// ✅ FIX 1: Dùng cùng hàm getResourcePath như GetVersionStatus
	// để đảm bảo tìm đúng path binary
	binaryPath := getResourcePath(name)
	if binaryPath == "" {
		// Fallback sang pm.GetBinaryPath
		binaryPath = a.pm.GetBinaryPath(name)
	}
	if binaryPath == "" {
		err := fmt.Errorf("%s not found in PATH or Homebrew directories", name)
		// ✅ FIX: Emit lỗi ra UI thay vì âm thầm return
		runtime.EventsEmit(a.ctx, "upgrade-error", map[string]interface{}{
			"name":  name,
			"error": err.Error(),
		})
		return err
	}

	runtime.EventsEmit(a.ctx, "upgrade-status", fmt.Sprintf("Upgrading %s...", name))

	if err := a.pm.UpgradeTool(name, binaryPath); err != nil {
		// ✅ FIX: Emit lỗi chi tiết ra UI
		runtime.EventsEmit(a.ctx, "upgrade-error", map[string]interface{}{
			"name":  name,
			"error": err.Error(),
		})
		return err
	}

	runtime.EventsEmit(a.ctx, "upgrade-status", fmt.Sprintf("%s upgraded successfully.", name))
	return nil
}

// LaunchSetupTerminal: delegate sang platform
func (a *App) LaunchSetupTerminal() error {
	go func() {
		runtime.EventsEmit(a.ctx, "setup:started", nil)

		if err := a.pm.LaunchSetup(); err != nil {
			runtime.EventsEmit(a.ctx, "setup:error", map[string]string{
				"error": err.Error(),
			})
			return
		}

		// Re-check sau khi LaunchSetup() hoàn tất
		result := a.CheckDependencies()
		runtime.EventsEmit(a.ctx, "setup:complete", result)
		LogInfo("[Setup] Setup complete. AllInstalled=%v", result.AllInstalled)
	}()
	return nil
}

// startup is called at application startup
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.pm.InjectBinDir()
	InitLogger()
	a.loadConfig()
	manager.LoadConfig()
	LogInfo("[Startup] YTDown started")
}

// CheckBinaries checks if yt-dlp, ffmpeg and gallery-dl are installed (legacy support)
func (a *App) CheckBinaries() map[string]interface{} {
	check := a.CheckDependencies()
	toolMap := make(map[string]interface{})

	for _, dep := range check.Dependencies {
		switch dep.Name {
		case "yt-dlp":
			toolMap["ytdlp"] = dep.Installed
		case "ffmpeg":
			toolMap["ffmpeg"] = dep.Installed
		case "gallery-dl":
			toolMap["gallerydl"] = dep.Installed
		}
	}

	return toolMap
}

// shutdown is called at application termination
func (a *App) shutdown(ctx context.Context) {
	LogInfo("[Shutdown] Starting cleanup...")

	// 1. Xóa temporary YouTube cookie file
	clearTemporaryYouTubeCookie()

	// 2. Xóa tất cả temp cookie files trong manager state
	manager.state.mu.Lock()
	if manager.state.tempFile != "" {
		_ = os.RemoveAll(filepath.Dir(manager.state.tempFile))
		manager.state.tempFile = ""
		LogInfo("[Cleanup] Removed temp cookie dir")
	}
	manager.state.mu.Unlock()

	// 3. Xóa cache yt-dlp, gallery-dl và temp folder của app
	cleanAppCache()

	// 4. Lưu config (làm trước khi xóa log)
	manager.SaveConfig()
	a.saveConfig()

	// 5. Xóa log file (làm CUỐI CÙNG)
	LogInfo("[Shutdown] Cleanup complete. Goodbye!")
	CleanupLogFile()
}

// cleanAppCache xóa cache của yt-dlp, gallery-dl và temp folder của app
func cleanAppCache() {
	usr, err := user.Current()
	if err != nil {
		return
	}

	cachePaths := []string{
		// macOS — yt-dlp
		filepath.Join(usr.HomeDir, ".cache", "yt-dlp"),
		filepath.Join(usr.HomeDir, "Library", "Caches", "yt-dlp"),
		// macOS — gallery-dl
		filepath.Join(usr.HomeDir, ".cache", "gallery-dl"),
		filepath.Join(usr.HomeDir, "Library", "Caches", "gallery-dl"),
		// Linux — yt-dlp / gallery-dl
		filepath.Join(usr.HomeDir, ".local", "share", "yt-dlp"),
		// ytdown logs
		filepath.Join(usr.HomeDir, ".config", "ytdown", "logs"),
		// temp fragments (download bị dở)
		filepath.Join(os.TempDir(), "ytdown"),
	}

	// ✅ Windows cache paths
	if appData := os.Getenv("APPDATA"); appData != "" {
		cachePaths = append(cachePaths,
			filepath.Join(appData, "yt-dlp", "cache"),
			filepath.Join(appData, "gallery-dl", "cache"),
		)
	}
	if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
		cachePaths = append(cachePaths,
			filepath.Join(localAppData, "yt-dlp"),
			filepath.Join(localAppData, "gallery-dl"),
		)
	}

	for _, path := range cachePaths {
		if _, err := os.Stat(path); err == nil {
			if err := os.RemoveAll(path); err == nil {
				LogInfo("[Cleanup] Removed: %s", path)
			} else {
				LogWarning("[Cleanup] Failed to remove %s: %v", path, err)
			}
		}
	}
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

func (a *App) UpdateCookieConfig(mode string, browser string) error {
	manager.mu.Lock()
	manager.config.Mode = CookieMode(mode)
	manager.config.SelectedBrowser = browser
	manager.mu.Unlock()
	manager.SaveConfig()

	if mode == string(CookieModeBrowser) && browser != "" {
		// Prefetch User-Agent asynchronously so it's ready when downloading
		go func(b string) {
			GetUserAgent(b)
		}(browser)
	}

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

	if data, err := os.ReadFile(configPath); err == nil {
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
		os.WriteFile(configPath, data, 0644)
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

// OpenSaveFolder: dùng a.pm.OpenFolder thay vì hardcode "open"
func (a *App) OpenSaveFolder(savePath string) {
	if savePath == "" {
		savePath = a.config.SavePath
	}
	if savePath != "" {
		a.pm.OpenFolder(savePath)
	}
}

func (a *App) OpenFile(filePath string) {
	if filePath != "" {
		a.pm.OpenFile(filePath)
	}
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
func (a *App) StartDownload(url, format, quality, savePath string, NoPlaylist bool, concurrentFragments int) string {
	if strings.TrimSpace(url) == "" {
		return "Error: URL is empty"
	}

	LogDebug("StartDownload called: %s %s %s %s", url, format, quality, savePath)

	go func() {
		LogDebug("Download goroutine started")
		err := DownloadVideo(a.ctx, -1, url, format, quality, savePath, NoPlaylist, concurrentFragments)
		if err != nil {
			LogError("Download error: %v", err)
			runtime.EventsEmit(a.ctx, "download-error", err.Error())
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

	// ✅ FIX BUG A: Không finalize nếu còn restricted failures chờ retry
	if len(a.currentBatch.RestrictedFailures) > 0 {
		a.batchMu.Unlock()
		// Thông báo cho frontend biết batch đang chờ cookie
		runtime.EventsEmit(a.ctx, "batch-waiting-cookie", map[string]interface{}{
			"count": len(a.currentBatch.RestrictedFailures),
		})
		return
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
	noPlaylist := a.currentBatch.NoPlaylist
	maxConcurrent := a.currentBatch.MaxConcurrent
	concurrentFragments := a.currentBatch.ConcurrentFragments
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

			err := DownloadVideo(itemCtx, index, url, format, quality, savePath, noPlaylist, concurrentFragments)

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
func (a *App) StartBatchDownload(urls []string, format, quality, savePath string, maxConcurrent int, NoPlaylist bool, concurrentFragments int) string {
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
		URLs:                append([]string(nil), urls...),
		Format:              format,
		Quality:             quality,
		SavePath:            savePath,
		NoPlaylist:          NoPlaylist,
		ConcurrentFragments: concurrentFragments,
		MaxConcurrent:       maxConcurrent,
		RestrictedFailures:  make(map[int]RestrictedFailure),
		ItemStates:          itemStates,
		ActiveCancels:       make(map[int]context.CancelFunc),
		Status:              "running",
		SessionID:           sessionID,
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

func (a *App) ResumeBatchDownload(format, quality, savePath string, maxConcurrent int, concurrentFragments int) string {
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
	a.currentBatch.ConcurrentFragments = concurrentFragments
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
	// ❌ Không dùng defer ở đây vì cần unlock sớm trước khi emit events

	if a.currentBatch == nil || (a.currentBatch.Status != "running" && a.currentBatch.Status != "paused") {
		a.batchMu.Unlock() // ✅ Unlock rồi return
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
	a.batchMu.Unlock() // ✅ Unlock trước khi gọi external functions (tránh deadlock)

	a.emitBatchStatuses(updatedStatuses)
	for _, cancel := range cancels {
		cancel()
	}

	runtime.EventsEmit(a.ctx, "batch-canceled", map[string]interface{}{})
	return nil
}

// RetryDownload retries downloading a failed video
func (a *App) RetryDownload(url, format, quality, savePath string) string {
	return a.StartDownload(url, format, quality, savePath, false, 2)
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
	// ✅ Capture sessionID NGAY TẠI ĐÂY, trước khi Unlock
	batchSessionID := a.currentBatch.SessionID
	concurrentFragments := a.currentBatch.ConcurrentFragments
	items := make([]retryItem, 0, len(a.currentBatch.RestrictedFailures))

	for index, failure := range a.currentBatch.RestrictedFailures {
		items = append(items, retryItem{index: index, url: failure.URL})
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

			err := DownloadVideo(a.ctx, item.index, item.url, format, quality, savePath, a.currentBatch.NoPlaylist, concurrentFragments)
			if err != nil {
				failure := classifyDownloadFailure(err, true)
				a.trackRestrictedFailure(item.index, item.url, err.Error())

				runtime.EventsEmit(a.ctx, "batch-status", map[string]interface{}{
					"index":  item.index,
					"status": "error",
				})

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

			a.batchMu.Lock()
			if a.currentBatch != nil {
				a.currentBatch.ItemStates[item.index] = "done"
			}
			a.batchMu.Unlock()

			runtime.EventsEmit(a.ctx, "batch-status", map[string]interface{}{
				"index":  item.index,
				"status": "done",
			})

			// Trigger finalize sau mỗi success (finalizeBatchRun tự check all-done)
			a.finalizeBatchRun(batchSessionID)
		}(item)
	}

	wg.Wait()

	// ✅ THÊM: đảm bảo finalize ngay cả khi tất cả retry đều fail
	a.finalizeBatchRun(batchSessionID)
}

// DismissRestrictedFailures clears all restricted failures and finalizes batch
func (a *App) DismissRestrictedFailures() error {
	a.batchMu.Lock()
	if a.currentBatch == nil || a.currentBatch.Status != "running" {
		a.batchMu.Unlock()
		return fmt.Errorf("no running batch session")
	}
	// Xóa toàn bộ restricted failures
	a.currentBatch.RestrictedFailures = make(map[int]RestrictedFailure)
	sessionID := a.currentBatch.SessionID
	a.batchMu.Unlock()

	// Finalize bình thường
	a.finalizeBatchRun(sessionID)
	return nil
}

// SelectFiles opens native file picker for multiple files
func (a *App) SelectFiles(fileType string) []string {
	pattern := "*.*"

	switch fileType {
	case "video":
		pattern = "*.mp4;*.mkv;*.avi;*.mov;*.wmv;*.flv;*.webm"
	case "image":
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
	switch fileType {
	case "video":
		extensions = []string{".mp4", ".mkv", ".avi", ".mov", ".wmv", ".flv", ".webm"}
	case "image":
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

			err := DownloadGalleryWithOpts(itemCtx, index, url, options)

			a.galleryMu.Lock()
			if a.currentGallery != nil {
				delete(a.currentGallery.ActiveCancels, index)
			}
			if a.currentGallery == nil || a.currentGallery.SessionID != sessionID {
				a.galleryMu.Unlock()
				return
			}

			// ✅ Case 1: Download thành công
			if err == nil {
				a.currentGallery.ItemStates[index] = "done"
				a.galleryMu.Unlock()
				runtime.EventsEmit(a.ctx, "gallery-status", map[string]interface{}{
					"index":  index,
					"status": "done",
				})
				a.finalizeGalleryBatchRun(sessionID)
				return
			}

			// ✅ Case 2: Bị cancel
			if err == context.Canceled || strings.Contains(err.Error(), context.Canceled.Error()) {
				a.currentGallery.ItemStates[index] = "canceled"
				a.galleryMu.Unlock()
				runtime.EventsEmit(a.ctx, "gallery-status", map[string]interface{}{
					"index":  index,
					"status": "canceled",
				})
				return
			}

			// ✅ Case 3: Lỗi thực sự — làm sạch message rồi emit
			rawErr := err.Error()
			cleanErr := rawErr
			for _, line := range strings.Split(rawErr, "\n") {
				line = strings.TrimSpace(line)
				if line != "" {
					cleanErr = strings.TrimPrefix(line, "gallery download failed: ")
					cleanErr = strings.TrimPrefix(cleanErr, "ERROR: ")
					if len(cleanErr) > 200 {
						cleanErr = cleanErr[:197] + "..."
					}
					break
				}
			}
			a.currentGallery.ItemStates[index] = "error"
			a.galleryMu.Unlock()
			runtime.EventsEmit(a.ctx, "gallery-status", map[string]interface{}{
				"index":   index,
				"status":  "error",
				"message": cleanErr,
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

// StartCompression compresses a list of files
func (a *App) StartCompression(filePaths []string, options CompressionOptions) error {
	if len(filePaths) == 0 {
		return fmt.Errorf("no files selected")
	}

	go func() {
		for i, path := range filePaths {
			if err := CompressFile(a.ctx, path, options, i); err != nil {
				runtime.EventsEmit(a.ctx, "compression-error", map[string]interface{}{
					"index": i,
					"error": err.Error(),
				})
			}
		}
		runtime.EventsEmit(a.ctx, "compression-complete", map[string]interface{}{})
	}()

	return nil
}

// normalizeVersion xóa leading zero trong từng phần của version
// Ví dụ: "2026.03.17" → "2026.3.17", "1.08.2" → "1.8.2"
func normalizeVersion(v string) string {
	v = strings.TrimPrefix(v, "v")
	parts := strings.Split(v, ".")
	for i, p := range parts {
		if n, err := strconv.Atoi(p); err == nil {
			parts[i] = strconv.Itoa(n) // tự động bỏ leading zero
		}
	}
	return strings.Join(parts, ".")
}

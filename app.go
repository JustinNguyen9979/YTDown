package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App struct
type App struct {
	ctx    context.Context
	config *Config
}

// Config struct for storing settings
type Config struct {
	SavePath string `json:"savePath"`
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{}
}

// startup is called at application startup
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.loadConfig()

	// Check binaries after a short delay to ensure UI is ready
	go func() {
		status := a.CheckBinaries()
		if !status["ytdlp"].(bool) {
			runtime.EventsEmit(ctx, "binary-warning", "yt-dlp is missing. Trying Homebrew install...")
			if err := ensureYTDLPInstalled(ctx); err != nil {
				runtime.EventsEmit(ctx, "binary-error", "yt-dlp install failed: "+err.Error())
				return
			}
			runtime.EventsEmit(ctx, "binary-warning", "yt-dlp installed via Homebrew.")
		} else if !status["ffmpeg"].(bool) {
			runtime.EventsEmit(ctx, "binary-warning", "ffmpeg is missing. Some formats (like MP3) will fail.")
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

func getBrewPath() string {
	for _, p := range []string{
		"/opt/homebrew/bin/brew",
		"/usr/local/bin/brew",
	} {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p
		}
	}
	return ""
}

func ensureYTDLPInstalled(_ context.Context) error {
	if path := getResourcePath("yt-dlp"); path != "" {
		return nil
	}

	brewPath := getBrewPath()
	if brewPath == "" {
		return fmt.Errorf("Homebrew is not installed")
	}

	cmd := exec.Command(brewPath, "install", "yt-dlp")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(output)))
	}

	if path := getResourcePath("yt-dlp"); path == "" {
		return fmt.Errorf("yt-dlp is still unavailable after brew install")
	}

	return nil
}

// shutdown is called at application termination
func (a *App) shutdown(ctx context.Context) {
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
					runtime.EventsEmit(a.ctx, "batch-error", map[string]interface{}{
						"index": i,
						"error": err.Error(),
					})
				} else {
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

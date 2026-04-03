package main

import (
	"context"
	"encoding/json"
	"io/ioutil"
	"os"
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

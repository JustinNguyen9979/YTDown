package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	runtimepkg "runtime"
	"strconv"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// DownloadVideo downloads a video using yt-dlp
func DownloadVideo(ctx context.Context, index int, url, format, quality, savePath string) error {
	ytdlpPath := getResourcePath("yt-dlp")
	ffmpegPath := getResourcePath("ffmpeg")

	println("[DL] yt-dlp path:", ytdlpPath)
	println("[DL] ffmpeg path:", ffmpegPath)

	if ytdlpPath == "" {
		return fmt.Errorf("yt-dlp not found - install with: brew install yt-dlp")
	}

	if ffmpegPath == "" {
		println("[DL] warning: ffmpeg not found, some formats may fail")
	}

	// Build yt-dlp arguments based on format and quality
	args := buildDownloadArgs(format, quality, savePath, ffmpegPath)
	args = append(args, url)

	println("[DL] Running command:", ytdlpPath, "with", len(args), "args")

	cmd := exec.Command(ytdlpPath, args...)

	// Capture stdout for progress tracking
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	/*
	// Create debug log file
	debugFile, _ := os.OpenFile("ytdlp_debug.log", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if debugFile != nil {
		defer debugFile.Close()
		debugFile.WriteString("\n--- NEW DOWNLOAD START ---\n")
		debugFile.WriteString("URL: " + url + "\n")
	}
	*/

	// Read progress output
	scanner := bufio.NewScanner(stdout)
	
	// Custom split function to handle both \n and \r
	scanner.Split(func(data []byte, atEOF bool) (advance int, token []byte, err error) {
		if atEOF && len(data) == 0 {
			return 0, nil, nil
		}
		for i, b := range data {
			if b == '\n' || b == '\r' {
				return i + 1, data[0:i], nil
			}
		}
		if atEOF {
			return len(data), data, nil
		}
		return 0, nil, nil
	})

	for scanner.Scan() {
		line := scanner.Text()
		
		/*
		// Write EVERY line to debug log
		if debugFile != nil && line != "" {
			debugFile.WriteString(line + "\n")
		}
		*/

		if strings.Contains(line, "[download]") {
			if strings.Contains(line, "Destination:") {
				// Extract filename from "[download] Destination: /path/to/Title.mp4"
				fullPath := strings.TrimPrefix(line, "[download] Destination: ")
				title := filepath.Base(fullPath)
				// Remove extension
				if ext := filepath.Ext(title); ext != "" {
					title = title[:len(title)-len(ext)]
				}
				runtime.EventsEmit(ctx, "video-title", title)
			} else if strings.Contains(line, "has already been downloaded") {
				// Handle case where file exists: "[download] Title.mp4 has already been downloaded"
				title := strings.TrimPrefix(line, "[download] ")
				title = strings.TrimSuffix(title, " has already been downloaded")
				if ext := filepath.Ext(title); ext != "" {
					title = title[:len(title)-len(ext)]
				}
				runtime.EventsEmit(ctx, "video-title", title)
				runtime.EventsEmit(ctx, "progress-update", map[string]interface{}{
					"index":      index,
					"percentage": 100.0,
					"speed":      "Done",
					"eta":        "00:00",
				})
			} else {
				progress := parseProgress(line)
				progress["index"] = index
				// Emit even if percentage is 0 to show starting state
				runtime.EventsEmit(ctx, "progress-update", progress)
			}
		}
		if strings.Contains(line, "[Merger]") || strings.Contains(line, "[ffmpeg]") || strings.Contains(line, "[VideoConvertor]") {
			println("[DL] Post-processing:", line)
			runtime.EventsEmit(ctx, "progress-update", map[string]interface{}{
				"index":      index,
				"percentage": 100.0,
				"speed":      "Processing...",
				"eta":        "Almost done",
			})
		}
	}

	// Also read stderr for error messages
	errScanner := bufio.NewScanner(stderr)
	for errScanner.Scan() {
		println("[DL] stderr:", errScanner.Text())
	}

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("download failed: %v", err)
	}

	return nil
}

// buildDownloadArgs builds yt-dlp command arguments
func buildDownloadArgs(format, quality, savePath, ffmpegPath string) []string {
	args := []string{}

	switch format {
	case "MP4":
		if quality == "Best" || quality == "Best Quality" {
			args = append(args, "-f", "bestvideo[ext=mp4]+bestaudio[ext=m4a]/best[ext=mp4]")
		} else {
			// Map quality to height
			qualityHeight := qualityToHeight(quality)
			args = append(args, "-f",
				fmt.Sprintf("bestvideo[height<=%s][ext=mp4]+bestaudio[ext=m4a]/best[height<=%s]", qualityHeight, qualityHeight))
		}
		args = append(args, "--merge-output-format", "mp4")

	case "MP3":
		args = append(args,
			"-f", "bestaudio",
			"--extract-audio",
			"--audio-format", "mp3",
			"--audio-quality", "0",
		)
	}

	// Common arguments
	args = append(args,
		"--concurrent-fragments", strconv.Itoa(runtimepkg.NumCPU()),
		"-o", filepath.Join(savePath, "%(title)s.%(ext)s"),
	)

	if ffmpegPath != "" {
		args = append(args, "--ffmpeg-location", ffmpegPath)
	}

	return args
}

// qualityToHeight converts quality string to pixel height
func qualityToHeight(quality string) string {
	heightMap := map[string]string{
		"1080p": "1080",
		"720p":  "720",
		"480p":  "480",
		"360p":  "360",
	}
	if h, ok := heightMap[quality]; ok {
		return h
	}
	return "1080"
}

// parseProgress parses yt-dlp progress output
func parseProgress(line string) map[string]interface{} {
	progress := map[string]interface{}{
		"percentage": 0.0,
		"speed":      "0 MB/s",
		"eta":        "—",
		"raw":        line,
	}

	// Extract percentage: " 57.3%" or "100%" or "100.0%"
	rePct := regexp.MustCompile(`(\d+(?:\.\d+)?)%`)
	if match := rePct.FindStringSubmatch(line); len(match) > 1 {
		if pct, err := strconv.ParseFloat(match[1], 64); err == nil {
			progress["percentage"] = pct
		}
	}

	// Extract speed: "at 3.20MiB/s"
	reSpeed := regexp.MustCompile(`at\s+([^\s]+)`)
	if match := reSpeed.FindStringSubmatch(line); len(match) > 1 {
		progress["speed"] = match[1]
	}

	// Extract ETA: "ETA 00:43"
	reETA := regexp.MustCompile(`ETA\s+([^\s]+)`)
	if match := reETA.FindStringSubmatch(line); len(match) > 1 {
		progress["eta"] = match[1]
	}

	return progress
}

// GetVideoMetadata fetches video title and duration
func GetVideoMetadata(url string) (string, error) {
	ytdlpPath := getResourcePath("yt-dlp")
	if ytdlpPath == "" {
		return "", fmt.Errorf("yt-dlp not found")
	}

	cmd := exec.Command(ytdlpPath, "-J", "--no-warnings", url)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	var data map[string]interface{}
	if err := json.Unmarshal(output, &data); err != nil {
		return "", err
	}

	if title, ok := data["title"].(string); ok {
		return title, nil
	}

	return "", fmt.Errorf("could not extract title")
}

// GetPlaylistVideos extracts all videos from a playlist
func GetPlaylistVideos(url string) ([]string, error) {
	ytdlpPath := getResourcePath("yt-dlp")
	if ytdlpPath == "" {
		return nil, fmt.Errorf("yt-dlp not found")
	}

	cmd := exec.Command(ytdlpPath, "--flat-playlist", "-J", url)
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var data map[string]interface{}
	if err := json.Unmarshal(output, &data); err != nil {
		return nil, err
	}

	var videos []string
	if entries, ok := data["entries"].([]interface{}); ok {
		for _, entry := range entries {
			if e, ok := entry.(map[string]interface{}); ok {
				if id, ok := e["id"].(string); ok {
					videos = append(videos, "https://www.youtube.com/watch?v="+id)
				}
			}
		}
	}

	return videos, nil
}

// getResourcePath finds binary in bundle or system paths
func getResourcePath(name string) string {
	// Try bundled resources first
	execPath, err := os.Executable()
	if err == nil {
		// For .app bundle: ../Resources/
		bundled := filepath.Join(filepath.Dir(execPath), "..", "Resources", name)
		if info, err := os.Stat(bundled); err == nil && !info.IsDir() {
			return bundled
		}

		// Also check current directory (for debug)
		localPath := filepath.Join(filepath.Dir(execPath), "resources", name)
		if info, err := os.Stat(localPath); err == nil && !info.IsDir() {
			return localPath
		}
	}

	// Check common system paths
	for _, p := range []string{
		"/opt/homebrew/bin/" + name,
		"/usr/local/bin/" + name,
		"/usr/bin/" + name,
	} {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p
		}
	}

	// Last resort: try system PATH
	if path, err := exec.LookPath(name); err == nil {
		return path
	}

	return ""
}

// SanitizeFilename removes invalid characters from filename
func SanitizeFilename(filename string) string {
	invalidChars := []string{"/", ":", "*", "?", "\"", "<", ">", "|"}
	result := filename
	for _, char := range invalidChars {
		result = strings.ReplaceAll(result, char, "_")
	}
	return result
}

// OpenFileInFinder opens the file in Finder and highlights it
func OpenFileInFinder(filePath string) error {
	cmd := exec.Command("open", "-R", filePath)
	return cmd.Run()
}

// GetDefaultSavePath returns default download folder
func GetDefaultSavePath() string {
	usr, err := user.Current()
	if err != nil {
		return "~/Downloads"
	}
	return filepath.Join(usr.HomeDir, "Downloads")
}

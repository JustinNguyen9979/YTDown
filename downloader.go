package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	runtimepkg "runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

var logFile *os.File
var logWriter *bufio.Writer
var logMutex sync.Mutex

type VideoInfo struct {
	Title     string `json:"title"`
	Thumbnail string `json:"thumbnail"`
	ID        string `json:"id"`
}

// initLogger initializes the logging system
func initLogger() error {
	currentUser, err := user.Current()
	if err != nil {
		return err
	}

	logDir := filepath.Join(currentUser.HomeDir, ".ytdown", "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return err
	}

	logPath := filepath.Join(logDir, "ytdown.log")
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}

	logFile = file
	logWriter = bufio.NewWriter(file)

	writeLog("INFO", "Logger initialized at: "+logPath)

	return nil
}

// writeLog writes a formatted log entry (thread-safe)
func writeLog(level, message string) {
	logMutex.Lock()
	defer logMutex.Unlock()

	if logWriter == nil {
		fmt.Printf("[%s] %s\n", level, message)
		return
	}

	timestamp := time.Now().Format("2006-01-02 15:04:05.000")
	logEntry := fmt.Sprintf("[%s] [%s] %s\n", timestamp, level, message)

	logWriter.WriteString(logEntry)
	logWriter.Flush()

	// Also print to console
	fmt.Print(logEntry)
}

// writeLogf writes a formatted log entry with printf (thread-safe)
func writeLogf(level, format string, args ...interface{}) {
	message := fmt.Sprintf(format, args...)
	writeLog(level, message)
}

// closeLogger closes the log file (thread-safe)
func closeLogger() {
	logMutex.Lock()
	defer logMutex.Unlock()

	if logWriter != nil {
		logWriter.Flush()
	}
	if logFile != nil {
		logFile.Close()
	}
}

// DownloadVideo downloads a video using yt-dlp
func DownloadVideo(ctx context.Context, index int, url, format, quality, savePath string) error {
	ytdlpPath := getResourcePath("yt-dlp")
	ffmpegPath := getResourcePath("ffmpeg")

	if ytdlpPath == "" {
		return fmt.Errorf("yt-dlp not found. Please install it or use the Setup Dependencies button.")
	}

	// Fetch metadata first to get title, thumbnail, and ID
	info, _ := GetVideoMetadata(url)
	if info != nil {
		runtime.EventsEmit(ctx, "video-info", map[string]interface{}{
			"index":     index,
			"title":     info.Title,
			"thumbnail": info.Thumbnail,
			"id":        info.ID,
		})
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

	// Read progress output
	scanner := bufio.NewScanner(stdout)

	// Set a larger buffer for the scanner (up to 1MB) to handle long output lines
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

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

	var finalFilePath string

	for scanner.Scan() {
		line := scanner.Text()

		if strings.Contains(line, "[download]") {
			if strings.Contains(line, "Destination:") {
				// Extract filename from "[download] Destination: /path/to/Title.mp4"
				fullPath := strings.TrimSpace(strings.TrimPrefix(line, "[download] Destination: "))
				finalFilePath = fullPath
				title := filepath.Base(fullPath)
				// Remove extension
				if ext := filepath.Ext(title); ext != "" {
					title = title[:len(title)-len(ext)]
				}
				runtime.EventsEmit(ctx, "video-title", title)
			} else if strings.Contains(line, "has already been downloaded") {
				// Handle case where file exists: "[download] /path/to/Title.mp4 has already been downloaded"
				fullPath := strings.TrimSpace(strings.TrimPrefix(line, "[download] "))
				fullPath = strings.TrimSuffix(fullPath, " has already been downloaded")
				finalFilePath = fullPath
				title := filepath.Base(fullPath)
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
			// For merger, extract the final file path if available
			if strings.Contains(line, "Merging formats into \"") {
				re := regexp.MustCompile(`Merging formats into "([^"]+)"`)
				if match := re.FindStringSubmatch(line); len(match) > 1 {
					finalFilePath = match[1]
				}
			}
			runtime.EventsEmit(ctx, "progress-update", map[string]interface{}{
				"index":      index,
				"percentage": 100.0,
				"speed":      "Processing...",
				"eta":        "Almost done",
			})
		}
	}

	if err := scanner.Err(); err != nil {
		println("[DL] scanner error:", err.Error())
	}

	// Also read stderr for error messages
	var stderrOutput strings.Builder
	errScanner := bufio.NewScanner(stderr)
	for errScanner.Scan() {
		line := errScanner.Text()
		println("[DL] stderr:", line)
		stderrOutput.WriteString(line + "\n")
	}

	if err := cmd.Wait(); err != nil {
		if stderrOutput.Len() > 0 {
			return fmt.Errorf("download failed: %s", stderrOutput.String())
		}
		return fmt.Errorf("download failed: %v", err)
	}

	// If we have a final file path, emit it as complete
	if finalFilePath != "" {
		runtime.EventsEmit(ctx, "download-complete", map[string]interface{}{
			"index":    index,
			"filePath": finalFilePath,
		})
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
		"--no-playlist",
		"--concurrent-fragments", strconv.Itoa(runtimepkg.NumCPU()),
		"-o", filepath.Join(savePath, "%(title)s.%(ext)s"),
	)

	if ffmpegPath != "" {
		args = append(args, "--ffmpeg-location", ffmpegPath)
	}

	if cookiePath := getTemporaryCookieFile(); cookiePath != "" {
		args = append(args, "--cookies", cookiePath)
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

// GetVideoMetadata fetches video title, thumbnail and ID
func GetVideoMetadata(url string) (*VideoInfo, error) {
	writeLogf("INFO", "GetVideoMetadata called with URL: %s", url)

	ytdlpPath := getResourcePath("yt-dlp")
	if ytdlpPath == "" {
		writeLog("ERROR", "yt-dlp path not found")
		return nil, fmt.Errorf("yt-dlp not found")
	}
	writeLogf("DEBUG", "yt-dlp path: %s", ytdlpPath)

	// Get title, thumbnail URL, and ID from yt-dlp
	args := []string{"--get-title", "--get-thumbnail", "--get-id", "--no-warnings"}
	if cookiePath := getTemporaryCookieFile(); cookiePath != "" {
		args = append(args, "--cookies", cookiePath)
		writeLog("DEBUG", "Using cookie file for authentication")
	}
	args = append(args, url)

	writeLogf("DEBUG", "Running yt-dlp with %d args: %v", len(args), args)

	cmd := exec.Command(ytdlpPath, args...)

	// Capture both stdout and stderr
	output, err := cmd.CombinedOutput()

	if err != nil {
		writeLogf("ERROR", "yt-dlp execution error: %v", err)
		writeLogf("ERROR", "yt-dlp output: %s", string(output))
		return nil, err
	}

	outputStr := strings.TrimSpace(string(output))
	writeLogf("DEBUG", "yt-dlp raw output length: %d bytes", len(outputStr))
	writeLogf("DEBUG", "yt-dlp raw output: %s", outputStr)

	lines := strings.Split(outputStr, "\n")
	writeLogf("DEBUG", "yt-dlp returned %d lines", len(lines))

	if len(lines) < 3 {
		writeLogf("ERROR", "Expected 3+ lines, got %d", len(lines))
		for i, line := range lines {
			writeLogf("DEBUG", "Line %d (%d chars): %s", i, len(line), line)
		}
		return nil, fmt.Errorf("could not extract title, thumbnail or ID")
	}

	title := strings.TrimSpace(lines[0])
	videoID := strings.TrimSpace(lines[1])
	thumbnailURL := strings.TrimSpace(lines[2])

	writeLogf("DEBUG", "Extracted - Title: %s", title)
	writeLogf("DEBUG", "Extracted - VideoID: %s", videoID)
	writeLogf("DEBUG", "Extracted - Thumbnail URL: %s", thumbnailURL)

	// Download thumbnail and convert to base64 data URL
	dataURL := downloadThumbnailAsBase64(thumbnailURL)

	if dataURL == "" {
		writeLog("WARN", "Failed to download thumbnail, returning empty string")
	} else {
		writeLogf("DEBUG", "Successfully converted thumbnail to data URL (size: %d chars)", len(dataURL))
	}

	return &VideoInfo{
		Title:     title,
		Thumbnail: dataURL,
		ID:        videoID,
	}, nil
}

// downloadThumbnailAsBase64 downloads thumbnail and returns as base64 data URL
func downloadThumbnailAsBase64(thumbnailURL string) string {
	if thumbnailURL == "" {
		writeLog("ERROR", "Thumbnail URL is empty")
		return ""
	}

	writeLogf("DEBUG", "Starting thumbnail download from: %s", thumbnailURL)

	// Download thumbnail with timeout
	client := &http.Client{Timeout: 10 * time.Second}
	startTime := time.Now()

	resp, err := client.Get(thumbnailURL)
	if err != nil {
		writeLogf("ERROR", "Failed to download thumbnail: %v", err)
		return ""
	}
	defer resp.Body.Close()

	writeLogf("DEBUG", "HTTP Status: %d (took %.2fs)", resp.StatusCode, time.Since(startTime).Seconds())

	if resp.StatusCode != http.StatusOK {
		writeLogf("ERROR", "Thumbnail download returned status: %d", resp.StatusCode)
		return ""
	}

	// Read thumbnail bytes
	thumbnailData, err := io.ReadAll(resp.Body)
	if err != nil {
		writeLogf("ERROR", "Failed to read thumbnail data: %v", err)
		return ""
	}

	writeLogf("DEBUG", "Downloaded %d bytes", len(thumbnailData))

	// Determine MIME type from URL
	mimeType := "image/jpeg"
	if strings.Contains(strings.ToLower(thumbnailURL), ".png") {
		mimeType = "image/png"
	} else if strings.Contains(strings.ToLower(thumbnailURL), ".webp") {
		mimeType = "image/webp"
	}
	writeLogf("DEBUG", "Detected MIME type: %s", mimeType)

	// Convert to base64 data URL
	encodeStart := time.Now()
	encoded := encodeBase64(thumbnailData)
	writeLogf("DEBUG", "Base64 encoding took %.2fs", time.Since(encodeStart).Seconds())

	dataURL := fmt.Sprintf("data:%s;base64,%s", mimeType, encoded)

	writeLogf("INFO", "Successfully converted thumbnail to data URL (%d chars, %.2fs total)", len(dataURL), time.Since(startTime).Seconds())
	return dataURL
}

// encodeBase64 encodes bytes to base64 string
func encodeBase64(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}

// GetPlaylistVideos extracts all videos from a playlist
func GetPlaylistVideos(url string) ([]string, error) {
	ytdlpPath := getResourcePath("yt-dlp")
	if ytdlpPath == "" {
		return nil, fmt.Errorf("yt-dlp not found")
	}

	args := []string{"--flat-playlist", "-J"}
	if cookiePath := getTemporaryCookieFile(); cookiePath != "" {
		args = append(args, "--cookies", cookiePath)
	}
	args = append(args, url)

	cmd := exec.Command(ytdlpPath, args...)
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

// getResourcePath finds binary in bundle, user app support, or system paths
func getResourcePath(name string) string {
	// 1. Try bundled resources first (for .app distribution)
	execPath, err := os.Executable()
	if err == nil {
		// For .app bundle: ../Resources/
		bundled := filepath.Join(filepath.Dir(execPath), "..", "Resources", name)
		if info, err := os.Stat(bundled); err == nil && !info.IsDir() {
			return bundled
		}

		// Also check current directory (for debug/dev)
		localPath := filepath.Join(filepath.Dir(execPath), "resources", name)
		if info, err := os.Stat(localPath); err == nil && !info.IsDir() {
			return localPath
		}
	}

	// 2. Try common system paths (Homebrew, etc.)
	for _, p := range []string{
		"/opt/homebrew/bin/" + name,
		"/usr/local/bin/" + name,
		"/usr/bin/" + name,
	} {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p
		}
	}

	// 3. Last resort: try system PATH
	if path, err := exec.LookPath(name); err == nil {
		return path
	}

	return ""
}

type DownloadFailure struct {
	RequiresCookie bool
	DisplayMessage string
	Details        []string
}

func classifyDownloadFailure(err error, cookiePresent bool) DownloadFailure {
	if err == nil {
		return DownloadFailure{}
	}

	message := strings.TrimSpace(err.Error())
	lower := strings.ToLower(message)
	requiresCookie := looksLikeRestrictedAuthError(lower)

	details := []string{}
	if requiresCookie {
		details = append(details, "Restricted video or login required.")
		if cookiePresent {
			details = append(details, "Cookie invalid or insufficient.")
			details = append(details, "Please copy a fresh YouTube cookie.")
		} else {
			details = append(details, "Temporary cookie required.")
			details = append(details, "Paste a YouTube Cookie header to retry.")
		}
		return DownloadFailure{
			RequiresCookie: true,
			DisplayMessage: "Error",
			Details:        details,
		}
	}

	details = append(details, "Download failed.")
	details = append(details, summarizeErrorForUI(message))
	return DownloadFailure{
		RequiresCookie: false,
		DisplayMessage: "Error",
		Details:        details,
	}
}

func looksLikeRestrictedAuthError(message string) bool {
	patterns := []string{
		"sign in to confirm your age",
		"login required",
		"members-only",
		"private video",
		"private video. sign in",
		"confirm you're not a bot",
		"use --cookies",
		"this video is private",
		"authentication required",
		"sign in",
		"http error 403",
		"requested content is not available",
	}

	for _, pattern := range patterns {
		if strings.Contains(message, pattern) {
			return true
		}
	}

	return false
}

func summarizeErrorForUI(message string) string {
	message = strings.TrimSpace(message)
	if message == "" {
		return "Unknown yt-dlp error."
	}

	lines := strings.Split(message, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		line = strings.TrimPrefix(line, "ERROR: ")
		line = strings.TrimPrefix(line, "download failed: ")
		if len(line) > 120 {
			line = line[:117] + "..."
		}
		return line
	}

	return "Unknown yt-dlp error."
}

func IsRestrictedAuthError(err error) bool {
	if err == nil {
		return false
	}

	return looksLikeRestrictedAuthError(strings.ToLower(err.Error())) || errors.Is(err, os.ErrPermission)
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

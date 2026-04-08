package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
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

type VideoInfo struct {
	Title     string `json:"title"`
	Thumbnail string `json:"thumbnail"`
}

// DownloadVideo downloads a video using yt-dlp
func DownloadVideo(ctx context.Context, index int, url, format, quality, savePath string) error {
	ytdlpPath := getResourcePath("yt-dlp")
	ffmpegPath := getResourcePath("ffmpeg")

	if ytdlpPath == "" {
		return fmt.Errorf("yt-dlp not found. Please install it or use the Setup Dependencies button.")
	}

	// Fetch metadata first to get title and thumbnail
	info, _ := GetVideoMetadata(url)
	if info != nil {
		runtime.EventsEmit(ctx, "video-info", map[string]interface{}{
			"index":     index,
			"title":     info.Title,
			"thumbnail": info.Thumbnail,
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

	for scanner.Scan() {
		line := scanner.Text()

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

// GetVideoMetadata fetches video title and thumbnail
func GetVideoMetadata(url string) (*VideoInfo, error) {
	ytdlpPath := getResourcePath("yt-dlp")
	if ytdlpPath == "" {
		return nil, fmt.Errorf("yt-dlp not found")
	}

	args := []string{"--get-title", "--get-thumbnail", "--no-warnings"}
	if cookiePath := getTemporaryCookieFile(); cookiePath != "" {
		args = append(args, "--cookies", cookiePath)
	}
	args = append(args, url)

	cmd := exec.Command(ytdlpPath, args...)
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) < 2 {
		return nil, fmt.Errorf("could not extract title or thumbnail")
	}

	return &VideoInfo{
		Title:     strings.TrimSpace(lines[0]),
		Thumbnail: strings.TrimSpace(lines[1]),
	}, nil
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

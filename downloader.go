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
	"strconv"
	"strings"
	"time"

	platform "ytdown/flatform"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type VideoInfo struct {
	Title     string `json:"title"`
	Thumbnail string `json:"thumbnail"`
	ID        string `json:"id"`
}

// ResolveShortURL follows redirects to find the final URL for short links
func ResolveShortURL(url string, userAgent string) string {
	// Only resolve links that are known to be shorteners
	if !strings.Contains(url, "xhslink.com") && !strings.Contains(url, "vt.tiktok.com") && !strings.Contains(url, "v.douyin.com") {
		return url
	}

	fmt.Printf("[URL] 🔍 Resolving short URL: %s using UA: %s\n", url, userAgent)

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Stop if we are being redirected to a login page
			if strings.Contains(req.URL.Path, "/login") || strings.Contains(req.URL.Host, "login") {
				return http.ErrUseLastResponse
			}
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
		Timeout: 10 * time.Second,
	}

	// Use the provided browser User-Agent
	ua := userAgent
	if ua == "" {
		ua = GetUserAgent("")
	}

	// Try GET instead of HEAD because some shorteners (like XHS) behave differently with HEAD
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		fmt.Printf("[URL] ❌ Failed to create request: %v\n", err)
		return url
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8")

	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("[URL] ❌ Resolution failed: %v\n", err)
		return url
	}
	defer resp.Body.Close()

	if resp.Request != nil && resp.Request.URL != nil {
		finalURL := resp.Request.URL.String()
		if finalURL != url {
			fmt.Printf("[URL] ✅ Resolved to: %s (Status: %d)\n", finalURL, resp.StatusCode)
			LogInfo("[URL] Resolved %s to %s", url, finalURL)
			return finalURL
		}
	}

	fmt.Printf("[URL] ⚠️  No redirect found for %s\n", url)
	return url
}

// DownloadVideo downloads a video using yt-dlp
func DownloadVideo(ctx context.Context, index int, url, format, quality, savePath string, noPlaylist bool, concurrentFragments int) error {
	ytdlpPath := getResourcePath("yt-dlp")
	ffmpegPath := getResourcePath("ffmpeg")

	if ytdlpPath == "" {
		return fmt.Errorf("yt-dlp not found. Please install it or use the Setup Dependencies button.")
	}

	if ffmpegPath == "" {
		LogError("[DL] ffmpeg not found in brew or system paths. Merging will likely fail.")
	} else {
		LogInfo("[DL] Using system ffmpeg from: %s", ffmpegPath)
	}

	// Fetch metadata first to get title, thumbnail, and ID
	info, _ := GetVideoMetadata(ctx, url)
	if info != nil {
		runtime.EventsEmit(ctx, "video-info", map[string]interface{}{
			"index":     index,
			"title":     info.Title,
			"thumbnail": info.Thumbnail,
			"id":        info.ID,
		})
	}

	// Build yt-dlp arguments based on format and quality
	args := buildDownloadArgs(ctx, url, format, quality, savePath, ffmpegPath, noPlaylist, concurrentFragments)
	args = append(args, url)
	// Add verbose for better debugging in app.log
	// args = append(args, "--verbose")

	LogInfo("[DL] Running command: %s with %d args: %v", ytdlpPath, len(args), args)

	cmd := platform.CommandContext(ctx, ytdlpPath, args...)

	// Ensure /opt/homebrew/bin and other common paths are in PATH so yt-dlp can find ffmpeg, deno, etc.
	cmd.Env = os.Environ()
	if platformManager != nil && platformManager.OSName() == "Windows" {
		cmd.Env = append(cmd.Env, "PYTHONIOENCODING=utf-8")
	}

	// CombinedOutput is simpler but we need to stream progress,
	// so we'll pipe stderr to stdout to avoid buffer deadlocks.

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		return err
	}

	// Read combined progress output
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
	var lastError string

	for scanner.Scan() {
		line := scanner.Text()

		// fmt.Println("[yt-dlp]", line)

		// Log every line from yt-dlp for debugging, but prefix it to distinguish
		// We'll use LogDebug for regular progress to avoid cluttering INFO if needed,
		// but since we want to catch "hangs", INFO is safer for now.
		if strings.Contains(line, "ERROR:") || strings.Contains(line, "WARNING:") {
			LogError("[yt-dlp] %s", line)
			// ← THÊM: chỉ lưu ERROR, bỏ qua WARNING
			if strings.Contains(line, "ERROR:") {
				lastError = strings.TrimSpace(strings.TrimPrefix(line, "ERROR: "))
			}
		} else {
			// Only log important transitions or status updates to app.log to keep it readable
			// but enough to see where it "hangs"
			if strings.Contains(line, "[ExtractAudio]") && strings.Contains(line, "Destination:") {
				fullPath := strings.TrimSpace(strings.TrimPrefix(line, "[ExtractAudio] Destination: "))
				finalFilePath = fullPath // Override bằng file audio thật
				title := filepath.Base(fullPath)
				if ext := filepath.Ext(title); ext != "" {
					title = title[:len(title)-len(ext)]
				}
				runtime.EventsEmit(ctx, "video-title", map[string]interface{}{
					"index": index,
					"title": title,
				})
			}
		}

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
				runtime.EventsEmit(ctx, "video-title", map[string]interface{}{
					"index": index,
					"title": title,
				})

			} else if strings.Contains(line, "has already been downloaded") {
				// Handle case where file exists: "[download] /path/to/Title.mp4 has already been downloaded"
				fullPath := strings.TrimSpace(strings.TrimPrefix(line, "[download] "))
				fullPath = strings.TrimSuffix(fullPath, " has already been downloaded")
				finalFilePath = fullPath
				title := filepath.Base(fullPath)
				if ext := filepath.Ext(title); ext != "" {
					title = title[:len(title)-len(ext)]
				}
				runtime.EventsEmit(ctx, "video-title", map[string]interface{}{
					"index": index,
					"title": title,
				})
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
		LogError("[DL] scanner error: %v", err)
	}

	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			LogInfo("[DL] Download cancelled by user")
			return ctx.Err()
		}
		LogError("[DL] Command failed with error: %v", err)

		if lastError != "" {
			lowerErr := strings.ToLower(lastError)
			if strings.Contains(lowerErr, "could not copy") && strings.Contains(lowerErr, "cookie database") {
				return fmt.Errorf("Không thể sao chép cookie từ Chrome/Edge (do trình duyệt đang mở và khóa file). Bạn nên đóng hoàn toàn trình duyệt Chrome/Edge, chuyển sang dùng Firefox, hoặc dùng 'Cookie thủ công'.")
			}
			return fmt.Errorf("download failed: %s", lastError)
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
func buildDownloadArgs(ctx context.Context, url, format, quality, savePath, ffmpegPath string, noPlaylist bool, concurrentFragments int) []string {
	args := []string{}

	switch format {

	case "MP4":
		if quality == "Best" || quality == "Best Quality" {
			// Ưu tiên stream mp4+m4a native → không cần re-encode
			// Fallback: bất kỳ bestvideo+bestaudio rồi merge sang mp4
			args = append(args, "-f",
				"bestvideo[ext=mp4]+bestaudio[ext=m4a]/bestvideo+bestaudio/best")
		} else {
			h := qualityToHeight(quality)
			args = append(args, "-f", fmt.Sprintf(
				"bestvideo[ext=mp4][height<=%s]+bestaudio[ext=m4a]/bestvideo[height<=%s]+bestaudio/best[height<=%s]",
				h, h, h))
		}
		args = append(args, "--merge-output-format", "mp4")
		// Nếu vẫn dính VP9/Opus thì recode → đảm bảo mọi player đọc được
		args = append(args, "--recode-video", "mp4")

	case "MKV":
		// MKV chứa được mọi codec — không cần ưu tiên ext, không cần recode
		if quality == "Best" || quality == "Best Quality" {
			args = append(args, "-f", "bestvideo+bestaudio/best")
		} else {
			h := qualityToHeight(quality)
			args = append(args, "-f", fmt.Sprintf(
				"bestvideo[height<=%s]+bestaudio/best[height<=%s]", h, h))
		}
		args = append(args, "--merge-output-format", "mkv")

	case "WEBM":
		if quality == "Best" || quality == "Best Quality" {
			// Ưu tiên VP9+Opus (webm native) → không re-encode
			// Fallback: bất kỳ stream → bắt buộc recode sang VP9/Opus
			args = append(args, "-f",
				"bestvideo[ext=webm]+bestaudio[ext=webm]/bestvideo[vcodec^=vp]+bestaudio/bestvideo+bestaudio/best")
		} else {
			h := qualityToHeight(quality)
			args = append(args, "-f", fmt.Sprintf(
				"bestvideo[ext=webm][height<=%s]+bestaudio[ext=webm]/bestvideo[vcodec^=vp][height<=%s]+bestaudio/bestvideo[height<=%s]+bestaudio/best[height<=%s]",
				h, h, h, h))
		}
		args = append(args, "--merge-output-format", "webm")
		// BẮT BUỘC: H264/AAC không hợp lệ trong container .webm → phải recode
		args = append(args, "--recode-video", "webm")

	case "MP3":
		args = append(args,
			"-f", "bestaudio/best",
			"--extract-audio",
			"--audio-format", "mp3",
			"--audio-quality", "0",
		)

	case "WAV":
		args = append(args,
			"-f", "bestaudio/best",
			"--extract-audio",
			"--audio-format", "wav",
		)

	case "FLAC":
		args = append(args,
			"-f", "bestaudio/best",
			"--extract-audio",
			"--audio-format", "flac",
		)

	case "M4A":
		args = append(args,
			"-f", "bestaudio[ext=m4a]/bestaudio/best",
			"--extract-audio",
			"--audio-format", "m4a",
			"--audio-quality", "0",
		)
	}

	if concurrentFragments < 1 {
		concurrentFragments = 2 // default value if invalid
	}

	// Common flags
	args = append(args,
		"--concurrent-fragments", strconv.Itoa(concurrentFragments),
		"--no-continue",
		"--force-overwrites",
		"--windows-filenames",
	)

	// Playlist mode và output template
	playlistFlag := "--no-playlist"
	outputTemplate := filepath.Join(savePath, "%(title)s [%(id)s].%(ext)s")

	if !noPlaylist {
		playlistFlag = "--yes-playlist"
		outputTemplate = filepath.Join(savePath, "%(playlist_title)s", "%(playlist_index)s - %(title)s [%(id)s].%(ext)s")
	}

	args = append(args, playlistFlag, "-o", outputTemplate)

	// Set appropriate referer
	if IsXiaohongshu(url) {
		args = append(args, "--referer", "https://www.xiaohongshu.com/")
	} else {
		args = append(args, "--referer", "https://www.youtube.com/")
	}

	if ffmpegPath != "" {
		args = append(args, "--ffmpeg-location", ffmpegPath)
	}

	// Get cookie and browser settings
	manager.mu.RLock()
	cookieMode := manager.config.Mode
	manager.mu.RUnlock()

	if cookieMode != CookieModeBrowser {
		manager.mu.RLock()
		selectedBrowser := manager.config.SelectedBrowser
		manager.mu.RUnlock()

		var userAgent string
		if cookieMode == CookieModeManual {
			userAgent = GetUserAgent(selectedBrowser)
		} else {
			userAgent = GetUserAgent("")
		}
		args = append(args, "--user-agent", userAgent)
	}

	cookieArgs := manager.GetCookieArgs(ctx, "yt-dlp", url)
	if len(cookieArgs) > 0 {
		args = append(args, cookieArgs...)
		// Thêm player client phù hợp cho YouTube age-restricted khi có cookie
		if strings.Contains(url, "youtube.com") || strings.Contains(url, "youtu.be") {
			args = append(args, "--extractor-args", "youtube:player_client=default,web_creator")
		}
	}

	return args
}

// qualityToHeight converts quality string to pixel height
func qualityToHeight(quality string) string {
	heightMap := map[string]string{
		"4320p": "4320",
		"2880p": "2880",
		"2160p": "2160",
		"1440p": "1440",
		"1080p": "1080",
		"720p":  "720",
		"480p":  "480",
		"360p":  "360",
		"240p":  "240",
		"144p":  "144",
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
func GetVideoMetadata(ctx context.Context, url string) (*VideoInfo, error) {
	ytdlpPath := getResourcePath("yt-dlp")
	if ytdlpPath == "" {
		return nil, fmt.Errorf("yt-dlp not found")
	}

	// Use -J for JSON output which is much more reliable than line-based output
	args := []string{"-J", "--no-warnings", "--no-playlist"}

	// Add referer for Xiaohongshu
	if IsXiaohongshu(url) {
		args = append(args, "--referer", "https://www.xiaohongshu.com/")
	}

	if cookieArgs := manager.GetCookieArgs(ctx, "yt-dlp", url); len(cookieArgs) > 0 {
		args = append(args, cookieArgs...)
	}
	args = append(args, url)

	LogDebug("[Metadata] Fetching JSON metadata for %s", url)
	cmd := platform.CommandContext(ctx, ytdlpPath, args...)

	var stderrBuf strings.Builder
	cmd.Stderr = &stderrBuf

	// Capture only stdout for the JSON data
	output, err := cmd.Output()
	if err != nil {
		// Stderr đã được capture sẵn, log trực tiếp không cần chạy lại yt-dlp
		LogError("[Metadata] yt-dlp failed: %v, stderr: %s", err, stderrBuf.String())
		return nil, err
	}

	var data map[string]interface{}
	if err := json.Unmarshal(output, &data); err != nil {
		LogError("[Metadata] JSON unmarshal failed: %v", err)
		return nil, err
	}

	title, _ := data["title"].(string)
	videoID, _ := data["id"].(string)
	thumbnailURL, _ := data["thumbnail"].(string)

	// Some extractors use "thumbnails" array instead of a single "thumbnail" field
	if thumbnailURL == "" {
		if thumbnails, ok := data["thumbnails"].([]interface{}); ok && len(thumbnails) > 0 {
			if lastThumb, ok := thumbnails[len(thumbnails)-1].(map[string]interface{}); ok {
				thumbnailURL, _ = lastThumb["url"].(string)
			}
		}
	}

	LogInfo("[Metadata] ✅ Found metadata: Title=%s, ID=%s, Thumbnail=%s", title, videoID, thumbnailURL)

	// Download thumbnail with proper headers/cookies
	dataURL := downloadThumbnailAsBase64(ctx, thumbnailURL, url)

	return &VideoInfo{
		Title:     title,
		Thumbnail: dataURL,
		ID:        videoID,
	}, nil
}

// downloadThumbnailAsBase64 downloads thumbnail and returns as base64 data URL
func downloadThumbnailAsBase64(ctx context.Context, thumbnailURL string, url string) string {
	if thumbnailURL == "" {
		return ""
	}

	// Determine if it's Xiaohongshu to apply specific headers
	isXHS := IsXiaohongshu(url)

	manager.mu.RLock()
	selectedBrowser := manager.config.SelectedBrowser
	manager.mu.RUnlock()
	userAgent := GetUserAgent(selectedBrowser)

	// Download thumbnail with timeout
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "GET", thumbnailURL, nil)
	if err != nil {
		return ""
	}

	// Set Headers
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/svg+xml,image/*,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9,vi;q=0.8")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Pragma", "no-cache")
	req.Header.Set("Sec-Fetch-Dest", "image")
	req.Header.Set("Sec-Fetch-Mode", "no-cors")
	req.Header.Set("Sec-Fetch-Site", "cross-site")

	if isXHS {
		req.Header.Set("Referer", "https://www.xiaohongshu.com/")
		// Get web_session from cache if available
		manager.state.mu.RLock()
		xhsSession := manager.state.xhsSession
		manager.state.mu.RUnlock()

		if xhsSession != "" {
			req.Header.Set("Cookie", "web_session="+xhsSession)
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		LogError("[Thumbnail] ❌ Connection error: %v", err)
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		LogError("[Thumbnail] ❌ Failed to download thumbnail: %d %s", resp.StatusCode, thumbnailURL)
		return ""
	}

	// Read thumbnail bytes
	thumbnailData, err := io.ReadAll(resp.Body)
	if err != nil {
		LogError("[Thumbnail] ❌ Read error: %v", err)
		return ""
	}

	if len(thumbnailData) < 100 {
		LogError("[Thumbnail] ❌ Thumbnail data too small: %d bytes", len(thumbnailData))
		return ""
	}

	// Determine MIME type
	mimeType := "image/jpeg"
	lowerURL := strings.ToLower(thumbnailURL)
	if strings.Contains(lowerURL, ".png") {
		mimeType = "image/png"
	} else if strings.Contains(lowerURL, ".webp") {
		mimeType = "image/webp"
	} else if strings.Contains(lowerURL, ".avif") {
		mimeType = "image/avif"
	}

	// Convert to base64 data URL
	encoded := base64.StdEncoding.EncodeToString(thumbnailData)
	return fmt.Sprintf("data:%s;base64,%s", mimeType, encoded)
}

// encodeBase64 encodes bytes to base64 string
func encodeBase64(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}

// GetPlaylistVideos extracts all videos from a playlist
func GetPlaylistVideos(ctx context.Context, url string) ([]string, error) {
	ytdlpPath := getResourcePath("yt-dlp")
	if ytdlpPath == "" {
		return nil, fmt.Errorf("yt-dlp not found")
	}

	args := []string{"--flat-playlist", "-J"}
	if cookieArgs := manager.GetCookieArgs(ctx, "yt-dlp", url); len(cookieArgs) > 0 {
		args = append(args, cookieArgs...)
	}
	args = append(args, url)

	cmd := platform.CommandContext(ctx, ytdlpPath, args...)
	// Khai báo buffer cho stderr TRƯỚC khi chạy
	var stderrBuf strings.Builder
	cmd.Stderr = &stderrBuf // ← Gắn vào cmd thay vì tạo process mới

	output, err := cmd.Output()
	if err != nil {
		// Dùng stderrBuf đã capture, không cần process thứ 2
		LogError("[Metadata] yt-dlp failed: %v, stderr: %s", err, stderrBuf.String())
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
				if webpageURL, ok := e["webpage_url"].(string); ok && webpageURL != "" {
					videos = append(videos, webpageURL)
				} else if id, ok := e["id"].(string); ok {
					videos = append(videos, "https://www.youtube.com/watch?v="+id)
				}
			}
		}
	}

	return videos, nil
}

// getResourcePath finds binary using the global platform manager
func getResourcePath(name string) string {
	if platformManager != nil {
		return platformManager.GetBinaryPath(name)
	}

	// Fallback (tests): check các đường dẫn phổ biến
	candidates := []string{
		"/opt/homebrew/bin/" + name, // macOS (Apple Silicon)
		"/usr/local/bin/" + name,    // macOS (Intel) / Linux
		"/usr/bin/" + name,          // Linux
	}

	if appData := os.Getenv("APPDATA"); appData != "" {
		candidates = append(candidates,
			filepath.Join(appData, "YTDown", "bin", name+".exe"),
		)
	}

	if userProfile := os.Getenv("USERPROFILE"); userProfile != "" {
		candidates = append(candidates,
			filepath.Join(userProfile, "scoop", "shims", name+".exe"),
		)
	}
	candidates = append(candidates,
		filepath.Join("C:\\ProgramData\\chocolatey\\bin", name+".exe"),
	)

	for _, p := range candidates {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p
		}
	}

	if path, err := exec.LookPath(name); err == nil {
		return path
	}

	if path, err := exec.LookPath(name + ".exe"); err == nil {
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

	if strings.Contains(lower, "could not copy") && strings.Contains(lower, "cookie database") {
		return DownloadFailure{
			RequiresCookie: false,
			DisplayMessage: "Lỗi khóa Cookie Chrome/Edge",
			Details: []string{
				"Trình duyệt Chrome/Edge đang chạy và khóa file dữ liệu cookie.",
				"Bạn nên tắt hoàn toàn Chrome/Edge, sử dụng trình duyệt Firefox (không bị khóa file), hoặc đổi sang dùng chế độ 'Cookie thủ công'.",
			},
		}
	}

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

	lower := strings.ToLower(message)
	if strings.Contains(lower, "could not copy") && strings.Contains(lower, "cookie database") {
		return "Không thể sao chép cookie từ Chrome/Edge (do trình duyệt đang mở và khóa file). Bạn nên đóng Chrome, dùng Firefox, hoặc dùng 'Cookie thủ công'."
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
	switch platformManager.OSName() {
	case "macOS":
		return exec.Command("open", "-R", filePath).Run() // reveal in Finder
	default:
		return platformManager.OpenFolder(filepath.Dir(filePath)) // Linux: xdg-open, Windows: explorer
	}
}

func GetDefaultSavePath() string {
	// ✅ Dùng platformManager nếu có
	if platformManager != nil {
		return platformManager.GetDownloadDir()
	}
	// Fallback khi chạy tests (platformManager chưa khởi tạo)
	usr, err := user.Current()
	if err != nil {
		return "~/Downloads"
	}
	return filepath.Join(usr.HomeDir, "Downloads")
}

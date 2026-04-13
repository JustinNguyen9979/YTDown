package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// GalleryInfo stores basic info about a gallery
type GalleryInfo struct {
	URL   string `json:"url"`
	Title string `json:"title"`
}

// DownloadGalleryWithOpts downloads images using gallery-dl with custom options
func DownloadGalleryWithOpts(ctx context.Context, index int, url string, options GalleryDownloadOptions) error {
	// Resolve short URLs before passing to gallery-dl
	resolvedURL := ResolveShortURL(url, manager.GetUA())

	if IsXiaohongshu(resolvedURL) {
		return DownloadXiaohongshuGallery(ctx, index, resolvedURL, options)
	}

	gallerydlPath := getResourcePath("gallery-dl")

	if gallerydlPath == "" {
		return fmt.Errorf("gallery-dl not found. Please install it using the Setup Dependencies button.")
	}

	args := []string{
		"--destination", options.SavePath,
		"-o", `directory=["{username|user|uploader|creator|title|category}"]`,
	}

	if options.UgoiraToWebm {
		args = append(args, "--ugoira", "webm")
	}

	// Get dynamic User-Agent from system
	userAgent := manager.GetUA()
	if userAgent != "" {
		args = append(args, "-o", "http.user-agent="+userAgent)
	}

	// Decide whether to use cookies based on the domain
	// TikTok often fails with 403 Forbidden if cookies are present but not perfect.
	// Instagram REQUIRES cookies to work.
	useCookies := true
	if strings.Contains(resolvedURL, "tiktok.com") {
		useCookies = false
		LogInfo("[GDL] Skipping cookies for TikTok to avoid 403 Forbidden errors")
	}

	cookieArgs := []string{}
	if useCookies {
		if ca := manager.GetCookieArgs(ctx, "gallery-dl", resolvedURL); len(ca) > 0 {
			cookieArgs = ca
		} else if options.Browser != "" {
			cookieArgs = []string{"--cookies-from-browser", options.Browser}
		}
		args = append(args, cookieArgs...)
	}

	// Force High Quality/Original for common sites
	args = append(args, "-o", "extractor.twitter.fullsize=True")
	args = append(args, "-o", "extractor.pixiv.ugoira=True")
	args = append(args, "-o", "extractor.tiktok.fullsize=True")

	if options.AllFormats {
		// Chọn All → không thêm filter gì cả, download tất cả
		LogInfo("[GDL] All formats selected — no filter applied")
	} else if len(options.Formats) > 0 {
		// Chọn 1 số format cụ thể
		quotedFormats := make([]string, len(options.Formats))
		for i, f := range options.Formats {
			quotedFormats[i] = "'" + f + "'"
		}
		filter := fmt.Sprintf("extension in (%s)", strings.Join(quotedFormats, ", "))
		args = append(args, "--filter", filter)
	} else {
		// Không chọn gì → default bỏ video
		args = append(args, "--filter", "extension not in ('mp4', 'm4v', 'webm', 'mov', 'avi', 'mkv', 'flv')")
	}

	if options.Archive {
		archivePath := filepath.Join(options.SavePath, "gallery-dl-archive.txt")
		args = append(args, "--download-archive", archivePath)
	}

	if options.ExtraArgs != "" {
		// Use a more robust way to split arguments that respects quotes
		// We'll implement a simple shell-style splitter here to avoid new dependencies
		extra, err := splitArguments(options.ExtraArgs)
		if err == nil {
			args = append(args, extra...)
		} else {
			LogError("[GDL] Error parsing extra args: %v", err)
		}
	}

	args = append(args, resolvedURL)

	LogInfo("[GDL] Running command: %s with args: %s", gallerydlPath, strings.Join(args, " "))

	titleEmitted := false

	preflight := getGalleryTitle(ctx, gallerydlPath, resolvedURL, userAgent, cookieArgs)
	if preflight.Title != "" {
		runtime.EventsEmit(ctx, "gallery-title", map[string]interface{}{
			"index": index,
			"title": preflight.Title,
		})
		titleEmitted = true
	}
	// ← count đã có SẴN trước khi download bắt đầu, giải quyết race condition
	totalCountFromPreflight := preflight.Count

	cmd := exec.CommandContext(ctx, gallerydlPath, args...)

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

	// ✅ Đọc stderr trong goroutine SONG SONG để tránh deadlock
	var stderrOutput strings.Builder
	var stderrMu sync.Mutex
	totalCount := totalCountFromPreflight
	LogInfo("[GDL] Pre-flight count: %d", totalCount)
	var totalCountMu sync.Mutex
	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		errScanner := bufio.NewScanner(stderr)
		for errScanner.Scan() {
			line := errScanner.Text()
			LogInfo("[GDL] stderr: %s", line)
			stderrMu.Lock()
			stderrOutput.WriteString(line + "\n")
			stderrMu.Unlock()
		}
	}()

	// Đọc stdout bình thường (không bị block bởi stderr nữa)
	scanner := bufio.NewScanner(stdout)
	count := 0

	for scanner.Scan() {
		line := strings.TrimPrefix(strings.TrimSpace(scanner.Text()), "# ")
		if line == "" {
			continue
		}

		if !filepath.IsAbs(line) {
			LogDebug("[gallery-dl] %s", line)
			continue
		}

		// Đây là filepath thật
		if !titleEmitted {
			if derivedTitle := extractGalleryTitleFromPath(line, options.SavePath); derivedTitle != "" {
				runtime.EventsEmit(ctx, "gallery-title", map[string]interface{}{
					"index": index,
					"title": derivedTitle,
				})
				titleEmitted = true
			}
		}

		count++

		// Tính % nếu biết tổng, không thì để 0 (frontend dùng animated bar)
		totalCountMu.Lock()
		tc := totalCount
		totalCountMu.Unlock()

		var percentage float64
		var speedText string
		if tc > 0 {
			percentage = float64(count) / float64(tc) * 100.0
			speedText = fmt.Sprintf("Downloaded %d/%d files", count, tc)
		} else {
			percentage = 0.0
			speedText = fmt.Sprintf("Downloaded %d files", count)
		}

		runtime.EventsEmit(ctx, "gallery-progress", map[string]interface{}{
			"index":      index,
			"percentage": percentage,
			"speed":      speedText,
			"eta":        "Downloading...",
		})
	}

	<-stderrDone // Đảm bảo đã đọc hết stderr trước khi tiếp tục

	if count > 0 {
		runtime.EventsEmit(ctx, "gallery-progress", map[string]interface{}{
			"index":      index,
			"percentage": 100.0, // ← 100% khi xong
			"speed":      fmt.Sprintf("Downloaded %d files", count),
			"eta":        "Done",
		})
	}

	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if stderrOutput.Len() > 0 {
			return fmt.Errorf("gallery download failed: %s", stderrOutput.String())
		}
		return fmt.Errorf("gallery download failed: %v", err)
	}

	runtime.EventsEmit(ctx, "gallery-complete", map[string]interface{}{
		"index":    index,
		"filePath": options.SavePath,
	})

	return nil
}

// DownloadGallery downloads images using gallery-dl (Legacy compatibility)
func DownloadGallery(ctx context.Context, index int, url, savePath string) error {
	return DownloadGalleryWithOpts(ctx, index, url, GalleryDownloadOptions{
		SavePath: savePath,
		Threads:  1,
	})
}

// splitArguments splits a command line string into separate arguments,
// respecting single and double quotes.
func splitArguments(s string) ([]string, error) {
	var args []string
	var current strings.Builder
	var inQuotes rune

	// ✅ range trên string tự decode UTF-8 → r là rune đúng
	for _, r := range s {
		if inQuotes != 0 {
			if r == inQuotes {
				inQuotes = 0
			} else {
				current.WriteRune(r)
			}
		} else {
			switch r {
			case '\'', '"':
				inQuotes = r
			case ' ', '\t', '\n', '\r':
				if current.Len() > 0 {
					args = append(args, current.String())
					current.Reset()
				}
			default:
				current.WriteRune(r)
			}
		}
	}

	if current.Len() > 0 {
		args = append(args, current.String())
	}

	if inQuotes != 0 {
		return nil, fmt.Errorf("unclosed quote")
	}

	return args, nil
}

func sanitizeFolderName(name string) string {
	reg := regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)
	clean := reg.ReplaceAllString(strings.TrimSpace(name), "_")
	if len(clean) > 100 {
		clean = clean[:100]
	}
	return clean
}

type GalleryPreflightInfo struct {
	Title string
	Count int
}

// getGalleryTitle chạy gallery-dl --print để lấy title/username nhanh (không tải file).
// Dùng delimiter để tách creator và title thành "Creator | Title".
func getGalleryTitle(ctx context.Context, gallerydlPath, url, userAgent string, cookieArgs []string) GalleryPreflightInfo {

	args := []string{
		"--dump-json",
		"--range", "1", // chỉ lấy item đầu tiên cho nhanh
		"-s", // simulate, không tải file
		url,
	}
	if userAgent != "" {
		args = append([]string{"-o", "http.user-agent=" + userAgent}, args...)
	}
	if len(cookieArgs) > 0 {
		args = append(cookieArgs, args...)
	}

	// Timeout 15 giây để tránh block download
	timeoutCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(timeoutCtx, gallerydlPath, args...)

	out, err := cmd.Output()
	if len(out) == 0 {
		LogInfo("[GDL] Pre-flight: no output for %s: %v", url, err)
		return GalleryPreflightInfo{}
	}
	if err != nil {
		LogInfo("[GDL] Pre-flight non-zero exit (OK): %v", err)
	}

	var info GalleryPreflightInfo

	// gallery-dl --dump-json trả về [[type_int, metadata_obj], ...]
	// Phải parse đúng cấu trúc này
	var outerArray []json.RawMessage
	if err := json.Unmarshal(out, &outerArray); err != nil {
		LogInfo("[GDL] Pre-flight: failed to parse outer array: %v", err)
		return GalleryPreflightInfo{}
	}

	for _, raw := range outerArray {
		// Mỗi phần tử là [2, {...metadata...}]
		var pair []json.RawMessage
		if err := json.Unmarshal(raw, &pair); err != nil || len(pair) < 2 {
			continue
		}

		// pair[0] = type int (2 = file), pair[1] = metadata object
		var item map[string]interface{}
		if err := json.Unmarshal(pair[1], &item); err != nil {
			continue
		}

		// Lấy count
		if c, ok := item["count"].(float64); ok && c > 0 {
			info.Count = int(c)
		}

		// Lấy title/username
		for _, key := range []string{"uploader", "user", "username", "creator", "channel", "fullname"} {
			if v, ok := item[key].(string); ok && v != "" {
				info.Title = v
				break
			}
		}
		if t, ok := item["title"].(string); ok && t != "" && info.Title != "" {
			info.Title = info.Title + " | " + t
		} else if t, ok := item["description"].(string); ok && t != "" && info.Title != "" {
			info.Title = info.Title + " | " + t
		}

		break // chỉ cần item đầu tiên
	}

	LogInfo("[GDL] Pre-flight → title=%q count=%d", info.Title, info.Count)
	return info
}

func extractGalleryTitleFromPath(filePath, saveRoot string) string {
	filePath = strings.TrimSpace(filePath)
	filePath = strings.Trim(filePath, `"'`)

	if filePath == "" || strings.HasPrefix(filePath, "[") {
		return ""
	}

	rel, err := filepath.Rel(saveRoot, filePath)
	if err == nil && rel != "" && rel != "." && !strings.HasPrefix(rel, "..") {
		parts := strings.Split(rel, string(filepath.Separator))
		if len(parts) > 1 && strings.TrimSpace(parts[0]) != "" {
			return strings.TrimSpace(parts[0])
		}
	}

	dir := filepath.Base(filepath.Dir(filePath))
	dir = strings.TrimSpace(dir)
	if dir == "" || dir == "." || dir == string(filepath.Separator) {
		return ""
	}

	return dir
}

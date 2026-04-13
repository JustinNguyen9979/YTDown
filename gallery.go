package main

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

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

	if len(options.Formats) > 0 {
		// Create a filter for selected extensions
		// e.g. extension in ('jpg', 'jpeg', 'png')
		quotedFormats := make([]string, len(options.Formats))
		for i, fmt := range options.Formats {
			quotedFormats[i] = "'" + fmt + "'"
		}
		filter := fmt.Sprintf("extension in (%s)", strings.Join(quotedFormats, ", "))
		args = append(args, "--filter", filter)
	} else {
		// If no formats selected, we still want to skip videos by default
		// if the user intended "Images" tab for images.
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

	if title := getGalleryTitle(ctx, gallerydlPath, resolvedURL, userAgent, cookieArgs); title != "" {
		runtime.EventsEmit(ctx, "gallery-title", map[string]interface{}{
			"index": index,
			"title": title,
		})
	}

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
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "[") {
			continue // bỏ qua log lines của gallery-dl
		}
		// Chỉ đếm file path thật sự
		count++
		runtime.EventsEmit(ctx, "gallery-progress", map[string]interface{}{
			"index":      index,
			"percentage": 0.0,
			"speed":      fmt.Sprintf("Downloaded %d files", count),
			"eta":        "Downloading...",
		})
	}

	<-stderrDone // Đảm bảo đã đọc hết stderr trước khi tiếp tục

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

// getGalleryTitle chạy gallery-dl --print để lấy title/username nhanh (không tải file)
func getGalleryTitle(ctx context.Context, gallerydlPath, url, userAgent string, cookieArgs []string) string {
	args := []string{
		"--print", "{title|description|uploader|user|username|category}",
		"-s", // simulate, không tải
		url,
	}
	if userAgent != "" {
		args = append([]string{"-o", "http.user-agent=" + userAgent}, args...)
	}

	if len(cookieArgs) > 0 {
		args = append(cookieArgs, args...)
	}

	cmd := exec.CommandContext(ctx, gallerydlPath, args...)
	out, err := cmd.Output()
	if err != nil || len(out) == 0 {
		return ""
	}

	// Lấy dòng đầu tiên không rỗng
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "[") {
			return line
		}
	}
	return ""
}

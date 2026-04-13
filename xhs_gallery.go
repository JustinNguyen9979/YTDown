package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	xhsmodule "ytdown/xhs"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

func DownloadXiaohongshuGallery(ctx context.Context, index int, url string, options GalleryDownloadOptions) error {
	fetchOptions := getXHSFetchOptions(ctx)
	info, err := xhsmodule.FetchWithOptions(ctx, url, fetchOptions)
	if err != nil {
		return err
	}

	runtime.EventsEmit(ctx, "gallery-title", map[string]interface{}{
		"index": index,
		"title": info.Title,
	})

	targetDir := options.SavePath
	if info.Author != "" {
		targetDir = filepath.Join(options.SavePath, sanitizeFolderName(info.Author))
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return fmt.Errorf("create xhs output dir failed: %w", err)
	}

	var urls []string
	switch info.Type {
	case "image":
		urls = info.ImageURLs
	case "video":
		if info.VideoURL == "" {
			return fmt.Errorf("xhs: video note has empty video URL")
		}
		urls = []string{info.VideoURL}
	default:
		return fmt.Errorf("xhs: unsupported note type %q", info.Type)
	}

	if len(urls) == 0 {
		return fmt.Errorf("xhs: no downloadable assets found")
	}

	client := &http.Client{Timeout: fetchOptions.Timeout}
	for i, assetURL := range urls {
		if err := downloadXHSAsset(ctx, client, fetchOptions, assetURL, filepath.Join(targetDir, buildXHSFilename(i+1, assetURL))); err != nil {
			return err
		}

		runtime.EventsEmit(ctx, "gallery-progress", map[string]interface{}{
			"index":      index,
			"percentage": float64(i+1) / float64(len(urls)) * 100,
			"speed":      fmt.Sprintf("Downloaded %d/%d files", i+1, len(urls)),
			"eta":        "Downloading...",
		})

	}

	runtime.EventsEmit(ctx, "gallery-complete", map[string]interface{}{
		"index":    index,
		"filePath": targetDir,
	})

	return nil
}

func getXHSFetchOptions(ctx context.Context) xhsmodule.FetchOptions {
	opts := xhsmodule.FetchOptions{
		UserAgent:   manager.GetUA(),
		ImageFormat: "best",
		Timeout:     30 * time.Second,
	}

	manager.mu.RLock()
	cfg := manager.config
	manager.mu.RUnlock()

	switch cfg.Mode {
	case CookieModeBrowser:
		if cfg.SelectedBrowser != "" {
			if session := manager.extractWebSessionFromBrowser(ctx, cfg.SelectedBrowser, "https://www.xiaohongshu.com/"); session != "" {
				opts.Cookie = "web_session=" + session
			}
		}
	case CookieModeManual:
		if header := getGalleryCookieHeader(); header != "" {
			if session := extractXHSSession(header); session != "" {
				opts.Cookie = "web_session=" + session
			} else {
				opts.Cookie = header
			}
		}
	}

	return opts
}

func extractXHSSession(header string) string {
	for _, part := range strings.Split(header, ";") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "web_session=") {
			return strings.TrimSpace(strings.TrimPrefix(part, "web_session="))
		}
	}
	return ""
}

func downloadXHSAsset(ctx context.Context, client *http.Client, opts xhsmodule.FetchOptions, assetURL, targetPath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, assetURL, nil)
	if err != nil {
		return fmt.Errorf("xhs: create asset request failed: %w", err)
	}

	if opts.UserAgent != "" {
		req.Header.Set("User-Agent", opts.UserAgent)
	}
	if opts.Referer != "" {
		req.Header.Set("Referer", opts.Referer)
	} else {
		req.Header.Set("Referer", "https://www.xiaohongshu.com/")
	}
	if opts.AcceptLanguage != "" {
		req.Header.Set("Accept-Language", opts.AcceptLanguage)
	}
	if opts.Cookie != "" {
		req.Header.Set("Cookie", opts.Cookie)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("xhs: asset request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("xhs: asset download failed with HTTP %d", resp.StatusCode)
	}

	file, err := os.Create(targetPath)
	if err != nil {
		return fmt.Errorf("xhs: create asset file failed: %w", err)
	}

	_, copyErr := io.Copy(file, resp.Body)
	closeErr := file.Close()
	if copyErr != nil {
		return fmt.Errorf("xhs: write asset failed: %w", copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("xhs: close asset file failed: %w", closeErr)
	}

	return nil
}

func buildXHSFilename(index int, assetURL string) string {
	base := extractXHSAssetID(assetURL)
	if base == "" {
		base = fmt.Sprintf("%02d", index)
	}
	return base + detectXHSExt(assetURL)
}

func detectXHSExt(assetURL string) string {
	lower := strings.ToLower(assetURL)
	switch {
	case strings.Contains(lower, ".png"):
		return ".png"
	case strings.Contains(lower, ".webp"):
		return ".webp"
	case strings.Contains(lower, ".heic"):
		return ".heic"
	case strings.Contains(lower, ".mp4"):
		return ".mp4"
	case strings.Contains(lower, ".mov"):
		return ".mov"
	case strings.Contains(lower, ".m3u8"):
		return ".m3u8"
	default:
		return ".jpg"
	}
}

func extractXHSAssetID(assetURL string) string {
	u, err := url.Parse(assetURL)
	if err != nil {
		return ""
	}

	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) == 0 {
		return ""
	}

	last := parts[len(parts)-1]
	if i := strings.Index(last, "!"); i != -1 {
		last = last[:i]
	}
	if i := strings.Index(last, "."); i != -1 {
		last = last[:i]
	}
	last = strings.TrimSpace(last)
	return last
}

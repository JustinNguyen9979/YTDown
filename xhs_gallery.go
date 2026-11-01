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

type xhsDownloadRoute uint8

const (
	xhsDownloadDirect xhsDownloadRoute = iota
	xhsDownloadYTDLP
)

func galleryVideoCookieHeader(options xhsmodule.FetchOptions) string {
	return options.Cookie
}

func routeXHSNote(noteType string) xhsDownloadRoute {
	if noteType == "video" {
		return xhsDownloadYTDLP
	}
	return xhsDownloadDirect
}

func downloadXHSNoteVideo(ctx context.Context, index int, noteURL, format, quality, savePath string, noPlaylist bool, threads int) error {
	fetchOptions, err := getXHSFetchOptions(ctx, noteURL)
	if err != nil {
		return err
	}
	info, err := fetchXHSNoteWithSessionFallback(ctx, noteURL, fetchOptions)
	if err != nil {
		// The HTML note parser is not needed for video downloads when yt-dlp
		// can extract the canonical page directly. This mirrors the verified
		// CLI flow: canonical URL + web_session + bestvideo fallback selector.
		if extractXHSSession(fetchOptions.Cookie) != "" {
			canonicalURL, canonicalErr := canonicalizeXHSVideoPageURL(noteURL)
			if canonicalErr == nil {
				LogWarning("[XHS] Note parser failed; retrying video through yt-dlp: %v", canonicalErr)
				directErr := downloadVideoWithXHSSessionFormat(
					ctx, index, canonicalURL, format, quality, savePath, noPlaylist,
					threads, fetchOptions.Cookie, true, "",
				)
				if directErr == nil {
					return nil
				}
				return fmt.Errorf("xhs: note parser failed: %v; direct yt-dlp fallback failed: %w", err, directErr)
			}
		}
		return fmt.Errorf("xhs: analyze note video failed: %w", err)
	}
	if routeXHSNote(info.Type) != xhsDownloadYTDLP {
		return fmt.Errorf("xhs: the selected note contains no video")
	}
	thumbnail := downloadThumbnailAsBase64(ctx, info.Thumbnail, noteURL)
	return downloadXHSVideoWithSessionAndCaption(ctx, index, info.VideoURL, thumbnail, info.Title, info.Caption, info.Author, format, quality, savePath, noPlaylist, threads, fetchOptions.Cookie, noteURL)
}

func downloadXHSVideo(ctx context.Context, index int, videoURL, title, author string, options GalleryDownloadOptions) error {
	return downloadXHSVideoWithSession(ctx, index, videoURL, title, author, "MP4", "Best", options.SavePath, true, options.Threads, "")
}

func downloadXHSVideoWithSessionAndCaption(ctx context.Context, index int, videoURL, thumbnail, title, caption, author, format, quality, savePath string, noPlaylist bool, threads int, cookieHeader string, noteURL ...string) error {
	started := time.Now()
	metadata := &VideoInfo{Title: title, Caption: caption, Creator: author, Thumbnail: thumbnail}
	if err := downloadVideoWithXHSSessionFormatMetadata(ctx, index, videoURL, format, quality, savePath, noPlaylist, threads, cookieHeader, true, "", metadata, noteURL...); err != nil {
		return fmt.Errorf("xhs video download failed: %w", err)
	}
	if err := appendLatestVideoCaption(savePath, author, title, caption, started); err != nil {
		LogWarning("[XHS] write video caption failed: %v", err)
	}
	return nil
}

func downloadXHSVideoWithSession(ctx context.Context, index int, videoURL, title, author, format, quality, savePath string, noPlaylist bool, threads int, cookieHeader string, noteURL ...string) error {
	if videoURL == "" {
		return fmt.Errorf("xhs: video note has empty video URL")
	}
	if _, err := url.ParseRequestURI(videoURL); err != nil {
		return fmt.Errorf("xhs: invalid video URL: %w", err)
	}

	// The shared yt-dlp path prepares the creator folder from metadata. Do not
	// pre-create an author folder here, otherwise XHS would become author/author.
	var downloadErr error
	if len(noteURL) > 0 && extractXHSSession(cookieHeader) != "" {
		downloadErr = downloadXHSWithSessionFallback(ctx, index, videoURL, format, quality, savePath, noPlaylist, threads, extractXHSSession(cookieHeader), noteURL[0])
	} else {
		downloadErr = downloadVideoWithXHSSession(ctx, index, videoURL, format, quality, savePath, noPlaylist, threads, cookieHeader, true, noteURL...)
	}
	if downloadErr != nil {
		return fmt.Errorf("xhs video download failed: %w", downloadErr)
	}
	runtime.EventsEmit(ctx, "video-title", map[string]interface{}{
		"index": index,
		"title": title,
	})
	return nil
}

func DownloadXiaohongshuGallery(ctx context.Context, index int, url string, options GalleryDownloadOptions) error {
	fetchOptions, err := getXHSFetchOptions(ctx, url)
	if err != nil {
		return err
	}
	info, err := fetchXHSNoteWithSessionFallback(ctx, url, fetchOptions)
	if err != nil {
		return err
	}

	runtime.EventsEmit(ctx, "gallery-title", map[string]interface{}{
		"index": index,
		"title": info.Title,
	})

	if routeXHSNote(info.Type) == xhsDownloadYTDLP {
		thumbnail := downloadThumbnailAsBase64(ctx, info.Thumbnail, url)
		return downloadXHSVideoWithSessionAndCaption(ctx, index, info.VideoURL, thumbnail, info.Title, info.Caption, info.Author, "MP4", "Best", options.SavePath, true, options.Threads, galleryVideoCookieHeader(fetchOptions), url)
	}

	creator := mediaCreator(info.Author)
	targetDir := filepath.Join(options.SavePath, creator, mediaTitle(info.Title))
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return fmt.Errorf("create xhs output dir failed: %w", err)
	}
	defer func() {
		// If every asset fails, remove the empty title/creator folders. Keep
		// folders containing at least one successfully downloaded asset.
		cleanupEmptyMediaDir(targetDir)
		cleanupEmptyMediaDir(filepath.Dir(targetDir))
	}()

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

	// ✅ THÊM: Apply file type filter — đồng bộ với gallery.go behavior
	if info.Type == "image" && !options.AllFormats && len(options.Formats) > 0 {
		original := len(urls)
		var filtered []string
		for _, u := range urls {
			// detectXHSExt trả về ".jpg", ".png"... → cắt dấu chấm để so sánh
			ext := strings.TrimPrefix(detectXHSExt(u), ".")
			for _, f := range options.Formats {
				if strings.EqualFold(ext, f) {
					filtered = append(filtered, u)
					break
				}
			}
		}
		urls = filtered
		fmt.Printf("[XHS] 🔍 File type filter: %d/%d assets kept (types: %v)\n",
			len(urls), original, options.Formats)
		if len(urls) == 0 {
			return fmt.Errorf("xhs: no assets matched the selected file types: %v", options.Formats)
		}
	}

	client := &http.Client{Timeout: fetchOptions.Timeout}

	var failedCount int
	var downloadedFiles []string
	for i, assetURL := range urls {
		targetPath := filepath.Join(targetDir, buildXHSFilename(i+1, assetURL))

		if err := downloadXHSAsset(ctx, client, fetchOptions, assetURL, targetPath); err != nil {
			// Lỗi 1 ảnh → log + bỏ qua, không return
			LogError("[XHS] Asset %d failed: %v", i+1, err)
			failedCount++
			runtime.EventsEmit(ctx, "gallery-asset-warning", map[string]interface{}{
				"index":    index,
				"assetNum": i + 1,
				"error":    err.Error(),
			})
			continue // ← tải tiếp ảnh tiếp theo
		}

		downloadedFiles = append(downloadedFiles, filepath.Base(targetPath))

		// Chỉ emit progress khi tải thành công
		runtime.EventsEmit(ctx, "gallery-progress", map[string]interface{}{
			"index":      index,
			"percentage": float64(i+1) / float64(len(urls)) * 100,
			"speed":      fmt.Sprintf("Downloaded %d/%d files", i+1, len(urls)),
			"eta":        "Downloading...",
		})
	}

	if failedCount > 0 {
		LogError("[XHS] %d/%d assets failed", failedCount, len(urls))
	}
	if len(downloadedFiles) > 0 {
		if err := appendCaptionRecord(targetDir, imageCaptionFiles(downloadedFiles), info.Title, info.Caption); err != nil {
			return fmt.Errorf("write captions.txt: %w", err)
		}
	}

	runtime.EventsEmit(ctx, "gallery-progress", map[string]interface{}{
		"index":      index,
		"percentage": 100.0,
		"speed":      fmt.Sprintf("Downloaded %d/%d files", len(urls)-failedCount, len(urls)),
		"eta":        "Done",
	})

	runtime.EventsEmit(ctx, "gallery-complete", map[string]interface{}{
		"index":    index,
		"filePath": targetDir,
	})
	return nil
}

func fetchXHSNoteWithSessionFallback(ctx context.Context, noteURL string, options xhsmodule.FetchOptions) (*xhsmodule.NoteInfo, error) {
	info, firstErr := xhsmodule.FetchWithOptions(ctx, noteURL, options)
	if firstErr == nil || ctx.Err() != nil || options.Cookie == "" {
		return info, firstErr
	}

	manager.mu.RLock()
	browser := normalizeBrowserName(manager.config.SelectedBrowser)
	profile := manager.config.SelectedProfile
	mode := manager.config.Mode
	manager.mu.RUnlock()
	if browser == "" || profile == "" || mode == CookieModeManual {
		return nil, firstErr
	}
	currentDomain, _ := manager.xhsSessionSource()
	if currentDomain == "" {
		return nil, firstErr
	}
	alternateSession, alternateDomain, err := manager.alternateXHSSession(ctx, browser, noteURL, currentDomain)
	if err != nil {
		return nil, fmt.Errorf("xhs: primary session failed: %v; alternate session export failed: %w", firstErr, err)
	}
	options.Cookie = "web_session=" + alternateSession
	info, err = xhsmodule.FetchWithOptions(ctx, noteURL, options)
	if err != nil {
		return nil, fmt.Errorf("xhs: primary session failed: %v; alternate %s session failed: %w", firstErr, alternateDomain, err)
	}
	manager.cacheXHSSessionCandidate(browser, profile, alternateSession, alternateDomain)
	return info, nil
}

func getXHSFetchOptions(ctx context.Context, noteURL string) (xhsmodule.FetchOptions, error) {
	manager.mu.RLock()
	cfg := manager.config
	manager.mu.RUnlock()

	opts := xhsmodule.FetchOptions{
		UserAgent:   GetUserAgent(cfg.SelectedBrowser),
		ImageFormat: "best",
		Timeout:     30 * time.Second,
	}

	switch cfg.Mode {
	case CookieModeBrowser:
		if cfg.SelectedBrowser != "" {
			session, err := manager.extractWebSessionFromBrowser(ctx, cfg.SelectedBrowser, noteURL)
			if err != nil {
				return xhsmodule.FetchOptions{}, err
			}
			cookie, err := xhsBrowserCookie(session)
			if err != nil {
				return xhsmodule.FetchOptions{}, err
			}
			opts.Cookie = cookie
		}
	case CookieModeNone:
		if cfg.SelectedBrowser != "" && cfg.SelectedProfile != "" {
			session, err := manager.extractWebSessionFromBrowser(ctx, cfg.SelectedBrowser, noteURL)
			if err != nil {
				return xhsmodule.FetchOptions{}, err
			}
			cookie, err := xhsBrowserCookie(session)
			if err != nil {
				return xhsmodule.FetchOptions{}, err
			}
			opts.Cookie = cookie
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

	return opts, nil
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

func xhsBrowserCookie(session string) (string, error) {
	if strings.TrimSpace(session) == "" {
		return "", fmt.Errorf("no web_session cookie found in selected browser profile; sign in to Xiaohongshu in that profile and try again")
	}
	return "web_session=" + session, nil
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

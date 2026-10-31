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
	urlpkg "net/url"
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
	Caption   string `json:"caption"`
	Creator   string `json:"creator"`
	Thumbnail string `json:"thumbnail"`
	ID        string `json:"id"`
}

type ytdlpFormatMetadata struct {
	FormatID         string                `json:"format_id"`
	Ext              string                `json:"ext"`
	Width            int                   `json:"width"`
	Height           int                   `json:"height"`
	FPS              float64               `json:"fps"`
	TBR              float64               `json:"tbr"`
	VCodec           string                `json:"vcodec"`
	ACodec           string                `json:"acodec"`
	Formats          []ytdlpFormatMetadata `json:"formats"`
	RequestedFormats []ytdlpFormatMetadata `json:"requested_formats"`
}

const xhsBestFormatSelector = "bestvideo*+bestaudio/best"

func (metadata ytdlpFormatMetadata) qualitySummary() ytdlpFormatMetadata {
	formats := metadata.RequestedFormats
	if len(formats) == 0 {
		formats = metadata.Formats
	}
	if len(formats) == 0 {
		return metadata
	}

	var bestVideo ytdlpFormatMetadata
	var bestAudioBitrate float64
	for _, format := range formats {
		if format.VCodec == "none" {
			if format.TBR > bestAudioBitrate {
				bestAudioBitrate = format.TBR
			}
			continue
		}
		if format.Height > bestVideo.Height ||
			(format.Height == bestVideo.Height && format.Width > bestVideo.Width) ||
			(format.Height == bestVideo.Height && format.Width == bestVideo.Width && format.FPS > bestVideo.FPS) ||
			(format.Height == bestVideo.Height && format.Width == bestVideo.Width && format.FPS == bestVideo.FPS && format.TBR > bestVideo.TBR) {
			bestVideo = format
		}
	}
	if bestVideo.Height == 0 && bestVideo.Width == 0 && bestVideo.FPS == 0 && bestVideo.TBR == 0 {
		return ytdlpFormatMetadata{}
	}
	bestVideo.TBR += bestAudioBitrate
	return bestVideo
}

func (metadata ytdlpFormatMetadata) isBetterThan(other ytdlpFormatMetadata) bool {
	if metadata.Height != other.Height {
		return metadata.Height > other.Height
	}
	if metadata.Width != other.Width {
		return metadata.Width > other.Width
	}
	if metadata.FPS != other.FPS {
		return metadata.FPS > other.FPS
	}
	return metadata.TBR > other.TBR
}

func (metadata ytdlpFormatMetadata) hasVideoQuality() bool {
	return metadata.Height > 0 || metadata.Width > 0 || metadata.FPS > 0 || metadata.TBR > 0
}

func isXHSShortLink(rawURL string) bool {
	parsed, err := urlpkg.ParseRequestURI(strings.TrimSpace(rawURL))
	if err != nil {
		return false
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	return (host == "xhslink.com" || strings.HasSuffix(host, ".xhslink.com")) &&
		(strings.HasPrefix(parsed.Path, "/o/") || strings.HasPrefix(parsed.Path, "/s/"))
}

func normalizeVideoInputURL(rawURL string) (string, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return "", fmt.Errorf("video URL is empty")
	}
	// FE có thể gửi URL đã bị escape theo dạng https:\/\/... hoặc có
	// backslash trước query separator. yt-dlp cần URL thật, không phải chuỗi escape.
	rawURL = strings.ReplaceAll(rawURL, "\\", "")
	if isXHSShortLink(rawURL) {
		manager.mu.RLock()
		selectedBrowser := manager.config.SelectedBrowser
		manager.mu.RUnlock()
		// Resolve only XHS short paths. Normal XHS/RedNote URLs keep the
		// existing canonicalization path unchanged.
		rawURL = ResolveShortURL(rawURL, GetUserAgent(selectedBrowser))
	}
	parsed, err := urlpkg.ParseRequestURI(rawURL)
	if err != nil || parsed.Hostname() == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		if err == nil {
			err = fmt.Errorf("URL must use http or https and include a hostname")
		}
		return "", fmt.Errorf("invalid video URL: %w", err)
	}
	if isXHSNoteURL(rawURL) {
		return canonicalizeXHSVideoPageURL(rawURL)
	}
	return parsed.String(), nil
}

func canonicalizeXHSVideoPageURL(rawURL string) (string, error) {
	rawURL = strings.ReplaceAll(strings.TrimSpace(rawURL), "\\", "")
	parsed, err := urlpkg.ParseRequestURI(rawURL)
	if err != nil {
		return "", fmt.Errorf("invalid XHS video page URL: %w", err)
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if host == "rednote.com" || strings.HasSuffix(host, ".rednote.com") ||
		host == "xiaohongshu.com" || strings.HasSuffix(host, ".xiaohongshu.com") ||
		host == "xhslink.com" || strings.HasSuffix(host, ".xhslink.com") {
		parsed.Host = "www.xiaohongshu.com"
		if port := parsed.Port(); port != "" {
			parsed.Host += ":" + port
		}
	}
	return parsed.String(), nil
}

func replaceEnv(env []string, key, value string) []string {
	prefix := key + "="
	filtered := make([]string, 0, len(env)+1)
	for _, entry := range env {
		if !strings.HasPrefix(entry, prefix) {
			filtered = append(filtered, entry)
		}
	}
	return append(filtered, prefix+value)
}

func ensureWritableDownloadTempDir() (string, error) {
	baseDir := os.TempDir()
	if baseDir == "" || baseDir == "/" {
		cacheDir, cacheErr := os.UserCacheDir()
		if cacheErr != nil {
			return "", fmt.Errorf("resolve user cache directory: %w", cacheErr)
		}
		if cacheDir == "" {
			return "", fmt.Errorf("resolve user cache directory: empty path")
		}
		baseDir = filepath.Join(cacheDir, "ytdown", "tmp")
	}
	if err := os.MkdirAll(baseDir, 0o700); err != nil {
		return "", err
	}
	tempDir, err := os.MkdirTemp(baseDir, "yt-dlp-*")
	if err != nil {
		return "", err
	}
	probe, err := os.CreateTemp(tempDir, "probe-*")
	if err != nil {
		_ = os.RemoveAll(tempDir)
		return "", err
	}
	probePath := probe.Name()
	if err := probe.Close(); err != nil {
		_ = os.Remove(probePath)
		_ = os.RemoveAll(tempDir)
		return "", err
	}
	_ = os.Remove(probePath)
	return tempDir, nil
}

func parseYTDLPFormatMetadata(data []byte) (ytdlpFormatMetadata, error) {
	var metadata ytdlpFormatMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return ytdlpFormatMetadata{}, fmt.Errorf("parse yt-dlp format metadata: %w", err)
	}
	return metadata.qualitySummary(), nil
}

func preferXHSDirectVideo(direct, extractor ytdlpFormatMetadata) bool {
	return direct.hasVideoQuality() && !extractor.isBetterThan(direct)
}

func buildYTDLPMetadataArgs(targetURL, formatSelector string, cookieArgs []string) []string {
	args := []string{"-J", "--no-warnings", "--no-playlist"}
	if IsXiaohongshu(targetURL) {
		args = append(args, "--referer", "https://www.xiaohongshu.com/")
	}
	if formatSelector != "" {
		args = append(args, "-f", formatSelector)
	}
	args = append(args, cookieArgs...)
	args = append(args, targetURL)
	return args
}

func inspectYTDLPFormat(ctx context.Context, targetURL, formatSelector string, cookieArgs []string) (ytdlpFormatMetadata, error) {
	ytdlpPath := getResourcePath("yt-dlp")
	if ytdlpPath == "" {
		return ytdlpFormatMetadata{}, fmt.Errorf("yt-dlp not found")
	}
	cmd := platform.CommandContext(ctx, ytdlpPath, buildYTDLPMetadataArgs(targetURL, formatSelector, cookieArgs)...)
	output, err := cmd.Output()
	if err != nil {
		return ytdlpFormatMetadata{}, err
	}
	return parseYTDLPFormatMetadata(output)
}

func selectXHSDirectFormat(formats []ytdlpFormatMetadata, outputFormat, quality string) (string, bool) {
	maxHeight := requestedQualityHeight(quality)
	outputFormat = strings.ToLower(strings.TrimSpace(outputFormat))
	var best ytdlpFormatMetadata
	for _, candidate := range formats {
		if candidate.FormatID == "" || candidate.VCodec == "none" || !candidate.hasVideoQuality() {
			continue
		}
		if maxHeight > 0 && candidate.Height > maxHeight {
			continue
		}
		if outputFormat != "" && candidate.Ext != "" && candidate.Ext != outputFormat {
			continue
		}
		if best.FormatID == "" || candidate.isBetterThan(best) {
			best = candidate
		}
	}
	if best.FormatID == "" {
		return "", false
	}
	if best.ACodec == "" || best.ACodec == "none" {
		return best.FormatID + "+bestaudio", true
	}
	return best.FormatID, true
}

func qualityFallbackSelector(quality, outputFormat string) string {
	_ = outputFormat // buildDownloadArgs handles output container/merging.
	maxHeight := requestedQualityHeight(quality)
	if maxHeight == 0 {
		return xhsBestFormatSelector
	}
	return fmt.Sprintf("bestvideo*[height<=%d]+bestaudio/best[height<=%d]", maxHeight, maxHeight)
}

func selectXHSFormatForQuality(directFormats, extractorFormats []ytdlpFormatMetadata, outputFormat, quality string) (selector string, useExtractor bool, err error) {
	if selector, ok := selectXHSDirectFormat(directFormats, outputFormat, quality); ok {
		return selector, false, nil
	}
	if len(extractorFormats) == 0 {
		return "", false, fmt.Errorf("XHS_FORMAT_INSPECTION: extractor returned no video formats")
	}
	return qualityFallbackSelector(quality, outputFormat), true, nil
}

func classifyXHSError(kind string, err error) error {
	if err == nil {
		return nil
	}
	prefixes := map[string]string{
		"cookie_export":     "XHS_COOKIE_EXPORT",
		"note_parse":        "XHS_NOTE_PARSE",
		"format_inspection": "XHS_FORMAT_INSPECTION",
		"filesystem":        "XHS_FILESYSTEM",
		"download":          "XHS_DOWNLOAD",
	}
	prefix := prefixes[kind]
	if prefix == "" {
		prefix = "XHS_DOWNLOAD"
	}
	return fmt.Errorf("%s: %w", prefix, err)
}

func chooseXHSVideoFormat(ctx context.Context, noteURL, directURL, outputFormat, quality string, cookieArgs []string) (string, bool, error) {
	canonicalNoteURL, err := canonicalizeXHSVideoPageURL(noteURL)
	if err != nil {
		return "", false, classifyXHSError("format_inspection", err)
	}
	directMetadata, directErr := inspectYTDLPFormats(ctx, directURL, cookieArgs)
	if directErr == nil {
		if selector, ok := selectXHSDirectFormat(directMetadata.Formats, outputFormat, quality); ok {
			return selector, false, nil
		}
	}

	extractorMetadata, extractorErr := inspectYTDLPFormats(ctx, canonicalNoteURL, cookieArgs)
	if extractorErr != nil {
		if directErr != nil {
			return "", false, classifyXHSError("format_inspection", fmt.Errorf("inspect direct candidate: %v; inspect extractor candidate: %w", directErr, extractorErr))
		}
		return "", false, classifyXHSError("format_inspection", extractorErr)
	}
	if _, _, err := selectXHSFormatForQuality(nil, extractorMetadata.Formats, outputFormat, quality); err != nil {
		return "", false, classifyXHSError("format_inspection", err)
	}
	return qualityFallbackSelector(quality, outputFormat), true, nil
}

func inspectYTDLPFormats(ctx context.Context, targetURL string, cookieArgs []string) (ytdlpFormatMetadata, error) {
	ytdlpPath := getResourcePath("yt-dlp")
	if ytdlpPath == "" {
		return ytdlpFormatMetadata{}, fmt.Errorf("yt-dlp not found")
	}
	cmd := platform.CommandContext(ctx, ytdlpPath, buildYTDLPMetadataArgs(targetURL, "", cookieArgs)...)
	output, err := cmd.Output()
	if err != nil {
		return ytdlpFormatMetadata{}, err
	}
	var metadata ytdlpFormatMetadata
	if err := json.Unmarshal(output, &metadata); err != nil {
		return ytdlpFormatMetadata{}, fmt.Errorf("parse yt-dlp format metadata: %w", err)
	}
	return metadata, nil
}

func requestedQualityHeight(quality string) int {
	quality = strings.TrimSpace(strings.ToLower(quality))
	if quality == "" || quality == "best" || quality == "best quality" {
		return 0
	}
	height, err := strconv.Atoi(strings.TrimSuffix(quality, "p"))
	if err != nil || height <= 0 {
		return 0
	}
	return height
}

func xhsDownloadURL(noteURL, directURL string, useExtractor bool) (string, error) {
	if !useExtractor {
		return directURL, nil
	}
	return canonicalizeXHSVideoPageURL(noteURL)
}

func retryXHSWithFallback(ctx context.Context, noteURL, fallbackFormat string, directErr error, alreadyExtractor bool, download func(downloadURL, formatSelector string) error) error {
	if directErr == nil || alreadyExtractor || ctx.Err() != nil {
		return directErr
	}
	fallbackURL, err := canonicalizeXHSVideoPageURL(noteURL)
	if err != nil {
		return fmt.Errorf("direct XHS video download failed: %v; prepare extractor fallback: %w", directErr, err)
	}
	if ctx.Err() != nil {
		return fmt.Errorf("direct XHS video download failed: %v; retry skipped because context ended: %w", directErr, ctx.Err())
	}
	if fallbackErr := download(fallbackURL, fallbackFormat); fallbackErr != nil {
		return fmt.Errorf("direct XHS video download failed: %v; extractor fallback failed: %w", directErr, fallbackErr)
	}
	return nil
}

func replaceFormatSelector(args []string, selector string) []string {
	if selector == "" {
		return args
	}
	updated := append([]string(nil), args...)
	for i := 0; i < len(updated)-1; i++ {
		if updated[i] == "-f" {
			updated[i+1] = selector
			return updated
		}
	}
	return append(updated, "-f", selector)
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

type videoDownloadRoute uint8

const (
	videoDownloadYTDLP videoDownloadRoute = iota
	videoDownloadXHSNote
)

// routeVideoDownload keeps the Video-tab XHS handling separate from the
// Images-tab gallery route. XHS note pages need the note parser first so the
// video CDN URL can be resolved before yt-dlp is invoked.
func routeVideoDownload(rawURL string) videoDownloadRoute {
	if isXHSNoteURL(rawURL) {
		return videoDownloadXHSNote
	}
	return videoDownloadYTDLP
}

// DownloadVideo routes XHS/RedNote note pages through the dedicated video
// preparation path. The path reuses the XHS note/video resolution logic but
// does not call DownloadXiaohongshuGallery or alter the Images-tab route.
func DownloadVideo(ctx context.Context, index int, url, format, quality, savePath string, noPlaylist bool, concurrentFragments int) error {
	normalizedURL, err := normalizeVideoInputURL(url)
	if err != nil {
		return err
	}
	if routeVideoDownload(normalizedURL) == videoDownloadXHSNote {
		return downloadXHSNoteVideo(ctx, index, normalizedURL, format, quality, savePath, noPlaylist, concurrentFragments)
	}
	return downloadVideoWithXHSSession(ctx, index, normalizedURL, format, quality, savePath, noPlaylist, concurrentFragments, "", false)
}

func downloadVideoWithXHSSession(ctx context.Context, index int, url, format, quality, savePath string, noPlaylist bool, concurrentFragments int, xhsSession string, metadataKnown bool, xhsNoteURL ...string) error {
	if len(xhsNoteURL) > 0 && strings.TrimSpace(xhsNoteURL[0]) != "" && extractXHSSession(xhsSession) != "" {
		return downloadXHSWithSessionFallback(ctx, index, url, format, quality, savePath, noPlaylist, concurrentFragments, extractXHSSession(xhsSession), xhsNoteURL[0])
	}
	return downloadVideoWithXHSSessionFormat(ctx, index, url, format, quality, savePath, noPlaylist, concurrentFragments, xhsSession, metadataKnown, "", xhsNoteURL...)
}

func downloadXHSWithSessionFallback(ctx context.Context, index int, videoURL, format, quality, savePath string, noPlaylist bool, concurrentFragments int, session, sourceURL string) error {
	firstErr := downloadVideoWithXHSSessionFormat(ctx, index, videoURL, format, quality, savePath, noPlaylist, concurrentFragments, "web_session="+session, true, "", sourceURL)
	if firstErr == nil {
		manager.mu.RLock()
		browser := normalizeBrowserName(manager.config.SelectedBrowser)
		profile := manager.config.SelectedProfile
		manager.mu.RUnlock()
		currentDomain := manager.xhsAttemptSource(session)
		if browser != "" && profile != "" && currentDomain != "" {
			manager.commitXHSSession(browser, profile, session, currentDomain)
		}
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}

	manager.mu.RLock()
	browser := normalizeBrowserName(manager.config.SelectedBrowser)
	profile := manager.config.SelectedProfile
	mode := manager.config.Mode
	manager.mu.RUnlock()
	currentDomain := manager.xhsAttemptSource(session)
	if currentDomain == "" {
		currentDomain, _ = manager.xhsSessionSource()
	}
	if browser == "" || profile == "" || mode == CookieModeManual || currentDomain == "" {
		return firstErr
	}
	alternateSession, alternateDomain, err := manager.alternateXHSSession(ctx, browser, sourceURL, currentDomain)
	if err != nil {
		return fmt.Errorf("primary XHS session download failed: %v; alternate session export failed: %w", firstErr, err)
	}
	if err := downloadVideoWithXHSSessionFormat(ctx, index, videoURL, format, quality, savePath, noPlaylist, concurrentFragments, "web_session="+alternateSession, true, "", sourceURL); err != nil {
		return fmt.Errorf("primary XHS session download failed: %v; alternate %s session download failed: %w", firstErr, alternateDomain, err)
	}
	manager.commitXHSSession(browser, profile, alternateSession, alternateDomain)
	return nil
}

func downloadVideoWithXHSSessionFormat(ctx context.Context, index int, url, format, quality, savePath string, noPlaylist bool, concurrentFragments int, xhsSession string, metadataKnown bool, forcedFormat string, xhsNoteURL ...string) error {
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

	var xhsCookieArgs []string
	var cleanupXHSCookie func()
	// A parsed XHS video is downloaded from its CDN URL, which is not an
	// xiaohongshu.com page URL. Use the original note URL as the cookie scope
	// so the session remains available for the CDN request.
	cookieScopeURL := url
	if len(xhsNoteURL) > 0 && strings.TrimSpace(xhsNoteURL[0]) != "" {
		cookieScopeURL = xhsNoteURL[0]
	}
	if xhsSession != "" && (IsXiaohongshu(url) || IsXiaohongshu(cookieScopeURL)) {
		var err error
		xhsCookieArgs, cleanupXHSCookie, err = manager.xhsSessionCookieArgsForDownload(extractXHSSession(xhsSession), cookieScopeURL)
		if err != nil {
			return err
		}
		if cleanupXHSCookie != nil {
			defer cleanupXHSCookie()
		}
	}

	// XHS note parsing may already know the title/video URL, but the FE still
	// needs the normal video-info event for its thumbnail and Play overlay.
	// Use the already-created session cookie file instead of exporting browser
	// cookies a second time.
	metadataURL := url
	if len(xhsNoteURL) > 0 && strings.TrimSpace(xhsNoteURL[0]) != "" {
		if canonicalURL, canonicalErr := canonicalizeXHSVideoPageURL(xhsNoteURL[0]); canonicalErr == nil {
			metadataURL = canonicalURL
		}
	}
	var info *VideoInfo
	if metadataKnown && len(xhsCookieArgs) > 0 {
		info, _ = getVideoMetadataWithCookieArgs(ctx, metadataURL, xhsCookieArgs)
	} else if !metadataKnown {
		info, _ = GetVideoMetadata(ctx, metadataURL)
	}
	manifestCreator := ""
	if info != nil {
		manifestCreator = info.Creator
	}
	dir, manifestFile, cleanupReservation, prepErr := prepareVideoMediaFolder(savePath, manifestCreator)
	if prepErr != nil {
		return fmt.Errorf("prepare media output folder: %w", prepErr)
	}
	defer cleanupReservation()
	savePath = dir
	if info != nil {
		runtime.EventsEmit(ctx, "video-info", map[string]interface{}{
			"index":     index,
			"title":     info.Title,
			"thumbnail": info.Thumbnail,
			"id":        info.ID,
		})
	}

	// Build yt-dlp arguments based on format and quality
	selectedURL := url
	selectedFormat := forcedFormat
	useExtractor := forcedFormat != ""
	// When the XHS note parser has already resolved a CDN video URL, keep that
	// URL. Do not send it back to the note-page extractor: that is the source
	// of the recurring "No video formats found" failure. Format inspection is
	// only needed when the input itself is still an XHS note page.
	parsedVideoURL := len(xhsNoteURL) > 0 && strings.TrimSpace(xhsNoteURL[0]) != "" && !isXHSNoteURL(url)
	if len(xhsNoteURL) > 0 && strings.TrimSpace(xhsNoteURL[0]) != "" && !parsedVideoURL {
		if selectedFormat == "" {
			chosenFormat, extractorSelected, chooseErr := chooseXHSVideoFormat(ctx, xhsNoteURL[0], url, format, quality, xhsCookieArgs)
			if chooseErr != nil {
				return chooseErr
			}
			selectedFormat = chosenFormat
			useExtractor = extractorSelected
		}
		chosenURL, urlErr := xhsDownloadURL(xhsNoteURL[0], url, useExtractor)
		if urlErr != nil {
			return fmt.Errorf("XHS_FORMAT_INSPECTION: choose download URL: %w", urlErr)
		}
		selectedURL = chosenURL
	}
	// Build container/referer arguments from the XHS page scope when the
	// actual media URL is on the CDN. Then replace only the final input with
	// the resolved CDN URL; this keeps the XHS referer while avoiding the page
	// extractor.
	argsURL := selectedURL
	if parsedVideoURL {
		argsURL = metadataURL
	}
	args, err := buildDownloadArgs(ctx, argsURL, format, quality, savePath, ffmpegPath, noPlaylist, concurrentFragments)
	if err != nil {
		return err
	}
	if selectedFormat != "" {
		args = replaceFormatSelector(args, selectedFormat)
	}
	if len(xhsCookieArgs) > 0 {
		args = replaceBrowserCookieArgsWithXHSSession(args, xhsCookieArgs)
	}
	args = append(args, selectedURL)
	// Add verbose for better debugging in app.log
	// args = append(args, "--verbose")

	LogInfo("[DL] Running command: %s with %d args: %v", ytdlpPath, len(args), args)

	cmd := platform.CommandContext(ctx, ytdlpPath, args...)

	// Ensure /opt/homebrew/bin and other common paths are in PATH so yt-dlp can find ffmpeg, deno, etc.
	cmd.Env = os.Environ()
	if tempDir, tempErr := ensureWritableDownloadTempDir(); tempErr != nil {
		return fmt.Errorf("create yt-dlp temp directory: %w", tempErr)
	} else {
		defer os.RemoveAll(tempDir)
		// TikTok's JS challenge uses Python tempfile. In a packaged Wails app,
		// TMPDIR can be inherited as "/", which makes Python try to create
		// files such as /tmppquamdw2.tmp on the read-only macOS root volume.
		// Set all common temp variables explicitly for yt-dlp and its helpers.
		cmd.Env = replaceEnv(cmd.Env, "TMPDIR", tempDir)
		cmd.Env = replaceEnv(cmd.Env, "TMP", tempDir)
		cmd.Env = replaceEnv(cmd.Env, "TEMP", tempDir)
	}
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
		LogProcessOutput("yt-dlp", line)

		// fmt.Println("[yt-dlp]", line)

		// Log every line from yt-dlp for debugging, but prefix it to distinguish
		// We'll use LogDebug for regular progress to avoid cluttering INFO if needed,
		// but since we want to catch "hangs", INFO is safer for now.
		if strings.Contains(line, "ERROR:") || strings.Contains(line, "WARNING:") {
			LogDownloadError("[yt-dlp] %s", line)
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
		if len(xhsNoteURL) > 0 && !useExtractor {
			directErr := err
			if lastError != "" {
				directErr = fmt.Errorf("download failed: %s", lastError)
			}
			fallbackFormat := qualityFallbackSelector(quality, format)
			return retryXHSWithFallback(ctx, xhsNoteURL[0], fallbackFormat, directErr, useExtractor || forcedFormat != "", func(fallbackURL, selector string) error {
				return downloadVideoWithXHSSessionFormat(ctx, index, fallbackURL, format, quality, savePath, noPlaylist, concurrentFragments, xhsSession, true, selector, xhsNoteURL[0])
			})
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
		if info != nil && manifestFile != "" {
			_ = captionFileRecord(filepath.Dir(finalFilePath), manifestFile+filepath.Ext(finalFilePath), info.Title, info.Caption)
		}
		runtime.EventsEmit(ctx, "download-complete", map[string]interface{}{
			"index":    index,
			"filePath": finalFilePath,
		})
	}

	return nil
}

// buildDownloadArgs builds yt-dlp command arguments
func buildDownloadArgs(ctx context.Context, url, format, quality, savePath, ffmpegPath string, noPlaylist bool, concurrentFragments int) ([]string, error) {
	args := []string{}

	switch format {

	case "MP4":
		if quality == "Best" || quality == "Best Quality" {
			// Chọn chất lượng cao nhất mà extractor thực sự trả về; không
			// giả định direct/native MP4 luôn tồn tại.
			args = append(args, "-f", xhsBestFormatSelector)
		} else {
			h := qualityToHeight(quality)
			args = append(args, "-f", fmt.Sprintf(
				"bestvideo[ext=mp4][height<=%s]+bestaudio[ext=m4a]/bestvideo[height<=%s]+bestaudio/best[height<=%s]",
				h, h, h))
		}
		args = append(args, "--merge-output-format", "mp4")
		// Ép audio về AAC ở bước Merger. TikTok có thể trả về audio MP3;
		// --recode-video sẽ bỏ qua nếu output đã là MP4, khiến MP3 nằm trong
		// MP4 và một số player macOS không phát được tiếng.
		args = append(args,
			"--postprocessor-args", "Merger+ffmpeg:-c:a aac -b:a 192k",
			"--recode-video", "mp4",
		)

	case "MKV":
		// MKV chứa được mọi codec — không cần recode.
		if quality == "Best" || quality == "Best Quality" {
			args = append(args, "-f", xhsBestFormatSelector)
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

	// Playlist mode và output template. Metadata-aware callers reserve a
	// creator folder and a compact video-N filename before invoking yt-dlp.
	playlistFlag := "--no-playlist"
	outputTemplate := filepath.Join(savePath, "%(title)s [%(id)s].%(ext)s")
	if reserved := takeVideoOutputTemplate(savePath); reserved != "" {
		outputTemplate = reserved
	}
	if !noPlaylist && outputTemplate == filepath.Join(savePath, "%(title)s [%(id)s].%(ext)s") {
		playlistFlag = "--yes-playlist"
		outputTemplate = filepath.Join(savePath, "%(playlist_title)s", "%(playlist_index)s - %(title)s [%(id)s].%(ext)s")
	} else if !noPlaylist {
		playlistFlag = "--yes-playlist"
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

	// TikTok's format availability probe invokes its JS challenge and can
	// create a temporary probe file at the filesystem root in packaged macOS
	// apps. Skip that probe; yt-dlp can still download the selected format.
	if parsedURL, parseErr := urlpkg.Parse(url); parseErr == nil &&
		(parsedURL.Hostname() == "tiktok.com" || strings.HasSuffix(parsedURL.Hostname(), ".tiktok.com")) {
		args = append(args, "--no-check-formats")
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

	cookieArgs, cookieErr := manager.GetCookieArgs(ctx, "yt-dlp", url)
	if cookieErr != nil {
		return nil, cookieErr
	}
	if len(cookieArgs) > 0 {
		args = append(args, cookieArgs...)
		// Thêm player client phù hợp cho YouTube age-restricted khi có cookie
		if strings.Contains(url, "youtube.com") || strings.Contains(url, "youtu.be") {
			args = append(args, "--extractor-args", "youtube:player_client=default,web_creator")
		}
	}

	return args, nil
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

// GetVideoMetadata fetches video title, thumbnail and ID.
func GetVideoMetadata(ctx context.Context, rawURL string) (*VideoInfo, error) {
	normalizedURL, normalizeErr := normalizeVideoInputURL(rawURL)
	if normalizeErr != nil {
		return nil, normalizeErr
	}
	cookieArgs, cookieErr := manager.GetCookieArgs(ctx, "yt-dlp", normalizedURL)
	if cookieErr != nil {
		return nil, cookieErr
	}
	return getVideoMetadataWithCookieArgs(ctx, normalizedURL, cookieArgs)
}

func getVideoMetadataWithCookieArgs(ctx context.Context, rawURL string, cookieArgs []string) (*VideoInfo, error) {
	normalizedURL, normalizeErr := normalizeVideoInputURL(rawURL)
	if normalizeErr != nil {
		return nil, normalizeErr
	}
	url := normalizedURL
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

	if len(cookieArgs) > 0 {
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
	caption, _ := data["description"].(string)
	creator, _ := data["uploader"].(string)
	if creator == "" {
		creator, _ = data["channel"].(string)
	}
	if creator == "" {
		creator, _ = data["creator"].(string)
	}
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
		Caption:   caption,
		Creator:   creator,
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
	selectedProfile := manager.config.SelectedProfile
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
		xhsSession := ""
		if normalizeBrowserName(selectedBrowser) == manager.state.xhsSessionBrowser && selectedProfile == manager.state.xhsSessionProfile {
			xhsSession = manager.state.xhsSession
		}
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
	cookieArgs, cookieErr := manager.GetCookieArgs(ctx, "yt-dlp", url)
	if cookieErr != nil {
		return nil, cookieErr
	}
	if len(cookieArgs) > 0 {
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
	ErrorKind      string
	RequiresCookie bool
	DisplayMessage string
	Details        []string
}

func downloadFailureLabel(kind string) string {
	labels := map[string]string{
		"cookie_export":     "Không thể xuất cookie từ trình duyệt",
		"note_parse":        "Không thể phân tích dữ liệu bài viết Xiaohongshu",
		"format_inspection": "Không thể kiểm tra định dạng video",
		"filesystem":        "Không thể ghi file vào thư mục đã chọn",
		"authentication":    "Xiaohongshu yêu cầu đăng nhập hoặc từ chối session",
		"download":          "Không thể tải video",
	}
	if label := labels[kind]; label != "" {
		return label
	}
	return "Lỗi tải video"
}

func classifyDownloadFailure(err error, cookiePresent bool, sourceURL string) DownloadFailure {
	if err == nil {
		return DownloadFailure{}
	}

	message := strings.TrimSpace(err.Error())
	lower := strings.ToLower(message)
	errorKind := "download"
	for marker, kind := range map[string]string{
		"xhs_cookie_export:":     "cookie_export",
		"xhs_note_parse:":        "note_parse",
		"xhs_format_inspection:": "format_inspection",
		"xhs_filesystem:":        "filesystem",
		"xhs_download:":          "download",
	} {
		if strings.Contains(lower, marker) {
			errorKind = kind
			break
		}
	}
	if strings.Contains(lower, "read-only file system") || strings.Contains(lower, "permission denied") ||
		strings.Contains(lower, "unable to open for writing") || strings.Contains(lower, "no space left on device") {
		errorKind = "filesystem"
	}

	if errorKind != "download" {
		return DownloadFailure{
			ErrorKind:      errorKind,
			RequiresCookie: false,
			DisplayMessage: downloadFailureLabel(errorKind),
			Details:        []string{summarizeErrorForUI(message)},
		}
	}

	if strings.Contains(lower, "could not copy") && strings.Contains(lower, "cookie database") {
		return DownloadFailure{
			ErrorKind:      "cookie_export",
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
		if IsXiaohongshu(sourceURL) {
			if cookiePresent {
				details = append(details, "Xiaohongshu session cookie (web_session) is invalid or insufficient.")
			} else {
				details = append(details, "A Xiaohongshu login session (web_session) is required.")
			}
		} else if cookiePresent {
			details = append(details, "Cookie invalid or insufficient.")
			details = append(details, "Please copy a fresh YouTube cookie.")
		} else {
			details = append(details, "Temporary cookie required.")
			details = append(details, "Paste a YouTube Cookie header to retry.")
		}
		return DownloadFailure{
			ErrorKind:      "authentication",
			RequiresCookie: true,
			DisplayMessage: downloadFailureLabel("authentication"),
			Details:        details,
		}
	}

	details = append(details, "Download failed.")
	details = append(details, summarizeErrorForUI(message))
	return DownloadFailure{
		ErrorKind:      errorKind,
		RequiresCookie: false,
		DisplayMessage: downloadFailureLabel(errorKind),
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

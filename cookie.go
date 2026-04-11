package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// CookieMode defines how cookies are handled
type CookieMode string

const (
	CookieModeNone    CookieMode = "none"
	CookieModeManual  CookieMode = "manual"
	CookieModeBrowser CookieMode = "browser"
)

// CookieConfig represents the persistent cookie settings
type CookieConfig struct {
	Mode            CookieMode `json:"mode"`
	SelectedBrowser string     `json:"selected_browser"`
	ManualHeader    string     `json:"manual_header,omitempty"` // Used for persistent manual cookies if desired
}

type temporaryCookieState struct {
	mu           sync.RWMutex
	header       string
	cookies      []parsedCookie
	tempFile     string
	xhsSession   string    // Cache for Xiaohongshu session
	xhsCacheTime time.Time // When the cache was last updated
}

type parsedCookie struct {
	Name  string
	Value string
}

// GetInstalledBrowsers returns IDs of supported browsers actually installed on the system
func GetInstalledBrowsers() []string {
	var available []string

	// macOS common browsers to check
	checkList := map[string]struct {
		bundleID string
		appPath  string
	}{
		"chrome":  {"com.google.Chrome", "/Applications/Google Chrome.app"},
		"firefox": {"org.mozilla.firefox", "/Applications/Firefox.app"},
		"safari":  {"com.apple.Safari", "/Applications/Safari.app"},
		"edge":    {"com.microsoft.edgemac", "/Applications/Microsoft Edge.app"},
		"brave":   {"com.brave.Browser", "/Applications/Brave Browser.app"},
		"opera":   {"com.operasoftware.Opera", "/Applications/Opera.app"},
		"vivaldi": {"com.vivaldi.Vivaldi", "/Applications/Vivaldi.app"},
	}

	for id, info := range checkList {
		if _, err := os.Stat(info.appPath); err == nil {
			available = append(available, id)
		}
	}

	return available
}

// GetUA returns a dynamic User-Agent based on the selected browser's actual version
func (m *CookieManager) GetUA() string {
	m.mu.RLock()
	browser := m.config.SelectedBrowser
	m.mu.RUnlock()

	if browser == "" {
		// Default to Safari (pre-installed on every Mac) if none selected
		browser = "safari"
	}

	// Try to get the actual User-Agent that yt-dlp would use for this browser
	ytdlp := getResourcePath("yt-dlp")
	if ytdlp != "" {
		// We ask for the user_agent it would use with this browser
		cmd := exec.Command(ytdlp, "--cookies-from-browser", browser, "--print", "user_agent", "--terminate-on-connect", "https://www.google.com")
		out, err := cmd.Output()
		if err == nil && len(out) > 0 {
			ua := strings.TrimSpace(string(out))
			if ua != "" {
				return ua
			}
		}
	}

	// Fallback to manual construction if yt-dlp fails or is not available
	osVersion := getMacOSVersion()
	osVersionUA := strings.ReplaceAll(osVersion, ".", "_")
	version := getBrowserVersionDynamic(browser)
	if version == "" {
		version = "17.0" // Generic fallback
	}

	switch strings.ToLower(browser) {
	case "chrome":
		return fmt.Sprintf("Mozilla/5.0 (Macintosh; Intel Mac OS X %s) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%s Safari/537.36", osVersionUA, version)
	case "firefox":
		return fmt.Sprintf("Mozilla/5.0 (Macintosh; Intel Mac OS X %s; rv:%s) Gecko/20100101 Firefox/%s", osVersion, version, version)
	case "safari":
		return fmt.Sprintf("Mozilla/5.0 (Macintosh; Intel Mac OS X %s) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/%s Safari/605.1.15", osVersionUA, version)
	case "edge":
		return fmt.Sprintf("Mozilla/5.0 (Macintosh; Intel Mac OS X %s) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%s Safari/537.36 Edg/%s", osVersionUA, version, version)
	default:
		// Generic WebKit based fallback
		return fmt.Sprintf("Mozilla/5.0 (Macintosh; Intel Mac OS X %s) AppleWebKit/605.1.15 (KHTML, like Gecko) Chrome/%s Safari/537.36", osVersionUA, version)
	}
}

func getBrowserVersionDynamic(id string) string {
	paths := map[string]string{
		"chrome":  "/Applications/Google Chrome.app/Contents/Info.plist",
		"firefox": "/Applications/Firefox.app/Contents/Info.plist",
		"safari":  "/Applications/Safari.app/Contents/Info.plist",
		"edge":    "/Applications/Microsoft Edge.app/Contents/Info.plist",
		"brave":   "/Applications/Brave Browser.app/Contents/Info.plist",
		"opera":   "/Applications/Opera.app/Contents/Info.plist",
		"vivaldi": "/Applications/Vivaldi.app/Contents/Info.plist",
	}

	plistPath, ok := paths[id]
	if !ok {
		return ""
	}

	cmd := exec.Command("defaults", "read", plistPath, "CFBundleShortVersionString")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func getMacOSVersion() string {
	cmd := exec.Command("sw_vers", "-productVersion")
	out, err := cmd.Output()
	if err != nil {
		return "10.15.7"
	}
	return strings.TrimSpace(string(out))
}

// Global cookie state manager
type CookieManager struct {
	mu     sync.RWMutex
	config CookieConfig
	state  temporaryCookieState
}

var manager = &CookieManager{
	config: CookieConfig{
		Mode: CookieModeNone,
	},
}

var cookieNamePattern = regexp.MustCompile(`^[A-Za-z0-9!#$%&'*+\-.^_` + "`" + `|~]+$`)

// writeCookiesToNetscapeFile converts a cookie header string (e.g., "name1=value1; name2=value2")
// into Netscape cookie file format and writes to a temp file
// url parameter is used to determine the correct domain for the cookies
func writeCookiesToNetscapeFile(headerString string, url string) (string, error) {
	// Create temp file
	tmpFile, err := os.CreateTemp("", "ytdown-cookie-*.txt")
	if err != nil {
		return "", err
	}
	defer tmpFile.Close()

	// Write Netscape cookie file header
	tmpFile.WriteString("# Netscape HTTP Cookie File\n")
	tmpFile.WriteString("# This file is generated by YTDown\n\n")

	// Detect domain from URL
	domain := ".youtube.com" // Default
	if strings.Contains(url, "instagram.com") {
		domain = ".instagram.com"
	} else if strings.Contains(url, "twitter.com") || strings.Contains(url, "x.com") {
		domain = ".twitter.com"
	} else if strings.Contains(url, "tiktok.com") {
		domain = ".tiktok.com"
	} else if strings.Contains(url, "xiaohongshu.com") || strings.Contains(url, "xhslink.com") {
		domain = ".xiaohongshu.com"
	} else if strings.Contains(url, "pixiv.net") {
		domain = ".pixiv.net"
	} else if strings.Contains(url, "youtube.com") {
		domain = ".youtube.com"
	}

	LogDebug("[Cookie] Detected domain for Netscape file: %s (from URL: %s)", domain, url)

	// Parse cookie string: "name1=value1; name2=value2; ..."
	pairs := strings.Split(headerString, ";")
	for _, pair := range pairs {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}

		// Split by first = to get name and value
		idx := strings.Index(pair, "=")
		if idx < 0 {
			continue
		}

		name := strings.TrimSpace(pair[:idx])
		value := strings.TrimSpace(pair[idx+1:])

		// Skip if name is empty
		if name == "" {
			continue
		}

		// Write in Netscape format: domain, flag, path, secure, expiration, name, value
		line := fmt.Sprintf("%s\tTRUE\t/\tTRUE\t0\t%s\t%s\n", domain, name, value)
		tmpFile.WriteString(line)
	}

	return tmpFile.Name(), nil
}

// GetConfigDir returns the path to the app's config directory
func GetConfigDir() string {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	dir := filepath.Join(configDir, "YTDown")
	_ = os.MkdirAll(dir, 0755)
	return dir
}

// LoadConfig loads the cookie configuration from disk
func (m *CookieManager) LoadConfig() {
	m.mu.Lock()
	defer m.mu.Unlock()

	dir := GetConfigDir()
	if dir == "" {
		return
	}

	path := filepath.Join(dir, "cookies_settings.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}

	_ = json.Unmarshal(data, &m.config)

	// If mode is manual, we don't restore the header from disk for security/freshness
	// unless we specifically want to. The user said manual should be cleared on exit,
	// so we keep it empty in memory on startup.
	if m.config.Mode == CookieModeManual {
		m.config.ManualHeader = ""
	}
}

// SaveConfig saves the cookie configuration to disk
func (m *CookieManager) SaveConfig() {
	m.mu.RLock()
	defer m.mu.RUnlock()

	dir := GetConfigDir()
	if dir == "" {
		return
	}

	path := filepath.Join(dir, "cookies_settings.json")

	// Prepare config for saving (clear manual header as it's session-only)
	saveCfg := m.config
	if saveCfg.Mode == CookieModeManual {
		saveCfg.ManualHeader = ""
	}

	data, _ := json.MarshalIndent(saveCfg, "", "  ")
	_ = os.WriteFile(path, data, 0644)
}

// GetCookieArgs returns the command line arguments for cookies
func (m *CookieManager) GetCookieArgs(ctx context.Context, tool string, url string) []string {
	m.mu.RLock()
	cfg := m.config
	m.mu.RUnlock()

	var args []string
	isXHS := IsXiaohongshu(url)

	switch cfg.Mode {
	case CookieModeBrowser:
		if cfg.SelectedBrowser != "" {
			// Special handling for Xiaohongshu: extract web_session and add as header
			if isXHS && tool == "yt-dlp" {
				fmt.Printf("[Cookie] 🔍 Detecting Xiaohongshu, trying to extract web_session from %s...\n", cfg.SelectedBrowser)
				session := m.extractWebSessionFromBrowser(ctx, cfg.SelectedBrowser, url)
				if session != "" {
					// Use --add-headers (plural) as in the user's successful command
					args = append(args, "--add-headers", "Cookie: web_session="+session)
					LogInfo("[Cookie] Successfully extracted web_session from %s: %s", cfg.SelectedBrowser, session)
					fmt.Printf("[Cookie] ✅ Found web_session: %s\n", session)

					// Re-enable browser cookies so yt-dlp can get the rest of the context
					args = append(args, "--cookies-from-browser", cfg.SelectedBrowser)
				} else {
					LogWarning("[Cookie] Could NOT find web_session in %s cookies. Make sure you are logged in to Xiaohongshu.", cfg.SelectedBrowser)
					fmt.Printf("[Cookie] ❌ Failed to find web_session in %s. Please check login status.\n", cfg.SelectedBrowser)
					// Fallback to regular browser cookies if extraction failed
					args = append(args, "--cookies-from-browser", cfg.SelectedBrowser)
				}
			} else {
				// Regular browser cookie handling for other sites
				args = append(args, "--cookies-from-browser", cfg.SelectedBrowser)
			}
		}
	case CookieModeManual:
		m.state.mu.RLock()
		header := m.state.header
		m.state.mu.RUnlock()

		if header != "" {
			// For both gallery-dl and yt-dlp, convert to Netscape cookie file format
			// This is more reliable than passing long cookie strings via command line
			netscapeFile, err := writeCookiesToNetscapeFile(header, url)
			if err == nil && netscapeFile != "" {
				if tool == "gallery-dl" {
					args = append(args, "--cookies", netscapeFile)
					LogInfo("[Cookie] Manual cookies written to Netscape file for gallery-dl: %s", netscapeFile)
				} else if tool == "yt-dlp" {
					args = append(args, "--cookies", netscapeFile)
					LogInfo("[Cookie] Manual cookies written to Netscape file: %s", netscapeFile)
				}
			} else {
				// Fallback for gallery-dl
				if tool == "gallery-dl" {
					args = append(args, "-o", "http.headers.Cookie="+header)
					LogWarning("[Cookie] Failed to write cookie file for gallery-dl, falling back to --header")
				} else if tool == "yt-dlp" {
					// Fallback to header if file creation fails
					args = append(args, "--add-headers", "Cookie: "+header)
					LogWarning("[Cookie] Failed to write cookie file, falling back to --add-headers")
				}
			}

			// For Xiaohongshu in manual mode...
			if isXHS && tool == "yt-dlp" && !strings.Contains(header, "web_session=") {
				// If not in manual header, we can't do much unless we also try browser
				LogWarning("[Cookie] Xiaohongshu detected but web_session missing from manual cookie")
			}
		}
	}
	return args
}

var xhsDomains = []string{
	"xiaohongshu.com",
	"xhslink.com",
	"redbookchina.com",
}

// IsXiaohongshu checks if the URL belongs to Xiaohongshu
func IsXiaohongshu(url string) bool {
	lower := strings.ToLower(url)
	for _, domain := range xhsDomains {
		if strings.Contains(lower, domain) {
			return true
		}
	}
	return false
}

// extractWebSessionFromBrowser uses yt-dlp to extract the web_session cookie from a browser
func (m *CookieManager) extractWebSessionFromBrowser(ctx context.Context, browser string, url string) string {
	// 1. Check Cache first (Valid for 5 minutes)
	m.state.mu.RLock()
	if m.state.xhsSession != "" && time.Since(m.state.xhsCacheTime) < 5*time.Minute {
		session := m.state.xhsSession
		m.state.mu.RUnlock()
		fmt.Printf("[Cookie] ⚡ Using CACHED web_session: %s\n", session)
		return session
	}
	m.state.mu.RUnlock()

	ytdlp := getResourcePath("yt-dlp")
	if ytdlp == "" {
		return ""
	}

	// Resolve URL first
	resolvedURL := ResolveShortURL(url, m.GetUA())

	// Use the base domain for cookie extraction for Xiaohongshu
	extractionURL := resolvedURL
	if IsXiaohongshu(resolvedURL) {
		extractionURL = "https://www.xiaohongshu.com/"
	}

	// Create a temporary file to store cookies
	tempDir, err := os.MkdirTemp("", "ytdown-xhs-*")
	if err != nil {
		return ""
	}
	defer os.RemoveAll(tempDir)
	tempCookieFile := filepath.Join(tempDir, "cookies.txt")

	checkCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	fmt.Printf("[Cookie] 🛠️  Exporting cookies from %s to temporary file...\n", browser)
	// Using --cookies to export to a file instead of --print-cookies
	cmd := exec.CommandContext(checkCtx, ytdlp, "--cookies-from-browser", browser, "--cookies", tempCookieFile, "--no-warnings", "--no-playlist", extractionURL)
	_, err = cmd.CombinedOutput()

	if err != nil {
		fmt.Printf("[Cookie] ℹ️  yt-dlp export status: %v\n", err)
	}

	// Read the temporary cookie file
	data, err := os.ReadFile(tempCookieFile)
	if err != nil {
		fmt.Printf("[Cookie] ❌ Could not read temporary cookie file: %v\n", err)
		return ""
	}

	outputStr := string(data)
	var session string

	// Parse Netscape cookie format in the file
	lines := strings.Split(outputStr, "\n")
	for _, line := range lines {
		if strings.Contains(line, "web_session") {
			parts := strings.Fields(line)
			// Netscape format: domain, flag, path, secure, expiration, name, value
			if len(parts) >= 7 && (parts[5] == "web_session" || strings.Contains(parts[5], "web_session")) {
				session = parts[6]
				break
			}
		}
	}

	// Fallback regex search in the file content
	if session == "" {
		re := regexp.MustCompile(`web_session\s+([a-zA-Z0-9]+)`)
		matches := re.FindStringSubmatch(outputStr)
		if len(matches) > 1 {
			session = matches[1]
		}
	}

	// 2. Update Cache if found
	if session != "" {
		m.state.mu.Lock()
		m.state.xhsSession = session
		m.state.xhsCacheTime = time.Now()
		m.state.mu.Unlock()
	}

	return session
}

func setManualCookie(raw string) error {
	// Try parsing as YouTube first to see if we should give a warning,
	// but don't fail if it's not YouTube.
	cookies, err := parseCookieInput(raw, false)
	if err != nil {
		return err
	}

	// For the temporary file, we'll use a set of common domains to make it "universal"
	commonDomains := []string{
		".youtube.com",
		".facebook.com",
		".instagram.com",
		".tiktok.com",
		".twitter.com",
		".x.com",
		".vimeo.com",
		".bilibili.com",
		".douyin.com",
		".reddit.com",
		".twitch.tv",
		".google.com",
		".pinterest.com",
		".linkedin.com",
		".xiaohongshu.com",
		".xhsapp.com",
	}

	tempFile, err := writeMultiDomainCookieFile(cookies, commonDomains)
	if err != nil {
		return err
	}

	manager.state.mu.Lock()
	if manager.state.tempFile != "" && manager.state.tempFile != tempFile {
		_ = os.RemoveAll(filepath.Dir(manager.state.tempFile))
	}

	pairs := make([]string, 0, len(cookies))
	for _, cookie := range cookies {
		pairs = append(pairs, cookie.Name+"="+cookie.Value)
	}

	manager.state.header = strings.Join(pairs, "; ")
	manager.state.cookies = cookies
	manager.state.tempFile = tempFile
	manager.state.mu.Unlock()

	manager.mu.Lock()
	manager.config.Mode = CookieModeManual
	manager.mu.Unlock()
	manager.SaveConfig()

	return nil
}

func writeMultiDomainCookieFile(cookies []parsedCookie, domains []string) (string, error) {
	dir, err := os.MkdirTemp("", "ytdown-cookie-*")
	if err != nil {
		return "", fmt.Errorf("failed to prepare temporary cookie storage: %w", err)
	}

	path := filepath.Join(dir, "cookies.txt")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		_ = os.RemoveAll(dir)
		return "", fmt.Errorf("failed to create temporary cookie file: %w", err)
	}
	defer file.Close()

	if _, err := file.WriteString("# Netscape HTTP Cookie File\n"); err != nil {
		_ = os.RemoveAll(dir)
		return "", fmt.Errorf("failed to write temporary cookie file: %w", err)
	}

	for _, domain := range domains {
		for _, cookie := range cookies {
			line := fmt.Sprintf("%s\tTRUE\t/\tTRUE\t2147483647\t%s\t%s\n", domain, cookie.Name, cookie.Value)
			if _, err := file.WriteString(line); err != nil {
				_ = os.RemoveAll(dir)
				return "", fmt.Errorf("failed to write temporary cookie file: %w", err)
			}
		}
	}

	return path, nil
}

// Compatibility functions for existing code

func setTemporaryYouTubeCookie(raw string) error {
	return setManualCookie(raw)
}

func setGalleryCookie(raw string) error {
	// Re-using the same logic as YouTube for unified cookies
	return setTemporaryYouTubeCookie(raw)
}

func getGalleryCookieHeader() string {
	manager.state.mu.RLock()
	defer manager.state.mu.RUnlock()
	return manager.state.header
}

func hasGalleryCookie() bool {
	manager.mu.RLock()
	mode := manager.config.Mode
	manager.mu.RUnlock()

	if mode == CookieModeBrowser {
		return true
	}

	manager.state.mu.RLock()
	defer manager.state.mu.RUnlock()
	return manager.state.header != ""
}

func hasTemporaryCookie() bool {
	manager.mu.RLock()
	mode := manager.config.Mode
	manager.mu.RUnlock()

	if mode == CookieModeBrowser {
		return true
	}

	manager.state.mu.RLock()
	defer manager.state.mu.RUnlock()
	return manager.state.tempFile != ""
}

func getTemporaryCookieFile() string {
	manager.state.mu.RLock()
	defer manager.state.mu.RUnlock()
	return manager.state.tempFile
}

func clearManualCookie() {
	manager.state.mu.Lock()
	if manager.state.tempFile != "" {
		_ = os.RemoveAll(filepath.Dir(manager.state.tempFile))
	}
	manager.state.header = ""
	manager.state.tempFile = ""
	manager.state.cookies = nil
	manager.state.mu.Unlock()

	manager.mu.Lock()
	manager.config.Mode = CookieModeNone
	manager.mu.Unlock()
	manager.SaveConfig()
}

func clearTemporaryYouTubeCookie() {
	clearManualCookie()
}

// Utility functions kept from original

func parseCookieInput(raw string, isYouTube bool) ([]parsedCookie, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, fmt.Errorf("cookie input is empty")
	}

	if extracted := extractCookieHeader(trimmed); extracted != "" {
		trimmed = extracted
	}

	parts := strings.Split(trimmed, ";")
	cookies := make([]parsedCookie, 0, len(parts))
	seen := make(map[string]bool)

	for _, part := range parts {
		token := strings.TrimSpace(strings.ReplaceAll(part, "\n", " "))
		token = strings.TrimSpace(strings.ReplaceAll(token, "\r", " "))
		if token == "" {
			continue
		}

		name, value, ok := strings.Cut(token, "=")
		if !ok {
			continue
		}

		name = strings.TrimSpace(name)
		value = strings.TrimSpace(value)
		if name == "" || value == "" {
			continue
		}
		if !cookieNamePattern.MatchString(name) {
			continue
		}

		if seen[name] {
			for i := range cookies {
				if cookies[i].Name == name {
					cookies[i].Value = value
					break
				}
			}
			continue
		}

		seen[name] = true
		cookies = append(cookies, parsedCookie{
			Name:  name,
			Value: value,
		})
	}

	if len(cookies) == 0 {
		return nil, fmt.Errorf("no valid cookie pairs found")
	}

	if isYouTube && !hasUsefulYouTubeAuthCookie(cookies) {
		return nil, fmt.Errorf("no usable YouTube authentication cookies found. Please copy a fresh YouTube Cookie header")
	}

	return cookies, nil
}

func extractCookieHeader(raw string) string {
	lines := strings.Split(raw, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(line), "cookie:") {
			return strings.TrimSpace(line[len("cookie:"):])
		}
	}
	return raw
}

func hasUsefulYouTubeAuthCookie(cookies []parsedCookie) bool {
	authNames := map[string]struct{}{
		"SAPISID":           {},
		"__Secure-1PSID":    {},
		"__Secure-3PSID":    {},
		"SID":               {},
		"SSID":              {},
		"HSID":              {},
		"LOGIN_INFO":        {},
		"__Secure-1PAPISID": {},
		"__Secure-3PAPISID": {},
	}

	for _, cookie := range cookies {
		if _, ok := authNames[cookie.Name]; ok {
			return true
		}
	}

	return false
}

func writeTemporaryCookieFile(cookies []parsedCookie, domain string) (string, error) {
	dir, err := os.MkdirTemp("", "ytdown-cookie-*")
	if err != nil {
		return "", fmt.Errorf("failed to prepare temporary cookie storage: %w", err)
	}

	path := filepath.Join(dir, "cookies.txt")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		_ = os.RemoveAll(dir)
		return "", fmt.Errorf("failed to create temporary cookie file: %w", err)
	}
	defer file.Close()

	if _, err := file.WriteString("# Netscape HTTP Cookie File\n"); err != nil {
		_ = os.RemoveAll(dir)
		return "", fmt.Errorf("failed to write temporary cookie file: %w", err)
	}

	for _, cookie := range cookies {
		d := domain
		if d == "" {
			d = ".example.com"
		}
		line := fmt.Sprintf("%s\tTRUE\t/\tTRUE\t2147483647\t%s\t%s\n", d, cookie.Name, cookie.Value)
		if _, err := file.WriteString(line); err != nil {
			_ = os.RemoveAll(dir)
			return "", fmt.Errorf("failed to write temporary cookie file: %w", err)
		}
	}

	return path, nil
}

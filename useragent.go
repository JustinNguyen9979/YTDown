package main

import (
	"fmt"
	"os/exec"
	"strings"
	"sync"
)

// ── Package-level UA cache (thay thế cachedUA/fetchingUA trong CookieManager) ──

var (
	uaCacheMu  sync.RWMutex
	uaCacheMap = map[string]string{} // browser → userAgent
	uaFetching = map[string]bool{}
)

// GetUserAgent trả về UA tốt nhất cho browser + OS hiện tại.
// Nếu browser == "", trả về Chrome UA mặc định cho OS hiện tại.
// Trên macOS: thử fetch UA thực từ yt-dlp (async), trả về static fallback ngay lập tức.
// Trên Windows/Linux: trả về static UA đúng OS ngay lập tức.
func GetUserAgent(browser string) string {
	if browser == "" {
		return getDefaultUserAgent()
	}

	// Check cache
	uaCacheMu.RLock()
	if ua, ok := uaCacheMap[browser]; ok {
		uaCacheMu.RUnlock()
		return ua
	}
	fetching := uaFetching[browser]
	uaCacheMu.RUnlock()

	// Chỉ fetch dynamic UA trên macOS (yt-dlp + browser plist)
	if !fetching && isMacOSPlatform() {
		uaCacheMu.Lock()
		uaFetching[browser] = true
		uaCacheMu.Unlock()

		go func(b string) {
			userAgent := fetchUAFromYTDLP(b)
			uaCacheMu.Lock()
			if userAgent != "" {
				uaCacheMap[b] = userAgent
			}
			uaFetching[b] = false
			uaCacheMu.Unlock()
		}(browser)
	}

	// Trả về static fallback ngay (không block)
	return getUserAgentForBrowser(browser)
}

// ClearUserAgentCache xóa cache khi đổi browser hoặc logout
func ClearUserAgentCache() {
	uaCacheMu.Lock()
	uaCacheMap = map[string]string{}
	uaFetching = map[string]bool{}
	uaCacheMu.Unlock()
}

// ── Các hàm nội bộ ──────────────────────────────────────────────────────────

// isMacOSPlatform kiểm tra OS hiện tại có phải macOS không
func isMacOSPlatform() bool {
	if platformManager != nil {
		return platformManager.OSName() == "macOS"
	}
	return true // fallback khi test
}

// getOSUserAgentString trả về chuỗi OS phù hợp với platform đang chạy
func getOSUserAgentString() string {
	if platformManager != nil {
		switch platformManager.OSName() {
		case "Windows":
			return "Windows NT 10.0; Win64; x64"
		case "Linux":
			return "X11; Linux x86_64"
		}
	}
	return "Macintosh; Intel Mac OS X 10_15_7"
}

// getDefaultUserAgent trả về Chrome UA đúng với OS hiện tại (không browser-specific)
func getDefaultUserAgent() string {
	return fmt.Sprintf(
		"Mozilla/5.0 (%s) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/123.0.0.0 Safari/537.36",
		getOSUserAgentString(),
	)
}

// getUserAgentForBrowser trả về static UA cho browser + OS hiện tại.
// Trên macOS: ưu tiên version thực từ .plist trước khi dùng fallback.
func getUserAgentForBrowser(browser string) string {
	// macOS: thử đọc version thực từ .plist
	if isMacOSPlatform() {
		if version := getBrowserVersionDynamic(browser); version != "" {
			return buildMacOSUserAgent(browser, version)
		}
	}

	// Cross-platform static fallback
	osStr := getOSUserAgentString()
	switch strings.ToLower(browser) {
	case "chrome", "google-chrome", "brave":
		return fmt.Sprintf(
			"Mozilla/5.0 (%s) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/123.0.0.0 Safari/537.36",
			osStr,
		)
	case "firefox":
		if platformManager != nil {
			switch platformManager.OSName() {
			case "Windows":
				return "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:124.0) Gecko/20100101 Firefox/124.0"
			case "Linux":
				return "Mozilla/5.0 (X11; Linux x86_64; rv:124.0) Gecko/20100101 Firefox/124.0"
			}
		}
		return "Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:124.0) Gecko/20100101 Firefox/124.0"
	case "safari":
		// Safari chỉ tồn tại trên macOS → luôn dùng Mac UA
		return "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Safari/605.1.15"
	case "edge":
		return fmt.Sprintf(
			"Mozilla/5.0 (%s) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/123.0.0.0 Safari/537.36 Edg/123.0.0.0",
			osStr,
		)
	default:
		return getDefaultUserAgent()
	}
}

// buildMacOSUserAgent tạo UA string cho macOS với version thực của browser
func buildMacOSUserAgent(browser, version string) string {
	osVersion := getMacOSVersion()
	osVersionUA := strings.ReplaceAll(osVersion, ".", "_")
	switch strings.ToLower(browser) {
	case "chrome", "brave":
		return fmt.Sprintf(
			"Mozilla/5.0 (Macintosh; Intel Mac OS X %s) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%s Safari/537.36",
			osVersionUA, version,
		)
	case "firefox":
		return fmt.Sprintf(
			"Mozilla/5.0 (Macintosh; Intel Mac OS X %s; rv:%s) Gecko/20100101 Firefox/%s",
			osVersion, version, version,
		)
	case "safari":
		return fmt.Sprintf(
			"Mozilla/5.0 (Macintosh; Intel Mac OS X %s) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/%s Safari/605.1.15",
			osVersionUA, version,
		)
	case "edge":
		return fmt.Sprintf(
			"Mozilla/5.0 (Macintosh; Intel Mac OS X %s) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%s Safari/537.36 Edg/%s",
			osVersionUA, version, version,
		)
	default:
		return fmt.Sprintf(
			"Mozilla/5.0 (Macintosh; Intel Mac OS X %s) AppleWebKit/605.1.15 (KHTML, like Gecko) Chrome/%s Safari/537.36",
			osVersionUA, version,
		)
	}
}

// getMacOSVersion đọc version macOS hiện tại (macOS only)
func getMacOSVersion() string {
	cmd := exec.Command("sw_vers", "-productVersion")
	out, err := cmd.Output()
	if err != nil {
		return "10.15.7"
	}
	return strings.TrimSpace(string(out))
}

// getBrowserVersionDynamic đọc version browser từ .plist (macOS only)
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

// fetchUAFromYTDLP lấy UA thực của browser qua yt-dlp (macOS only, chạy async)
func fetchUAFromYTDLP(browser string) string {
	ytdlp := getResourcePath("yt-dlp")
	if ytdlp == "" {
		return ""
	}
	cmd := exec.Command(ytdlp,
		"--cookies-from-browser", browser,
		"--print", "user_agent",
		"--terminate-on-connect",
		"https://www.google.com",
	)
	out, _ := cmd.Output()
	if len(out) > 0 {
		userAgent := strings.TrimSpace(string(out))
		if userAgent != "" && !strings.HasPrefix(userAgent, "[") && strings.Contains(userAgent, "Mozilla") {
			return userAgent
		}
	}
	return ""
}

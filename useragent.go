package main

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"sync"
)

// ── Package-level UA cache ───────────────────────────────────────────────────

var (
	uaCacheMu  sync.RWMutex
	uaCacheMap = map[string]string{}
	uaFetching = map[string]bool{}
)

// GetUserAgent trả về UA tốt nhất cho browser + OS hiện tại.
// browser == "": trả về Chrome UA mặc định phù hợp OS.
// macOS: fetch async UA thực từ yt-dlp, trả về static ngay lập tức.
// Windows/Linux: dùng version thực đọc từ exe/binary, trả về ngay.
func GetUserAgent(browser string) string {
	if browser == "" {
		return getDefaultUserAgent()
	}

	uaCacheMu.RLock()
	if ua, ok := uaCacheMap[browser]; ok {
		uaCacheMu.RUnlock()
		return ua
	}
	fetching := uaFetching[browser]
	uaCacheMu.RUnlock()

	// macOS: fetch UA thực qua yt-dlp (async, không block)
	if !fetching && (runtime.GOOS == "darwin" || runtime.GOOS == "linux") {
		uaCacheMu.Lock()
		uaFetching[browser] = true
		uaCacheMu.Unlock()

		go func(b string) {
			ua := fetchUAFromYTDLP(b)
			uaCacheMu.Lock()
			if ua != "" {
				uaCacheMap[b] = ua
			}
			uaFetching[b] = false
			uaCacheMu.Unlock()
		}(browser)
	}

	return getUserAgentForBrowser(browser)
}

// ClearUserAgentCache xóa cache khi đổi browser hoặc logout
func ClearUserAgentCache() {
	uaCacheMu.Lock()
	uaCacheMap = map[string]string{}
	uaFetching = map[string]bool{}
	uaCacheMu.Unlock()
}

// ── Internal builders ────────────────────────────────────────────────────────

func getOSUserAgentString() string {
	switch runtime.GOOS {
	case "windows":
		return "Windows NT 10.0; Win64; x64"
	case "linux":
		return "X11; Linux x86_64"
	}
	return "Macintosh; Intel Mac OS X 10_15_7"
}

func getDefaultUserAgent() string {
	return fmt.Sprintf(
		"Mozilla/5.0 (%s) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/123.0.0.0 Safari/537.36",
		getOSUserAgentString(),
	)
}

// getUserAgentForBrowser trả về UA tốt nhất cho browser.
// Ưu tiên version thực từ GetBrowserVersion() (cross-platform) → static fallback.
func getUserAgentForBrowser(browser string) string {
	if version := GetBrowserVersion(browser); version != "" {
		return buildUserAgentWithVersion(browser, version)
	}

	osStr := getOSUserAgentString()
	switch strings.ToLower(browser) {
	case "chrome", "google-chrome", "brave":
		return fmt.Sprintf(
			"Mozilla/5.0 (%s) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/123.0.0.0 Safari/537.36",
			osStr,
		)
	case "firefox":
		switch runtime.GOOS {
		case "windows":
			return "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:124.0) Gecko/20100101 Firefox/124.0"
		case "linux":
			return "Mozilla/5.0 (X11; Linux x86_64; rv:124.0) Gecko/20100101 Firefox/124.0"
		}
		return "Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:124.0) Gecko/20100101 Firefox/124.0"
	case "safari":
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

// buildUserAgentWithVersion tạo UA string với version thực — cross-platform.
func buildUserAgentWithVersion(browser, version string) string {
	switch runtime.GOOS {
	case "darwin":
		return buildMacOSUserAgent(browser, version)
	case "windows":
		return buildWindowsUserAgent(browser, version)
	case "linux":
		return buildLinuxUserAgent(browser, version)
	}
	return getDefaultUserAgent()
}

func buildMacOSUserAgent(browser, version string) string {
	osVer := getMacOSVersion()
	osVerUA := strings.ReplaceAll(osVer, ".", "_")
	switch strings.ToLower(browser) {
	case "chrome", "brave":
		return fmt.Sprintf(
			"Mozilla/5.0 (Macintosh; Intel Mac OS X %s) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%s Safari/537.36",
			osVerUA, version,
		)
	case "firefox":
		return fmt.Sprintf(
			"Mozilla/5.0 (Macintosh; Intel Mac OS X %s; rv:%s) Gecko/20100101 Firefox/%s",
			osVer, version, version,
		)
	case "safari":
		return fmt.Sprintf(
			"Mozilla/5.0 (Macintosh; Intel Mac OS X %s) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/%s Safari/605.1.15",
			osVerUA, version,
		)
	case "edge":
		return fmt.Sprintf(
			"Mozilla/5.0 (Macintosh; Intel Mac OS X %s) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%s Safari/537.36 Edg/%s",
			osVerUA, version, version,
		)
	default:
		return fmt.Sprintf(
			"Mozilla/5.0 (Macintosh; Intel Mac OS X %s) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%s Safari/537.36",
			osVerUA, version,
		)
	}
}

func buildWindowsUserAgent(browser, version string) string {
	const osStr = "Windows NT 10.0; Win64; x64"
	switch strings.ToLower(browser) {
	case "chrome", "brave":
		return fmt.Sprintf(
			"Mozilla/5.0 (%s) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%s Safari/537.36",
			osStr, version,
		)
	case "firefox":
		return fmt.Sprintf(
			"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:%s) Gecko/20100101 Firefox/%s",
			version, version,
		)
	case "edge":
		return fmt.Sprintf(
			"Mozilla/5.0 (%s) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%s Safari/537.36 Edg/%s",
			osStr, version, version,
		)
	default:
		return fmt.Sprintf(
			"Mozilla/5.0 (%s) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%s Safari/537.36",
			osStr, version,
		)
	}
}

func buildLinuxUserAgent(browser, version string) string {
	const osStr = "X11; Linux x86_64"
	switch strings.ToLower(browser) {
	case "chrome", "brave":
		return fmt.Sprintf(
			"Mozilla/5.0 (%s) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%s Safari/537.36",
			osStr, version,
		)
	case "firefox":
		return fmt.Sprintf(
			"Mozilla/5.0 (X11; Linux x86_64; rv:%s) Gecko/20100101 Firefox/%s",
			version, version,
		)
	case "edge":
		return fmt.Sprintf(
			"Mozilla/5.0 (%s) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%s Safari/537.36 Edg/%s",
			osStr, version, version,
		)
	default:
		return fmt.Sprintf(
			"Mozilla/5.0 (%s) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%s Safari/537.36",
			osStr, version,
		)
	}
}

func getMacOSVersion() string {
	out, err := exec.Command("sw_vers", "-productVersion").Output()
	if err != nil {
		return "10.15.7"
	}
	return strings.TrimSpace(string(out))
}

func fetchUAFromYTDLP(browser string) string {
	ytdlp := getResourcePath("yt-dlp")
	if ytdlp == "" {
		return ""
	}
	out, err := exec.Command(ytdlp,
		"--cookies-from-browser", browser,
		"--print", "user_agent",
		"--terminate-on-connect",
		"https://www.google.com",
	).Output()
	if err != nil || len(out) == 0 {
		return ""
	}
	ua := strings.TrimSpace(string(out))
	if ua == "" || strings.HasPrefix(ua, "[") || !strings.Contains(ua, "Mozilla") {
		return ""
	}
	return ua
}

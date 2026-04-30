package main

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/browserutils/kooky"
	_ "github.com/browserutils/kooky/browser/all"
)

// BrowserInfo chứa thông tin về browser được cài trong hệ thống
type BrowserInfo struct {
	ID          string
	DisplayName string
}

// browserNameNormalizer: chuẩn hóa tên kooky trả về → ID nhất quán dùng nội bộ
// Chỉ dùng để normalize tên, KHÔNG dùng để whitelist/loại bỏ browser
var browserNameNormalizer = map[string]string{
	"chrome":        "chrome",
	"googlechrome":  "chrome",
	"chromium":      "chromium",
	"firefox":       "firefox",
	"safari":        "safari",
	"edge":          "edge",
	"microsoftedge": "edge",
	"brave":         "brave",
	"bravebrowser":  "brave",
	"opera":         "opera",
	"vivaldi":       "vivaldi",
}

// browserDisplayNames: ID → tên hiển thị đẹp cho UI
var browserDisplayNames = map[string]string{
	"chrome":   "Google Chrome",
	"chromium": "Chromium",
	"firefox":  "Mozilla Firefox",
	"safari":   "Safari",
	"edge":     "Microsoft Edge",
	"brave":    "Brave Browser",
	"opera":    "Opera",
	"vivaldi":  "Vivaldi",
}

// macOSPlistPaths: ID → Info.plist path (chỉ dùng để đọc version, không liên quan detect)
var macOSPlistPaths = map[string]string{
	"chrome":   "/Applications/Google Chrome.app/Contents/Info.plist",
	"chromium": "/Applications/Chromium.app/Contents/Info.plist",
	"firefox":  "/Applications/Firefox.app/Contents/Info.plist",
	"safari":   "/Applications/Safari.app/Contents/Info.plist",
	"edge":     "/Applications/Microsoft Edge.app/Contents/Info.plist",
	"brave":    "/Applications/Brave Browser.app/Contents/Info.plist",
	"opera":    "/Applications/Opera.app/Contents/Info.plist",
	"vivaldi":  "/Applications/Vivaldi.app/Contents/Info.plist",
}

// linuxCmdNames: ID → tên binary trên Linux (chỉ dùng để đọc version)
var linuxCmdNames = map[string][]string{
	"chrome":   {"google-chrome", "google-chrome-stable", "chromium", "chromium-browser"},
	"chromium": {"chromium", "chromium-browser", "chromium-stable"},
	"firefox":  {"firefox", "firefox-esr"},
	"edge":     {"microsoft-edge", "microsoft-edge-stable"},
	"brave":    {"brave-browser", "brave-browser-stable", "brave"},
	"opera":    {"opera"},
	"vivaldi":  {"vivaldi", "vivaldi-stable"},
}

// ── Public API ───────────────────────────────────────────────────────────────

func DetectInstalledBrowsers() []BrowserInfo {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stores := kooky.FindAllCookieStores(ctx)

	seen := map[string]bool{}
	var result []BrowserInfo

	for _, store := range stores {
		// Validation thực: file cookie DB có tồn tại không?
		// epiphany/lynx/uzbl có config dir nhưng không có DB file → bị loại
		cookiePath := store.FilePath()
		if cookiePath == "" {
			continue
		}
		if info, err := os.Stat(cookiePath); err != nil || info.IsDir() {
			continue
		}

		// Normalize tên → ID nhất quán
		raw := strings.ToLower(strings.ReplaceAll(store.Browser(), " ", ""))
		id, ok := browserNameNormalizer[raw]
		if !ok {
			id = raw // Browser lạ vẫn giữ lại
		}

		if seen[id] {
			continue
		}
		seen[id] = true

		displayName, known := browserDisplayNames[id]
		if !known {
			displayName = store.Browser()
		}

		result = append(result, BrowserInfo{ID: id, DisplayName: displayName})
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].ID < result[j].ID
	})
	return result
}

// GetInstalledBrowsers là backward-compatible wrapper trả về []string IDs.
func GetInstalledBrowsers() []string {
	browsers := DetectInstalledBrowsers()
	ids := make([]string, len(browsers))
	for i, b := range browsers {
		ids[i] = b.ID
	}
	return ids
}

// GetBrowserVersion đọc version thực của browser — hoạt động trên cả 3 OS.
func GetBrowserVersion(browserID string) string {
	switch runtime.GOOS {
	case "darwin":
		return browserVersionMacOS(browserID)
	case "windows":
		return browserVersionWindows(browserID)
	case "linux":
		return browserVersionLinux(browserID)
	}
	return ""
}

// ── Version readers ──────────────────────────────────────────────────────────

func browserVersionMacOS(id string) string {
	plist, ok := macOSPlistPaths[id]
	if !ok {
		return ""
	}
	out, err := exec.Command("defaults", "read", plist, "CFBundleShortVersionString").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func browserVersionWindows(id string) string {
	exePath := getBrowserExePathWindows(id)
	if exePath == "" {
		return ""
	}
	script := `(Get-Item "` + exePath + `").VersionInfo.ProductVersion`
	out, err := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func browserVersionLinux(id string) string {
	names, ok := linuxCmdNames[id]
	if !ok {
		return ""
	}
	for _, name := range names {
		path, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		out, err := exec.Command(path, "--version").Output()
		if err != nil {
			continue
		}
		parts := strings.Fields(strings.TrimSpace(string(out)))
		if len(parts) > 0 {
			return parts[len(parts)-1]
		}
	}
	return ""
}

func getBrowserExePathWindows(id string) string {
	type entry struct{ env, rel string }
	winPaths := map[string][]entry{
		"chrome": {
			{"LOCALAPPDATA", `Google\Chrome\Application\chrome.exe`},
			{"PROGRAMFILES", `Google\Chrome\Application\chrome.exe`},
			{"PROGRAMFILES(X86)", `Google\Chrome\Application\chrome.exe`},
		},
		"firefox": {
			{"PROGRAMFILES", `Mozilla Firefox\firefox.exe`},
			{"PROGRAMFILES(X86)", `Mozilla Firefox\firefox.exe`},
		},
		"edge": {
			{"PROGRAMFILES(X86)", `Microsoft\Edge\Application\msedge.exe`},
			{"PROGRAMFILES", `Microsoft\Edge\Application\msedge.exe`},
		},
		"brave": {
			{"LOCALAPPDATA", `BraveSoftware\Brave-Browser\Application\brave.exe`},
			{"PROGRAMFILES", `BraveSoftware\Brave-Browser\Application\brave.exe`},
		},
		"opera": {
			{"LOCALAPPDATA", `Programs\Opera\launcher.exe`},
		},
		"vivaldi": {
			{"LOCALAPPDATA", `Vivaldi\Application\vivaldi.exe`},
		},
	}
	entries, ok := winPaths[id]
	if !ok {
		return ""
	}
	for _, e := range entries {
		base := os.Getenv(e.env)
		if base == "" {
			continue
		}
		full := base + `\` + e.rel
		if _, err := os.Stat(full); err == nil {
			return full
		}
	}
	return ""
}

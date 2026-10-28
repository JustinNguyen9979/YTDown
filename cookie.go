package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	platform "ytdown/flatform"

	"github.com/browserutils/kooky"
	_ "github.com/browserutils/kooky/browser/all"
	kookybrave "github.com/browserutils/kooky/browser/brave"
	kookychrome "github.com/browserutils/kooky/browser/chrome"
	kookychromium "github.com/browserutils/kooky/browser/chromium"
	kookyedge "github.com/browserutils/kooky/browser/edge"
	kookyfirefox "github.com/browserutils/kooky/browser/firefox"
	kookyopera "github.com/browserutils/kooky/browser/opera"
	kookysafari "github.com/browserutils/kooky/browser/safari"
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
	Mode                    CookieMode `json:"mode"`
	SelectedBrowser         string     `json:"selected_browser"`
	SelectedProfile         string     `json:"selected_profile,omitempty"`
	SelectedProfileBookmark []byte     `json:"-"`
	ManualHeader            string     `json:"manual_header,omitempty"` // Used for persistent manual cookies if desired
}

type persistedCookieConfig struct {
	Mode            CookieMode `json:"mode"`
	SelectedBrowser string     `json:"selected_browser"`
	SelectedProfile string     `json:"selected_profile,omitempty"`
	Bookmark        []byte     `json:"selected_profile_bookmark,omitempty"`
	ManualHeader    string     `json:"manual_header,omitempty"`
}

func (c CookieConfig) persisted() persistedCookieConfig {
	return persistedCookieConfig{
		Mode: c.Mode, SelectedBrowser: c.SelectedBrowser,
		SelectedProfile: c.SelectedProfile, Bookmark: c.SelectedProfileBookmark,
		ManualHeader: c.ManualHeader,
	}
}

func (c *CookieConfig) restorePersisted(p persistedCookieConfig) {
	c.Mode = p.Mode
	c.SelectedBrowser = p.SelectedBrowser
	c.SelectedProfile = p.SelectedProfile
	c.SelectedProfileBookmark = p.Bookmark
	c.ManualHeader = p.ManualHeader
}

type temporaryCookieState struct {
	mu                sync.RWMutex
	header            string
	cookies           []parsedCookie
	tempFile          string
	xhsSession        string            // Cache for Xiaohongshu session
	xhsCookieFiles    map[string]string // Temporary Netscape cookie files keyed by allowed XHS domain scope
	xhsSessionBrowser string            // Browser associated with the cached session
	xhsSessionProfile string            // Profile associated with the cached session
	xhsCacheTime      time.Time         // When the cache was last updated
	domainBrowserData map[string]*BrowserExportData
}

type parsedCookie struct {
	Domain  string `json:"domain,omitempty"`
	Path    string `json:"path,omitempty"`
	Name    string `json:"name"`
	Value   string `json:"value"`
	Secure  bool   `json:"secure,omitempty"`
	Expires int64  `json:"expires,omitempty"`
}

type BrowserExportData struct {
	BrowserName string            `json:"browser"`
	UserAgent   string            `json:"user_agent"`
	WebSession  string            `json:"web_session,omitempty"`
	Cookies     []parsedCookie    `json:"cookies"`
	Headers     map[string]string `json:"headers,omitempty"`
	ExportTime  time.Time         `json:"-"`
}

// Global cookie state manager
type CookieManager struct {
	mu             sync.RWMutex
	config         CookieConfig
	state          temporaryCookieState
	extractionMu   sync.Mutex
	xhsCookieUseMu sync.RWMutex
}

var manager = &CookieManager{
	config: CookieConfig{
		Mode: CookieModeNone,
	},
	state: temporaryCookieState{
		domainBrowserData: make(map[string]*BrowserExportData),
	},
}

var cookieNamePattern = regexp.MustCompile(`^[A-Za-z0-9!#$%&'*+\-.^_` + "`" + `|~]+$`)

type tempBrowserSource struct {
	cookiePath  string
	profilePath string
	cleanup     func()
}

func normalizeBrowserName(name string) string {
	normalized := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(name), " ", ""))
	if id, ok := browserNameNormalizer[normalized]; ok {
		return id
	}
	return normalized
}

func shouldUseTemporaryBrowserStore() bool {
	return runtime.GOOS == "linux" || runtime.GOOS == "windows"
}

func isChromiumBasedBrowser(browser string) bool {
	switch normalizeBrowserName(browser) {
	case "chrome", "chromium", "edge", "brave", "opera", "vivaldi":
		return true
	default:
		return false
	}
}

func copyFileIfExists(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.IsDir() {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}

	return nil
}

func buildTempBrowserSourceFromStore(store kooky.CookieStore, browser string) (*tempBrowserSource, error) {
	if store == nil {
		return nil, fmt.Errorf("cookie store is nil")
	}

	dbPath := store.FilePath()
	if dbPath == "" {
		return nil, fmt.Errorf("cookie store has no file path")
	}

	profileDir := filepath.Dir(dbPath)
	if filepath.Base(profileDir) == "Network" {
		profileDir = filepath.Dir(profileDir)
	}
	if profileDir == "" {
		return nil, fmt.Errorf("cannot derive profile directory from %s", dbPath)
	}

	tempRoot, err := os.MkdirTemp("", "ytdown-browser-*")
	if err != nil {
		return nil, fmt.Errorf("cannot create temp browser dir: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(tempRoot) }

	tempProfileDir := filepath.Join(tempRoot, filepath.Base(profileDir))
	tempCookiePath := filepath.Join(tempProfileDir, filepath.Base(dbPath))
	if filepath.Base(filepath.Dir(dbPath)) == "Network" {
		tempCookiePath = filepath.Join(tempProfileDir, "Network", filepath.Base(dbPath))
	}

	if err := copyFileIfExists(dbPath, tempCookiePath); err != nil {
		cleanup()
		return nil, fmt.Errorf("cannot copy cookie DB: %w", err)
	}

	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if err := copyFileIfExists(dbPath+suffix, tempCookiePath+suffix); err != nil {
			cleanup()
			return nil, fmt.Errorf("cannot copy sidecar %s: %w", suffix, err)
		}
	}

	if isChromiumBasedBrowser(browser) {
		localState := filepath.Join(filepath.Dir(profileDir), "Local State")
		if err := copyFileIfExists(localState, filepath.Join(tempRoot, "Local State")); err != nil {
			cleanup()
			return nil, fmt.Errorf("cannot copy Local State: %w", err)
		}
	}

	return &tempBrowserSource{
		cookiePath:  tempCookiePath,
		profilePath: tempProfileDir,
		cleanup:     cleanup,
	}, nil
}

func openKookyStoreFromPath(browser, cookiePath string, filters ...kooky.Filter) (kooky.CookieStore, error) {
	switch normalizeBrowserName(browser) {
	case "chrome":
		return kookychrome.CookieStore(cookiePath, filters...)
	case "chromium", "vivaldi":
		return kookychromium.CookieStore(cookiePath, filters...)
	case "firefox":
		return kookyfirefox.CookieStore(cookiePath, filters...)
	case "edge":
		return kookyedge.CookieStore(cookiePath, filters...)
	case "brave":
		return kookybrave.CookieStore(cookiePath, filters...)
	case "opera":
		return kookyopera.CookieStore(cookiePath, filters...)
	case "safari":
		return kookysafari.CookieStore(cookiePath, filters...)
	default:
		return nil, fmt.Errorf("temporary kooky store unsupported for %s", browser)
	}
}

func buildYTDLPBrowserSpec(browser, profilePath string) string {
	if profilePath == "" {
		return browser
	}
	return browser + ":" + filepath.ToSlash(profilePath)
}

func browserProfilePathFromStore(store kooky.CookieStore) string {
	if store == nil {
		return ""
	}
	return browserProfilePathFromCookiePath(store.FilePath())
}

func browserProfilePathFromCookiePath(cookiePath string) string {
	if cookiePath == "" {
		return ""
	}
	info, err := os.Stat(cookiePath)
	if err != nil || info.IsDir() {
		return ""
	}

	profilePath := filepath.Dir(cookiePath)
	if filepath.Base(profilePath) == "Network" {
		profilePath = filepath.Dir(profilePath)
	}
	return profilePath
}

func ytdlpBrowserSpecFromCookiePath(browser, cookiePath string) string {
	return buildYTDLPBrowserSpec(normalizeBrowserName(browser), browserProfilePathFromCookiePath(cookiePath))
}

func firefoxProfileFromINI(root string) string {
	contents, err := os.ReadFile(filepath.Join(root, "profiles.ini"))
	if err != nil {
		return ""
	}
	profile, _ := firefoxProfileFromINIContents(root, contents)
	return profile
}

func firefoxProfileFromINIContents(root string, contents []byte) (string, error) {
	sections := make(map[string]map[string]string)
	section := ""
	for _, line := range strings.Split(string(contents), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(line[1 : len(line)-1])
			sections[section] = make(map[string]string)
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if ok && section != "" {
			sections[section][strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}

	defaultPath := ""
	for name, values := range sections {
		if strings.HasPrefix(name, "Install") && values["Locked"] == "1" && values["Default"] != "" {
			defaultPath = values["Default"]
			break
		}
	}
	if defaultPath == "" {
		legacyDefault := ""
		legacyDefaultCount := 0
		for name, values := range sections {
			if strings.HasPrefix(name, "Profile") && values["Default"] == "1" {
				legacyDefault = values["Path"]
				legacyDefaultCount++
			}
		}
		if legacyDefaultCount == 1 {
			defaultPath = legacyDefault
		}
	}
	if defaultPath == "" {
		return "", nil
	}

	for name, values := range sections {
		if !strings.HasPrefix(name, "Profile") || values["Path"] != defaultPath {
			continue
		}
		profilePath := filepath.FromSlash(defaultPath)
		if values["IsRelative"] == "1" {
			profilePath = filepath.Join(root, profilePath)
		}
		profilePath, err := filepath.Abs(profilePath)
		if err != nil {
			return "", err
		}
		if _, err := cookieStorePathFromProfile("firefox", profilePath); err != nil {
			return "", err
		}
		return profilePath, nil
	}
	return "", nil
}

func firefoxYTDLPBrowserSpec(root string) string {
	if profilePath := firefoxProfileFromINI(root); profilePath != "" {
		return buildYTDLPBrowserSpec("firefox", profilePath)
	}
	return ""
}

func defaultBrowserProfilePath(browser, home string) string {
	profile, _ := discoverDefaultBrowserProfile(browser, home, nil)
	return profile
}

func discoverDefaultBrowserProfile(browser, home string, validate func(string, string) error) (string, error) {
	browser = normalizeBrowserName(browser)
	if home == "" {
		return "", fmt.Errorf("cannot determine home directory for %s", browser)
	}
	profile, err := defaultBrowserProfilePathWithError(browser, home)
	if err != nil {
		return "", err
	}
	if profile == "" {
		return "", fmt.Errorf("no default %s profile with a supported cookie database was found", browser)
	}
	if validate != nil {
		if err := validate(browser, profile); err != nil {
			if errors.Is(err, os.ErrPermission) {
				return "", browserProfileAccessError(browser, err)
			}
			return "", err
		}
	}
	return profile, nil
}

func defaultBrowserProfilePathWithError(browser, home string) (string, error) {
	browser = normalizeBrowserName(browser)
	if home == "" {
		return "", fmt.Errorf("cannot determine home directory for %s", browser)
	}
	if browser == "firefox" {
		root := filepath.Join(home, "Library", "Application Support", "Firefox")
		contents, err := os.ReadFile(filepath.Join(root, "profiles.ini"))
		if err != nil {
			if errors.Is(err, os.ErrPermission) {
				return "", browserProfileAccessError(browser, err)
			}
			return "", nil
		}
		return firefoxProfileFromINIContents(root, contents)
	}

	var profileParts []string
	switch browser {
	case "chrome":
		profileParts = []string{"Library", "Application Support", "Google", "Chrome", "Default"}
	case "chromium":
		profileParts = []string{"Library", "Application Support", "Chromium", "Default"}
	case "edge":
		profileParts = []string{"Library", "Application Support", "Microsoft Edge", "Default"}
	case "brave":
		profileParts = []string{"Library", "Application Support", "BraveSoftware", "Brave-Browser", "Default"}
	case "opera":
		profileParts = []string{"Library", "Application Support", "com.operasoftware.Opera", "Stable"}
	case "vivaldi":
		profileParts = []string{"Library", "Application Support", "Vivaldi", "Default"}
	case "safari":
		profile := filepath.Join(home, "Library", "Containers", "com.apple.Safari", "Data", "Library")
		if _, err := cookieStorePathFromProfile(browser, profile); err == nil {
			return profile, nil
		} else if errors.Is(err, os.ErrPermission) {
			return "", browserProfileAccessError(browser, err)
		}
		return "", nil
	default:
		return "", fmt.Errorf("browser profile discovery is unsupported for %s", browser)
	}

	profile := filepath.Join(append([]string{home}, profileParts...)...)
	if _, err := cookieStorePathFromProfile(browser, profile); err == nil {
		return profile, nil
	} else if errors.Is(err, os.ErrPermission) {
		return "", browserProfileAccessError(browser, err)
	}
	return "", nil
}

func buildAutomaticBrowserConfig(browser, home string) (CookieConfig, error) {
	browser = normalizeBrowserName(browser)
	profilePath, err := discoverDefaultBrowserProfile(browser, home, cookieStorePathValidation)
	if err != nil {
		return CookieConfig{}, err
	}
	return CookieConfig{
		Mode:            CookieModeBrowser,
		SelectedBrowser: browser,
		SelectedProfile: profilePath,
	}, nil
}

func rediscoverBrowserCookieConfig(current CookieConfig, home string) (CookieConfig, error) {
	if current.Mode != CookieModeBrowser || current.SelectedBrowser == "" {
		return current, nil
	}
	return buildAutomaticBrowserConfig(current.SelectedBrowser, home)
}

func cookieStorePathValidation(browser, profilePath string) error {
	_, err := cookieStorePathFromProfile(browser, profilePath)
	return err
}

func resolveSelectedBrowserProfile(browser, selectedPath string) (string, error) {
	browser = normalizeBrowserName(browser)
	if _, err := cookieStorePathFromProfile(browser, selectedPath); err == nil {
		return selectedPath, nil
	}

	if browser == "firefox" {
		if profile := firefoxProfileFromINI(selectedPath); profile != "" {
			return profile, nil
		}
	}

	entries, err := os.ReadDir(selectedPath)
	if err != nil {
		return "", fmt.Errorf("cannot inspect selected %s folder: %w", browser, err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		candidate := filepath.Join(selectedPath, entry.Name())
		if _, err := cookieStorePathFromProfile(browser, candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("no supported cookie database found in selected %s folder or its immediate profile folders", browser)
}

func defaultBrowserDirectory(browser, home string) string {
	browser = normalizeBrowserName(browser)
	if browser == "firefox" {
		return filepath.Join(home, "Library", "Application Support", "Firefox")
	}
	if profile := defaultBrowserProfilePath(browser, home); profile != "" {
		return profile
	}
	var parts []string
	switch browser {
	case "chrome":
		parts = []string{"Library", "Application Support", "Google", "Chrome"}
	case "chromium":
		parts = []string{"Library", "Application Support", "Chromium"}
	case "edge":
		parts = []string{"Library", "Application Support", "Microsoft Edge"}
	case "brave":
		parts = []string{"Library", "Application Support", "BraveSoftware", "Brave-Browser"}
	case "opera":
		parts = []string{"Library", "Application Support", "com.operasoftware.Opera"}
	case "vivaldi":
		parts = []string{"Library", "Application Support", "Vivaldi"}
	case "safari":
		parts = []string{"Library", "Containers", "com.apple.Safari", "Data", "Library"}
	default:
		return home
	}
	return filepath.Join(append([]string{home}, parts...)...)
}

func cookieStorePathFromProfile(browser, profilePath string) (string, error) {
	browser = normalizeBrowserName(browser)
	if profilePath == "" {
		return "", fmt.Errorf("browser profile path is empty")
	}

	var candidates []string
	switch browser {
	case "firefox":
		candidates = []string{"cookies.sqlite"}
	case "chrome", "chromium", "edge", "brave", "opera", "vivaldi":
		candidates = []string{filepath.Join("Network", "Cookies"), "Cookies"}
	case "safari":
		candidates = []string{filepath.Join("Cookies", "Cookies.binarycookies"), "Cookies.binarycookies"}
	default:
		return "", fmt.Errorf("browser profile selection is unsupported for %s", browser)
	}

	for _, candidate := range candidates {
		cookiePath := filepath.Join(profilePath, candidate)
		info, err := os.Stat(cookiePath)
		if err != nil {
			if errors.Is(err, os.ErrPermission) {
				return "", browserProfileAccessError(browser, err)
			}
			continue
		}
		if info.IsDir() {
			continue
		}
		file, err := os.Open(cookiePath)
		if err != nil {
			if errors.Is(err, os.ErrPermission) {
				return "", browserProfileAccessError(browser, err)
			}
			return "", fmt.Errorf("cannot read %s cookie database: %w", browser, err)
		}
		_ = file.Close()
		return cookiePath, nil
	}
	return "", fmt.Errorf("no supported cookie database found in %s profile", browser)
}

func browserProfileAccessError(browser string, err error) error {
	processGuidance := "add YTDown to Full Disk Access, quit YTDown completely, then reopen it"
	if strings.EqualFold(strings.TrimSpace(Version), "Dev") {
		processGuidance = "add the Terminal or IDE running Wails dev to Full Disk Access, quit that process completely, then restart Wails dev"
	}
	return fmt.Errorf("macOS blocked access to the auto-detected %s profile. In System Settings > Privacy & Security > Full Disk Access, %s: %w", browser, processGuidance, err)
}

func ytdlpBrowserSpecForSelection(browser, profilePath string) string {
	return buildYTDLPBrowserSpec(normalizeBrowserName(browser), profilePath)
}

func hasSelectedBrowserProfile(selectedBrowser, selectedProfile, requestedBrowser string) bool {
	return selectedProfile != "" && normalizeBrowserName(selectedBrowser) == normalizeBrowserName(requestedBrowser)
}

func (m *CookieManager) ytdlpBrowserSpec(ctx context.Context, browser string) (string, error) {
	browser = normalizeBrowserName(browser)
	m.mu.RLock()
	selectedBrowser, selectedProfile := normalizeBrowserName(m.config.SelectedBrowser), m.config.SelectedProfile
	m.mu.RUnlock()
	if hasSelectedBrowserProfile(selectedBrowser, selectedProfile, browser) {
		if _, err := cookieStorePathFromProfile(browser, selectedProfile); err != nil {
			return "", err
		}
		return ytdlpBrowserSpecForSelection(browser, selectedProfile), nil
	}
	if browser == "firefox" {
		if home, err := os.UserHomeDir(); err == nil {
			root := filepath.Join(home, "Library", "Application Support", "Firefox")
			if spec := firefoxYTDLPBrowserSpec(root); spec != "" {
				return spec, nil
			}
		}
	}
	store, err := m.findBrowserStore(ctx, browser)
	if err != nil {
		return "", err
	}
	if store != nil {
		profilePath := browserProfilePathFromStore(store)
		_ = store.Close()
		if profilePath != "" {
			return buildYTDLPBrowserSpec(browser, profilePath), nil
		}
	}
	// Let yt-dlp discover a default only when the user did not select a profile.
	return browser, nil
}

func (m *CookieManager) findBrowserStore(ctx context.Context, browser string) (kooky.CookieStore, error) {
	browser = normalizeBrowserName(browser)
	m.mu.RLock()
	selectedBrowser, selectedProfile := normalizeBrowserName(m.config.SelectedBrowser), m.config.SelectedProfile
	m.mu.RUnlock()
	if selectedBrowser == browser && selectedProfile != "" {
		cookiePath, err := cookieStorePathFromProfile(browser, selectedProfile)
		if err != nil {
			return nil, err
		}
		store, err := openKookyStoreFromPath(browser, cookiePath)
		if err != nil {
			return nil, fmt.Errorf("open selected %s cookie store: %w", browser, err)
		}
		return store, nil
	}

	var fallback kooky.CookieStore
	for _, store := range kooky.FindAllCookieStores(ctx) {
		if !strings.EqualFold(normalizeBrowserName(store.Browser()), browser) {
			_ = store.Close()
			continue
		}
		if store.IsDefaultProfile() {
			if fallback != nil {
				_ = fallback.Close()
			}
			return store, nil
		}
		if fallback == nil {
			fallback = store
		} else {
			_ = store.Close()
		}
	}
	return fallback, nil
}

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
	} else if IsXiaohongshu(url) {
		domain = ".xiaohongshu.com" // vẫn set cookie domain gốc vì server login dùng domain này
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
		line := fmt.Sprintf("%s\tTRUE\t/\tTRUE\t2147483647\t%s\t%s\n", domain, name, value)
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

	var persisted persistedCookieConfig
	if err := json.Unmarshal(data, &persisted); err != nil {
		return
	}
	m.config.restorePersisted(persisted)

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

	// Manual headers are session-only and must not be persisted.
	saveCfg := m.config.persisted()
	if saveCfg.Mode == CookieModeManual {
		saveCfg.ManualHeader = ""
	}

	data, err := json.MarshalIndent(saveCfg, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0600)
}

// GetCookieArgs returns the command line arguments for cookies
func (m *CookieManager) GetCookieArgs(ctx context.Context, tool string, targetURL string) ([]string, error) {
	m.mu.RLock()
	cfg := m.config
	m.mu.RUnlock()

	var args []string
	isXHS := IsXiaohongshu(targetURL)

	switch cfg.Mode {
	case CookieModeBrowser:
		browser := normalizeBrowserName(cfg.SelectedBrowser)
		if browser == "" {
			break
		}
		if tool == "yt-dlp" {
			browserSpec, err := m.ytdlpBrowserSpec(ctx, browser)
			if err != nil {
				return nil, err
			}
			args = append(args, "--cookies-from-browser", browserSpec)
			break
		}

		data, err := m.GetBrowserData(ctx, browser, targetURL)
		if err != nil {
			return nil, err
		}
		if data == nil || len(data.Cookies) == 0 {
			return nil, fmt.Errorf("no usable cookies found in selected %s profile", browser)
		}
		tempFile, err := writeParsedCookiesToNetscapeFile(data.Cookies)
		if err != nil {
			return nil, fmt.Errorf("write temporary cookies: %w", err)
		}
		if tempFile != "" {
			args = append(args, "--cookies", tempFile)
		}
		if data.UserAgent != "" && tool == "gallery-dl" {
			args = append(args, "-o", "http.headers.User-Agent="+data.UserAgent)
		}

	case CookieModeManual:
		m.state.mu.RLock()
		header := m.state.header
		tempFile := m.state.tempFile
		m.state.mu.RUnlock()

		if header != "" && tempFile != "" {
			args = append(args, "--cookies", tempFile)
			LogInfo("[Cookie] Reusing existing cookie file: %s", tempFile)
		} else if header != "" {
			LogWarning("[Cookie] No temp cookie file found, falling back to header")
			switch tool {
			case "gallery-dl":
				args = append(args, "-o", "http.headers.Cookie="+header)
			case "yt-dlp":
				args = append(args, "--add-headers", "Cookie: "+header)
			}
		}

		if isXHS && tool == "yt-dlp" && header != "" && !strings.Contains(header, "web_session=") {
			LogWarning("[Cookie] Xiaohongshu detected but web_session missing from manual cookie")
		}
	}
	return args, nil
}

func (m *CookieManager) GetBrowserData(ctx context.Context, browser string, targetURL string) (*BrowserExportData, error) {
	browser = normalizeBrowserName(browser)
	parsed, _ := url.Parse(targetURL)
	targetHost := ""
	if parsed != nil {
		targetHost = parsed.Hostname()
	}

	baseDomain := ""
	if targetHost != "" {
		parts := strings.Split(targetHost, ".")
		if len(parts) >= 2 {
			baseDomain = parts[len(parts)-2] + "." + parts[len(parts)-1]
		}
	}
	if baseDomain == "" {
		baseDomain = "unknown"
	}

	m.mu.RLock()
	selectedBrowser := normalizeBrowserName(m.config.SelectedBrowser)
	selectedProfile := m.config.SelectedProfile
	m.mu.RUnlock()
	if selectedBrowser != browser {
		selectedProfile = ""
	}
	cacheKey := browser + "|" + selectedProfile + "|" + baseDomain

	m.state.mu.RLock()
	if m.state.domainBrowserData == nil {
		m.state.mu.RUnlock()
		m.state.mu.Lock()
		m.state.domainBrowserData = make(map[string]*BrowserExportData)
		m.state.mu.Unlock()
		m.state.mu.RLock()
	}
	data := m.state.domainBrowserData[cacheKey]
	m.state.mu.RUnlock()

	if data != nil && time.Since(data.ExportTime) < 5*time.Minute {
		return data, nil
	}

	m.extractionMu.Lock()
	defer m.extractionMu.Unlock()

	m.state.mu.RLock()
	data = m.state.domainBrowserData[cacheKey]
	m.state.mu.RUnlock()

	if data != nil && time.Since(data.ExportTime) < 5*time.Minute {
		return data, nil
	}

	newData, err := m.ExtractBrowserDataForDomain(ctx, browser, targetHost)
	if err != nil {
		return nil, err
	}

	m.state.mu.Lock()
	m.state.domainBrowserData[cacheKey] = newData
	m.state.mu.Unlock()

	return newData, nil
}

func (m *CookieManager) ExtractBrowserDataForDomain(ctx context.Context, browser string, targetHost string) (*BrowserExportData, error) {
	// ── Bước 1: Thử kooky ──────────────────────────────────────────────
	data, kookyErr := m.extractBrowserDataViaKooky(ctx, browser, targetHost)
	if kookyErr == nil && data != nil {
		hasValid := false
		for _, c := range data.Cookies {
			if c.Value != "" {
				hasValid = true
				break
			}
		}
		if hasValid {
			LogInfo("[Cookie] ✅ kooky succeeded for %s (%d cookies)", browser, len(data.Cookies))
			return data, nil
		}
		LogInfo("[Cookie] kooky returned 0 valid cookies for %s", browser)
		m.mu.RLock()
		profileSelected := normalizeBrowserName(m.config.SelectedBrowser) == normalizeBrowserName(browser) && m.config.SelectedProfile != ""
		m.mu.RUnlock()
		if profileSelected {
			return nil, fmt.Errorf("selected %s profile contains no valid cookies", browser)
		}
	} else if kookyErr != nil {
		m.mu.RLock()
		selectedProfile := normalizeBrowserName(m.config.SelectedBrowser) == normalizeBrowserName(browser) && m.config.SelectedProfile != ""
		m.mu.RUnlock()
		if selectedProfile {
			return nil, kookyErr
		}
		LogWarning("[Cookie] kooky failed for %s: %v → falling back to yt-dlp", browser, kookyErr)
	}

	// ── Bước 2: Fallback sang yt-dlp ───────────────────────────────────
	LogInfo("[Cookie] Falling back to yt-dlp for %s", browser)
	return m.extractBrowserDataViaYTDLP(ctx, browser, targetHost)
}

func (m *CookieManager) extractBrowserDataViaKooky(ctx context.Context, browser string, targetHost string) (result *BrowserExportData, err error) {
	browser = normalizeBrowserName(browser)
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("kooky panic: %v", r)
			result = nil
		}
	}()

	m.mu.RLock()
	selectedBrowser, selectedProfile := normalizeBrowserName(m.config.SelectedBrowser), m.config.SelectedProfile
	m.mu.RUnlock()
	stores := make([]kooky.CookieStore, 0)
	selectedCookiePath := ""
	if selectedBrowser == browser && selectedProfile != "" {
		var pathErr error
		selectedCookiePath, pathErr = cookieStorePathFromProfile(browser, selectedProfile)
		if pathErr != nil {
			return nil, pathErr
		}
		selectedStore, openErr := openKookyStoreFromPath(browser, selectedCookiePath, kooky.Valid)
		if openErr != nil {
			return nil, fmt.Errorf("cannot open selected %s cookie store: %w", browser, openErr)
		}
		stores = append(stores, selectedStore)
	} else {
		stores = kooky.FindAllCookieStores(ctx)
	}

	var allCookies []parsedCookie
	var xhsSession string

	var relevantDomains []string
	if targetHost != "" {
		parts := strings.Split(targetHost, ".")
		if len(parts) >= 2 {
			base := parts[len(parts)-2] + "." + parts[len(parts)-1]
			relevantDomains = append(relevantDomains, base)
		}
	}
	// Special domain logic
	if strings.Contains(targetHost, "youtube.com") || strings.Contains(targetHost, "youtu.be") {
		relevantDomains = []string{"youtube.com"} // ONLY youtube.com, removed google.com per user request
	} else if IsXiaohongshu(targetHost) {
		relevantDomains = xhsDomains // tự động lấy toàn bộ domain đã khai báo, gồm rednote.com
	}

	var filters []kooky.Filter
	filters = append(filters, kooky.Valid)

	cookieMap := make(map[string]parsedCookie)

	getBaseDomain := func(d string) string {
		d = strings.TrimPrefix(d, ".")
		parts := strings.Split(d, ".")
		if len(parts) >= 2 {
			return parts[len(parts)-2] + "." + parts[len(parts)-1]
		}
		return d
	}

	for _, store := range stores {
		storeClosed := false
		closeStore := func() {
			if !storeClosed {
				_ = store.Close()
				storeClosed = true
			}
		}
		if !strings.EqualFold(normalizeBrowserName(store.Browser()), browser) {
			closeStore()
			continue
		}
		if selectedCookiePath != "" && filepath.Clean(store.FilePath()) != filepath.Clean(selectedCookiePath) {
			closeStore()
			continue
		}

		readStore := store
		cleanupReadStore := closeStore
		if shouldUseTemporaryBrowserStore() {
			tempSource, tempErr := buildTempBrowserSourceFromStore(store, browser)
			closeStore()
			if tempErr != nil {
				LogWarning("[Cookie] cannot prepare temporary store for %s (%s): %v, skipping store",
					browser, store.FilePath(), tempErr)
				continue
			}

			tempStore, tempErr := openKookyStoreFromPath(browser, tempSource.cookiePath, filters...)
			if tempErr != nil {
				tempSource.cleanup()
				LogWarning("[Cookie] cannot open temporary store for %s (%s): %v, skipping store",
					browser, tempSource.cookiePath, tempErr)
				continue
			}

			readStore = tempStore
			cleanupReadStore = func() {
				_ = tempStore.Close()
				tempSource.cleanup()
			}
			LogInfo("[Cookie] reading %s cookies from temporary store: %s", browser, tempSource.cookiePath)
		}

		type cookieResult struct{ cookies []*kooky.Cookie }
		traverseCtx, cancelTraverse := context.WithTimeout(ctx, 10*time.Second)
		ch := make(chan cookieResult, 1)
		go func(s kooky.CookieStore) {
			defer cancelTraverse()
			defer cleanupReadStore()
			defer func() {
				if r := recover(); r != nil {
					LogWarning("[Cookie] kooky TraverseCookies panic: %v", r)
					ch <- cookieResult{nil}
				}
			}()
			ch <- cookieResult{s.TraverseCookies(filters...).Collect(traverseCtx)}
		}(readStore)

		var cookies []*kooky.Cookie
		select {
		case result := <-ch:
			cookies = result.cookies
		case <-traverseCtx.Done():
			LogWarning("[Cookie] kooky traversal timed out or was cancelled for %s", browser)
			continue
		}
		for _, c := range cookies {
			// domain filter
			if len(relevantDomains) > 0 {
				matched := false
				for _, rd := range relevantDomains {
					if strings.HasSuffix(c.Domain, rd) {
						matched = true
						break
					}
				}
				if !matched {
					continue
				}
			}

			// skip excessively large cookies (YouTube max header is 8KB)
			if len(c.Name)+len(c.Value) > 1000 {
				continue
			}

			baseDom := getBaseDomain(c.Domain)
			key := fmt.Sprintf("%s|%s", baseDom, c.Name) // Strict deduplication by base domain and name

			// Prefer cookies with actual values if a duplicate exists
			if existing, exists := cookieMap[key]; exists {
				if c.Value != "" && existing.Value == "" {
					cookieMap[key] = parsedCookie{
						Domain:  c.Domain,
						Path:    c.Path,
						Name:    c.Name,
						Value:   c.Value,
						Secure:  c.Secure,
						Expires: c.Expires.Unix(),
					}
				}
			} else {
				cookieMap[key] = parsedCookie{
					Domain:  c.Domain,
					Path:    c.Path,
					Name:    c.Name,
					Value:   c.Value,
					Secure:  c.Secure,
					Expires: c.Expires.Unix(),
				}
			}

			if IsXiaohongshu(c.Domain) && c.Name == "web_session" {
				xhsSession = c.Value
			}
		}
	}

	for _, c := range cookieMap {
		allCookies = append(allCookies, c)
	}

	userAgent := GetUserAgent(browser)

	data := &BrowserExportData{
		BrowserName: browser,
		UserAgent:   userAgent,
		WebSession:  xhsSession,
		Cookies:     allCookies,
		Headers:     make(map[string]string),
		ExportTime:  time.Now(),
	}

	if xhsSession != "" {
		data.Headers["Cookie"] = "web_session=" + xhsSession
	}

	hasValidCookies := false
	for _, c := range allCookies {
		if c.Value != "" {
			hasValidCookies = true
			break
		}
	}

	if !hasValidCookies {
		if hasSelectedBrowserProfile(selectedBrowser, selectedProfile, browser) {
			return nil, fmt.Errorf("selected %s profile contains no valid cookies", browser)
		}
		LogInfo("[Cookie] kooky returned no valid cookies for %s, falling back to yt-dlp", browser)
		return m.extractBrowserDataViaYTDLP(ctx, browser, targetHost)
	}

	return data, nil
}

func (m *CookieManager) exportBrowserDataToJSON(data *BrowserExportData) {
	dir := GetConfigDir()
	if dir == "" {
		return
	}
	path := filepath.Join(dir, "browser_export.json")
	bytes, _ := json.MarshalIndent(data, "", "  ")
	_ = os.WriteFile(path, bytes, 0644)
}

func writeParsedCookiesToNetscapeFile(cookies []parsedCookie) (string, error) {
	dir := filepath.Join(os.TempDir(), "ytdown")
	os.MkdirAll(dir, 0755)

	tmpFile, err := os.CreateTemp(dir, "cookies-*.txt")
	if err != nil {
		return "", err
	}
	path := tmpFile.Name()
	defer tmpFile.Close()

	tmpFile.WriteString("# Netscape HTTP Cookie File\n")

	LogInfo("[Cookie] Writing %d cookies to %s", len(cookies), path)
	var cookieNames []string

	for _, c := range cookies {
		domain := c.Domain
		if domain == "" {
			continue
		}

		if !strings.HasPrefix(domain, ".") {
			domain = "." + domain
		}
		domainSpecified := "TRUE"

		cPath := c.Path
		if cPath == "" {
			cPath = "/"
		}
		secure := "FALSE"
		if c.Secure {
			secure = "TRUE"
		}
		expires := c.Expires
		if expires <= 0 {
			expires = 2147483647
		}

		name := strings.ReplaceAll(c.Name, "\n", "")
		name = strings.ReplaceAll(name, "\r", "")
		name = strings.ReplaceAll(name, "\t", "%09")

		value := strings.ReplaceAll(c.Value, "\n", "")
		value = strings.ReplaceAll(value, "\r", "")
		value = strings.ReplaceAll(value, "\t", "%09")

		line := fmt.Sprintf("%s\t%s\t%s\t%s\t%d\t%s\t%s\n", domain, domainSpecified, cPath, secure, expires, name, value)
		tmpFile.WriteString(line)

		cookieNames = append(cookieNames, name)
	}

	LogInfo("[Cookie] Included cookies: %s", strings.Join(cookieNames, ", "))
	return path, nil
}

var xhsDomains = []string{
	"xiaohongshu.com",
	"xhslink.com",
	"redbookchina.com",
	"rednote.com",
	"xhscdn.com",
}

// IsXiaohongshu checks if the URL belongs to Xiaohongshu
func isXHSNoteURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" {
		return false
	}
	host := strings.ToLower(strings.TrimPrefix(u.Hostname(), "www."))
	if host == "xhslink.com" {
		return true
	}
	if host != "xiaohongshu.com" && host != "rednote.com" {
		return false
	}
	path := strings.ToLower(u.Path)
	return strings.HasPrefix(path, "/discovery/item/") ||
		strings.HasPrefix(path, "/explore/") ||
		u.Query().Get("noteId") != ""
}

func IsXiaohongshu(url string) bool {
	lower := strings.ToLower(url)
	for _, domain := range xhsDomains {
		if strings.Contains(lower, domain) {
			return true
		}
	}
	return false
}

func (m *CookieManager) cachedXHSSession(browser, profile string) string {
	m.state.mu.RLock()
	defer m.state.mu.RUnlock()
	if normalizeBrowserName(m.state.xhsSessionBrowser) != normalizeBrowserName(browser) ||
		filepath.Clean(m.state.xhsSessionProfile) != filepath.Clean(profile) {
		return ""
	}
	return m.state.xhsSession
}

func (m *CookieManager) invalidateXHSSessionCache(browser, profile string) {
	m.xhsCookieUseMu.Lock()
	defer m.xhsCookieUseMu.Unlock()
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	if normalizeBrowserName(m.state.xhsSessionBrowser) == normalizeBrowserName(browser) &&
		filepath.Clean(m.state.xhsSessionProfile) == filepath.Clean(profile) {
		for _, cookieFile := range m.state.xhsCookieFiles {
			_ = os.RemoveAll(filepath.Dir(cookieFile))
		}
		m.state.xhsCookieFiles = nil
		m.state.xhsSession = ""
		m.state.xhsSessionBrowser = ""
		m.state.xhsSessionProfile = ""
		m.state.xhsCacheTime = time.Time{}
	}
}

// clearSensitiveCookieState removes in-memory credentials and their temporary cookie file.
func (m *CookieManager) clearSensitiveCookieState() {
	m.xhsCookieUseMu.Lock()
	defer m.xhsCookieUseMu.Unlock()
	m.extractionMu.Lock()
	defer m.extractionMu.Unlock()

	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	if m.state.tempFile != "" {
		_ = os.RemoveAll(filepath.Dir(m.state.tempFile))
	}
	for _, cookieFile := range m.state.xhsCookieFiles {
		_ = os.RemoveAll(filepath.Dir(cookieFile))
	}
	m.state.header = ""
	m.state.cookies = nil
	m.state.tempFile = ""
	m.state.xhsCookieFiles = nil
	m.state.domainBrowserData = make(map[string]*BrowserExportData)
	m.state.xhsSession = ""
	m.state.xhsSessionBrowser = ""
	m.state.xhsSessionProfile = ""
	m.state.xhsCacheTime = time.Time{}
}

// extractWebSessionFromBrowser reads web_session directly from the selected browser cookie store.
func (m *CookieManager) extractWebSessionFromBrowser(ctx context.Context, browser string, _ string) (string, error) {
	browser = normalizeBrowserName(browser)
	m.extractionMu.Lock()
	defer m.extractionMu.Unlock()

	m.mu.RLock()
	selectedBrowser := normalizeBrowserName(m.config.SelectedBrowser)
	selectedProfile := m.config.SelectedProfile
	m.mu.RUnlock()
	if selectedBrowser != browser || selectedProfile == "" {
		return "", fmt.Errorf("select a %s browser profile before downloading Xiaohongshu", browser)
	}

	if session := m.cachedXHSSession(browser, selectedProfile); session != "" {
		return session, nil
	}

	// Export from the exact browser/profile selected by the user. This is the
	// same browser-cookie export mechanism used by yt-dlp downloads.
	data, err := m.extractBrowserDataViaYTDLP(ctx, browser, "www.xiaohongshu.com")
	if err != nil {
		return "", err
	}
	if data == nil || strings.TrimSpace(data.WebSession) == "" {
		return "", fmt.Errorf("no web_session cookie found in selected browser profile; sign in to Xiaohongshu in that profile and try again")
	}

	session := strings.TrimSpace(data.WebSession)
	m.state.mu.Lock()
	m.state.xhsSession = session
	m.state.xhsSessionBrowser = browser
	m.state.xhsSessionProfile = selectedProfile
	m.state.xhsCacheTime = time.Now()
	m.state.mu.Unlock()
	LogInfo("[Cookie] web_session read from selected %s profile", browser)
	return session, nil
}

func xhsSessionCookieArgs(session string) ([]string, func(), error) {
	return xhsSessionCookieArgsForURL(session, "https://www.xiaohongshu.com/")
}

func xhsSessionCookieArgsForURL(session, targetURL string) ([]string, func(), error) {
	session = strings.TrimSpace(session)
	if session == "" {
		return nil, nil, fmt.Errorf("Xiaohongshu web_session is empty; sign in and retry")
	}
	domains, err := xhsCookieDomainsForURL(targetURL)
	if err != nil {
		return nil, nil, err
	}
	cookies := make([]parsedCookie, 0, len(domains))
	for _, domain := range domains {
		cookies = append(cookies, parsedCookie{
			Domain:  domain,
			Path:    "/",
			Name:    "web_session",
			Value:   session,
			Secure:  true,
			Expires: 2147483647,
		})
	}
	cookieFile, err := writeParsedCookiesToTemporaryFile(cookies)
	if err != nil {
		return nil, nil, fmt.Errorf("write temporary XHS session cookie: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(filepath.Dir(cookieFile)) }
	return []string{"--cookies", cookieFile}, cleanup, nil
}

func xhsCookieDomainsForURL(targetURL string) ([]string, error) {
	parsed, err := url.Parse(targetURL)
	if err != nil || parsed.Hostname() == "" {
		return nil, fmt.Errorf("invalid XHS media URL")
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	domains := []string{".xiaohongshu.com"}
	for _, allowed := range []string{"rednote.com", "xhslink.com", "redbookchina.com", "xhscdn.com"} {
		if host == allowed || strings.HasSuffix(host, "."+allowed) {
			domains = append(domains, "."+allowed)
			return domains, nil
		}
	}
	if host == "xiaohongshu.com" || strings.HasSuffix(host, ".xiaohongshu.com") {
		return domains, nil
	}
	return nil, fmt.Errorf("refusing to scope Xiaohongshu session to non-XHS host")
}

func writeParsedCookiesToTemporaryFile(cookies []parsedCookie) (string, error) {
	dir, err := os.MkdirTemp("", "ytdown-cookie-")
	if err != nil {
		return "", fmt.Errorf("failed to prepare temporary cookie storage: %w", err)
	}
	path := filepath.Join(dir, "cookies.txt")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
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
		domain := cookie.Domain
		if !strings.HasPrefix(domain, ".") {
			domain = "." + domain
		}
		line := fmt.Sprintf("%s\tTRUE\t/\tTRUE\t%d\t%s\t%s\n", domain, cookie.Expires, cookie.Name, cookie.Value)
		if _, err := file.WriteString(line); err != nil {
			_ = os.RemoveAll(dir)
			return "", fmt.Errorf("failed to write temporary cookie file: %w", err)
		}
	}
	return path, nil
}

func (m *CookieManager) hasCookieForURL(targetURL string) bool {
	m.mu.RLock()
	mode := m.config.Mode
	browser := normalizeBrowserName(m.config.SelectedBrowser)
	profile := m.config.SelectedProfile
	m.mu.RUnlock()
	if mode == CookieModeBrowser {
		if IsXiaohongshu(targetURL) {
			m.state.mu.RLock()
			defer m.state.mu.RUnlock()
			return strings.TrimSpace(m.state.xhsSession) != "" &&
				normalizeBrowserName(m.state.xhsSessionBrowser) == browser &&
				filepath.Clean(m.state.xhsSessionProfile) == filepath.Clean(profile)
		}
		return true
	}
	if mode == CookieModeManual {
		m.state.mu.RLock()
		header := m.state.header
		m.state.mu.RUnlock()
		if IsXiaohongshu(targetURL) {
			return extractXHSSession(header) != ""
		}
		return header != ""
	}
	return false
}

func (m *CookieManager) xhsSessionCookieArgsForDownload(session, targetURL string) ([]string, func(), error) {
	session = strings.TrimSpace(session)
	if session == "" {
		return nil, nil, fmt.Errorf("Xiaohongshu web_session is empty; sign in and retry")
	}
	domains, err := xhsCookieDomainsForURL(targetURL)
	if err != nil {
		return nil, nil, err
	}
	scopeKey := strings.Join(domains, "|")

	m.xhsCookieUseMu.RLock()
	unlock := func() { m.xhsCookieUseMu.RUnlock() }
	m.state.mu.Lock()
	if m.state.xhsSession != session {
		m.state.mu.Unlock()
		args, cleanup, err := xhsSessionCookieArgsForURL(session, targetURL)
		if err != nil {
			unlock()
			return nil, nil, err
		}
		return args, func() {
			cleanup()
			unlock()
		}, nil
	}
	if cookieFile := m.state.xhsCookieFiles[scopeKey]; cookieFile != "" {
		if _, err := os.Stat(cookieFile); err == nil {
			m.state.mu.Unlock()
			return []string{"--cookies", cookieFile}, unlock, nil
		}
	}

	cookies := make([]parsedCookie, 0, len(domains))
	for _, domain := range domains {
		cookies = append(cookies, parsedCookie{
			Domain: domain, Path: "/", Name: "web_session", Value: session,
			Secure: true, Expires: 2147483647,
		})
	}
	cookieFile, err := writeParsedCookiesToTemporaryFile(cookies)
	if err != nil {
		m.state.mu.Unlock()
		unlock()
		return nil, nil, fmt.Errorf("write temporary XHS session cookie: %w", err)
	}
	if m.state.xhsCookieFiles == nil {
		m.state.xhsCookieFiles = make(map[string]string)
	}
	m.state.xhsCookieFiles[scopeKey] = cookieFile
	m.state.mu.Unlock()
	return []string{"--cookies", cookieFile}, unlock, nil
}

func replaceBrowserCookieArgsWithXHSSession(args, sessionArgs []string) []string {
	filtered := make([]string, 0, len(args)+len(sessionArgs))
	for i := 0; i < len(args); i++ {
		if args[i] == "--cookies-from-browser" {
			i++
			continue
		}
		filtered = append(filtered, args[i])
	}
	return append(filtered, sessionArgs...)
}

func xhsCookieExportResult(commandErr error, data []byte, readErr error) (string, error) {
	if commandErr != nil {
		return "", xhsCookieExportError(commandErr, readErr)
	}
	if readErr != nil {
		return "", xhsCookieExportError(nil, readErr)
	}
	return extractWebSessionFromNetscapeCookieFile(string(data))
}

func xhsCookieExportError(commandErr, readErr error) error {
	if commandErr != nil {
		return fmt.Errorf("yt-dlp could not export cookies from the selected browser profile; verify browser access and macOS privacy permissions, then retry")
	}
	return fmt.Errorf("could not read the temporary XHS cookie export; verify temporary storage access and retry: %w", readErr)
}

func extractWebSessionFromNetscapeCookieFile(content string) (string, error) {
	var rednoteSession string
	for _, line := range strings.Split(content, "\n") {
		parts := strings.Fields(line)
		if len(parts) < 7 || parts[5] != "web_session" || parts[6] == "" {
			continue
		}
		domain := strings.TrimPrefix(strings.ToLower(parts[0]), ".")
		if domain == "xiaohongshu.com" || strings.HasSuffix(domain, ".xiaohongshu.com") {
			return parts[6], nil
		}
		if domain == "rednote.com" || strings.HasSuffix(domain, ".rednote.com") {
			rednoteSession = parts[6]
		}
	}
	if rednoteSession != "" {
		return rednoteSession, nil
	}
	return "", fmt.Errorf("no web_session cookie found in selected browser profile; sign in to Xiaohongshu in that profile and try again")
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
		".xhslink.com",
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
	manager.state.xhsSession = ""
	manager.state.xhsCacheTime = time.Time{}
	manager.state.domainBrowserData = nil
	manager.state.mu.Unlock()
	ClearUserAgentCache()

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

// ── Linux cookie extraction via yt-dlp ────────────────────────────────────

func browserCookieExportArgs(browserSpec, cookieFile string) []string {
	return []string{
		"--cookies-from-browser", browserSpec,
		"--cookies", cookieFile,
		"--no-warnings",
		"--no-playlist",
	}
}

// extractBrowserDataViaYTDLP dùng yt-dlp --cookies-from-browser thay vì kooky
// để tránh vấn đề GNOME Keyring/KWallet decrypt trên Linux.
func (m *CookieManager) extractBrowserDataViaYTDLP(ctx context.Context, browser string, targetHost string) (*BrowserExportData, error) {
	browser = normalizeBrowserName(browser)
	ytdlp := getResourcePath("yt-dlp")
	if ytdlp == "" {
		return nil, fmt.Errorf("yt-dlp not found")
	}

	tempDir, err := os.MkdirTemp("", "ytdown-cookies-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)
	tempCookieFile := filepath.Join(tempDir, "cookies.txt")

	cmdCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	browserSpec, specErr := m.ytdlpBrowserSpec(ctx, browser)
	if specErr != nil {
		return nil, specErr
	}
	if shouldUseTemporaryBrowserStore() {
		store, storeErr := m.findBrowserStore(ctx, browser)
		if storeErr != nil {
			return nil, storeErr
		}
		if store != nil {
			tempSource, tempErr := buildTempBrowserSourceFromStore(store, browser)
			_ = store.Close()
			if tempErr != nil {
				LogWarning("[Cookie] Cannot prepare temporary browser source for yt-dlp (%s): %v", browser, tempErr)
			} else {
				defer tempSource.cleanup()
				browserSpec = buildYTDLPBrowserSpec(browser, tempSource.profilePath)
				LogInfo("[Cookie] yt-dlp using temporary browser profile for %s: %s", browser, tempSource.profilePath)
			}
		}
	}

	cmd := platform.CommandContext(cmdCtx, ytdlp, browserCookieExportArgs(browserSpec, tempCookieFile)...)
	if output, exportErr := cmd.CombinedOutput(); exportErr != nil {
		LogDownloadError("browser cookie export failed for %s: %s", browser, string(output))
		return nil, xhsCookieExportError(exportErr, nil)
	}

	raw, err := os.ReadFile(tempCookieFile)
	if err != nil {
		return nil, fmt.Errorf("cookie file not written by yt-dlp: %w", err)
	}

	cookies, xhsSession := parseNetscapeCookieFileForDomain(string(raw), targetHost)
	userAgent := GetUserAgent(browser)

	result := &BrowserExportData{
		BrowserName: browser,
		UserAgent:   userAgent,
		WebSession:  xhsSession,
		Cookies:     cookies,
		Headers:     make(map[string]string),
		ExportTime:  time.Now(),
	}
	if xhsSession != "" {
		result.Headers["Cookie"] = "web_session=" + xhsSession
	}
	LogInfo("[Cookie] yt-dlp extracted %d cookies for %s from %s",
		len(cookies), targetHost, browser)
	return result, nil
}

// parseNetscapeCookieFileForDomain parse Netscape cookie file, filter theo domain
// và bắt web_session cho Xiaohongshu.
func parseNetscapeCookieFileForDomain(content, targetHost string) ([]parsedCookie, string) {
	var cookies []parsedCookie
	var xhsSession string
	var rednoteSession string

	// Build relevant domains
	var relevantDomains []string
	if IsXiaohongshu(targetHost) {
		relevantDomains = xhsDomains
	} else if strings.Contains(targetHost, "youtube.com") || strings.Contains(targetHost, "youtu.be") {
		relevantDomains = []string{"youtube.com"}
	} else if targetHost != "" {
		parts := strings.Split(targetHost, ".")
		if len(parts) >= 2 {
			relevantDomains = []string{parts[len(parts)-2] + "." + parts[len(parts)-1]}
		}
	}

	seen := make(map[string]bool)
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Netscape format: domain \t domainFlag \t path \t secure \t expiry \t name \t value
		parts := strings.Split(line, "\t")
		if len(parts) < 7 {
			continue
		}
		domain, path, name, value := parts[0], parts[2], parts[5], parts[6]

		// Filter by domain
		if len(relevantDomains) > 0 {
			matched := false
			for _, rd := range relevantDomains {
				if strings.Contains(domain, rd) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}

		// Dedup
		key := strings.TrimPrefix(domain, ".") + "|" + name
		if seen[key] {
			continue
		}
		seen[key] = true

		var expiry int64
		fmt.Sscanf(parts[4], "%d", &expiry)
		secure := strings.EqualFold(parts[3], "TRUE")

		cookies = append(cookies, parsedCookie{
			Domain:  domain,
			Path:    path,
			Name:    name,
			Value:   value,
			Secure:  secure,
			Expires: expiry,
		})

		if name == "web_session" {
			normalizedDomain := strings.TrimPrefix(strings.ToLower(domain), ".")
			if normalizedDomain == "xiaohongshu.com" || strings.HasSuffix(normalizedDomain, ".xiaohongshu.com") {
				xhsSession = value
			} else if rednoteSession == "" && (normalizedDomain == "rednote.com" || strings.HasSuffix(normalizedDomain, ".rednote.com")) {
				rednoteSession = value
			}
		}
	}
	if xhsSession == "" {
		xhsSession = rednoteSession
	}
	return cookies, xhsSession
}

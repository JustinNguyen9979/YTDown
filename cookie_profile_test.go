package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCookieStorePathFromProfile(t *testing.T) {
	root := t.TempDir()
	tests := []struct {
		name       string
		browser    string
		createPath string
	}{
		{name: "firefox profile", browser: "firefox", createPath: "cookies.sqlite"},
		{name: "chromium network store", browser: "chrome", createPath: filepath.Join("Network", "Cookies")},
		{name: "chromium legacy store", browser: "edge", createPath: "Cookies"},
		{name: "vivaldi chromium store", browser: "vivaldi", createPath: filepath.Join("Network", "Cookies")},
		{name: "Safari cookie store", browser: "safari", createPath: filepath.Join("Cookies", "Cookies.binarycookies")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profile := filepath.Join(root, tt.name)
			cookiePath := filepath.Join(profile, tt.createPath)
			if err := os.MkdirAll(filepath.Dir(cookiePath), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(cookiePath, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := cookieStorePathFromProfile(tt.browser, profile)
			if err != nil {
				t.Fatalf("cookieStorePathFromProfile() error = %v", err)
			}
			if got != cookiePath {
				t.Fatalf("cookieStorePathFromProfile() = %q, want %q", got, cookiePath)
			}
		})
	}
}

func TestResolveSelectedBrowserProfileUsesProfileSelectedInParentDirectory(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "Firefox")
	profile := filepath.Join(parent, "Profiles", "default-release")
	if err := os.MkdirAll(profile, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profile, "cookies.sqlite"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	ini := "[InstallA1B2C3]\nDefault=Profiles/default-release\nLocked=1\n[Profile0]\nPath=Profiles/default-release\nIsRelative=1\n"
	if err := os.WriteFile(filepath.Join(parent, "profiles.ini"), []byte(ini), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := resolveSelectedBrowserProfile("firefox", parent)
	if err != nil {
		t.Fatalf("resolveSelectedBrowserProfile() error = %v", err)
	}
	if got != profile {
		t.Fatalf("resolveSelectedBrowserProfile() = %q, want %q", got, profile)
	}
}

func TestResolveSelectedBrowserProfileFindsChromiumDefaultChild(t *testing.T) {
	parent := t.TempDir()
	profile := filepath.Join(parent, "Default")
	cookieDB := filepath.Join(profile, "Network", "Cookies")
	if err := os.MkdirAll(filepath.Dir(cookieDB), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cookieDB, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := resolveSelectedBrowserProfile("chrome", parent)
	if err != nil {
		t.Fatalf("resolveSelectedBrowserProfile() error = %v", err)
	}
	if got != profile {
		t.Fatalf("resolveSelectedBrowserProfile() = %q, want %q", got, profile)
	}
}

func TestCookieStorePathFromProfileRejectsMissingOrUnsupportedStore(t *testing.T) {
	for _, tt := range []struct {
		name    string
		browser string
	}{
		{name: "missing Firefox DB", browser: "firefox"},
		{name: "missing Chromium DB", browser: "chrome"},
		{name: "unsupported browser", browser: "netscape"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := cookieStorePathFromProfile(tt.browser, t.TempDir())
			if err == nil {
				t.Fatal("cookieStorePathFromProfile() expected an error")
			}
		})
	}
}

func TestXHSCookieExportErrorDistinguishesExportFailureFromMissingSession(t *testing.T) {
	sentinel := "COOKIE_SECRET_SENTINEL"
	tests := []struct {
		name        string
		commandErr  error
		readErr     error
		wantMessage string
		wantSecret  bool
	}{
		{
			name:        "yt-dlp export failed and cookie file is missing",
			commandErr:  errors.New("yt-dlp exited with status 1"),
			readErr:     os.ErrNotExist,
			wantMessage: "could not export cookies from the selected browser profile",
		},
		{
			name:        "export succeeded but cookie file cannot be read",
			readErr:     os.ErrPermission,
			wantMessage: "could not read the temporary XHS cookie export",
		},
		{
			name:       "export failure output is not exposed",
			commandErr: errors.New("yt-dlp failed with " + sentinel),
			readErr:    os.ErrNotExist,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := xhsCookieExportError(tt.commandErr, tt.readErr)
			if err == nil {
				t.Fatal("xhsCookieExportError() returned nil")
			}
			message := err.Error()
			if tt.wantMessage != "" && !strings.Contains(message, tt.wantMessage) {
				t.Fatalf("xhsCookieExportError() = %q, want message containing %q", message, tt.wantMessage)
			}
			if strings.Contains(message, sentinel) {
				t.Fatal("xhsCookieExportError() exposed sensitive command output")
			}
		})
	}
}

func TestXHSCookieExportFailureRejectsExistingCookieFile(t *testing.T) {
	existingCookieFile := "# Netscape HTTP Cookie File\n.xiaohongshu.com\tTRUE\t/\tTRUE\t2147483647\tweb_session\tstale-session-sentinel\n"
	commandErr := errors.New("yt-dlp exited with status 1")
	if session, err := xhsCookieExportResult(commandErr, []byte(existingCookieFile), nil); err == nil {
		t.Fatalf("xhsCookieExportResult() returned stale session %q after export failed", session)
	} else if !strings.Contains(err.Error(), "yt-dlp could not export cookies") {
		t.Fatalf("xhsCookieExportResult() error = %q, want export failure guidance", err)
	}
}

func TestXHSCookieExportSuccessReturnsSessionFromOutput(t *testing.T) {
	content := "# Netscape HTTP Cookie File\n.xiaohongshu.com\tTRUE\t/\tTRUE\t2147483647\tweb_session\tsynthetic-session-sentinel\n"
	session, err := xhsCookieExportResult(nil, []byte(content), nil)
	if err != nil {
		t.Fatalf("xhsCookieExportResult() error = %v", err)
	}
	if session != "synthetic-session-sentinel" {
		t.Fatalf("xhsCookieExportResult() returned %q, want synthetic session", session)
	}
}

func TestBrowserCookieExportArgsDoNotIncludeURL(t *testing.T) {
	args := browserCookieExportArgs("firefox:/Users/test/Library/Application Support/Firefox/Profiles/default", "/tmp/cookies.txt")
	want := []string{
		"--cookies-from-browser", "firefox:/Users/test/Library/Application Support/Firefox/Profiles/default",
		"--cookies", "/tmp/cookies.txt",
		"--no-warnings",
		"--no-playlist",
	}
	if !bytes.Equal([]byte(strings.Join(args, "\x00")), []byte(strings.Join(want, "\x00"))) {
		t.Fatalf("browserCookieExportArgs() = %#v, want %#v", args, want)
	}
	for _, arg := range args {
		if strings.HasPrefix(arg, "https://") {
			t.Fatalf("browserCookieExportArgs() unexpectedly contains URL argument %q", arg)
		}
	}
}

func TestBrowserCookieExportArgsOutputCanYieldXHSSession(t *testing.T) {
	args := browserCookieExportArgs("firefox", "/tmp/cookies.txt")
	if len(args) == 0 || args[len(args)-1] == "https://www.xiaohongshu.com/" {
		t.Fatalf("browserCookieExportArgs() must not require a site URL: %#v", args)
	}

	content := "# Netscape HTTP Cookie File\n.xiaohongshu.com\tTRUE\t/\tTRUE\t2147483647\tweb_session\tsynthetic-session-sentinel\n"
	_, session := parseNetscapeCookieFileForDomain(content, "www.xiaohongshu.com")
	if session != "synthetic-session-sentinel" {
		t.Fatalf("parseNetscapeCookieFileForDomain() returned session %q, want synthetic session", session)
	}
}

func TestBrowserProfileAccessErrorExplainsManualFullDiskAccess(t *testing.T) {
	err := browserProfileAccessError("firefox", os.ErrPermission)
	message := err.Error()
	if !strings.Contains(message, "Full Disk Access") || !strings.Contains(strings.ToLower(message), "system settings") {
		t.Fatalf("browserProfileAccessError() = %q, want actionable manual Full Disk Access guidance", message)
	}
	if !strings.Contains(message, "firefox") {
		t.Fatalf("browserProfileAccessError() = %q, want browser name", message)
	}
	if !strings.Contains(message, "Terminal") || !strings.Contains(message, "IDE") || !strings.Contains(message, "restart") {
		t.Fatalf("browserProfileAccessError() = %q, want development-process FDA guidance and restart instructions", message)
	}
}

func TestCookieConfigPersistsSelectedProfile(t *testing.T) {
	bookmark := []byte("synthetic-security-scoped-bookmark")
	config := CookieConfig{
		Mode:                    CookieModeBrowser,
		SelectedBrowser:         "chrome",
		SelectedProfile:         "/Users/test/Library/Application Support/Google/Chrome/Profile 2",
		SelectedProfileBookmark: bookmark,
	}
	encoded, err := json.Marshal(config.persisted())
	if err != nil {
		t.Fatalf("json.Marshal(CookieConfig) error = %v", err)
	}
	var persisted persistedCookieConfig
	if err := json.Unmarshal(encoded, &persisted); err != nil {
		t.Fatalf("json.Unmarshal(persistedCookieConfig) error = %v", err)
	}
	var restored CookieConfig
	restored.restorePersisted(persisted)
	if restored.SelectedProfile != config.SelectedProfile {
		t.Fatalf("restored profile = %q, want %q", restored.SelectedProfile, config.SelectedProfile)
	}
	if !bytes.Equal(restored.SelectedProfileBookmark, bookmark) {
		t.Fatalf("restored bookmark = %q, want original synthetic bookmark", restored.SelectedProfileBookmark)
	}
	want := "chrome:/Users/test/Library/Application Support/Google/Chrome/Profile 2"
	if got := ytdlpBrowserSpecForSelection(config.SelectedBrowser, config.SelectedProfile); got != want {
		t.Fatalf("ytdlpBrowserSpecForSelection() = %q, want %q", got, want)
	}
}

func TestBuildAutomaticBrowserConfigSelectsDiscoveredProfile(t *testing.T) {
	home := t.TempDir()
	profile := filepath.Join(home, "Library", "Application Support", "Google", "Chrome", "Default")
	cookieDB := filepath.Join(profile, "Network", "Cookies")
	if err := os.MkdirAll(filepath.Dir(cookieDB), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cookieDB, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	config, err := buildAutomaticBrowserConfig("Google Chrome", home)
	if err != nil {
		t.Fatalf("buildAutomaticBrowserConfig() error = %v", err)
	}
	if config.Mode != CookieModeBrowser || config.SelectedBrowser != "chrome" || config.SelectedProfile != profile {
		t.Fatalf("buildAutomaticBrowserConfig() = %#v, want Chrome default profile %q", config, profile)
	}
	if len(config.SelectedProfileBookmark) != 0 {
		t.Fatal("automatic profile selection must not require a picker bookmark")
	}
}

func TestBuildAutomaticBrowserConfigIgnoresPreviouslySelectedProfile(t *testing.T) {
	home := t.TempDir()
	defaultProfile := filepath.Join(home, "Library", "Application Support", "Firefox", "Profiles", "default-release")
	cookiesDB := filepath.Join(defaultProfile, "cookies.sqlite")
	if err := os.MkdirAll(defaultProfile, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cookiesDB, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	profilesINI := "[InstallA1B2C3]\nDefault=Profiles/default-release\nLocked=1\n[Profile0]\nPath=Profiles/default-release\nIsRelative=1\n"
	firefoxRoot := filepath.Dir(filepath.Dir(defaultProfile))
	if err := os.WriteFile(filepath.Join(firefoxRoot, "profiles.ini"), []byte(profilesINI), 0o600); err != nil {
		t.Fatal(err)
	}

	config, err := buildAutomaticBrowserConfig("firefox", home)
	if err != nil {
		t.Fatalf("buildAutomaticBrowserConfig() error = %v", err)
	}
	if config.SelectedProfile != defaultProfile {
		t.Fatalf("selected profile = %q, want auto-detected default %q", config.SelectedProfile, defaultProfile)
	}
}

func TestRediscoverBrowserCookieConfigUsesCurrentBrowserNotStaleBrowserProfile(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	profile := filepath.Join(home, "Library", "Application Support", "Firefox", "Profiles", "default-release")
	cookieDB := filepath.Join(profile, "cookies.sqlite")
	if err := os.MkdirAll(filepath.Dir(cookieDB), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cookieDB, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	firefoxRoot := filepath.Join(home, "Library", "Application Support", "Firefox")
	ini := "[InstallA1B2C3]\nDefault=Profiles/default-release\nLocked=1\n[Profile0]\nPath=Profiles/default-release\nIsRelative=1\n"
	if err := os.WriteFile(filepath.Join(firefoxRoot, "profiles.ini"), []byte(ini), 0o600); err != nil {
		t.Fatal(err)
	}

	current := CookieConfig{Mode: CookieModeBrowser, SelectedBrowser: "firefox", SelectedProfile: filepath.Join(root, "stale-profile")}
	got, err := rediscoverBrowserCookieConfig(current, home)
	if err != nil {
		t.Fatalf("rediscoverBrowserCookieConfig() error = %v", err)
	}
	if got.SelectedProfile != profile {
		t.Fatalf("SelectedProfile = %q, want newly detected profile %q", got.SelectedProfile, profile)
	}
}

func TestRediscoverBrowserCookieConfigReplacesStaleProfileWithDefault(t *testing.T) {
	home := t.TempDir()
	profile := filepath.Join(home, "Library", "Application Support", "Google", "Chrome", "Default")
	cookieDB := filepath.Join(profile, "Network", "Cookies")
	if err := os.MkdirAll(filepath.Dir(cookieDB), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cookieDB, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	current := CookieConfig{
		Mode:            CookieModeBrowser,
		SelectedBrowser: "chrome",
		SelectedProfile: filepath.Join(home, "manual-old-profile"),
	}
	got, err := rediscoverBrowserCookieConfig(current, home)
	if err != nil {
		t.Fatalf("rediscoverBrowserCookieConfig() error = %v", err)
	}
	if got.SelectedProfile != profile {
		t.Fatalf("SelectedProfile = %q, want auto-detected default %q", got.SelectedProfile, profile)
	}
}

func TestBuildAutomaticBrowserConfigRejectsMissingProfileWithoutMislabelingPermission(t *testing.T) {
	_, err := buildAutomaticBrowserConfig("firefox", t.TempDir())
	if err == nil {
		t.Fatal("buildAutomaticBrowserConfig() expected an error when no Firefox profile exists")
	}
	if !strings.Contains(err.Error(), "no default firefox profile") {
		t.Fatalf("buildAutomaticBrowserConfig() error = %q, want missing-profile diagnosis", err)
	}
	if strings.Contains(err.Error(), "Full Disk Access") {
		t.Fatalf("missing profile was incorrectly described as a permission failure: %v", err)
	}
}

func TestBrowserProfileAccessErrorNamesDevAndPackagedProcesses(t *testing.T) {
	oldVersion := Version
	t.Cleanup(func() { Version = oldVersion })

	Version = "Dev"
	devMessage := browserProfileAccessError("firefox", os.ErrPermission).Error()
	devMessageLower := strings.ToLower(devMessage)
	for _, expected := range []string{"full disk access", "terminal or ide running wails dev", "restart wails dev"} {
		if !strings.Contains(devMessageLower, expected) {
			t.Errorf("development permission error = %q, want %q guidance", devMessage, expected)
		}
	}

	Version = "2026.10.28"
	packagedMessage := browserProfileAccessError("firefox", os.ErrPermission).Error()
	for _, expected := range []string{"Full Disk Access", "YTDown", "quit YTDown completely", "reopen it"} {
		if !strings.Contains(packagedMessage, expected) {
			t.Errorf("packaged permission error = %q, want %q guidance", packagedMessage, expected)
		}
	}
	if strings.Contains(packagedMessage, "Terminal") || strings.Contains(packagedMessage, "IDE") {
		t.Errorf("packaged permission error should not direct users to dev host: %q", packagedMessage)
	}
}

func TestDiscoverDefaultBrowserProfilePreservesPermissionErrors(t *testing.T) {
	home := t.TempDir()
	profile := filepath.Join(home, "Library", "Application Support", "Google", "Chrome", "Default")
	cookieDB := filepath.Join(profile, "Network", "Cookies")
	if err := os.MkdirAll(filepath.Dir(cookieDB), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cookieDB, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := discoverDefaultBrowserProfile("chrome", home, func(_, _ string) error {
		return os.ErrPermission
	})
	if got != "" || !errors.Is(err, os.ErrPermission) {
		t.Fatalf("discoverDefaultBrowserProfile() = (%q, %v), want permission-denied error", got, err)
	}
	if !strings.Contains(err.Error(), "Full Disk Access") {
		t.Fatalf("discoverDefaultBrowserProfile() error = %q, want FDA guidance", err)
	}
}

func TestDiscoverDefaultBrowserProfileKeepsMissingProfileDistinct(t *testing.T) {
	got, err := discoverDefaultBrowserProfile("firefox", t.TempDir(), nil)
	if got != "" || err == nil {
		t.Fatalf("discoverDefaultBrowserProfile() = (%q, %v), want missing-profile error", got, err)
	}
	if errors.Is(err, os.ErrPermission) || strings.Contains(err.Error(), "Full Disk Access") {
		t.Fatalf("missing profile was mislabeled as permission denial: %v", err)
	}
}

func TestCookieConfigLoadsLegacyJSONWithoutBookmark(t *testing.T) {
	var config CookieConfig
	if err := json.Unmarshal([]byte(`{"mode":"browser","selected_browser":"firefox","selected_profile":"/tmp/Profile 1"}`), &config); err != nil {
		t.Fatalf("json.Unmarshal(legacy CookieConfig) error = %v", err)
	}
	if config.SelectedProfile != "/tmp/Profile 1" || len(config.SelectedProfileBookmark) != 0 {
		t.Fatalf("legacy config = %#v, want profile without bookmark", config)
	}
}

func TestHasSelectedBrowserProfile(t *testing.T) {
	for _, tt := range []struct {
		name, selectedBrowser, selectedProfile, requestedBrowser string
		want                                                     bool
	}{
		{name: "matching selected profile", selectedBrowser: "Google Chrome", selectedProfile: "/tmp/Profile 1", requestedBrowser: "chrome", want: true},
		{name: "different browser", selectedBrowser: "firefox", selectedProfile: "/tmp/Profile 1", requestedBrowser: "chrome", want: false},
		{name: "no profile selected", selectedBrowser: "chrome", requestedBrowser: "chrome", want: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasSelectedBrowserProfile(tt.selectedBrowser, tt.selectedProfile, tt.requestedBrowser); got != tt.want {
				t.Fatalf("hasSelectedBrowserProfile() = %v, want %v", got, tt.want)
			}
		})
	}
}

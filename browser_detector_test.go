package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDetectBrowsersFromAppDirectories(t *testing.T) {
	root := t.TempDir()
	apps := filepath.Join(root, "Applications")
	for _, name := range []string{"Google Chrome.app", "Firefox.app", "Unrelated.app"} {
		if err := os.MkdirAll(filepath.Join(apps, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	got := detectBrowsersFromAppDirectories([]string{apps})
	want := []BrowserInfo{
		{ID: "chrome", DisplayName: "Google Chrome"},
		{ID: "firefox", DisplayName: "Mozilla Firefox"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("detectBrowsersFromAppDirectories() = %#v, want %#v", got, want)
	}
}

func TestDetectBrowsersFromAppDirectoriesFindsSymlinkedApplications(t *testing.T) {
	root := t.TempDir()
	apps := filepath.Join(root, "Applications")
	realBundle := filepath.Join(root, "Google Chrome.app")
	if err := os.MkdirAll(realBundle, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(apps, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realBundle, filepath.Join(apps, "Google Chrome.app")); err != nil {
		t.Fatal(err)
	}

	got := detectBrowsersFromAppDirectories([]string{apps})
	want := []BrowserInfo{{ID: "chrome", DisplayName: "Google Chrome"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("detectBrowsersFromAppDirectories() = %#v, want %#v", got, want)
	}
}

func TestDetectBrowsersFromAppDirectoriesFindsUserApplications(t *testing.T) {
	root := t.TempDir()
	userApps := filepath.Join(root, "Applications")
	if err := os.MkdirAll(filepath.Join(userApps, "Google Chrome.app"), 0o755); err != nil {
		t.Fatal(err)
	}

	got := detectBrowsersFromAppDirectories([]string{filepath.Join(root, "System Applications"), userApps})
	want := []BrowserInfo{{ID: "chrome", DisplayName: "Google Chrome"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("detectBrowsersFromAppDirectories() = %#v, want %#v", got, want)
	}
}

func TestDetectBrowsersFromAppDirectoriesIgnoresMissingDirectories(t *testing.T) {
	got := detectBrowsersFromAppDirectories([]string{filepath.Join(t.TempDir(), "missing")})
	if len(got) != 0 {
		t.Fatalf("detectBrowsersFromAppDirectories() = %#v, want no browsers", got)
	}
}

func TestDefaultBrowserProfilePathFindsSupportedDefaults(t *testing.T) {
	home := t.TempDir()
	tests := []struct {
		browser string
		parts   []string
		store   string
	}{
		{browser: "chrome", parts: []string{"Library", "Application Support", "Google", "Chrome", "Default"}, store: filepath.Join("Network", "Cookies")},
		{browser: "edge", parts: []string{"Library", "Application Support", "Microsoft Edge", "Default"}, store: filepath.Join("Network", "Cookies")},
		{browser: "brave", parts: []string{"Library", "Application Support", "BraveSoftware", "Brave-Browser", "Default"}, store: filepath.Join("Network", "Cookies")},
		{browser: "vivaldi", parts: []string{"Library", "Application Support", "Vivaldi", "Default"}, store: filepath.Join("Network", "Cookies")},
		{browser: "opera", parts: []string{"Library", "Application Support", "com.operasoftware.Opera", "Stable"}, store: filepath.Join("Network", "Cookies")},
		{browser: "safari", parts: []string{"Library", "Containers", "com.apple.Safari", "Data", "Library"}, store: filepath.Join("Cookies", "Cookies.binarycookies")},
	}
	for _, tt := range tests {
		t.Run(tt.browser, func(t *testing.T) {
			profile := filepath.Join(append([]string{home}, tt.parts...)...)
			cookieDB := filepath.Join(profile, tt.store)
			if err := os.MkdirAll(filepath.Dir(cookieDB), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(cookieDB, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			if got := defaultBrowserProfilePath(tt.browser, home); got != profile {
				t.Fatalf("defaultBrowserProfilePath() = %q, want %q", got, profile)
			}
		})
	}
}

func TestDefaultBrowserProfilePathUsesFirefoxProfilesINI(t *testing.T) {
	home := t.TempDir()
	firefoxRoot := filepath.Join(home, "Library", "Application Support", "Firefox")
	profile := filepath.Join(firefoxRoot, "Profiles", "test.default-release")
	if err := os.MkdirAll(profile, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profile, "cookies.sqlite"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	ini := "[InstallA1B2C3]\nDefault=Profiles/test.default-release\nLocked=1\n[Profile0]\nPath=Profiles/test.default-release\nIsRelative=1\n"
	if err := os.WriteFile(filepath.Join(firefoxRoot, "profiles.ini"), []byte(ini), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := defaultBrowserProfilePath("firefox", home); got != profile {
		t.Fatalf("defaultBrowserProfilePath() = %q, want %q", got, profile)
	}
}

func TestDefaultBrowserProfilePathFallsBackWhenNoCookieStore(t *testing.T) {
	if got := defaultBrowserProfilePath("chrome", t.TempDir()); got != "" {
		t.Fatalf("defaultBrowserProfilePath() = %q, want empty path", got)
	}
}

func TestIsXHSNoteURLRoutesRednotePostsThroughXHSParser(t *testing.T) {
	for _, rawURL := range []string{
		"https://www.rednote.com/discovery/item/6a44b3f5000000001700b738?source=webshare",
		"https://www.xiaohongshu.com/explore/6a44b3f5000000001700b738",
		"https://www.xhslink.com/a/example",
	} {
		if !isXHSNoteURL(rawURL) {
			t.Errorf("isXHSNoteURL(%q) = false, want true", rawURL)
		}
	}
}

func TestIsXHSNoteURLDoesNotRouteMediaOrOtherDomains(t *testing.T) {
	for _, rawURL := range []string{
		"https://sns-video-bd.xhscdn.com/stream/video.mp4",
		"https://example.com/discovery/item/6a44b3f5000000001700b738",
		"https://www.youtube.com/watch?v=example",
	} {
		if isXHSNoteURL(rawURL) {
			t.Errorf("isXHSNoteURL(%q) = true, want false", rawURL)
		}
	}
}

func TestNormalizeBrowserNameUsesCanonicalIDs(t *testing.T) {
	for input, want := range map[string]string{
		"Google Chrome":    "chrome",
		"Firefox":          "firefox",
		" Microsoft Edge ": "edge",
	} {
		if got := normalizeBrowserName(input); got != want {
			t.Errorf("normalizeBrowserName(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestGetCookieArgsUsesSelectedBrowserForYTDLP(t *testing.T) {
	manager := &CookieManager{
		config: CookieConfig{Mode: CookieModeBrowser, SelectedBrowser: "Google Chrome"},
		state:  temporaryCookieState{domainBrowserData: make(map[string]*BrowserExportData)},
	}

	got, err := manager.GetCookieArgs(t.Context(), "yt-dlp", "https://www.xiaohongshu.com/explore")
	if err != nil {
		t.Fatalf("GetCookieArgs() error = %v", err)
	}
	want := []string{"--cookies-from-browser", "chrome"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("GetCookieArgs() = %#v, want %#v", got, want)
	}
}

func TestGetCookieArgsUsesSelectedBrowserProfile(t *testing.T) {
	profile := filepath.Join(t.TempDir(), "Profile 2")
	cookieDir := filepath.Join(profile, "Network")
	if err := os.MkdirAll(cookieDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cookieDir, "Cookies"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	manager := &CookieManager{
		config: CookieConfig{Mode: CookieModeBrowser, SelectedBrowser: "Google Chrome", SelectedProfile: profile},
		state:  temporaryCookieState{domainBrowserData: make(map[string]*BrowserExportData)},
	}

	got, err := manager.GetCookieArgs(t.Context(), "yt-dlp", "https://www.xiaohongshu.com/explore")
	if err != nil {
		t.Fatalf("GetCookieArgs() error = %v", err)
	}
	want := []string{"--cookies-from-browser", "chrome:" + filepath.ToSlash(profile)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("GetCookieArgs() = %#v, want %#v", got, want)
	}
}

func TestGetCookieArgsRejectsSelectedProfileWithoutCookieStore(t *testing.T) {
	manager := &CookieManager{
		config: CookieConfig{Mode: CookieModeBrowser, SelectedBrowser: "firefox", SelectedProfile: t.TempDir()},
		state:  temporaryCookieState{domainBrowserData: make(map[string]*BrowserExportData)},
	}
	if _, err := manager.GetCookieArgs(t.Context(), "yt-dlp", "https://www.xiaohongshu.com/explore"); err == nil {
		t.Fatal("GetCookieArgs() expected an error for selected profile without cookies.sqlite")
	}
}

func TestGetCookieArgsDoesNotChooseBrowserWhenUnset(t *testing.T) {
	manager := &CookieManager{
		config: CookieConfig{Mode: CookieModeBrowser},
		state:  temporaryCookieState{domainBrowserData: make(map[string]*BrowserExportData)},
	}

	got, err := manager.GetCookieArgs(t.Context(), "yt-dlp", "https://www.xiaohongshu.com/explore")
	if err != nil {
		t.Fatalf("GetCookieArgs() error = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("GetCookieArgs() = %#v, want no browser cookie arguments", got)
	}
}

func TestYTDLPBrowserSpecUsesSelectedBrowserID(t *testing.T) {
	manager.mu.Lock()
	previous := manager.config
	manager.config = CookieConfig{Mode: CookieModeBrowser, SelectedBrowser: "Google Chrome"}
	manager.mu.Unlock()
	t.Cleanup(func() {
		manager.mu.Lock()
		manager.config = previous
		manager.mu.Unlock()
	})

	got, err := manager.ytdlpBrowserSpec(t.Context(), "Google Chrome")
	if err != nil {
		t.Fatalf("ytdlpBrowserSpec() error = %v", err)
	}
	if got != "chrome" {
		t.Fatalf("ytdlpBrowserSpec() = %q, want %q", got, "chrome")
	}
}

func TestBuildYTDLPBrowserSpecWithProfile(t *testing.T) {
	got := buildYTDLPBrowserSpec("chrome", "/Users/test/Library/Application Support/Google/Chrome/Default")
	want := "chrome:/Users/test/Library/Application Support/Google/Chrome/Default"
	if got != want {
		t.Fatalf("buildYTDLPBrowserSpec() = %q, want %q", got, want)
	}
}

func TestBuildYTDLPBrowserSpecWithoutProfile(t *testing.T) {
	if got := buildYTDLPBrowserSpec("firefox", ""); got != "firefox" {
		t.Fatalf("buildYTDLPBrowserSpec() = %q, want %q", got, "firefox")
	}
}

func TestYTDLPBrowserSpecFromFirefoxCookieDatabase(t *testing.T) {
	profile := filepath.Join(t.TempDir(), "Profiles", "default-release")
	if err := os.MkdirAll(profile, 0o755); err != nil {
		t.Fatal(err)
	}
	cookieDB := filepath.Join(profile, "cookies.sqlite")
	if err := os.WriteFile(cookieDB, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	got := ytdlpBrowserSpecFromCookiePath("firefox", cookieDB)
	want := "firefox:" + filepath.ToSlash(profile)
	if got != want {
		t.Fatalf("ytdlpBrowserSpecFromCookiePath() = %q, want %q", got, want)
	}
}

func TestYTDLPBrowserSpecFromFirefoxCookieDatabaseFallsBackWhenMissing(t *testing.T) {
	cookieDB := filepath.Join(t.TempDir(), "Profiles", "missing", "cookies.sqlite")
	if got := ytdlpBrowserSpecFromCookiePath("firefox", cookieDB); got != "firefox" {
		t.Fatalf("ytdlpBrowserSpecFromCookiePath() = %q, want %q", got, "firefox")
	}
}

func TestYTDLPBrowserSpecFromFirefoxCookieDatabaseRejectsProfilesDirectory(t *testing.T) {
	profilesDir := t.TempDir()
	if got := ytdlpBrowserSpecFromCookiePath("firefox", profilesDir); got != "firefox" {
		t.Fatalf("ytdlpBrowserSpecFromCookiePath() = %q, want %q", got, "firefox")
	}
}

func TestFirefoxProfileFromINISelectsInstallDefaultWithCookieDatabase(t *testing.T) {
	root := t.TempDir()
	profile := filepath.Join(root, "Profiles", "abc.default-release")
	if err := os.MkdirAll(profile, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profile, "cookies.sqlite"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	profilesINI := "[InstallA1B2C3]\nDefault=Profiles/abc.default-release\nLocked=1\n\n[Profile0]\nName=default-release\nIsRelative=1\nPath=Profiles/abc.default-release\n"
	if err := os.WriteFile(filepath.Join(root, "profiles.ini"), []byte(profilesINI), 0o600); err != nil {
		t.Fatal(err)
	}

	got := firefoxProfileFromINI(root)
	if got != profile {
		t.Fatalf("firefoxProfileFromINI() = %q, want %q", got, profile)
	}
}

func TestFirefoxProfileFromINISelectsAbsoluteDefault(t *testing.T) {
	root := t.TempDir()
	profile := filepath.Join(root, "External", "release")
	if err := os.MkdirAll(profile, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profile, "cookies.sqlite"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	ini := "[InstallA1B2C3]\nDefault=" + filepath.ToSlash(profile) + "\nLocked=1\n[Profile0]\nPath=" + filepath.ToSlash(profile) + "\nIsRelative=0\n"
	if err := os.WriteFile(filepath.Join(root, "profiles.ini"), []byte(ini), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := firefoxProfileFromINI(root); got != profile {
		t.Fatalf("firefoxProfileFromINI() = %q, want %q", got, profile)
	}
}

func TestFirefoxProfileFromINIRejectsMissingINIAndCookieDatabaseDirectory(t *testing.T) {
	t.Run("missing ini", func(t *testing.T) {
		if got := firefoxProfileFromINI(t.TempDir()); got != "" {
			t.Fatalf("firefoxProfileFromINI() = %q, want empty path", got)
		}
	})

	t.Run("cookie database is directory", func(t *testing.T) {
		root := t.TempDir()
		profile := filepath.Join(root, "Profiles", "default")
		if err := os.MkdirAll(filepath.Join(profile, "cookies.sqlite"), 0o755); err != nil {
			t.Fatal(err)
		}
		ini := "[InstallA1B2C3]\nDefault=Profiles/default\nLocked=1\n[Profile0]\nPath=Profiles/default\nIsRelative=1\n"
		if err := os.WriteFile(filepath.Join(root, "profiles.ini"), []byte(ini), 0o600); err != nil {
			t.Fatal(err)
		}
		if got := firefoxProfileFromINI(root); got != "" {
			t.Fatalf("firefoxProfileFromINI() = %q, want empty path", got)
		}
	})
}

func TestFirefoxProfileFromINISelectsDeclaredDefaultAmongProfiles(t *testing.T) {
	root := t.TempDir()
	defaultProfile := filepath.Join(root, "Profiles", "default")
	otherProfile := filepath.Join(root, "Profiles", "other")
	for _, profile := range []string{defaultProfile, otherProfile} {
		if err := os.MkdirAll(profile, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(profile, "cookies.sqlite"), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ini := "[InstallA1B2C3]\nDefault=Profiles/default\nLocked=1\n[Profile0]\nPath=Profiles/default\nIsRelative=1\n[Profile1]\nPath=Profiles/other\nIsRelative=1\n"
	if err := os.WriteFile(filepath.Join(root, "profiles.ini"), []byte(ini), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := firefoxProfileFromINI(root); got != defaultProfile {
		t.Fatalf("firefoxProfileFromINI() = %q, want declared default %q", got, defaultProfile)
	}
}

func TestFirefoxProfileFromINIRejectsAmbiguousLegacyDefaults(t *testing.T) {
	root := t.TempDir()
	ini := "[Profile0]\nPath=Profiles/first\nIsRelative=1\nDefault=1\n[Profile1]\nPath=Profiles/second\nIsRelative=1\nDefault=1\n"
	if err := os.WriteFile(filepath.Join(root, "profiles.ini"), []byte(ini), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := firefoxProfileFromINI(root); got != "" {
		t.Fatalf("firefoxProfileFromINI() = %q, want empty path for ambiguous defaults", got)
	}
}

func TestFirefoxProfileFromINIFallsBackToUniqueLegacyDefault(t *testing.T) {
	root := t.TempDir()
	profile := filepath.Join(root, "Profiles", "legacy.default")
	if err := os.MkdirAll(profile, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profile, "cookies.sqlite"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	profilesINI := "[Profile0]\nName=default\nIsRelative=1\nPath=Profiles/legacy.default\nDefault=1\n"
	if err := os.WriteFile(filepath.Join(root, "profiles.ini"), []byte(profilesINI), 0o600); err != nil {
		t.Fatal(err)
	}

	if got := firefoxProfileFromINI(root); got != profile {
		t.Fatalf("firefoxProfileFromINI() = %q, want %q", got, profile)
	}
}

func TestFirefoxProfileFromINIIgnoresProfilesWithoutCookieDatabase(t *testing.T) {
	root := t.TempDir()
	profile := filepath.Join(root, "Profiles", "empty.default")
	if err := os.MkdirAll(profile, 0o755); err != nil {
		t.Fatal(err)
	}
	profilesINI := "[Profile0]\nName=default\nIsRelative=1\nPath=Profiles/empty.default\nDefault=1\n"
	if err := os.WriteFile(filepath.Join(root, "profiles.ini"), []byte(profilesINI), 0o600); err != nil {
		t.Fatal(err)
	}

	if got := firefoxProfileFromINI(root); got != "" {
		t.Fatalf("firefoxProfileFromINI() = %q, want empty path", got)
	}
}

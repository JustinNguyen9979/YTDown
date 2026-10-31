package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	xhsmodule "ytdown/xhs"
)

func TestVideoDownloadRouteSeparatesXHSNotePreparation(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want videoDownloadRoute
	}{
		{name: "Rednote note uses dedicated XHS video path", url: "https://www.rednote.com/discovery/item/6a44b3f5000000001700b738?source=webshare", want: videoDownloadXHSNote},
		{name: "Xiaohongshu note uses dedicated XHS video path", url: "https://www.xiaohongshu.com/explore/6a44b3f5000000001700b738", want: videoDownloadXHSNote},
		{name: "XHS short link uses dedicated XHS video path", url: "http://xhslink.com/o/AHiqLuW5rvi", want: videoDownloadXHSNote},
		{name: "XHS CDN media uses normal yt-dlp path", url: "https://sns-video-bd.xhscdn.com/stream/video.mp4", want: videoDownloadYTDLP},
		{name: "lookalike Rednote domain uses normal yt-dlp path", url: "https://rednote.com.evil.example/discovery/item/note123", want: videoDownloadYTDLP},
		{name: "YouTube uses normal yt-dlp path", url: "https://www.youtube.com/watch?v=example", want: videoDownloadYTDLP},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := routeVideoDownload(tt.url); got != tt.want {
				t.Fatalf("routeVideoDownload(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}

func TestSanitizeDownloadErrorRedactsCookieSecrets(t *testing.T) {
	for _, tt := range []struct {
		name    string
		input   string
		secrets []string
		marker  string
	}{
		{
			name:    "web session",
			input:   "request failed: web_session=synthetic-session",
			secrets: []string{"synthetic-session"},
			marker:  "web_session=[REDACTED]",
		},
		{
			name:    "cookie header",
			input:   "request failed: Cookie: auth=synthetic-auth-token",
			secrets: []string{"synthetic-auth-token"},
			marker:  "Cookie: [REDACTED]",
		},
		{
			name:    "query token",
			input:   "request failed: https://rednote.com/path?token=synthetic-query-token",
			secrets: []string{"synthetic-query-token"},
			marker:  "token=[REDACTED]",
		},
		{
			name:    "XHS share token",
			input:   "request failed: https://www.xiaohongshu.com/path?xsec_token=synthetic-xsec-token",
			secrets: []string{"synthetic-xsec-token"},
			marker:  "xsec_token=[REDACTED]",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeDownloadErrorForLog(tt.input)
			for _, secret := range tt.secrets {
				if strings.Contains(got, secret) {
					t.Fatalf("sanitizeDownloadErrorForLog() leaked a secret in %q", got)
				}
			}
			if !strings.Contains(got, tt.marker) {
				t.Fatalf("sanitizeDownloadErrorForLog() = %q, want marker %q", got, tt.marker)
			}
		})
	}
}

func TestSanitizeDownloadErrorPreservesSafeDiagnostics(t *testing.T) {
	input := "download failed: HTTP Error 403 for https://sns-video-bd.xhscdn.com/video.mp4"
	if got := sanitizeDownloadErrorForLog(input); got != input {
		t.Fatalf("sanitizeDownloadErrorForLog() = %q, want safe diagnostic unchanged", got)
	}
}

func TestClassifyDownloadFailureUsesSourceSiteGuidance(t *testing.T) {
	restrictedErr := errors.New("Sign in required: HTTP Error 403")
	tests := []struct {
		name           string
		sourceURL      string
		cookiePresent  bool
		wantContains   string
		wantNotContain string
	}{
		{
			name:           "Rednote without extracted session asks for login session",
			sourceURL:      "https://www.rednote.com/discovery/item/synthetic-id",
			cookiePresent:  false,
			wantContains:   "login session (web_session) is required",
			wantNotContain: "invalid or insufficient",
		},
		{
			name:           "Rednote with extracted session reports rejection",
			sourceURL:      "https://www.rednote.com/discovery/item/synthetic-id",
			cookiePresent:  true,
			wantContains:   "invalid or insufficient",
			wantNotContain: "login session (web_session) is required",
		},
		{
			name:           "YouTube requests a cookie after missing browser session",
			sourceURL:      "https://www.youtube.com/watch?v=synthetic-id",
			cookiePresent:  false,
			wantContains:   "Temporary cookie required",
			wantNotContain: "web_session",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			failure := classifyDownloadFailure(restrictedErr, tt.cookiePresent, tt.sourceURL)
			joined := strings.Join(failure.Details, " ")
			if !failure.RequiresCookie {
				t.Fatal("classifyDownloadFailure() RequiresCookie = false, want true")
			}
			if !strings.Contains(joined, tt.wantContains) {
				t.Errorf("details %q do not contain %q", joined, tt.wantContains)
			}
			if strings.Contains(joined, tt.wantNotContain) {
				t.Errorf("details %q unexpectedly contain %q", joined, tt.wantNotContain)
			}
		})
	}
}

func TestGetXHSFetchOptionsUsesSelectedBrowserSessionWhenModeNone(t *testing.T) {
	originalManager := manager
	manager = &CookieManager{
		config: CookieConfig{
			Mode:            CookieModeNone,
			SelectedBrowser: "firefox",
			SelectedProfile: "/synthetic/Firefox/Profile 1",
		},
		state: temporaryCookieState{
			domainBrowserData: make(map[string]*BrowserExportData),
			xhsSession:        "synthetic-web-session-sentinel",
			xhsSessionBrowser: "firefox",
			xhsSessionProfile: "/synthetic/Firefox/Profile 1",
		},
	}
	defer func() { manager = originalManager }()

	opts, err := getXHSFetchOptions(context.Background(), "https://www.rednote.com/discovery/item/synthetic-id")
	if err != nil {
		t.Fatalf("getXHSFetchOptions() error = %v", err)
	}
	if opts.Cookie != "web_session=synthetic-web-session-sentinel" {
		t.Fatalf("getXHSFetchOptions() Cookie = %q, want selected-profile session", opts.Cookie)
	}
	if got := sanitizeDownloadErrorForLog("request failed: " + opts.Cookie); strings.Contains(got, "synthetic-web-session-sentinel") {
		t.Fatalf("sanitized error leaked session: %q", got)
	}
}

func TestGetXHSFetchOptionsDoesNotUseBrowserSessionWithoutSelectedProfile(t *testing.T) {
	originalManager := manager
	manager = &CookieManager{
		config: CookieConfig{Mode: CookieModeNone, SelectedBrowser: "firefox"},
		state: temporaryCookieState{
			domainBrowserData: make(map[string]*BrowserExportData),
			xhsSession:        "synthetic-session-sentinel",
			xhsSessionBrowser: "firefox",
			xhsSessionProfile: "/synthetic/Firefox/Profile 1",
		},
	}
	defer func() { manager = originalManager }()

	opts, err := getXHSFetchOptions(context.Background(), "https://www.rednote.com/discovery/item/synthetic-id")
	if err != nil {
		t.Fatalf("getXHSFetchOptions() error = %v", err)
	}
	if opts.Cookie != "" {
		t.Fatal("getXHSFetchOptions() used a browser session without a selected profile")
	}
}

func TestGetXHSFetchOptionsPreservesManualCookieMode(t *testing.T) {
	originalManager := manager
	manager = &CookieManager{
		config: CookieConfig{Mode: CookieModeManual},
		state: temporaryCookieState{
			header:            "web_session=synthetic-manual-session; other=value",
			domainBrowserData: make(map[string]*BrowserExportData),
		},
	}
	defer func() { manager = originalManager }()

	opts, err := getXHSFetchOptions(context.Background(), "https://www.xiaohongshu.com/explore/synthetic-id")
	if err != nil {
		t.Fatalf("getXHSFetchOptions() error = %v", err)
	}
	if opts.Cookie != "web_session=synthetic-manual-session" {
		t.Fatalf("getXHSFetchOptions() Cookie = %q, want manual web_session", opts.Cookie)
	}
}

func TestHasCookieForURLRequiresAnExtractedXHSSession(t *testing.T) {
	manager := &CookieManager{
		config: CookieConfig{Mode: CookieModeBrowser, SelectedBrowser: "firefox"},
		state: temporaryCookieState{
			domainBrowserData: make(map[string]*BrowserExportData),
			xhsSession:        "",
			xhsSessionBrowser: "firefox",
			xhsSessionProfile: "/synthetic/Firefox/Profile 1",
		},
	}
	manager.mu.Lock()
	manager.config.SelectedProfile = "/synthetic/Firefox/Profile 1"
	manager.mu.Unlock()
	url := "https://www.rednote.com/discovery/item/synthetic-id"
	if manager.hasCookieForURL(url) {
		t.Fatal("browser cookie mode alone must not imply an extracted XHS session")
	}
	manager.state.xhsSession = "synthetic-session-sentinel"
	if !manager.hasCookieForURL(url) {
		t.Fatal("a cached XHS session should be reported as present")
	}
}

func TestXHSSessionCookieArgsWriteSessionToTemporaryFile(t *testing.T) {
	const session = "synthetic-web-session-sentinel"
	args, cleanup, err := xhsSessionCookieArgsForURL(session, "https://sns-video-bd.xhscdn.com/stream/video.mp4")
	if err != nil {
		t.Fatalf("xhsSessionCookieArgs() error = %v", err)
	}
	defer cleanup()
	if len(args) != 2 || args[0] != "--cookies" {
		t.Fatalf("xhsSessionCookieArgs() = %#v, want --cookies <temp-file>", args)
	}
	data, err := os.ReadFile(args[1])
	if err != nil {
		t.Fatalf("read temporary cookie file: %v", err)
	}
	cookieFile := string(data)
	if !strings.Contains(cookieFile, "web_session\t"+session) {
		t.Fatal("temporary cookie file does not contain the synthetic web_session")
	}
	if !strings.Contains(cookieFile, ".xhscdn.com\tTRUE\t/\tTRUE\t") {
		t.Fatal("temporary cookie file does not scope web_session to the XHS CDN host")
	}
	if !strings.Contains(cookieFile, "# Netscape HTTP Cookie File\n") {
		t.Fatal("temporary cookie file is missing its Netscape header line")
	}
	if strings.Contains(cookieFile, `\\n`) || strings.Contains(cookieFile, `\\t`) {
		t.Fatal("temporary cookie file contains escaped delimiters instead of actual separators")
	}
	if strings.Contains(cookieFile, ".attacker.example") {
		t.Fatal("temporary cookie file must not include unrelated domains")
	}
	if strings.Contains(strings.Join(args, " "), session) {
		t.Fatal("web_session was placed in process arguments")
	}
}

func TestXHSSessionCookieArgsRejectsUntrustedHost(t *testing.T) {
	if _, cleanup, err := xhsSessionCookieArgsForURL("synthetic-session", "https://cdn.attacker.example/video.mp4"); err == nil {
		if cleanup != nil {
			cleanup()
		}
		t.Fatal("xhsSessionCookieArgsForURL() should reject a non-XHS host")
	}
}

func TestReplaceBrowserCookieArgsWithXHSSession(t *testing.T) {
	args := []string{"--cookies-from-browser", "firefox:/synthetic/Profile", "-f", "bestvideo+bestaudio"}
	xhsArgs, cleanup, err := xhsSessionCookieArgs("synthetic-session-sentinel")
	if err != nil {
		t.Fatalf("xhsSessionCookieArgs() error = %v", err)
	}
	defer cleanup()

	got := replaceBrowserCookieArgsWithXHSSession(args, xhsArgs)
	joined := strings.Join(got, " ")
	if strings.Contains(joined, "--cookies-from-browser") || strings.Contains(joined, "synthetic-session-sentinel") {
		t.Fatalf("replacement args expose browser cookie source or session: %#v", got)
	}
	if !strings.Contains(joined, "--cookies ") || !strings.Contains(joined, "-f bestvideo+bestaudio") {
		t.Fatalf("replacement args = %#v, want temp cookie file and preserved format args", got)
	}
}

func TestXHSSessionCookieArgsRejectEmptySession(t *testing.T) {
	if _, cleanup, err := xhsSessionCookieArgs(""); err == nil {
		if cleanup != nil {
			cleanup()
		}
		t.Fatal("xhsSessionCookieArgs() expected empty-session error")
	}
}

func TestInvalidateXHSSessionCacheForSelectedProfile(t *testing.T) {
	manager := &CookieManager{
		state: temporaryCookieState{
			domainBrowserData:  make(map[string]*BrowserExportData),
			xhsSession:         "stale-session-sentinel",
			xhsSessionDomain:   "rednote.com",
			xhsSessionVerified: true,
			xhsAttemptSession:  "attempt-session-sentinel",
			xhsAttemptDomain:   "xiaohongshu.com",
			xhsSessionBrowser:  "chrome",
			xhsSessionProfile:  "/synthetic/Profile 1",
			xhsCacheTime:       time.Now(),
		},
	}
	manager.invalidateXHSSessionCache("chrome", "/synthetic/Profile 1")
	manager.state.mu.RLock()
	defer manager.state.mu.RUnlock()
	if manager.state.xhsSession != "" || manager.state.xhsSessionDomain != "" || manager.state.xhsSessionVerified ||
		manager.state.xhsAttemptSession != "" || manager.state.xhsAttemptDomain != "" || !manager.state.xhsCacheTime.IsZero() {
		t.Fatal("invalidateXHSSessionCache() did not clear cached and attempted sessions")
	}
}

func TestCacheXHSSessionCandidatePreservesVerifiedSession(t *testing.T) {
	manager := &CookieManager{
		state: temporaryCookieState{
			domainBrowserData:  make(map[string]*BrowserExportData),
			xhsSession:         "verified-session",
			xhsSessionDomain:   "rednote.com",
			xhsSessionVerified: true,
			xhsSessionBrowser:  "firefox",
			xhsSessionProfile:  "/synthetic/Firefox/Profile 1",
		},
	}

	manager.cacheXHSSessionCandidate("firefox", "/synthetic/Firefox/Profile 1", "alternate-session", "xiaohongshu.com")
	gotSession, gotDomain, verified := manager.cachedXHSSessionDetails("firefox", "/synthetic/Firefox/Profile 1")
	if gotSession != "verified-session" || gotDomain != "rednote.com" || !verified {
		t.Fatalf("cached session = (%q, %q, %v), want verified Rednote session", gotSession, gotDomain, verified)
	}
	if got := manager.xhsAttemptSource("alternate-session"); got != "xiaohongshu.com" {
		t.Fatalf("attempt source = %q, want xiaohongshu.com", got)
	}
}

func TestCommitXHSSessionReplacesCacheAfterSuccessfulDownload(t *testing.T) {
	manager := &CookieManager{state: temporaryCookieState{domainBrowserData: make(map[string]*BrowserExportData)}}
	manager.commitXHSSession("Firefox", "/synthetic/Firefox/Profile 1", "verified-session", "xiaohongshu.com")

	gotSession, gotDomain, verified := manager.cachedXHSSessionDetails("firefox", "/synthetic/Firefox/Profile 1")
	if gotSession != "verified-session" || gotDomain != "xiaohongshu.com" || !verified {
		t.Fatalf("cached session = (%q, %q, %v), want verified Xiaohongshu session", gotSession, gotDomain, verified)
	}
}

func TestCachedXHSSessionPersistsUntilProfileInvalidation(t *testing.T) {
	manager := &CookieManager{
		state: temporaryCookieState{
			domainBrowserData: make(map[string]*BrowserExportData),
			xhsSession:        "synthetic-session-sentinel",
			xhsSessionBrowser: "firefox",
			xhsSessionProfile: "/synthetic/Firefox/Profile 1",
			xhsCacheTime:      time.Now().Add(-24 * time.Hour),
		},
	}

	if got := manager.cachedXHSSession("Firefox", "/synthetic/Firefox/Profile 1"); got != "synthetic-session-sentinel" {
		t.Fatalf("cachedXHSSession() = %q, want cached session regardless of age", got)
	}
	if got := manager.cachedXHSSession("firefox", "/synthetic/Firefox/Profile 2"); got != "" {
		t.Fatalf("cachedXHSSession() for another profile = %q, want empty", got)
	}

	manager.invalidateXHSSessionCache("firefox", "/synthetic/Firefox/Profile 1")
	if got := manager.cachedXHSSession("firefox", "/synthetic/Firefox/Profile 1"); got != "" {
		t.Fatalf("cachedXHSSession() after invalidation = %q, want empty", got)
	}
}

func TestCachedXHSSessionCookieFileIsReusedByScopeUntilCleared(t *testing.T) {
	manager := &CookieManager{
		state: temporaryCookieState{
			domainBrowserData: make(map[string]*BrowserExportData),
			xhsSession:        "synthetic-session-sentinel",
			xhsSessionBrowser: "firefox",
			xhsSessionProfile: "/synthetic/Firefox/Profile 1",
		},
	}
	xhsURL := "https://www.xiaohongshu.com/explore/synthetic-id"
	cdnURL := "https://sns-video-bd.xhscdn.com/stream/video.mp4"

	args, cleanup, err := manager.xhsSessionCookieArgsForDownload("synthetic-session-sentinel", xhsURL)
	if err != nil {
		t.Fatalf("xhsSessionCookieArgsForDownload() error = %v", err)
	}
	if cleanup == nil {
		t.Fatal("cached cookie file use must return a release function")
	}
	firstPath := args[1]
	secondArgs, secondCleanup, err := manager.xhsSessionCookieArgsForDownload("synthetic-session-sentinel", xhsURL)
	if err != nil {
		cleanup()
		t.Fatalf("second xhsSessionCookieArgsForDownload() error = %v", err)
	}
	if secondCleanup == nil || secondArgs[1] != firstPath {
		cleanup()
		if secondCleanup != nil {
			secondCleanup()
		}
		t.Fatalf("same-scope request did not reuse cached cookie file")
	}
	secondCleanup()
	cleanup()

	cdnArgs, cdnCleanup, err := manager.xhsSessionCookieArgsForDownload("synthetic-session-sentinel", cdnURL)
	if err != nil {
		t.Fatalf("CDN xhsSessionCookieArgsForDownload() error = %v", err)
	}
	if cdnCleanup == nil || cdnArgs[1] == firstPath {
		if cdnCleanup != nil {
			cdnCleanup()
		}
		t.Fatal("different host scope must use a separate cached cookie file")
	}
	cdnCleanup()

	manager.clearSensitiveCookieState()
	if _, err := os.Stat(filepath.Dir(firstPath)); !os.IsNotExist(err) {
		t.Fatalf("primary-scope cookie directory still exists after clear, stat error = %v", err)
	}
	if _, err := os.Stat(filepath.Dir(cdnArgs[1])); !os.IsNotExist(err) {
		t.Fatalf("CDN-scope cookie directory still exists after clear, stat error = %v", err)
	}
}

func TestClearSensitiveCookieStateRemovesCachedSecretsAndTempFile(t *testing.T) {
	dir := t.TempDir()
	cookiePath := filepath.Join(dir, "cookies.txt")
	if err := os.WriteFile(cookiePath, []byte("synthetic-cookie"), 0o600); err != nil {
		t.Fatal(err)
	}
	manager := &CookieManager{
		state: temporaryCookieState{
			header:             "web_session=synthetic-session-sentinel",
			cookies:            []parsedCookie{{Name: "web_session", Value: "synthetic-session-sentinel"}},
			xhsCookieFiles:     map[string]string{"xiaohongshu.com": cookiePath},
			domainBrowserData:  map[string]*BrowserExportData{"xhs": {WebSession: "synthetic-session-sentinel"}},
			xhsSession:         "synthetic-session-sentinel",
			xhsSessionDomain:   "rednote.com",
			xhsSessionVerified: true,
			xhsAttemptSession:  "attempt-session-sentinel",
			xhsAttemptDomain:   "xiaohongshu.com",
			xhsSessionBrowser:  "firefox",
			xhsSessionProfile:  "/synthetic/Firefox/Profile 1",
			xhsCacheTime:       time.Now(),
		},
	}

	manager.clearSensitiveCookieState()

	manager.state.mu.RLock()
	defer manager.state.mu.RUnlock()
	if manager.state.header != "" || len(manager.state.cookies) != 0 || manager.state.tempFile != "" ||
		len(manager.state.xhsCookieFiles) != 0 || len(manager.state.domainBrowserData) != 0 || manager.state.xhsSession != "" ||
		manager.state.xhsSessionDomain != "" || manager.state.xhsSessionVerified || manager.state.xhsAttemptSession != "" ||
		manager.state.xhsAttemptDomain != "" || manager.state.xhsSessionBrowser != "" || manager.state.xhsSessionProfile != "" ||
		!manager.state.xhsCacheTime.IsZero() {
		t.Fatal("clearSensitiveCookieState() left cookie data in memory")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("temporary cookie directory still exists, stat error = %v", err)
	}
}

func TestXHSGalleryVideoUsesFetchedSession(t *testing.T) {
	cookie := "web_session=synthetic-session-sentinel"
	fetchOptions := xhsmodule.FetchOptions{Cookie: cookie}
	got := galleryVideoCookieHeader(fetchOptions)
	if got != cookie {
		t.Fatalf("galleryVideoCookieHeader() = %q, want fetched session header", got)
	}
}

func TestXHSDownloadRoute(t *testing.T) {
	tests := []struct {
		name     string
		noteType string
		want     xhsDownloadRoute
	}{
		{name: "image note uses direct image downloader", noteType: "image", want: xhsDownloadDirect},
		{name: "video note uses yt-dlp", noteType: "video", want: xhsDownloadYTDLP},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := routeXHSNote(tt.noteType); got != tt.want {
				t.Fatalf("routeXHSNote(%q) = %v, want %v", tt.noteType, got, tt.want)
			}
		})
	}
}

func TestIsXiaohongshuRecognizesVideoCDN(t *testing.T) {
	if !IsXiaohongshu("https://sns-video-bd.xhscdn.com/stream/video.mp4") {
		t.Fatal("IsXiaohongshu() did not recognize Xiaohongshu video CDN")
	}
}

func TestExtractWebSessionFromNetscapeCookieFile(t *testing.T) {
	content := "# Netscape HTTP Cookie File\n.xiaohongshu.com\tTRUE\t/\tTRUE\t2147483647\tweb_session\tfake-session-value\n"
	got, err := extractWebSessionFromNetscapeCookieFile(content)
	if err != nil {
		t.Fatalf("extractWebSessionFromNetscapeCookieFile() error = %v", err)
	}
	if got != "fake-session-value" {
		t.Fatalf("extractWebSessionFromNetscapeCookieFile() = %q, want fake session", got)
	}
}

func TestExtractWebSessionFromNetscapeCookieFilePrefersRednoteDomain(t *testing.T) {
	content := "# Netscape HTTP Cookie File\n" +
		".rednote.com\tTRUE\t/\tTRUE\t2147483647\tweb_session\trednote-session\n" +
		".xiaohongshu.com\tTRUE\t/\tTRUE\t2147483647\tweb_session\txhs-session\n"
	got, err := extractWebSessionFromNetscapeCookieFile(content)
	if err != nil {
		t.Fatalf("extractWebSessionFromNetscapeCookieFile() error = %v", err)
	}
	if got != "rednote-session" {
		t.Fatalf("extractWebSessionFromNetscapeCookieFile() = %q, want Rednote-scoped session", got)
	}
}

func TestParseNetscapeCookieFileForDomainUsesRequestedSessionDomain(t *testing.T) {
	content := "# Netscape HTTP Cookie File\n" +
		".rednote.com\tTRUE\t/\tTRUE\t2147483647\tweb_session\trednote-session\n" +
		".xiaohongshu.com\tTRUE\t/\tTRUE\t2147483647\tweb_session\txhs-session\n"
	for _, tt := range []struct {
		host string
		want string
	}{
		{host: "www.rednote.com", want: "rednote-session"},
		{host: "www.xiaohongshu.com", want: "xhs-session"},
	} {
		_, got := parseNetscapeCookieFileForDomain(content, tt.host)
		if got != tt.want {
			t.Errorf("parseNetscapeCookieFileForDomain(%q) session = %q, want %q", tt.host, got, tt.want)
		}
	}
}

func TestExtractWebSessionFromNetscapeCookieFileRejectsMissingSession(t *testing.T) {
	content := "# Netscape HTTP Cookie File\n.xiaohongshu.com\tTRUE\t/\tTRUE\t2147483647\tother_cookie\tvalue\n"
	if _, err := extractWebSessionFromNetscapeCookieFile(content); err == nil {
		t.Fatal("extractWebSessionFromNetscapeCookieFile() expected a missing web_session error")
	}
}

func TestXHSBrowserCookieRejectsEmptySession(t *testing.T) {
	if _, err := xhsBrowserCookie(""); err == nil {
		t.Fatal("xhsBrowserCookie() expected a missing session error")
	}
}

func TestXHSBrowserCookieFormatsSession(t *testing.T) {
	got, err := xhsBrowserCookie("fake-session-value")
	if err != nil {
		t.Fatalf("xhsBrowserCookie() error = %v", err)
	}
	if got != "web_session=fake-session-value" {
		t.Fatalf("xhsBrowserCookie() = %q, want cookie header", got)
	}
}

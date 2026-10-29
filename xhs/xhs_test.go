package xhs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
)

func TestFetchWithDomainFallbackTriesOriginalBeforeAlternate(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		firstHost  string
		secondHost string
	}{
		{
			name:       "xiaohongshu to rednote",
			input:      "https://www.xiaohongshu.com/explore/note123?source=share&xsec_token=synthetic",
			firstHost:  "www.xiaohongshu.com",
			secondHost: "www.rednote.com",
		},
		{
			name:       "rednote to xiaohongshu",
			input:      "https://www.rednote.com/discovery/item/note456?source=share&xsec_token=synthetic",
			firstHost:  "www.rednote.com",
			secondHost: "www.xiaohongshu.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var attempted []string
			got, err := fetchWithDomainFallback(context.Background(), tt.input, FetchOptions{}, func(_ context.Context, candidate string, _ FetchOptions) (*NoteInfo, error) {
				attempted = append(attempted, candidate)
				if len(attempted) == 1 {
					return nil, fmt.Errorf("primary domain unavailable")
				}
				return &NoteInfo{NoteID: "fallback-note"}, nil
			})
			if err != nil {
				t.Fatalf("fetchWithDomainFallback() error = %v", err)
			}
			if got == nil || got.NoteID != "fallback-note" {
				t.Fatalf("fetchWithDomainFallback() = %#v, want fallback note", got)
			}
			if len(attempted) != 2 {
				t.Fatalf("attempted %d URLs, want primary and alternate: %#v", len(attempted), attempted)
			}
			for i, wantHost := range []string{tt.firstHost, tt.secondHost} {
				parsed, err := url.Parse(attempted[i])
				if err != nil {
					t.Fatalf("url.Parse(%q) error = %v", attempted[i], err)
				}
				if parsed.Host != wantHost || parsed.Path != mustURLPath(t, tt.input) || parsed.RawQuery != "source=share&xsec_token=synthetic" {
					t.Fatalf("attempted URL %q does not preserve host order, path, and query", attempted[i])
				}
			}
		})
	}
}

func TestFetchWithDomainFallbackDoesNotRetryUnsupportedDomain(t *testing.T) {
	for _, noteURL := range []string{
		"https://xhslink.com/synthetic?token=abc",
		"https://www.rednote.com.evil.example/explore/note123",
	} {
		t.Run(noteURL, func(t *testing.T) {
			attempts := 0
			_, err := fetchWithDomainFallback(context.Background(), noteURL, FetchOptions{}, func(_ context.Context, _ string, _ FetchOptions) (*NoteInfo, error) {
				attempts++
				return nil, fmt.Errorf("request failed")
			})
			if err == nil {
				t.Fatal("fetchWithDomainFallback() expected error")
			}
			if attempts != 1 {
				t.Fatalf("attempted %d times for unsupported domain, want 1", attempts)
			}
		})
	}
}

func TestFetchWithDomainFallbackDoesNotRetryDeadlineExceeded(t *testing.T) {
	attempts := 0
	_, err := fetchWithDomainFallback(context.Background(), "https://www.rednote.com/explore/note789", FetchOptions{}, func(_ context.Context, _ string, _ FetchOptions) (*NoteInfo, error) {
		attempts++
		return nil, context.DeadlineExceeded
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("fetchWithDomainFallback() error = %v, want deadline exceeded", err)
	}
	if attempts != 1 {
		t.Fatalf("attempted %d times after deadline, want 1", attempts)
	}
}

func TestFetchWithDomainFallbackStopsAfterContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	attempts := 0
	_, err := fetchWithDomainFallback(ctx, "https://www.rednote.com/explore/note789?xsec_token=synthetic", FetchOptions{}, func(_ context.Context, _ string, _ FetchOptions) (*NoteInfo, error) {
		attempts++
		cancel()
		return nil, fmt.Errorf("request failed")
	})
	if err == nil {
		t.Fatal("fetchWithDomainFallback() expected error")
	}
	if attempts != 1 {
		t.Fatalf("attempted %d times after cancellation, want 1", attempts)
	}
}

func TestFetchWithDomainFallbackDoesNotRetrySuccessfulPrimary(t *testing.T) {
	attempts := 0
	_, err := fetchWithDomainFallback(context.Background(), "https://xiaohongshu.com/explore/note123", FetchOptions{}, func(_ context.Context, _ string, _ FetchOptions) (*NoteInfo, error) {
		attempts++
		return &NoteInfo{NoteID: "primary-note"}, nil
	})
	if err != nil {
		t.Fatalf("fetchWithDomainFallback() error = %v", err)
	}
	if attempts != 1 {
		t.Fatalf("attempted %d times, want primary only", attempts)
	}
}

func mustURLPath(t *testing.T, rawURL string) string {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("url.Parse(%q) error = %v", rawURL, err)
	}
	return parsed.Path
}

func TestCanonicalizeNoteURL(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "rednote note maps to xiaohongshu preserving path and query",
			input: "https://www.rednote.com/discovery/item/6a44b3f5000000001700b738?source=webshare&xsec_token=synthetic",
			want:  "https://www.xiaohongshu.com/discovery/item/6a44b3f5000000001700b738?source=webshare&xsec_token=synthetic",
		},
		{
			name:  "xiaohongshu URL remains unchanged",
			input: "https://www.xiaohongshu.com/explore/64bca1234def567890abcd12?xsec_token=synthetic",
			want:  "https://www.xiaohongshu.com/explore/64bca1234def567890abcd12?xsec_token=synthetic",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := canonicalizeNoteURL(tt.input)
			if err != nil {
				t.Fatalf("canonicalizeNoteURL() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("canonicalizeNoteURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseNoteID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		url  string
		want string
	}{
		{
			name: "explore URL",
			url:  "https://www.xiaohongshu.com/explore/64bca1234def567890abcd12?xsec_token=abc",
			want: "64bca1234def567890abcd12",
		},
		{
			name: "discovery URL",
			url:  "https://www.xiaohongshu.com/discovery/item/64bca1234def567890abcd12",
			want: "64bca1234def567890abcd12",
		},
		{
			name: "query noteId",
			url:  "https://example.com/share?noteId=64bca1234def567890abcd12&foo=bar",
			want: "64bca1234def567890abcd12",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseNoteID(tt.url)
			if err != nil {
				t.Fatalf("parseNoteID() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("parseNoteID() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFetchWithOptionsExplainsLoginRedirect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/login?redirect=%2Fdiscovery%2Fitem%2Fsynthetic", http.StatusFound)
	}))
	defer server.Close()

	_, err := FetchWithOptions(context.Background(), server.URL+"/discovery/item/6a44b3f5000000001700b738", FetchOptions{})
	if err == nil {
		t.Fatal("FetchWithOptions() expected a login redirect error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "login") || !strings.Contains(err.Error(), "web_session") {
		t.Fatalf("FetchWithOptions() error = %q, want actionable login/web_session guidance", err)
	}
}

func TestParseNoteIDError(t *testing.T) {
	t.Parallel()

	_, err := parseNoteID("https://www.xiaohongshu.com/explore/not-a-hex-id")
	if err == nil {
		t.Fatal("parseNoteID() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "không tìm thấy Note ID") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExtractStateVariants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		html string
	}{
		{
			name: "script closing pattern",
			html: `<html><script>window.__INITIAL_STATE__ = {"note":{"noteDetailMap":{"abc":{"note":{"title":"hello"}}}}}</script></html>`,
		},
		{
			name: "function suffix pattern",
			html: `<html><script>window.__INITIAL_STATE__ = {"note":{"noteDetailMap":{"abc":{"note":{"title":"hello"}}}}};(function(){})</script></html>`,
		},
		{
			name: "fallback pattern with undefined replacement",
			html: `<html><script>__INITIAL_STATE__ = {"note":{"noteDetailMap":{"abc":{"note":{"title":"hello","extra":undefined,"items":[undefined]}}}}};</script></html>`,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			state, err := extractState(tt.html)
			if err != nil {
				t.Fatalf("extractState() error = %v", err)
			}
			if _, ok := state["note"]; !ok {
				t.Fatalf("extractState() missing note key: %#v", state)
			}
		})
	}
}

func TestExtractStateErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		html string
		want string
	}{
		{
			name: "missing state",
			html: `<html><body>no embedded state</body></html>`,
			want: "XHS_NOTE_PARSE",
		},
		{
			name: "invalid JSON",
			html: `<html><script>window.__INITIAL_STATE__ = {"note": invalid }</script></html>`,
			want: "XHS_NOTE_PARSE",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := extractState(tt.html)
			if err == nil {
				t.Fatal("extractState() expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("extractState() error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestFindNoteSuccessAndFallback(t *testing.T) {
	t.Parallel()

	state := mustState(t, `{
		"note": {
			"noteDetailMap": {
				"target-id": {
					"note": {
						"noteId": "target-id",
						"title": "carousel title",
						"type": "normal",
						"user": {"nickname": "alice", "userId": "u1"},
						"imageList": [
							{"urlDefault": "https://sns-webpic-qc.xhscdn.com/a/b/c/d/e/spectrum/token-one!suffix"}
						]
					}
				},
				"fallback-id": {
					"note": {
						"noteId": "fallback-id",
						"title": "fallback title",
						"type": "normal",
						"user": {"nickname": "bob", "userId": "u2"},
						"imageList": []
					}
				}
			}
		}
	}`)

	note, err := findNote(state, "target-id")
	if err != nil {
		t.Fatalf("findNote() error = %v", err)
	}
	if note.Title != "carousel title" {
		t.Fatalf("findNote() title = %q, want %q", note.Title, "carousel title")
	}

	fallbackState := mustState(t, `{
		"note": {
			"noteDetailMap": {
				"fallback-id": {
					"note": {
						"noteId": "fallback-id",
						"title": "fallback title",
						"type": "normal",
						"user": {"nickname": "bob", "userId": "u2"},
						"imageList": []
					}
				}
			}
		}
	}`)

	fallbackNote, err := findNote(fallbackState, "missing-id")
	if err != nil {
		t.Fatalf("findNote() fallback error = %v", err)
	}
	if fallbackNote.NoteID != "fallback-id" {
		t.Fatalf("findNote() fallback noteID = %q, want %q", fallbackNote.NoteID, "fallback-id")
	}
}

func TestFindNoteErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		state map[string]json.RawMessage
		want  string
	}{
		{
			name:  "missing note section",
			state: mustState(t, `{}`),
			want:  "thiếu key 'note' trong state",
		},
		{
			name:  "missing noteDetailMap",
			state: mustState(t, `{"note": {}}`),
			want:  "thiếu key 'noteDetailMap'",
		},
		{
			name:  "empty noteDetailMap",
			state: mustState(t, `{"note": {"noteDetailMap": {}}}`),
			want:  "không có trong noteDetailMap",
		},
		{
			name:  "missing nested note",
			state: mustState(t, `{"note": {"noteDetailMap": {"abc": {}}}}`),
			want:  "thiếu key 'note' bên trong entry",
		},
		{
			name:  "invalid nested note json",
			state: mustState(t, `{"note": {"noteDetailMap": {"abc": {"note": "bad"}}}}`),
			want:  "parse NoteDetail lỗi",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := findNote(tt.state, "abc")
			if err == nil {
				t.Fatal("findNote() expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("findNote() error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestImageToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "long path trims prefix and suffix",
			raw:  "https://sns-webpic-qc.xhscdn.com/202412/aa/bb/cc/dd/spectrum/1040g2sg319abcdef!nd_dft_wlteh_webp_3",
			want: "spectrum/1040g2sg319abcdef",
		},
		{
			name: "short path keeps body",
			raw:  "https://sns-img.xhscdn.com/spectrum/simple-token!abc",
			want: "spectrum/simple-token",
		},
		{
			name: "invalid URL returns original input",
			raw:  "://bad-url",
			want: "://bad-url",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := imageToken(tt.raw)
			if got != tt.want {
				t.Fatalf("imageToken() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCDNURL(t *testing.T) {
	t.Parallel()

	token := "spectrum/1040g2sg319abcdef"
	tests := []struct {
		name   string
		format string
		want   string
	}{
		{
			name:   "png",
			format: "png",
			want:   "https://ci.xiaohongshu.com/spectrum/1040g2sg319abcdef?imageView2/format/png",
		},
		{
			name:   "auto fallback",
			format: "auto",
			want:   "https://sns-img-bd.xhscdn.com/spectrum/1040g2sg319abcdef",
		},
		{
			name:   "unknown format falls back",
			format: "gif",
			want:   "https://sns-img-bd.xhscdn.com/spectrum/1040g2sg319abcdef",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := cdnURL(token, tt.format)
			if got != tt.want {
				t.Fatalf("cdnURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveURLFollowsRedirects(t *testing.T) {
	t.Parallel()

	opts := FetchOptions{
		Cookie:    "web_session=test-cookie",
		UserAgent: "Custom Test Agent/1.0",
		Referer:   "https://example.com/share",
	}
	var seenCookie string
	var seenUserAgent string
	var seenReferer string

	final := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenCookie = r.Header.Get("Cookie")
		seenUserAgent = r.Header.Get("User-Agent")
		seenReferer = r.Header.Get("Referer")
		_, _ = w.Write([]byte("ok"))
	}))
	defer final.Close()

	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, final.URL+"/discovery/item/64bca1234def567890abcd12?xsec_token=abc", http.StatusFound)
	}))
	defer redirect.Close()

	resolved, err := resolveURL(context.Background(), redirect.URL+"/o/AgJB2qw08pH", opts)
	if err != nil {
		t.Fatalf("resolveURL() error = %v", err)
	}
	if !strings.Contains(resolved, "/discovery/item/64bca1234def567890abcd12") {
		t.Fatalf("resolveURL() = %q", resolved)
	}
	if seenCookie != opts.Cookie {
		t.Fatalf("redirected request Cookie = %q, want %q", seenCookie, opts.Cookie)
	}
	if seenUserAgent != opts.UserAgent {
		t.Fatalf("redirected request User-Agent = %q, want %q", seenUserAgent, opts.UserAgent)
	}
	if seenReferer != opts.Referer {
		t.Fatalf("redirected request Referer = %q, want %q", seenReferer, opts.Referer)
	}
}

func TestBuildInfoImagePost(t *testing.T) {
	t.Parallel()

	note := &noteDetailJSON{
		Title: "My Carousel",
		Type:  "normal",
		User: struct {
			Nickname string `json:"nickname"`
			UserID   string `json:"userId"`
		}{
			Nickname: "luna",
			UserID:   "user-1",
		},
		ImageList: []struct {
			URLDefault string `json:"urlDefault"`
			URLPre     string `json:"urlPre"`
			URL        string `json:"url"`
			InfoList   []struct {
				ImageScene string `json:"imageScene"`
				URL        string `json:"url"`
			} `json:"infoList"`
			Stream *struct {
				H264 []struct {
					MasterURL string `json:"masterUrl"`
				} `json:"h264"`
			} `json:"stream"`
		}{
			{
				URLDefault: "https://sns-webpic-qc.xhscdn.com/202412/aa/bb/cc/dd/spectrum/first-token!abc",
			},
			{
				URL: "https://sns-webpic-qc.xhscdn.com/202412/aa/bb/cc/dd/spectrum/second-token!abc",
				Stream: &struct {
					H264 []struct {
						MasterURL string `json:"masterUrl"`
					} `json:"h264"`
				}{
					H264: []struct {
						MasterURL string `json:"masterUrl"`
					}{
						{MasterURL: "https://live.example.com/photo.mov"},
					},
				},
			},
			{},
		},
	}

	info := buildInfo(note, "note-1", "")
	if info.NoteID != "note-1" {
		t.Fatalf("NoteID = %q, want %q", info.NoteID, "note-1")
	}
	if info.Title != "My Carousel" {
		t.Fatalf("Title = %q, want %q", info.Title, "My Carousel")
	}
	if info.Author != "luna" {
		t.Fatalf("Author = %q, want %q", info.Author, "luna")
	}
	if info.Type != "image" {
		t.Fatalf("Type = %q, want %q", info.Type, "image")
	}

	wantURLs := []string{
		"https://sns-webpic-qc.xhscdn.com/202412/aa/bb/cc/dd/spectrum/first-token!abc",
		"https://sns-webpic-qc.xhscdn.com/202412/aa/bb/cc/dd/spectrum/second-token!abc",
		"https://live.example.com/photo.mov",
	}
	if len(info.ImageURLs) != len(wantURLs) {
		t.Fatalf("len(ImageURLs) = %d, want %d", len(info.ImageURLs), len(wantURLs))
	}
	for i, want := range wantURLs {
		if info.ImageURLs[i] != want {
			t.Fatalf("ImageURLs[%d] = %q, want %q", i, info.ImageURLs[i], want)
		}
	}
}

func TestBuildInfoVideoPost(t *testing.T) {
	t.Parallel()

	note := &noteDetailJSON{
		Title: "My Video",
		Type:  "video",
		User: struct {
			Nickname string `json:"nickname"`
			UserID   string `json:"userId"`
		}{
			Nickname: "luna",
			UserID:   "user-1",
		},
		Video: &struct {
			Media struct {
				Stream struct {
					H264 []struct {
						MasterURL string `json:"masterUrl"`
					} `json:"h264"`
				} `json:"stream"`
			} `json:"media"`
		}{},
	}
	note.Video.Media.Stream.H264 = []struct {
		MasterURL string `json:"masterUrl"`
	}{
		{MasterURL: "https://video.example.com/master.m3u8"},
	}

	info := buildInfo(note, "video-note", "webp")
	if info.Type != "video" {
		t.Fatalf("Type = %q, want %q", info.Type, "video")
	}
	if info.VideoURL != "https://video.example.com/master.m3u8" {
		t.Fatalf("VideoURL = %q", info.VideoURL)
	}
	if len(info.ImageURLs) != 0 {
		t.Fatalf("ImageURLs should be empty for video note, got %d", len(info.ImageURLs))
	}
}

func TestGetHTML(t *testing.T) {
	t.Parallel()

	opts := FetchOptions{
		Cookie:         "web_session=test-cookie",
		UserAgent:      "Custom Test Agent/2.0",
		Referer:        "https://example.com/from-test",
		AcceptLanguage: "vi-VN,vi;q=0.9,en;q=0.8",
	}
	var gotUserAgent string
	var gotReferer string
	var gotAcceptLanguage string
	var gotAccept string
	var gotCookie string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUserAgent = r.Header.Get("User-Agent")
		gotReferer = r.Header.Get("Referer")
		gotAcceptLanguage = r.Header.Get("Accept-Language")
		gotAccept = r.Header.Get("Accept")
		gotCookie = r.Header.Get("Cookie")
		_, _ = w.Write([]byte("<html>ok</html>"))
	}))
	defer server.Close()

	html, err := getHTML(context.Background(), server.URL, opts)
	if err != nil {
		t.Fatalf("getHTML() error = %v", err)
	}
	if html != "<html>ok</html>" {
		t.Fatalf("getHTML() body = %q", html)
	}
	if gotUserAgent != opts.UserAgent {
		t.Fatalf("User-Agent = %q, want %q", gotUserAgent, opts.UserAgent)
	}
	if gotReferer != opts.Referer {
		t.Fatalf("Referer = %q, want %q", gotReferer, opts.Referer)
	}
	if gotAcceptLanguage != opts.AcceptLanguage || gotAccept != defaultAccept {
		t.Fatalf("expected Accept headers to be set, got Accept-Language=%q Accept=%q", gotAcceptLanguage, gotAccept)
	}
	if gotCookie != opts.Cookie {
		t.Fatalf("Cookie = %q, want %q", gotCookie, opts.Cookie)
	}
}

func TestGetHTMLErrorStatus(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer server.Close()

	_, err := getHTML(context.Background(), server.URL, FetchOptions{})
	if err == nil {
		t.Fatal("getHTML() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "HTTP 403") {
		t.Fatalf("getHTML() error = %v", err)
	}
}

func TestFetchEndToEndOffline(t *testing.T) {
	t.Parallel()

	html := `<html><head></head><body><script>window.__INITIAL_STATE__ = {
		"note": {
			"noteDetailMap": {
				"64bca1234def567890abcd12": {
					"note": {
						"noteId": "64bca1234def567890abcd12",
						"title": "fixture carousel",
						"type": "normal",
						"user": {"nickname": "fixture-user", "userId": "u-123"},
						"imageList": [
							{"urlDefault": "https://sns-webpic-qc.xhscdn.com/202412/aa/bb/cc/dd/spectrum/fixture-token-one!abc"},
							{"url": "https://sns-webpic-qc.xhscdn.com/202412/aa/bb/cc/dd/spectrum/fixture-token-two!abc"}
						]
					}
				}
			}
		}
	}</script></body></html>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(html))
	}))
	defer server.Close()

	noteURL := server.URL + "/explore/64bca1234def567890abcd12?xsec_token=abc"
	info, err := FetchWithOptions(context.Background(), noteURL, FetchOptions{
		Cookie:      "web_session=test-cookie",
		ImageFormat: "webp",
	})
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}

	if info.NoteID != "64bca1234def567890abcd12" {
		t.Fatalf("NoteID = %q", info.NoteID)
	}
	if info.Title != "fixture carousel" {
		t.Fatalf("Title = %q", info.Title)
	}
	if info.Author != "fixture-user" {
		t.Fatalf("Author = %q", info.Author)
	}
	if info.Type != "image" {
		t.Fatalf("Type = %q", info.Type)
	}

	wantURLs := []string{
		"https://ci.xiaohongshu.com/spectrum/fixture-token-one?imageView2/format/webp",
		"https://ci.xiaohongshu.com/spectrum/fixture-token-two?imageView2/format/webp",
	}
	if len(info.ImageURLs) != len(wantURLs) {
		t.Fatalf("len(ImageURLs) = %d, want %d", len(info.ImageURLs), len(wantURLs))
	}
	for i, want := range wantURLs {
		if info.ImageURLs[i] != want {
			t.Fatalf("ImageURLs[%d] = %q, want %q", i, info.ImageURLs[i], want)
		}
	}
}

func TestFetchResolvesShortLinkOffline(t *testing.T) {
	t.Parallel()

	finalHTML := `<html><body><script>window.__INITIAL_STATE__ = {
		"note": {
			"noteDetailMap": {
				"64bca1234def567890abcd12": {
					"note": {
						"noteId": "64bca1234def567890abcd12",
						"title": "resolved by short-link",
						"type": "normal",
						"user": {"nickname": "fixture-user", "userId": "u-123"},
						"imageList": [
							{"urlDefault": "https://sns-webpic-qc.xhscdn.com/202412/aa/bb/cc/dd/spectrum/fixture-token-one!abc"}
						]
					}
				}
			}
		}
	}</script></body></html>`

	final := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(finalHTML))
	}))
	defer final.Close()

	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, final.URL+"/discovery/item/64bca1234def567890abcd12?xsec_token=abc", http.StatusFound)
	}))
	defer redirect.Close()

	info, err := FetchWithOptions(context.Background(), redirect.URL+"/o/AgJB2qw08pH", FetchOptions{
		Cookie: "web_session=test-cookie",
	})
	if err != nil {
		t.Fatalf("Fetch() short-link error = %v", err)
	}
	if info.NoteID != "64bca1234def567890abcd12" {
		t.Fatalf("NoteID = %q", info.NoteID)
	}
	if info.Title != "resolved by short-link" {
		t.Fatalf("Title = %q", info.Title)
	}
	if len(info.ImageURLs) != 1 {
		t.Fatalf("len(ImageURLs) = %d, want 1", len(info.ImageURLs))
	}
	if info.ImageURLs[0] != "https://sns-webpic-qc.xhscdn.com/202412/aa/bb/cc/dd/spectrum/fixture-token-one!abc" {
		t.Fatalf("ImageURLs[0] = %q", info.ImageURLs[0])
	}
}

func TestFetchLive(t *testing.T) {
	noteURL := os.Getenv("XHS_TEST_URL")
	cookie := os.Getenv("XHS_COOKIE")
	ua := os.Getenv("XHS_USER_AGENT")

	if noteURL == "" || cookie == "" {
		t.Skip("set XHS_TEST_URL and XHS_COOKIE to run live Xiaohongshu fetch test")
	}

	info, err := FetchWithOptions(context.Background(), noteURL, FetchOptions{
		Cookie:      cookie,
		UserAgent:   ua,
		ImageFormat: "webp",
	})
	if err != nil {
		t.Fatalf("Fetch() live error = %v", err)
	}
	if info == nil {
		t.Fatal("Fetch() returned nil info")
	}
	if info.NoteID == "" {
		t.Fatal("live fetch returned empty NoteID")
	}
	if info.Title == "" {
		t.Fatal("live fetch returned empty Title")
	}
	if info.Type == "" {
		t.Fatal("live fetch returned empty Type")
	}
	if info.Type == "image" && len(info.ImageURLs) == 0 {
		t.Fatal("live fetch returned image note without ImageURLs")
	}
	if info.Type == "video" && info.VideoURL == "" {
		t.Fatal("live fetch returned video note without VideoURL")
	}

	t.Logf("live fetch ok: noteID=%s type=%s title=%q images=%d video=%t author=%q",
		info.NoteID, info.Type, info.Title, len(info.ImageURLs), info.VideoURL != "", info.Author)
}

func TestFetchCompatibilityWrapper(t *testing.T) {
	t.Parallel()

	html := `<html><body><script>window.__INITIAL_STATE__ = {
		"note": {
			"noteDetailMap": {
				"64bca1234def567890abcd12": {
					"note": {
						"noteId": "64bca1234def567890abcd12",
						"title": "wrapper compatibility",
						"type": "normal",
						"user": {"nickname": "fixture-user", "userId": "u-123"},
						"imageList": [
							{"urlDefault": "https://sns-webpic-qc.xhscdn.com/202412/aa/bb/cc/dd/spectrum/fixture-token-one!abc"}
						]
					}
				}
			}
		}
	}</script></body></html>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(html))
	}))
	defer server.Close()

	info, err := Fetch(context.Background(), server.URL+"/explore/64bca1234def567890abcd12", "web_session=test-cookie", "png")
	if err != nil {
		t.Fatalf("Fetch() wrapper error = %v", err)
	}
	if len(info.ImageURLs) != 1 || info.ImageURLs[0] != "https://ci.xiaohongshu.com/spectrum/fixture-token-one?imageView2/format/png" {
		t.Fatalf("Fetch() wrapper returned unexpected image URLs: %#v", info.ImageURLs)
	}
}

func mustState(t *testing.T, raw string) map[string]json.RawMessage {
	t.Helper()

	var state map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	return state
}

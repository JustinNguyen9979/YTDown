package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCanonicalizeXHSVideoPageURL(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "rednote host is rewritten and query is preserved",
			input: "https://www.rednote.com/discovery/item/note123?source=share&xsec_token=synthetic",
			want:  "https://www.xiaohongshu.com/discovery/item/note123?source=share&xsec_token=synthetic",
		},
		{
			name:  "rednote apex is rewritten",
			input: "https://rednote.com/explore/note123?token=synthetic",
			want:  "https://www.xiaohongshu.com/explore/note123?token=synthetic",
		},
		{
			name:  "rednote mobile subdomain is rewritten to www Xiaohongshu",
			input: "https://m.rednote.com/discovery/item/note123?source=share&xsec_token=synthetic",
			want:  "https://www.xiaohongshu.com/discovery/item/note123?source=share&xsec_token=synthetic",
		},
		{
			name:  "xhslink alias maps to xiaohongshu",
			input: "http://xhslink.com/o/AHiqLuW5rvi",
			want:  "http://www.xiaohongshu.com/o/AHiqLuW5rvi",
		},
		{
			name:  "xiaohongshu host remains unchanged",
			input: "https://www.xiaohongshu.com/explore/note123?source=share",
			want:  "https://www.xiaohongshu.com/explore/note123?source=share",
		},
		{
			name:  "xiaohongshu apex is rewritten to www",
			input: "https://xiaohongshu.com/explore/note123?source=share",
			want:  "https://www.xiaohongshu.com/explore/note123?source=share",
		},
		{
			name:  "xiaohongshu subdomain is rewritten to www",
			input: "https://m.xiaohongshu.com/explore/note123?source=share",
			want:  "https://www.xiaohongshu.com/explore/note123?source=share",
		},
		{
			name:  "uppercase rednote domain is rewritten",
			input: "https://WWW.REDNOTE.COM/explore/note123?source=share",
			want:  "https://www.xiaohongshu.com/explore/note123?source=share",
		},
		{
			name:  "lookalike domain remains unchanged",
			input: "https://rednote.com.evil.example/explore/note123",
			want:  "https://rednote.com.evil.example/explore/note123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := canonicalizeXHSVideoPageURL(tt.input)
			if err != nil {
				t.Fatalf("canonicalizeXHSVideoPageURL() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("canonicalizeXHSVideoPageURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormalizeVideoInputURLCleansEscapesAndCanonicalizesRednote(t *testing.T) {
	input := `https:\/\/www.rednote.com\/discovery\/item\/note123\\?source=share\\&xsec_token=synthetic`
	got, err := normalizeVideoInputURL(input)
	if err != nil {
		t.Fatalf("normalizeVideoInputURL() error = %v", err)
	}
	want := "https://www.xiaohongshu.com/discovery/item/note123?source=share&xsec_token=synthetic"
	if got != want {
		t.Fatalf("normalizeVideoInputURL() = %q, want %q", got, want)
	}
}

func TestNormalizeVideoInputURLRejectsMalformedURLAndPreservesLookalikeDomain(t *testing.T) {
	for _, input := range []string{"", "not-a-url"} {
		if _, err := normalizeVideoInputURL(input); err == nil {
			t.Fatalf("normalizeVideoInputURL(%q) expected an error", input)
		}
	}
	lookalike := "https://rednote.com.evil.example/discovery/item/note123"
	got, err := normalizeVideoInputURL(lookalike)
	if err != nil || got != lookalike {
		t.Fatalf("normalizeVideoInputURL() = (%q, %v), want unchanged lookalike URL", got, err)
	}
}

func TestReplaceEnvReplacesExistingValues(t *testing.T) {
	env := []string{"PATH=/usr/bin", "TMPDIR=/", "TMPDIR=/stale", "TMP=/stale"}
	got := replaceEnv(env, "TMPDIR", "/Users/test/tmp")
	got = replaceEnv(got, "TMP", "/Users/test/tmp")
	joined := strings.Join(got, "\n")
	if strings.Count(joined, "TMPDIR=") != 1 || !strings.Contains(joined, "TMPDIR=/Users/test/tmp") {
		t.Fatalf("replaceEnv() TMPDIR = %#v", got)
	}
	if strings.Count(joined, "TMP=") != 1 || !strings.Contains(joined, "TMP=/Users/test/tmp") {
		t.Fatalf("replaceEnv() TMP = %#v", got)
	}
}

func TestEnsureWritableDownloadTempDir(t *testing.T) {
	dir, err := ensureWritableDownloadTempDir()
	if err != nil {
		t.Fatalf("ensureWritableDownloadTempDir() error = %v", err)
	}
	defer os.RemoveAll(dir)

	probe := filepath.Join(dir, "write-probe")
	if err := os.WriteFile(probe, []byte("ok"), 0o600); err != nil {
		t.Fatalf("temporary directory is not writable: %v", err)
	}
}

func TestBuildYTDLPMetadataArgs(t *testing.T) {
	args := buildYTDLPMetadataArgs("https://www.xiaohongshu.com/discovery/item/note123?token=synthetic", xhsBestFormatSelector, []string{"--cookies", "/tmp/synthetic-cookie-file"})
	joined := strings.Join(args, " ")
	for _, want := range []string{"-J", "-f " + xhsBestFormatSelector, "--cookies /tmp/synthetic-cookie-file", "https://www.xiaohongshu.com/discovery/item/note123?token=synthetic"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("buildYTDLPMetadataArgs() = %#v, missing %q", args, want)
		}
	}
	if strings.Contains(joined, "synthetic-session") {
		t.Fatalf("metadata args expose a session value: %#v", args)
	}
}

func TestParseYTDLPFormatMetadata(t *testing.T) {
	tests := []struct {
		name string
		data string
		want ytdlpFormatMetadata
	}{
		{
			name: "requested formats combine selected video and audio bitrate",
			data: `{"id":"note123","requested_formats":[{"format_id":"video-1440","width":2560,"height":1440,"fps":30,"tbr":4200,"vcodec":"avc1"},{"format_id":"audio","tbr":180,"vcodec":"none"}],"formats":[{"format_id":"smaller-video","width":1280,"height":720,"fps":30,"tbr":900,"vcodec":"avc1"}]}`,
			want: ytdlpFormatMetadata{Width: 2560, Height: 1440, FPS: 30, TBR: 4380, VCodec: "avc1"},
		},
		{
			name: "available formats choose the best video and audio bitrate",
			data: `{"id":"note123","formats":[{"format_id":"video-1080","width":1920,"height":1080,"fps":60,"tbr":4000,"vcodec":"avc1"},{"format_id":"video-720","width":1280,"height":720,"fps":30,"tbr":1600,"vcodec":"avc1"},{"format_id":"audio-1","tbr":150,"vcodec":"none"},{"format_id":"audio-2","tbr":200,"vcodec":"none"}]}`,
			want: ytdlpFormatMetadata{Width: 1920, Height: 1080, FPS: 60, TBR: 4200, VCodec: "avc1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseYTDLPFormatMetadata(json.RawMessage(tt.data))
			if err != nil {
				t.Fatalf("parseYTDLPFormatMetadata() error = %v", err)
			}
			if got.Width != tt.want.Width || got.Height != tt.want.Height || got.FPS != tt.want.FPS || got.TBR != tt.want.TBR || got.VCodec != tt.want.VCodec {
				t.Fatalf("parsed metadata = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestParseYTDLPFormatMetadataRejectsInvalidJSON(t *testing.T) {
	if _, err := parseYTDLPFormatMetadata([]byte(`{"formats":[`)); err == nil {
		t.Fatal("parseYTDLPFormatMetadata() expected error for invalid JSON")
	}
}

func TestSelectXHSDirectFormatUsesQualityFromFrontend(t *testing.T) {
	formats := []ytdlpFormatMetadata{
		{FormatID: "low", Ext: "mp4", Width: 1280, Height: 720, FPS: 30, TBR: 1800, VCodec: "avc1", ACodec: "mp4a"},
		{FormatID: "high", Ext: "mp4", Width: 1920, Height: 1080, FPS: 60, TBR: 5200, VCodec: "avc1", ACodec: "mp4a"},
	}
	got, ok := selectXHSDirectFormat(formats, "MP4", "720p")
	if !ok || got != "low" {
		t.Fatalf("selectXHSDirectFormat() = (%q, %v), want (low, true) for FE quality 720p", got, ok)
	}
}

func TestSelectXHSDirectFormatCombinesVideoOnlyWithAudio(t *testing.T) {
	formats := []ytdlpFormatMetadata{
		{FormatID: "video-1080", Width: 1920, Height: 1080, FPS: 30, TBR: 4200, VCodec: "avc1"},
		{FormatID: "audio", VCodec: "none", TBR: 192},
	}
	got, ok := selectXHSDirectFormat(formats, "MP4", "Best Quality")
	if !ok || got != "video-1080+bestaudio" {
		t.Fatalf("selectXHSDirectFormat() = (%q, %v), want video plus best audio", got, ok)
	}
}

func TestBuildDownloadArgsDisablesTikTokFormatProbe(t *testing.T) {
	args, err := buildDownloadArgs(context.Background(), "https://www.tiktok.com/@user/video/123", "MP4", "Best", "/tmp/output", "/usr/bin/ffmpeg", true, 2)
	if err != nil {
		t.Fatalf("buildDownloadArgs() error = %v", err)
	}
	if !containsArg(args, "--no-check-formats") {
		t.Fatalf("TikTok args = %#v, missing --no-check-formats", args)
	}

	otherArgs, err := buildDownloadArgs(context.Background(), "https://www.youtube.com/watch?v=123", "MP4", "Best", "/tmp/output", "/usr/bin/ffmpeg", true, 2)
	if err != nil {
		t.Fatalf("buildDownloadArgs() for YouTube error = %v", err)
	}
	if containsArg(otherArgs, "--no-check-formats") {
		t.Fatalf("YouTube args unexpectedly contain --no-check-formats: %#v", otherArgs)
	}
}

func containsArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}

func TestBuildDownloadArgsUsesFrontendSavePathAndQuality(t *testing.T) {
	const savePath = "/tmp/ytdown-selected-output"
	args, err := buildDownloadArgs(context.Background(), "https://www.xiaohongshu.com/explore/note123", "MP4", "720p", savePath, "/usr/bin/ffmpeg", true, 2)
	if err != nil {
		t.Fatalf("buildDownloadArgs() error = %v", err)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{"height<=720", filepath.Join(savePath, "%(title)s [%(id)s].%(ext)s")} {
		if !strings.Contains(joined, want) {
			t.Fatalf("buildDownloadArgs() = %#v, missing %q", args, want)
		}
	}
}

func TestClassifyDownloadFailureKeepsXHSPipelineErrorsDistinct(t *testing.T) {
	for _, tt := range []struct {
		name string
		err  error
		kind string
	}{
		{name: "cookie export", err: errors.New("XHS_COOKIE_EXPORT: browser denied cookie file"), kind: "cookie_export"},
		{name: "note parse", err: errors.New("XHS_NOTE_PARSE: invalid character in literal null"), kind: "note_parse"},
		{name: "format inspection", err: errors.New("XHS_FORMAT_INSPECTION: metadata command failed"), kind: "format_inspection"},
		{name: "filesystem", err: errors.New("XHS_FILESYSTEM: read-only file system"), kind: "filesystem"},
		{name: "download", err: errors.New("XHS_DOWNLOAD: HTTP 500"), kind: "download"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			failure := classifyDownloadFailure(tt.err, true, "https://www.rednote.com/discovery/item/note123")
			if failure.ErrorKind != tt.kind || failure.RequiresCookie {
				t.Fatalf("failure = (%q, %v), want kind %q without cookie prompt", failure.ErrorKind, failure.RequiresCookie, tt.kind)
			}
		})
	}
}

func TestBuildDownloadArgsUsesRequestedSavePathAndQuality(t *testing.T) {
	const savePath = "/tmp/ytdown-selected-output"
	args, err := buildDownloadArgs(context.Background(), "https://www.xiaohongshu.com/explore/note123", "MP4", "720p", savePath, "/usr/bin/ffmpeg", true, 2)
	if err != nil {
		t.Fatalf("buildDownloadArgs() error = %v", err)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{"-f", "height<=720", filepath.Join(savePath, "%(title)s [%(id)s].%(ext)s")} {
		if !strings.Contains(joined, want) {
			t.Fatalf("buildDownloadArgs() = %#v, missing %q", args, want)
		}
	}
}

func TestSelectXHSFormatHonorsRequestedQuality(t *testing.T) {
	direct := []ytdlpFormatMetadata{
		{FormatID: "direct-1080", Ext: "mp4", Width: 1920, Height: 1080, FPS: 60, TBR: 5000, VCodec: "avc1", ACodec: "mp4a"},
		{FormatID: "direct-720", Ext: "mp4", Width: 1280, Height: 720, FPS: 30, TBR: 2200, VCodec: "avc1", ACodec: "mp4a"},
	}
	extractor := []ytdlpFormatMetadata{{FormatID: "extractor-1440", Ext: "mp4", Width: 2560, Height: 1440, FPS: 30, TBR: 7000, VCodec: "avc1"}}
	selected, useExtractor, err := selectXHSFormatForQuality(direct, extractor, "MP4", "720p")
	if err != nil {
		t.Fatalf("selectXHSFormatForQuality() error = %v", err)
	}
	if useExtractor || selected != "direct-720" {
		t.Fatalf("selection = (%q, %v), want direct-720 without extractor", selected, useExtractor)
	}
}

func TestChooseXHSVideoFormatKeepsValidDirectWhenExtractorInspectionFails(t *testing.T) {
	// The injectable helper below covers the decision independently of yt-dlp.
	directFormats := []ytdlpFormatMetadata{{FormatID: "direct-720", Ext: "mp4", Width: 1280, Height: 720, FPS: 30, TBR: 1800, VCodec: "avc1", ACodec: "mp4a"}}
	selected, useExtractor, err := selectXHSFormatForQuality(directFormats, nil, "MP4", "720p")
	if err != nil {
		t.Fatalf("selectXHSFormatForQuality() error = %v", err)
	}
	if useExtractor || selected != "direct-720" {
		t.Fatalf("selection = (%q, %v), want direct-720 even without extractor formats", selected, useExtractor)
	}
}

func TestSelectXHSFormatFallsBackToBestVideoAudioSelector(t *testing.T) {
	direct := []ytdlpFormatMetadata{{FormatID: "direct-480", Ext: "webm", Width: 854, Height: 480, FPS: 30, TBR: 900, VCodec: "vp9", ACodec: "opus"}}
	extractor := []ytdlpFormatMetadata{{FormatID: "extractor-1080", Ext: "mp4", Width: 1920, Height: 1080, FPS: 60, TBR: 5000, VCodec: "avc1"}}
	selected, useExtractor, err := selectXHSFormatForQuality(direct, extractor, "MP4", "720p")
	if err != nil {
		t.Fatalf("selectXHSFormatForQuality() error = %v", err)
	}
	if !useExtractor || selected != "bestvideo*[height<=720]+bestaudio/best[height<=720]" {
		t.Fatalf("selection = (%q, %v), want quality-limited extractor fallback", selected, useExtractor)
	}
	bestSelector, bestUsesExtractor, err := selectXHSFormatForQuality(direct, extractor, "MKV", "Best Quality")
	if err != nil {
		t.Fatalf("selectXHSFormatForQuality() error = %v", err)
	}
	if !bestUsesExtractor || bestSelector != xhsBestFormatSelector {
		t.Fatalf("best selection = (%q, %v), want fallback %q", bestSelector, bestUsesExtractor, xhsBestFormatSelector)
	}
}

func TestClassifyDownloadFailureSeparatesXHSPipelineErrors(t *testing.T) {
	for _, tt := range []struct {
		name string
		err  error
		kind string
	}{
		{name: "cookie export", err: errors.New("XHS_COOKIE_EXPORT: yt-dlp could not export cookies"), kind: "cookie_export"},
		{name: "note JSON", err: errors.New("XHS_NOTE_PARSE: invalid character 'e' in literal null"), kind: "note_parse"},
		{name: "format inspection", err: errors.New("XHS_FORMAT_INSPECTION: invalid metadata"), kind: "format_inspection"},
		{name: "filesystem", err: errors.New("XHS_FILESYSTEM: read-only file system"), kind: "filesystem"},
		{name: "download", err: errors.New("XHS_DOWNLOAD: HTTP 500"), kind: "download"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			failure := classifyDownloadFailure(tt.err, true, "https://www.rednote.com/discovery/item/note123")
			if failure.RequiresCookie {
				t.Fatal("classifyDownloadFailure() incorrectly requested a cookie")
			}
			if failure.ErrorKind != tt.kind {
				t.Fatalf("error kind = %q, want %q", failure.ErrorKind, tt.kind)
			}
			joined := strings.Join(failure.Details, " ")
			if strings.Contains(joined, "web_session") || strings.Contains(joined, "invalid or insufficient") {
				t.Fatalf("non-auth error was mislabeled as session failure: %q", joined)
			}
		})
	}
}

func TestXHSDownloadURLUsesCanonicalPostForExtractorSelector(t *testing.T) {
	noteURL := "https://www.rednote.com/discovery/item/note123?source=share&xsec_token=synthetic"
	got, err := xhsDownloadURL(noteURL, "https://sns-video-bd.xhscdn.com/direct.m3u8", true)
	if err != nil {
		t.Fatalf("xhsDownloadURL() error = %v", err)
	}
	want := "https://www.xiaohongshu.com/discovery/item/note123?source=share&xsec_token=synthetic"
	if got != want {
		t.Fatalf("xhsDownloadURL() = %q, want %q", got, want)
	}
}

func TestXHSDownloadURLUsesDirectURLWhenDirectWins(t *testing.T) {
	directURL := "https://sns-video-bd.xhscdn.com/direct.m3u8"
	got, err := xhsDownloadURL("https://www.rednote.com/discovery/item/note123", directURL, false)
	if err != nil {
		t.Fatalf("xhsDownloadURL() error = %v", err)
	}
	if got != directURL {
		t.Fatalf("xhsDownloadURL() = %q, want %q", got, directURL)
	}
}

func TestReplaceFormatSelector(t *testing.T) {
	args := []string{"-f", "bestvideo[ext=mp4]+bestaudio[ext=m4a]/best", "--merge-output-format", "mp4"}
	got := replaceFormatSelector(args, "bestvideo*+bestaudio")
	if got[1] != "bestvideo*+bestaudio" {
		t.Fatalf("format selector = %q, want bestvideo*+bestaudio", got[1])
	}
	if args[1] == got[1] {
		t.Fatal("replaceFormatSelector() mutated the input args")
	}
}

func TestRetryXHSWithFallbackDirectSuccessDoesNotRetry(t *testing.T) {
	calls := 0
	err := retryXHSWithFallback(context.Background(), "https://www.rednote.com/explore/note123?token=synthetic", "", nil, false, func(_, _ string) error {
		calls++
		return nil
	})
	if err != nil {
		t.Fatalf("retryXHSWithFallback() error = %v", err)
	}
	if calls != 0 {
		t.Fatalf("fallback attempts = %d, want 0", calls)
	}
}

func TestRetryXHSWithFallbackRetriesDirectFailure(t *testing.T) {
	calls := 0
	var gotURLs, gotFormats []string
	directErr := errors.New("direct stream unsupported")
	err := retryXHSWithFallback(context.Background(), "https://m.rednote.com/discovery/item/note123?token=synthetic", xhsBestFormatSelector, directErr, false, func(downloadURL, format string) error {
		calls++
		gotURLs = append(gotURLs, downloadURL)
		gotFormats = append(gotFormats, format)
		return nil
	})
	if err != nil {
		t.Fatalf("retryXHSWithFallback() error = %v", err)
	}
	if calls != 1 {
		t.Fatalf("fallback attempts = %d, want 1", calls)
	}
	if gotURLs[0] != "https://www.xiaohongshu.com/discovery/item/note123?token=synthetic" || gotFormats[0] != xhsBestFormatSelector {
		t.Fatalf("fallback = (%q, %q), want canonical post URL and extractor selector", gotURLs[0], gotFormats[0])
	}
}

func TestRetryXHSWithFallbackReturnsFallbackError(t *testing.T) {
	calls := 0
	directErr := errors.New("direct stream failed")
	fallbackErr := errors.New("extractor stream failed")
	err := retryXHSWithFallback(context.Background(), "https://www.rednote.com/explore/note123", xhsBestFormatSelector, directErr, false, func(_, _ string) error {
		calls++
		return fallbackErr
	})
	if !errors.Is(err, fallbackErr) || !strings.Contains(err.Error(), directErr.Error()) {
		t.Fatalf("retryXHSWithFallback() error = %v, want both direct and fallback errors", err)
	}
	if calls != 1 {
		t.Fatalf("fallback attempts = %d, want 1", calls)
	}
}

func TestRetryXHSWithFallbackSkipsRetryWhenContextDone(t *testing.T) {
	tests := []struct {
		name string
		ctx  func() (context.Context, context.CancelFunc)
	}{
		{
			name: "canceled",
			ctx: func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx, cancel
			},
		},
		{
			name: "deadline exceeded",
			ctx: func() (context.Context, context.CancelFunc) {
				return context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := tt.ctx()
			defer cancel()
			calls := 0
			err := retryXHSWithFallback(ctx, "https://www.rednote.com/explore/note123", "", errors.New("direct stream failed"), false, func(_, _ string) error {
				calls++
				return errors.New("extractor stream failed")
			})
			if err == nil {
				t.Fatal("retryXHSWithFallback() expected error")
			}
			if calls != 0 {
				t.Fatalf("fallback attempts = %d, want 0", calls)
			}
		})
	}
}

func TestRetryXHSWithFallbackRetriesDirectFormatFailure(t *testing.T) {
	calls := 0
	var gotURL, gotFormat string
	directErr := errors.New("direct format download failed")
	err := retryXHSWithFallback(context.Background(), "https://www.rednote.com/discovery/item/note123", xhsBestFormatSelector, directErr, false, func(downloadURL, format string) error {
		calls++
		gotURL, gotFormat = downloadURL, format
		return nil
	})
	if err != nil {
		t.Fatalf("retryXHSWithFallback() error = %v", err)
	}
	if calls != 1 || gotURL != "https://www.xiaohongshu.com/discovery/item/note123" || gotFormat != xhsBestFormatSelector {
		t.Fatalf("fallback = calls:%d url:%q format:%q, want one extractor fallback", calls, gotURL, gotFormat)
	}
}

func TestRetryXHSWithFallbackDoesNotRetryExtractorSelection(t *testing.T) {
	calls := 0
	directErr := errors.New("extractor stream failed")
	err := retryXHSWithFallback(context.Background(), "https://www.rednote.com/explore/note123", xhsBestFormatSelector, directErr, true, func(_, format string) error {
		calls++
		if format != xhsBestFormatSelector {
			t.Fatalf("format = %q, want extractor selector", format)
		}
		return errors.New("unexpected retry")
	})
	if !errors.Is(err, directErr) {
		t.Fatalf("retryXHSWithFallback() error = %v, want original extractor error", err)
	}
	if calls != 0 {
		t.Fatalf("fallback attempts = %d, want 0", calls)
	}
}

func TestPreferXHSVideoCandidateUsesHighestQuality(t *testing.T) {
	tests := []struct {
		name       string
		direct     ytdlpFormatMetadata
		extractor  ytdlpFormatMetadata
		wantDirect bool
	}{
		{
			name:       "direct wins with higher resolution",
			direct:     ytdlpFormatMetadata{Width: 2560, Height: 1440, FPS: 30, TBR: 3000},
			extractor:  ytdlpFormatMetadata{Width: 1920, Height: 1080, FPS: 60, TBR: 5000},
			wantDirect: true,
		},
		{
			name:       "extractor wins with higher resolution",
			direct:     ytdlpFormatMetadata{Width: 1280, Height: 720, FPS: 60, TBR: 8000},
			extractor:  ytdlpFormatMetadata{Width: 1920, Height: 1080, FPS: 30, TBR: 3500},
			wantDirect: false,
		},
		{
			name:       "direct wins with higher frame rate at same resolution",
			direct:     ytdlpFormatMetadata{Width: 1920, Height: 1080, FPS: 60, TBR: 4000},
			extractor:  ytdlpFormatMetadata{Width: 1920, Height: 1080, FPS: 30, TBR: 6000},
			wantDirect: true,
		},
		{
			name:       "bitrate breaks ties after resolution and frame rate",
			direct:     ytdlpFormatMetadata{Width: 1920, Height: 1080, FPS: 30, TBR: 5000},
			extractor:  ytdlpFormatMetadata{Width: 1920, Height: 1080, FPS: 30, TBR: 4000},
			wantDirect: true,
		},
		{
			name:       "unknown metadata favors extractor selector",
			direct:     ytdlpFormatMetadata{},
			extractor:  ytdlpFormatMetadata{Width: 1920, Height: 1080},
			wantDirect: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := preferXHSDirectVideo(tt.direct, tt.extractor); got != tt.wantDirect {
				t.Fatalf("preferXHSDirectVideo() = %t, want %t", got, tt.wantDirect)
			}
		})
	}
}

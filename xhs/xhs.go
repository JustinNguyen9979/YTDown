// Package xhs cung cấp thuật toán lấy ảnh cuộn (carousel) từ Xiaohongshu.
// Chỉ sử dụng Go standard library — không cần cài thêm gì cả.
//
// Cách dùng trong YTDown:
//
//	info, err := xhs.FetchWithOptions(ctx, "https://www.xiaohongshu.com/explore/NOTE_ID?xsec_token=XXX", xhs.FetchOptions{
//	    Cookie:      "web_session=YOUR_COOKIE",
//	    ImageFormat: "best",
//	})
//	for _, url := range info.ImageURLs {
//	    // download từng url ...
//	}
package xhs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// ─── Public types ─────────────────────────────────────────────────────────────

// NoteInfo chứa thông tin bài viết và danh sách URL ảnh đã resolve
type NoteInfo struct {
	NoteID    string   // ID bài viết
	Title     string   // Tiêu đề
	Author    string   // Tên tác giả
	Type      string   // "image" | "video"
	ImageURLs []string // CDN URLs của từng ảnh trong carousel (đã sẵn sàng tải)
	VideoURL  string   // URL video (chỉ có khi Type == "video")
}

// FetchOptions chứa các tham số đầu vào có thể truyền từ bên ngoài.
// Mọi field đều optional; nếu bỏ trống module sẽ dùng default an toàn.
type FetchOptions struct {
	// Cookie là toàn bộ header Cookie, thường gồm web_session=...
	Cookie string

	// UserAgent override User-Agent mặc định khi resolve short-link và tải HTML.
	UserAgent string

	// Referer override Referer mặc định.
	// Nếu bỏ trống sẽ dùng https://www.xiaohongshu.com/
	Referer string

	// AcceptLanguage override Accept-Language mặc định.
	AcceptLanguage string

	// ImageFormat điều khiển URL ảnh trả về.
	// Hỗ trợ: "", "best", "auto", "png", "webp", "jpeg", "heic"
	// "", "best" và "auto" sẽ trả ảnh gốc để ưu tiên chất lượng cao nhất.
	ImageFormat string

	// Timeout áp dụng cho cả bước resolve short-link lẫn tải HTML.
	// Nếu <= 0 sẽ dùng 30 giây.
	Timeout time.Duration
}

// ─── Entry point ──────────────────────────────────────────────────────────────

const defaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
const defaultReferer = "https://www.xiaohongshu.com/"
const defaultAcceptLanguage = "zh-CN,zh;q=0.9,en;q=0.8"
const defaultAccept = "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"
const defaultTimeout = 30 * time.Second

func InferReferer(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return defaultReferer
	}
	host := strings.TrimPrefix(u.Host, "www.")
	return "https://www." + host + "/"
}

// Fetch giữ tương thích ngược với API cũ.
// Ưu tiên dùng FetchWithOptions khi cần truyền tham số từ nguồn bên ngoài.
func Fetch(ctx context.Context, noteURL, cookie, format string) (*NoteInfo, error) {
	return FetchWithOptions(ctx, noteURL, FetchOptions{
		Cookie:      cookie,
		ImageFormat: format,
	})
}

// FetchWithOptions tải dữ liệu từ Xiaohongshu với tham số cấu hình truyền từ bên ngoài.
func FetchWithOptions(ctx context.Context, noteURL string, opts FetchOptions) (*NoteInfo, error) {
	// 1. Resolve short-link (xhslink.com) về URL note thật
	resolvedURL, err := resolveURL(ctx, noteURL, opts)
	if err != nil {
		return nil, err
	}

	// 2. Parse Note ID từ URL đã resolve
	noteID, err := parseNoteID(resolvedURL)
	if err != nil {
		return nil, err
	}

	// 3. Tải HTML trang bài viết
	html, err := getHTML(ctx, resolvedURL, opts)
	if err != nil {
		return nil, err
	}

	// 4. Parse window.__INITIAL_STATE__ từ HTML → JSON
	state, err := extractState(html)
	if err != nil {
		return nil, err
	}

	// 5. Điều hướng JSON → lấy NoteDetail
	note, err := findNote(state, noteID)
	if err != nil {
		return nil, err
	}

	// 6. Resolve URL CDN cho từng ảnh
	return buildInfo(note, noteID, opts.ImageFormat), nil
}

// ─── Step 1: Parse Note ID ────────────────────────────────────────────────────

var noteIDPatterns = []*regexp.Regexp{
	regexp.MustCompile(`/explore/([a-f0-9]+)`),
	regexp.MustCompile(`/discovery/item/([a-f0-9]+)`),
	regexp.MustCompile(`[?&]noteId=([a-f0-9]+)`),
}

func parseNoteID(rawURL string) (string, error) {
	for _, p := range noteIDPatterns {
		if m := p.FindStringSubmatch(rawURL); len(m) > 1 {
			return m[1], nil
		}
	}
	if u, err := url.Parse(rawURL); err == nil {
		if id := u.Query().Get("noteId"); id != "" {
			return id, nil
		}
	}
	return "", fmt.Errorf("xhs: không tìm thấy Note ID trong URL: %s", rawURL)
}

// ─── Step 2: GET HTML ─────────────────────────────────────────────────────────

func newRequest(ctx context.Context, method, targetURL string, opts FetchOptions) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, targetURL, nil)
	if err != nil {
		return nil, fmt.Errorf("xhs: tạo request lỗi: %w", err)
	}
	req.Header.Set("User-Agent", chooseString(opts.UserAgent, defaultUserAgent))
	req.Header.Set("Referer", chooseString(opts.Referer, InferReferer(targetURL)))
	req.Header.Set("Accept-Language", chooseString(opts.AcceptLanguage, defaultAcceptLanguage))
	req.Header.Set("Accept", defaultAccept)
	if opts.Cookie != "" {
		req.Header.Set("Cookie", opts.Cookie)
	}
	return req, nil
}

func resolveURL(ctx context.Context, rawURL string, opts FetchOptions) (string, error) {
	req, err := newRequest(ctx, http.MethodGet, rawURL, opts)
	if err != nil {
		return "", err
	}

	client := &http.Client{
		Timeout: effectiveTimeout(opts),
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("stopped after 10 redirects")
			}
			forwardHeaders(req, via)
			return nil
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("xhs: resolve short-link thất bại: %w", err)
	}
	defer resp.Body.Close()

	return resp.Request.URL.String(), nil
}

func getHTML(ctx context.Context, noteURL string, opts FetchOptions) (string, error) {
	req, err := newRequest(ctx, http.MethodGet, noteURL, opts)
	if err != nil {
		return "", err
	}

	client := &http.Client{Timeout: effectiveTimeout(opts)}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("xhs: request thất bại: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("xhs: server trả về HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("xhs: đọc response lỗi: %w", err)
	}
	return string(body), nil
}

func effectiveTimeout(opts FetchOptions) time.Duration {
	if opts.Timeout > 0 {
		return opts.Timeout
	}
	return defaultTimeout
}

func chooseString(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

func forwardHeaders(req *http.Request, via []*http.Request) {
	if len(via) == 0 {
		return
	}

	last := via[len(via)-1]
	for _, header := range []string{"User-Agent", "Referer", "Accept-Language", "Accept", "Cookie"} {
		if value := last.Header.Get(header); value != "" {
			req.Header.Set(header, value)
		}
	}
}

// ─── Step 3: Parse window.__INITIAL_STATE__ ───────────────────────────────────
//
// XHS nhúng toàn bộ dữ liệu bài viết vào thẻ <script> trong HTML
// dưới dạng: window.__INITIAL_STATE__ = { ... }
// Không có API riêng — đây là cách XHS-Downloader hoạt động.

var statePatterns = []*regexp.Regexp{
	// Dạng phổ biến nhất
	regexp.MustCompile(`window\.__INITIAL_STATE__\s*=\s*(\{[\s\S]*?\})\s*</script>`),
	// Dạng kết thúc bằng ;(function
	regexp.MustCompile(`window\.__INITIAL_STATE__\s*=\s*(\{[\s\S]*?\})\s*;\s*\(function`),
	// Fallback tổng quát
	regexp.MustCompile(`__INITIAL_STATE__\s*=\s*(\{[\s\S]*?\})\s*[;<]`),
}

func extractState(html string) (map[string]json.RawMessage, error) {
	var raw string
	for _, p := range statePatterns {
		if m := p.FindStringSubmatch(html); len(m) > 1 {
			raw = m[1]
			break
		}
	}
	if raw == "" {
		return nil, fmt.Errorf("xhs: không tìm thấy __INITIAL_STATE__ trong HTML — kiểm tra lại cookie")
	}

	// XHS dùng JS `undefined` (không hợp lệ trong JSON) → thay bằng null
	raw = strings.ReplaceAll(raw, ":undefined", ":null")
	raw = strings.ReplaceAll(raw, ",undefined", ",null")
	raw = strings.ReplaceAll(raw, "[undefined", "[null")

	var state map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		return nil, fmt.Errorf("xhs: parse JSON lỗi: %w", err)
	}
	return state, nil
}

// ─── Step 4: Điều hướng JSON lấy NoteDetail ──────────────────────────────────
//
// Cấu trúc JSON:
//   state
//   └── "note"
//       └── "noteDetailMap"
//           └── "{noteID}"
//               └── "note"
//                   ├── "imageList": [{urlDefault, url, ...}, ...]
//                   ├── "title": "..."
//                   ├── "type": "normal" | "video"
//                   └── "user": {nickname, userId}

// noteDetailJSON là struct tương ứng với field "note" bên trong noteDetailMap
type noteDetailJSON struct {
	NoteID    string `json:"noteId"`
	Title     string `json:"title"`
	Type      string `json:"type"` // "normal" = ảnh cuộn, "video" = video
	ImageList []struct {
		URLDefault string `json:"urlDefault"`
		URLPre     string `json:"urlPre"`
		URL        string `json:"url"`
		InfoList   []struct {
			ImageScene string `json:"imageScene"`
			URL        string `json:"url"`
		} `json:"infoList"`
		// Live photo (ảnh động) nằm trong stream.h264
		Stream *struct {
			H264 []struct {
				MasterURL string `json:"masterUrl"`
			} `json:"h264"`
		} `json:"stream"`
	} `json:"imageList"`
	User struct {
		Nickname string `json:"nickname"`
		UserID   string `json:"userId"`
	} `json:"user"`
	Video *struct {
		Media struct {
			Stream struct {
				H264 []struct {
					MasterURL string `json:"masterUrl"`
				} `json:"h264"`
			} `json:"stream"`
		} `json:"media"`
	} `json:"video"`
}

func findNote(state map[string]json.RawMessage, noteID string) (*noteDetailJSON, error) {
	// state["note"]
	noteSection, ok := state["note"]
	if !ok {
		return nil, fmt.Errorf("xhs: thiếu key 'note' trong state")
	}
	var noteMap map[string]json.RawMessage
	if err := json.Unmarshal(noteSection, &noteMap); err != nil {
		return nil, fmt.Errorf("xhs: parse 'note' section lỗi: %w", err)
	}

	// state["note"]["noteDetailMap"]
	detailMapRaw, ok := noteMap["noteDetailMap"]
	if !ok {
		return nil, fmt.Errorf("xhs: thiếu key 'noteDetailMap'")
	}
	var noteDetailMap map[string]json.RawMessage
	if err := json.Unmarshal(detailMapRaw, &noteDetailMap); err != nil {
		return nil, fmt.Errorf("xhs: parse 'noteDetailMap' lỗi: %w", err)
	}

	// state["note"]["noteDetailMap"][noteID]
	// Nếu không khớp → lấy entry đầu tiên (fallback)
	entry, ok := noteDetailMap[noteID]
	if !ok {
		for _, v := range noteDetailMap {
			entry = v
			break
		}
	}
	if entry == nil {
		return nil, fmt.Errorf("xhs: note ID '%s' không có trong noteDetailMap", noteID)
	}

	// entry["note"]
	var wrapper map[string]json.RawMessage
	if err := json.Unmarshal(entry, &wrapper); err != nil {
		return nil, fmt.Errorf("xhs: parse note entry lỗi: %w", err)
	}
	noteRaw, ok := wrapper["note"]
	if !ok {
		return nil, fmt.Errorf("xhs: thiếu key 'note' bên trong entry")
	}

	var detail noteDetailJSON
	if err := json.Unmarshal(noteRaw, &detail); err != nil {
		return nil, fmt.Errorf("xhs: parse NoteDetail lỗi: %w", err)
	}
	return &detail, nil
}

// ─── Step 5: Build CDN URLs ───────────────────────────────────────────────────
//
// Mỗi ảnh trong imageList có rawURL dạng:
//   https://sns-webpic-qc.xhscdn.com/202412/abc123/spectrum/1040g2sg319abcdef!nd_dft_wlteh_webp_3
//
// Thuật toán extract token (từ XHS-Downloader image.py):
//   1. Lấy path của URL
//   2. Bỏ 5 segment đầu (domain timestamp + hash prefix)
//   3. Cắt tại "!" để bỏ image-processing suffix
//   → token = "spectrum/1040g2sg319abcdef"
//
// CDN URL cuối:
//   https://ci.xiaohongshu.com/{token}?imageView2/format/png  (convert format)
//   https://sns-img-bd.xhscdn.com/{token}                    (format gốc)

func imageToken(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")

	start := 0
	for i, part := range parts {
		if part == "notes_pre_post" || part == "spectrum" {
			start = i
			break
		}
	}

	var token string
	if start > 0 && start < len(parts) {
		token = strings.Join(parts[start:], "/")
	} else if len(parts) >= 2 {
		token = strings.Join(parts[len(parts)-2:], "/")
	} else {
		token = strings.TrimPrefix(u.Path, "/")
	}

	// Bỏ suffix xử lý ảnh (vd: !nd_dft_wlteh_webp_3)
	if i := strings.Index(token, "!"); i != -1 {
		token = token[:i]
	}
	return token
}

func cdnURL(token, format string) string {
	switch format {
	case "png", "webp", "jpeg", "heic":
		return fmt.Sprintf("https://ci.xiaohongshu.com/%s?imageView2/format/%s", token, format)
	default: // "auto" → ảnh gốc, format do server quyết định
		return fmt.Sprintf("https://sns-img-bd.xhscdn.com/%s", token)
	}
}

func buildInfo(note *noteDetailJSON, noteID, format string) *NoteInfo {
	if format == "" || format == "best" {
		format = "auto"
	}
	info := &NoteInfo{
		NoteID: noteID,
		Title:  note.Title,
		Author: note.User.Nickname,
		Type:   "image",
	}

	// Bài video
	if note.Type == "video" {
		info.Type = "video"
		if note.Video != nil && len(note.Video.Media.Stream.H264) > 0 {
			info.VideoURL = note.Video.Media.Stream.H264[0].MasterURL
		}
		return info
	}

	// Bài ảnh cuộn: duyệt toàn bộ imageList
	for _, img := range note.ImageList {
		raw := preferredImageURL(img)
		if raw == "" {
			continue
		}
		info.ImageURLs = append(info.ImageURLs, resolveImageURL(raw, format))

		// Live photo đính kèm (nếu có)
		if img.Stream != nil && len(img.Stream.H264) > 0 {
			info.ImageURLs = append(info.ImageURLs, img.Stream.H264[0].MasterURL)
		}
	}
	return info
}

func preferredImageURL(img struct {
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
}) string {
	for _, scene := range []string{"WB_DFT", "WB_PRV"} {
		for _, item := range img.InfoList {
			if item.ImageScene == scene && item.URL != "" {
				return normalizeAssetURL(item.URL)
			}
		}
	}

	for _, candidate := range []string{img.URLDefault, img.URLPre, img.URL} {
		if candidate != "" {
			return normalizeAssetURL(candidate)
		}
	}
	return ""
}

func resolveImageURL(rawURL, format string) string {
	rawURL = normalizeAssetURL(rawURL)
	if format == "auto" {
		return rawURL
	}
	token := imageToken(rawURL)
	return cdnURL(token, format)
}

func normalizeAssetURL(rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	if strings.HasPrefix(rawURL, "http://") {
		return "https://" + strings.TrimPrefix(rawURL, "http://")
	}
	if strings.HasPrefix(rawURL, "//") {
		return "https:" + rawURL
	}
	return rawURL
}

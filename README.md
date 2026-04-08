# 🎬 YTDown - Trình tải Video & Chuyển đổi Media cho macOS

YTDown là ứng dụng Desktop mạnh mẽ, đơn giản dành cho macOS, giúp bạn tải video chất lượng cao và trích xuất âm thanh từ YouTube, Facebook, TikTok và hàng trăm nền tảng khác.

---

## 📥 Tải về ngay (Cho người dùng)

Để sử dụng ứng dụng ngay lập tức mà không cần quan tâm đến code, bạn chỉ cần tải file cài đặt bên dưới:

[![Download YTDown](https://img.shields.io/badge/Tải_về_cho-macOS_.dmg-0a84ff?style=for-the-badge&logo=apple)](https://github.com/JustinNguyen9979/YTDown/releases)

> **Lưu ý:** Sau khi tải về, nếu macOS báo "App is damaged" hoặc "Unidentified Developer", hãy nhấn chuột phải vào ứng dụng và chọn **Open**.

---

## 🛠 Hướng dẫn cài đặt môi trường (Cho người mới)

Nếu bạn muốn tự tay Build ứng dụng từ mã nguồn, hãy làm theo các bước đơn giản sau:

### 1. Mở Terminal
Nhấn phím `Command (⌘) + Space`, gõ **Terminal** và nhấn **Enter**. Một cửa sổ lệnh sẽ hiện ra.

### 2. Cài đặt Homebrew (Nếu chưa có)
Homebrew là trình quản lý gói dành cho macOS. Hãy copy dòng lệnh sau và dán vào Terminal, sau đó nhấn **Enter**:
```bash
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
```

**Lưu ý quan trọng:** Sau khi chạy xong lệnh trên, bạn cần thêm Homebrew vào đường dẫn hệ thống (PATH) bằng cách chạy 2 lệnh sau:
```bash
(echo; echo 'eval "$(/opt/homebrew/bin/brew shellenv)"') >> ~/.zshrc
eval "$(/opt/homebrew/bin/brew shellenv)"
```

### 3. Cài đặt các công cụ hỗ trợ
Sau khi cài xong Homebrew, hãy dán lệnh này để cài đặt các thành phần cần thiết:
```bash
brew install yt-dlp ffmpeg go
```

### 4. Cài đặt Wails CLI
Đây là công cụ để build ứng dụng này từ mã nguồn Go:
```bash
go install github.com/wailsapp/wails/v2/cmd/wails@latest
```

---

## 🏗 Hướng dẫn Build ứng dụng từ mã nguồn

Dành cho các bạn muốn đóng góp hoặc tùy chỉnh ứng dụng:

1. **Tải mã nguồn về máy:**
   ```bash
   git clone https://github.com/JustinNguyen9979/YTDown.git
   cd YTDown
   ```

2. **Cài đặt thư viện:**
   ```bash
   go mod tidy
   ```

3. **Chạy ứng dụng ở chế độ phát triển:**
   ```bash
   wails dev
   ```

4. **Build bản chính thức (.app):**
   ```bash
   wails build -platform darwin/universal -ldflags "-s -w"
   ```
   *Ứng dụng hoàn thiện sẽ nằm trong thư mục `build/bin/YTDown.app`.*

5. **Tạo file cài đặt (.dmg):**
   Sử dụng script build có sẵn trong dự án:
   ```bash
   bash build.sh
   ```

## 🌟 Tính năng chính

- Hỗ trợ tải video chất lượng cao từ nhiều nguồn: **YouTube, Facebook/Instagram Reels, TikTok, Xiaohongshu/Rednote**,...
- Hỗ trợ download bằng **cookie** cho các video ytb bị giới hạn.
- **Cookie sẽ bị xóa ngay sau khi đóng app.**
- Tự động nhận diện và xử lý liên kết thông minh.
- Hỗ trợ tải từng video đơn lẻ hoặc toàn bộ danh sách phát (Playlist).
- Tùy chọn định dạng xuất tệp: `MP4` (Video) hoặc `MP3` (Âm thanh).
- Chọn chất lượng video mong muốn (1080p, 720p, 4k...).
- Tự động kiểm tra và cập nhật `yt-dlp` ngay trong App.
- Hiển thị Thumbnail video, xem video trực tiếp từ Thumbnails.

---

## 📂 Cấu trúc dự án

```text
YTDown/
├── app.go          # Logic xử lý giao diện và cập nhật
├── downloader.go   # Core xử lý tải video với yt-dlp
├── compressor.go   # Xử lý nén video/hình ảnh
├── main.go         # Điểm khởi đầu của ứng dụng
├── frontend/       # Giao diện người dùng (JS/HTML/CSS)
├── build.sh        # Script đóng gói ứng dụng chuyên nghiệp
└── README.md       # Tài liệu hướng dẫn
```

## 📄 License

Dự án được phát hành dưới bản quyền **MIT**.

## ☕ Ủng hộ tác giả

Nếu YTDown giúp ích cho công việc của bạn, hãy mời mình một ly cà phê nhé:

- **Ngân hàng:** MB Bank
- **Số tài khoản:** `0798888888888`
- **Chủ tài khoản:** `Nguyen Duc Huy`

Cảm ơn bạn đã sử dụng YTDown! 🚀

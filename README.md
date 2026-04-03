# 🎬 YTDown - YouTube Downloader for macOS

[![Platform](https://img.shields.io/badge/platform-macOS-000000.svg?style=flat-square&logo=apple)](https://apple.com)
[![Framework](https://img.shields.io/badge/framework-Wails_v2-red.svg?style=flat-square&logo=go)](https://wails.io/)
[![Go](https://img.shields.io/badge/language-Go-00ADD8.svg?style=flat-square&logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/license-MIT-green.svg?style=flat-square)](LICENSE)

**YTDown** là một ứng dụng tải video YouTube hiện đại, mượt mà dành riêng cho macOS, được xây dựng bằng **Wails v2** và **Go**. Với giao diện **Glassmorphism** sang trọng, YTDown mang lại trải nghiệm tải video trực quan và mạnh mẽ.

---

## ✨ Tính Năng Nổi Bật (Features)

*   ✅ **Tải Video Đơn Lẻ (Single Download)**: Hỗ trợ dán link và tải nhanh chóng.
*   ✅ **Tải Hàng Loạt (Batch Download)**: Quản lý hàng đợi tải nhiều video cùng lúc.
*   ✅ **Hỗ Trợ Playlist**: Tự động nhận diện playlist và cho phép chọn video cần tải.
*   ✅ **Đa Dạng Định Dạng**: Hỗ trợ chuyển đổi sang **MP4** hoặc trích xuất âm thanh **MP3**.
*   ✅ **Tùy Chọn Chất Lượng**: Lựa chọn độ phân giải từ **360p** đến **Best Quality (4K/8K)**.
*   ✅ **Tiến Trình Thời Gian Thực**: Theo dõi tốc độ tải, dung lượng qua giao diện trực quan.
*   ✅ **Tốc Độ Tối Đa**: Tối ưu hóa tải đa luồng bằng cách sử dụng toàn bộ nhân CPU (`--concurrent-fragments`).

---

## 🎨 Giao Diện (UI/UX)

Ứng dụng được thiết kế theo phong cách **Liquid Glass**:
- Hiệu ứng làm mờ nền (Backdrop blur) cực đẹp.
- Font chữ hệ thống `-apple-system` chuẩn Apple.
- Layout thông minh, dễ dàng chuyển đổi giữa các chế độ tải.

---

## 🛠️ Yêu Cầu Hệ Thống (Requirements)

*   **Hệ điều hành**: macOS 12 Monterey trở lên.
*   **Công cụ đi kèm**: 
    *   `yt-dlp` (Đã được bundle sẵn trong app hoặc cài qua `brew install yt-dlp`).
    *   `ffmpeg` (Cần thiết để gộp video/audio - Cài qua `brew install ffmpeg`).

---

## 🚀 Hướng Dẫn Cài Đặt Cho Developer

Nếu bạn muốn tự build từ mã nguồn:

### 1. Cài đặt Wails CLI
```bash
go install github.com/wailsapp/wails/v2/cmd/wails@latest
```

### 2. Clone và Cài Đặt Dependencies
```bash
git clone https://github.com/JustinNguyen9979/YTDown.git
cd YTDown
go mod tidy
```

### 3. Chạy Ở Chế Độ Phát Triển (Dev Mode)
```bash
wails dev
```

### 4. Build Ứng Dụng (.app)
```bash
# Build cho kiến trúc hiện tại
wails build -platform darwin

# Build bản Universal (chạy cả Intel & Apple Silicon)
wails build -platform darwin -tags universal
```
Sản phẩm sau khi build sẽ nằm trong thư mục: `build/bin/YTDown.app`

---

## ⚙️ Cấu Trúc Dự Án (Project Structure)

```text
YTDown/
├── main.go            # Điểm khởi đầu của ứng dụng
├── app.go             # Logic điều hướng và vòng đời ứng dụng
├── downloader.go      # Xử lý logic tải video (yt-dlp wrapper)
├── frontend/          # Giao diện người dùng (HTML/CSS/JS)
├── resources/         # Chứa binary yt-dlp & ffmpeg
└── build/             # Cấu hình build và icon cho macOS
```

---

## 📄 Giấy Phép (License)

Dự án này được phát hành dưới giấy phép **MIT**. Bạn có thể tự do chỉnh sửa và phân phối.

---
**Built with ❤️ using Wails & Go**

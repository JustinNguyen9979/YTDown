# 🎬 YTDown - Trình tải Video & Chuyển đổi Media Đa nền tảng

YTDown là ứng dụng Desktop mạnh mẽ, đơn giản dành cho **macOS, Windows và Linux**, giúp bạn tải video chất lượng cao và trích xuất âm thanh từ YouTube, Facebook, TikTok và hàng trăm nền tảng khác.

---

## Chọn phiên bản phù hợp với hệ điều hành của bạn:

### 🍏 Cài đặt qua Homebrew (Cho macOS)

```bash
brew update
brew tap justinNguyen9979/ytdown
brew install --cask ytdown
```

### 🐧 Cài đặt qua APT (Cho Debian/Ubuntu)

```bash
echo "deb [trusted=yes] https://JustinNguyen9979.github.io/YTDown/apt stable main" \
  | sudo tee /etc/apt/sources.list.d/ytdown.list
sudo apt update
sudo apt install ytdown
```

### 🪟 Cài đặt qua Winget (Cho Windows)
```bash
winget install JustinNguyen.YTDown
```

---

## 🔄 Hướng dẫn Cập nhật (Upgrade) thủ công

Trong trường hợp bạn muốn tự cập nhật YTDown lên phiên bản mới nhất thông qua dòng lệnh, hãy sử dụng các lệnh sau tương ứng với hệ điều hành của bạn:

### 🍏 macOS (Homebrew)
```bash
brew update
brew upgrade --cask ytdown
```

### 🐧 Linux (Debian/Ubuntu - APT)
```bash
sudo apt update
sudo apt install --only-upgrade ytdown
```

### 🪟 Windows (Winget)
```bash
winget upgrade --id JustinNguyen.YTDown
```

---

## 🛠 Tự động cài đặt Dependencies

YTDown được thiết kế để hoạt động "mì ăn liền". Khi bạn mở app lần đầu, nó sẽ tự động kiểm tra và hướng dẫn cài đặt các công cụ hỗ trợ (`ffmpeg`, `yt-dlp`, `gallery-dl`) tùy theo hệ điều hành:

*   **macOS:** Tự động cài qua **Homebrew**.
*   **Windows:** Tự động cài qua **Winget**.
*   **Linux:** Tự động cài qua trình quản lý gói của hệ thống (apt, dnf, pacman...).

---

## 🛠 Hướng dẫn cài đặt môi trường (Cho người mới)

Nếu bạn muốn tự tay Build ứng dụng từ mã nguồn, hãy làm theo các bước đơn giản sau:

### 1. Mở Terminal
Nhấn phím `Command (⌘) + Space`, gõ **Terminal** và nhấn **Enter**. Một cửa sổ lệnh sẽ hiện ra.

### 2. Cài đặt Homebrew (Nếu chưa có)

Có thể sử dụng các AI như Gemini, ChatGPT để hỏi cách cài đặt Homebrew phù hợp với dòng máy hiện tại đang sử dụng.

Homebrew là trình quản lý gói dành cho macOS. Hãy copy dòng lệnh sau và dán vào Terminal, sau đó nhấn **Enter**:
```bash
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
```

**⚠️ Lưu ý quan trọng:** Sau khi cài xong, bạn cần thêm Homebrew vào PATH.

**Nếu dùng Mac chip Apple Silicon (M1/M2/M3):**
```bash
(echo; echo 'eval "$(/opt/homebrew/bin/brew shellenv)"') >> ~/.zshrc
eval "$(/opt/homebrew/bin/brew shellenv)"
```

**Nếu dùng Mac chip Intel (x86_64):**
```bash
(echo; echo 'eval "$(/usr/local/bin/brew shellenv)"') >> ~/.zshrc
eval "$(/usr/local/bin/brew shellenv)"
```


> 💡 **Không biết chip gì?** Nhấn vào menu Apple () → **About This Mac** → xem dòng **Chip** hoặc **Processor**.

Chạy lệnh này để khởi động lại zsh.
```bash
source ~/.zshrc
```

---

## 🏗 Hướng dẫn Build ứng dụng từ mã nguồn

Dành cho các bạn muốn tùy chỉnh ứng dụng:

1. **Cài đặt các công cụ hỗ trợ (Cho Development)**

Dành cho MacOS:
```bash
# Công cụ phát triển (bắt buộc)
brew install go node

# Dependencies cho YTDown (application sẽ tự động cài khi chạy)
# Nếu bạn muốn cài trước để test local:
brew install ffmpeg yt-dlp gallery-dl
```

Dành cho Debian:
```bash
sudo apt update
sudo apt install golang nodejs npm ffmpeg yt-dlp gallery-dl
```

Dành cho Windows:
```bash
winget install GoLang.Go OpenJS.NodeJS
winget install Gyan.FFmpeg yt-dlp.yt-dlp mikf.gallery-dl
```

**Lưu ý:** Khi chạy app (dev hoặc production), app sẽ **tự động kiểm tra** và **yêu cầu cài** các dependencies nếu thiếu.

2. **Cài đặt Wails CLI**

Đây là công cụ để build ứng dụng này từ mã nguồn Go:
```bash
go install github.com/wailsapp/wails/v2/cmd/wails@latest
```

3. **Tải mã nguồn về máy:**
```bash
git clone https://github.com/JustinNguyen9979/YTDown.git
cd YTDown
```

4. **Cài đặt thư viện:**
```bash
go mod tidy
```

5. **Chạy ứng dụng ở chế độ phát triển:**
```bash
wails dev
```

6. **Build bản chính thức:**

Build cho hệ điều hành hiện tại của bạn
```bash
wails build -ldflags "-s -w"
```
Hoặc nếu bạn đang dùng macOS và muốn build bản hỗ trợ cả chip Intel & Apple Silicon (M1/M2/M3):
```bash
wails build -platform darwin/universal -ldflags "-s -w"
```
*Ứng dụng hoàn thiện sẽ nằm trong thư mục `build/bin/`.*


## 🌟 Tính năng chính

- Hỗ trợ tải video chất lượng cao từ nhiều nguồn: **YouTube, Facebook/Instagram Reels, TikTok, Xiaohongshu/Rednote**,...
- Hỗ trợ download bằng **cookie** cho các video ytb bị giới hạn độ tuổi.
- Tự động nhận diện và xử lý liên kết thông minh.
- Hỗ trợ tải từng video đơn lẻ hoặc toàn bộ danh sách phát (Playlist).
- Tùy chọn định dạng xuất tệp: `MP4` (Video) hoặc `MP3` (Âm thanh).
- Chọn chất lượng video mong muốn (1080p, 720p, 4k...).
- Hiển thị Thumbnail video, xem video trực tiếp từ Thumbnails.
- Download ảnh cuộn từ X, Instagram, Tikok...

---

## 📂 Cấu trúc dự án

```text
YTDown/
├── app.go          # Logic xử lý giao diện và cập nhật
├── downloader.go   # Core xử lý tải video với yt-dlp
├── compressor.go   # Xử lý nén video/hình ảnh
├── main.go         # Điểm khởi đầu của ứng dụng
├── frontend/       # Giao diện người dùng (JS/HTML/CSS)
├── build.sh        # Script đóng gói macOS (.dmg)
├── build-windows.sh # Script đóng gói Windows (.exe)
├── build-linux.sh   # Script đóng gói Linux (.AppImage)
   └── README.md       # Tài liệu hướng dẫn
├── dependency_checker.go  # Tự động kiểm tra & cài dependencies
├── app_update.go          # Tự động kiểm tra cập nhật phiên bản
```

## 📄 License

Dự án được phát hành dưới bản quyền **MIT**.

## ☕ Ủng hộ tác giả

Nếu YTDown giúp ích cho công việc của bạn, hãy mời mình một ly cà phê nhé:

- **Ngân hàng:** MB Bank
- **Số tài khoản:** `079 88888 88888`
- **Chủ tài khoản:** `Nguyen Duc Huy`

### 🌍 International supporters
[![Ko-fi](https://img.shields.io/badge/Ko--fi-FF5E5B?style=for-the-badge&logo=ko-fi&logoColor=white)](https://ko-fi.com/justinnguyenvn)
> Support via PayPal — available worldwide 🌏

Cảm ơn bạn đã sử dụng YTDown! 🚀

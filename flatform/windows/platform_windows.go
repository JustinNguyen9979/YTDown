package windows

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

const (
	ytdlpExeURL     = "https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp.exe"
	galleryDLExeURL = "https://github.com/mikf/gallery-dl/releases/latest/download/gallery-dl.exe"
	// ffmpeg: BtbN static build (winget package Gyan.FFmpeg)
	ffmpegZipURL = "https://github.com/BtbN/ffmpeg-builds/releases/latest/download/ffmpeg-master-latest-win64-gpl.zip"
)

type Manager struct {
	binDir string // %APPDATA%\YTDown\bin — stores downloaded .exe
}

func New() *Manager {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		home, _ := os.UserHomeDir()
		appData = filepath.Join(home, "AppData", "Roaming")
	}
	m := &Manager{
		binDir: filepath.Join(appData, "YTDown", "bin"),
	}
	os.MkdirAll(m.binDir, 0755)
	return m
}

// InjectBinDir prepends our managed bin dir into the process PATH.
// After this, exec.LookPath("yt-dlp") works in downloader.go, gallery.go, etc.
// No changes needed in those files at all.
func (m *Manager) InjectBinDir() {
	current := os.Getenv("PATH")
	if !strings.Contains(current, m.binDir) {
		os.Setenv("PATH", m.binDir+";"+current)
	}
}

func (m *Manager) GetBinaryPath(tool string) string {
	toolExe := tool
	if !strings.HasSuffix(toolExe, ".exe") {
		toolExe += ".exe"
	}
	// Check process PATH (includes binDir after InjectBinDir)
	if p, err := exec.LookPath(toolExe); err == nil {
		return p
	}
	// Explicit fallback paths
	for _, d := range []string{
		m.binDir,
		filepath.Join(os.Getenv("USERPROFILE"), "scoop", "shims"),
		`C:\ProgramData\chocolatey\bin`,
	} {
		p := filepath.Join(d, toolExe)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

var requiredTools = []string{"ffmpeg", "yt-dlp", "gallery-dl"}

func (m *Manager) CheckDependencies() ([]string, error) {
	var missing []string
	for _, t := range requiredTools {
		if m.GetBinaryPath(t) == "" {
			missing = append(missing, t)
		}
	}
	return missing, nil
}

func (m *Manager) InstallDependency(name string) error {
	switch name {
	case "yt-dlp":
		return m.downloadExe(ytdlpExeURL, "yt-dlp.exe")
	case "gallery-dl":
		return m.downloadExe(galleryDLExeURL, "gallery-dl.exe")
	case "ffmpeg":
		return m.downloadFFmpegZip()
	}
	return fmt.Errorf("no install method for %s on Windows", name)
}

func (m *Manager) downloadExe(url, filename string) error {
	dest := filepath.Join(m.binDir, filename)
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("download %s: %w", filename, err)
	}
	defer resp.Body.Close()
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, resp.Body)
	return err
}

func (m *Manager) downloadFFmpegZip() error {
	zipPath := filepath.Join(m.binDir, "_ffmpeg.zip")
	if err := m.downloadExe(ffmpegZipURL, "_ffmpeg.zip"); err != nil {
		return err
	}
	defer os.Remove(zipPath)
	// Extract using built-in PowerShell (always available on Win 10+)
	extractDir := filepath.Join(m.binDir, "_ffmpeg_extracted")
	ps := exec.Command("powershell", "-NoProfile", "-Command",
		fmt.Sprintf(`Expand-Archive -Path '%s' -DestinationPath '%s' -Force`, zipPath, extractDir))
	if err := ps.Run(); err != nil {
		return fmt.Errorf("extract ffmpeg zip: %w", err)
	}
	defer os.RemoveAll(extractDir)
	// Find and copy ffmpeg.exe + ffprobe.exe from nested subdirectory
	return filepath.Walk(extractDir, func(path string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() {
			return err
		}
		base := strings.ToLower(filepath.Base(path))
		if base == "ffmpeg.exe" || base == "ffprobe.exe" {
			dest := filepath.Join(m.binDir, filepath.Base(path))
			in, _ := os.Open(path)
			out, _ := os.Create(dest)
			io.Copy(out, in)
			in.Close()
			out.Close()
		}
		return nil
	})
}

func (m *Manager) InstallInstructions(tools []string) string {
	return fmt.Sprintf(
		"YTDown cần các công cụ sau:\n\n%s\n\nTự động tải và cài đặt vào:\n%s\n\nĐồng ý?",
		strings.Join(tools, "\n"), m.binDir,
	)
}

func (m *Manager) PackageManagerName() string { return "direct download" }

func (m *Manager) PackageManagerAvailable() bool { return true }

func (m *Manager) UpgradeTool(name, binaryPath string) error {
	if binaryPath == "" {
		return fmt.Errorf("%s not found, cannot upgrade", name)
	}

	var cmd *exec.Cmd
	switch name {
	case "yt-dlp":
		// -U = self-update, tự replace file exe hiện tại
		cmd = exec.Command(binaryPath, "-U")
	case "gallery-dl":
		// --update = self-update, hoạt động giống hệt macOS/Linux
		cmd = exec.Command(binaryPath, "--update")
	case "ffmpeg":
		// ffmpeg không có self-update → re-download zip về binDir
		return m.downloadFFmpegZip()
	default:
		return fmt.Errorf("no upgrade method for %s", name)
	}

	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("upgrade %s: %w\n%s", name, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (m *Manager) LaunchSetup() error {
	script := fmt.Sprintf(`
$ErrorActionPreference = 'Stop'
$binDir = '%s'
New-Item -ItemType Directory -Force -Path $binDir | Out-Null

function Download-File($url, $dest) {
    Write-Host "Downloading $(Split-Path $dest -Leaf)..."
    $tmp = $dest + '.tmp'
    try {
        Invoke-WebRequest -Uri $url -OutFile $tmp -UseBasicParsing
        Move-Item -Force $tmp $dest
        Write-Host "  OK: $dest"
    } catch {
        if (Test-Path $tmp) { Remove-Item $tmp -Force }
        throw "Failed to download $url : $_"
    }
}

# ── yt-dlp ──────────────────────────────────────────────
$ytdlp = Join-Path $binDir 'yt-dlp.exe'
if (Test-Path $ytdlp) {
    Write-Host "yt-dlp found, running self-update..."
    & $ytdlp -U
} else {
    Download-File '%s' $ytdlp
}

# ── gallery-dl ───────────────────────────────────────────
$gallerydl = Join-Path $binDir 'gallery-dl.exe'
if (Test-Path $gallerydl) {
    Write-Host "gallery-dl found, running self-update..."
    & $gallerydl --update
} else {
    Download-File '%s' $gallerydl
}

# ── ffmpeg ───────────────────────────────────────────────
$ffmpeg = Join-Path $binDir 'ffmpeg.exe'
if (-not (Test-Path $ffmpeg)) {
    Write-Host "Downloading ffmpeg (this may take a moment)..."
    $zipPath = Join-Path $binDir '_ffmpeg.zip'
    $extractDir = Join-Path $binDir '_ffmpeg_extracted'

    Download-File '%s' $zipPath

    Write-Host "Extracting ffmpeg..."
    Expand-Archive -Path $zipPath -DestinationPath $extractDir -Force

    # Tìm và copy ffmpeg.exe + ffprobe.exe từ subdir bất kỳ
    Get-ChildItem -Path $extractDir -Recurse -Include 'ffmpeg.exe','ffprobe.exe' | ForEach-Object {
        $dest = Join-Path $binDir $_.Name
        Copy-Item $_.FullName $dest -Force
        Write-Host "  Extracted: $dest"
    }

    Remove-Item $zipPath -Force -ErrorAction SilentlyContinue
    Remove-Item $extractDir -Recurse -Force -ErrorAction SilentlyContinue
} else {
    Write-Host "ffmpeg already installed (no self-update available, skip)."
}

Write-Host ""
Write-Host "Setup complete! All tools are ready in: $binDir"
Write-Host ""
Write-Host "Press any key to close this window..."
$null = $Host.UI.RawUI.ReadKey("NoEcho,IncludeKeyDown")
`, m.binDir, ytdlpExeURL, galleryDLExeURL, ffmpegZipURL)

	scriptPath := filepath.Join(os.TempDir(), "ytdown_setup.ps1")
	if err := os.WriteFile(scriptPath, []byte(script), 0644); err != nil {
		return fmt.Errorf("cannot write setup script: %w", err)
	}

	cmd := exec.Command(
		"powershell",
		"-NoProfile",
		"-WindowStyle", "Normal",
		"-ExecutionPolicy", "Bypass",
		"-File", scriptPath,
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: false}

	if err := cmd.Start(); err != nil { // ← Start() không block UI
		return fmt.Errorf("cannot launch setup: %w", err)
	}

	return nil
}

func (m *Manager) GetDownloadDir() string {
	if d := os.Getenv("USERPROFILE"); d != "" {
		return filepath.Join(d, "Downloads")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Downloads")
}

func (m *Manager) GetConfigDir() string {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		home, _ := os.UserHomeDir()
		appData = filepath.Join(home, "AppData", "Roaming")
	}
	return filepath.Join(appData, "ytdown")
}

func (m *Manager) AppDataDir() string {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		home, _ := os.UserHomeDir()
		appData = filepath.Join(home, "AppData", "Roaming")
	}
	return filepath.Join(appData, "YTDown")
}

func (m *Manager) OpenFolder(path string) error {
	return exec.Command("explorer", path).Start()
}

func (m *Manager) OpenFile(path string) error {
	return exec.Command("cmd", "/c", "start", "", path).Start()
}

func (m *Manager) OSName() string            { return "Windows" }
func (m *Manager) UpdateAssetSuffix() string { return "-windows-setup.zip" }

func (p *Manager) InstallAppUpdate(downloadURL string, parentPID int) error {
	tmpDir, err := os.MkdirTemp("", "ytdown-update-*")
	if err != nil {
		return err
	}

	zipName := filepath.Base(downloadURL) // VD: YTDown-2026.5.6.2-Windows-Setup.zip
	// Tên .exe = thay .zip thành .exe
	exeName := strings.TrimSuffix(zipName, ".zip") + ".exe"

	zipPath := filepath.Join(tmpDir, zipName)
	exePath := filepath.Join(tmpDir, exeName)

	// Viết PowerShell script chạy ẩn (hidden window)
	psScript := fmt.Sprintf(`
$parentPid = %d
$zipPath   = '%s'
$exePath   = '%s'
$tmpDir    = '%s'

# Chờ app chính thoát
while (Get-Process -Id $parentPid -ErrorAction SilentlyContinue) { Start-Sleep -Milliseconds 500 }

# Download .zip
Invoke-WebRequest -Uri '%s' -OutFile $zipPath -UseBasicParsing

# Giải nén
Expand-Archive -Path $zipPath -DestinationPath $tmpDir -Force

# Chạy installer silent (NSIS silent flag /S)
Start-Process -FilePath $exePath -ArgumentList '/S' -Wait

# Dọn dẹp
Remove-Item -Path $tmpDir -Recurse -Force
`, parentPID, zipPath, exePath, tmpDir, downloadURL)

	scriptPath := filepath.Join(tmpDir, "update.ps1")
	if err := os.WriteFile(scriptPath, []byte(psScript), 0o755); err != nil {
		return err
	}

	// Chạy PowerShell hoàn toàn ẩn - không hiện cửa sổ
	cmd := exec.Command("powershell.exe",
		"-WindowStyle", "Hidden",
		"-NonInteractive",
		"-ExecutionPolicy", "Bypass",
		"-File", scriptPath,
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Start() // Start() không Start() - không chờ, app tự quit sau đó
}

func (m *Manager) GetLatestVersion(name string) string {
	binaryPath := m.GetBinaryPath(name)
	if binaryPath == "" {
		return ""
	}

	switch name {
	case "yt-dlp":
		// yt-dlp --update --dry-run không thực sự update
		// Output khi đã latest: "yt-dlp is up to date (2025.04.30)"
		// Output khi có bản mới: "Updating yt-dlp to 2025.05.01 ..."
		out, err := exec.Command(binaryPath, "--update", "--dry-run").CombinedOutput()
		if err != nil && len(out) == 0 {
			return ""
		}
		return parseYtdlpUpdateVersion(string(out))

	case "gallery-dl":
		// gallery-dl --update --check không thực sự update
		// Output: "gallery-dl 1.28.1 is up-to-date" hoặc "Updating to version 1.29.0"
		out, err := exec.Command(binaryPath, "--update", "--check").CombinedOutput()
		if err != nil && len(out) == 0 {
			return ""
		}
		return parseGallerydlUpdateVersion(string(out))
	}
	return ""
}

func parseYtdlpUpdateVersion(output string) string {
	output = strings.TrimSpace(output)
	// Trường hợp: "yt-dlp is up to date (2025.04.30)"
	if idx := strings.Index(output, "("); idx != -1 {
		if end := strings.Index(output[idx:], ")"); end != -1 {
			ver := strings.TrimSpace(output[idx+1 : idx+end])
			if isDateVersion(ver) {
				return ver
			}
		}
	}
	// Trường hợp: "Updating yt-dlp to 2025.05.01"
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		for i, f := range fields {
			if (f == "to" || f == "version") && i+1 < len(fields) {
				ver := strings.TrimRight(fields[i+1], ".,")
				if isDateVersion(ver) {
					return ver
				}
			}
		}
	}
	return ""
}

func parseGallerydlUpdateVersion(output string) string {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		for i, f := range fields {
			if f == "version" && i+1 < len(fields) {
				ver := strings.TrimRight(fields[i+1], ".,")
				if isSemver(ver) {
					return ver
				}
			}
			// "gallery-dl 1.28.1 is up-to-date"
			if f == "gallery-dl" && i+1 < len(fields) {
				ver := strings.TrimRight(fields[i+1], ".,")
				if isSemver(ver) {
					return ver
				}
			}
		}
	}
	return ""
}

func isDateVersion(s string) bool {
	parts := strings.Split(s, ".")
	return len(parts) == 3 && len(parts[0]) == 4
}

func isSemver(s string) bool {
	parts := strings.Split(s, ".")
	return len(parts) >= 2
}

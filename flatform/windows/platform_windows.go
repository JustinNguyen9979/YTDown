package windows

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	ytdlpExeURL     = "https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp.exe"
	galleryDLExeURL = "https://github.com/mikf/gallery-dl/releases/latest/download/gallery-dl.exe"
	// ffmpeg: BtbN static build (winget package Gyan.FFmpeg)
	ffmpegZipURL = "https://github.com/BtbN/ffmpeg-builds/releases/latest/download/ffmpeg-master-latest-win64-gpl.zip"
)

type Manager struct {
	binDir     string // %APPDATA%\YTDown\bin — stores downloaded .exe
	wingetPath string
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
	if p, err := exec.LookPath("winget"); err == nil {
		m.wingetPath = p
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

var wingetIDs = map[string]string{
	"yt-dlp":     "yt-dlp.yt-dlp",
	"ffmpeg":     "Gyan.FFmpeg",
	"gallery-dl": "mikf.gallery-dl",
}

func (m *Manager) InstallDependency(name string) error {
	// Strategy 1: winget (adds to system PATH automatically)
	if m.wingetPath != "" {
		if id, ok := wingetIDs[name]; ok {
			cmd := exec.Command(m.wingetPath, "install", "--id", id,
				"--accept-package-agreements", "--accept-source-agreements", "--silent")
			cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
			if err := cmd.Run(); err == nil {
				return nil
			}
		}
	}
	// Strategy 2: Direct .exe download to binDir
	// (InjectBinDir ensures these are findable via LookPath)
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
	method := "tải tự động"
	if m.wingetPath != "" {
		method = "winget"
	}
	return fmt.Sprintf(
		"YTDown cần các công cụ sau:\n\n%s\n\nCài đặt tự động qua %s?",
		strings.Join(tools, "\n"), method,
	)
}

func (m *Manager) PackageManagerName() string {
	if m.wingetPath != "" {
		return "winget"
	}
	return "direct download"
}

func (m *Manager) PackageManagerAvailable() bool { return true }

func (m *Manager) UpgradeTool(name, binaryPath string) error {
	// Self-update
	switch name {
	case "yt-dlp":
		if err := exec.Command(binaryPath, "-U").Run(); err == nil {
			return nil
		}
	case "gallery-dl":
		if err := exec.Command(binaryPath, "--update").Run(); err == nil {
			return nil
		}
	}
	// winget upgrade
	if m.wingetPath != "" {
		if id, ok := wingetIDs[name]; ok {
			if err := exec.Command(m.wingetPath, "upgrade", "--id", id, "--silent").Run(); err == nil {
				return nil
			}
		}
	}
	// Re-download .exe
	return m.InstallDependency(name)
}

func (m *Manager) LaunchSetup() error {
	script := fmt.Sprintf(`# YTDown Windows Setup
$binDir = '%s'
New-Item -ItemType Directory -Force -Path $binDir | Out-Null

function Download-Exe($url, $dest) {
    Write-Host "Downloading $dest..."
    Invoke-WebRequest -Uri $url -OutFile (Join-Path $binDir $dest) -UseBasicParsing
}

# Try winget first
if (Get-Command winget -ErrorAction SilentlyContinue) {
    winget install --id yt-dlp.yt-dlp --accept-package-agreements --accept-source-agreements --silent
    winget install --id Gyan.FFmpeg --accept-package-agreements --accept-source-agreements --silent
    winget install --id mikf.gallery-dl --accept-package-agreements --accept-source-agreements --silent
} else {
    Download-Exe '%s' 'yt-dlp.exe'
    Download-Exe '%s' 'gallery-dl.exe'
}
Write-Host "Setup complete! Closing in 3s..."
Start-Sleep 3`, m.binDir, ytdlpExeURL, galleryDLExeURL)

	scriptPath := filepath.Join(os.TempDir(), "ytdown_setup.ps1")
	os.WriteFile(scriptPath, []byte(script), 0644)
	return exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass",
		"-File", scriptPath).Start()
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
func (m *Manager) UpdateAssetSuffix() string { return "-Windows-Setup.exe" }

func (m *Manager) InstallAppUpdate(downloadURL string, parentPID int) error {
	tmpDir, _ := os.MkdirTemp("", "ytdown-update-*")
	installerPath := filepath.Join(tmpDir, "YTDown-Setup.exe")
	script := fmt.Sprintf(`$pid = %d
$url = '%s'
$out = '%s'
while (Get-Process -Id $pid -ErrorAction SilentlyContinue) { Start-Sleep -Milliseconds 500 }
Invoke-WebRequest -Uri $url -OutFile $out -UseBasicParsing
Start-Process -FilePath $out -ArgumentList '/S' -Wait`,
		parentPID, downloadURL, installerPath)

	scriptPath := filepath.Join(tmpDir, "update.ps1")
	os.WriteFile(scriptPath, []byte(script), 0644)
	return exec.Command("powershell", "-NoProfile", "-WindowStyle", "Hidden",
		"-ExecutionPolicy", "Bypass", "-File", scriptPath).Start()
}

func (m *Manager) GetLatestVersion(name string) string {
	repos := map[string]string{
		"yt-dlp":     "yt-dlp/yt-dlp",
		"gallery-dl": "mikf/gallery-dl",
	}
	repo, ok := repos[name]
	if !ok {
		return ""
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("https://api.github.com/repos/" + repo + "/releases/latest")
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	var data struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return ""
	}
	return strings.TrimPrefix(data.TagName, "v")
}

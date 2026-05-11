package windows

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

const (
	// Winget IDs for required tools
	ytdlpID     = "yt-dlp.yt-dlp"
	ffmpegID    = "Gyan.FFmpeg"
	gallerydlID = "mikf.gallery-dl"
	// App Winget ID
	appWingetID = "JustinNguyen.YTDown"
)

type Manager struct {
	binDir string // legacy path, still kept for InjectBinDir compatibility if needed
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
func (m *Manager) InjectBinDir() {
	// Winget installs tools to system PATH, so we just ensure m.binDir is also there for legacy reasons.
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
	// Check process PATH (includes winget paths and binDir)
	if p, err := exec.LookPath(toolExe); err == nil {
		return p
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
	// We force the use of LaunchSetup to ensure a terminal is shown for installation
	return m.LaunchSetup()
}

func (m *Manager) InstallInstructions(tools []string) string {
	return fmt.Sprintf(
		"YTDown cần các công cụ sau:\n\n%s\n\nỨng dụng sẽ mở Terminal để cài đặt chúng thông qua Winget (Windows Package Manager). Đồng ý?",
		strings.Join(tools, "\n"),
	)
}

func (m *Manager) PackageManagerName() string { return "winget" }

func (m *Manager) PackageManagerAvailable() bool {
	_, err := exec.LookPath("winget")
	return err == nil
}

func (m *Manager) UpgradeTool(name, binaryPath string) error {
	if !m.PackageManagerAvailable() {
		return fmt.Errorf("winget not found, cannot upgrade")
	}

	var wingetID string
	switch name {
	case "yt-dlp":
		wingetID = ytdlpID
	case "gallery-dl":
		wingetID = gallerydlID
	case "ffmpeg":
		wingetID = ffmpegID
	default:
		return fmt.Errorf("no winget ID for %s", name)
	}

	// Run winget upgrade in a hidden window if it's a silent upgrade,
	// but since we want transparency, we could also use LaunchSetup style.
	// For now, let's use a hidden command for background upgrades if requested.
	cmd := exec.Command("winget", "upgrade", "--id", wingetID, "--silent", "--accept-source-agreements", "--accept-package-agreements")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	return cmd.Run()
}

func (m *Manager) LaunchSetup() error {
	// Script to install missing dependencies via winget
	script := fmt.Sprintf(`
$ErrorActionPreference = 'Continue'
Write-Host "--- YTDown Dependency Setup ---" -ForegroundColor Cyan
Write-Host "Checking for winget..."

if (!(Get-Command winget -ErrorAction SilentlyContinue)) {
    Write-Host "Error: winget is not installed on this system." -ForegroundColor Red
    Write-Host "Please install 'App Installer' from Microsoft Store."
    Pause
    exit
}

$tools = @{
    "yt-dlp" = "%s"
    "ffmpeg" = "%s"
    "gallery-dl" = "%s"
}

foreach ($name in $tools.Keys) {
    $id = $tools[$name]
    Write-Host "Checking $name..."
    if (!(Get-Command "$name.exe" -ErrorAction SilentlyContinue)) {
        Write-Host "Installing $name ($id) via winget..." -ForegroundColor Yellow
        winget install --id $id --source winget --accept-source-agreements --accept-package-agreements
        if ($LASTEXITCODE -eq 0) {
            Write-Host "Successfully installed $name" -ForegroundColor Green
        } else {
            Write-Host "Failed to install $name. You might need to run this as Administrator or install it manually." -ForegroundColor Red
        }
    } else {
        Write-Host "$name is already installed." -ForegroundColor Green
    }
}

Write-Host ""
Write-Host "Setup complete! Please restart YTDown if tools are still not detected." -ForegroundColor Cyan
Write-Host "Press any key to close this window..."
$null = $Host.UI.RawUI.ReadKey("NoEcho,IncludeKeyDown")
`, ytdlpID, ffmpegID, gallerydlID)

	scriptPath := filepath.Join(os.TempDir(), "ytdown_setup.ps1")
	if err := os.WriteFile(scriptPath, []byte(script), 0644); err != nil {
		return fmt.Errorf("cannot write setup script: %w", err)
	}

	// Launch PowerShell in a VISIBLE window
	cmd := exec.Command(
		"powershell",
		"-NoProfile",
		"-ExecutionPolicy", "Bypass",
		"-File", scriptPath,
	)
	// Ensure the window is visible
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("cannot launch setup terminal: %w", err)
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
	absPath, err := filepath.Abs(path)
	if err == nil {
		path = absPath
	}
	os.MkdirAll(path, 0755)
	cmd := exec.Command("explorer", path)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Start()
}

func (m *Manager) OpenFile(path string) error {
	absPath, err := filepath.Abs(path)
	if err == nil {
		path = absPath
	}
	cmd := exec.Command("cmd", "/c", "start", "", path)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Start()
}

func (m *Manager) OSName() string            { return "Windows" }
func (m *Manager) UpdateAssetSuffix() string { return "-windows-setup.zip" }

func (p *Manager) InstallAppUpdate(downloadURL string, parentPID int) error {
	// Script to run winget upgrade in a visible terminal
	psScript := fmt.Sprintf(`
$parentPid = %d
$appId     = '%s'

Write-Host "--- YTDown App Update ---" -ForegroundColor Cyan
Write-Host "Waiting for YTDown to close..."

# Chờ app chính thoát
while (Get-Process -Id $parentPid -ErrorAction SilentlyContinue) { 
    Start-Sleep -Milliseconds 500 
}

Write-Host "Running winget upgrade for $appId..." -ForegroundColor Yellow
winget upgrade --id $appId --source winget --accept-source-agreements --accept-package-agreements

if ($LASTEXITCODE -eq 0) {
    Write-Host ""
    Write-Host "Update successful! You can now restart YTDown." -ForegroundColor Green
} else {
    Write-Host ""
    Write-Host "Update failed or was cancelled. (Exit code: $LASTEXITCODE)" -ForegroundColor Red
}

Write-Host ""
Write-Host "Press any key to close this window..."
$null = $Host.UI.RawUI.ReadKey("NoEcho,IncludeKeyDown")
`, parentPID, appWingetID)

	tmpDir := os.TempDir()
	scriptPath := filepath.Join(tmpDir, "ytdown_app_update.ps1")
	if err := os.WriteFile(scriptPath, []byte(psScript), 0o644); err != nil {
		return err
	}

	// Chạy PowerShell hoàn toàn hiện - cho user thấy quá trình update
	cmd := exec.Command("powershell.exe",
		"-NoProfile",
		"-ExecutionPolicy", "Bypass",
		"-File", scriptPath,
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: false}
	return cmd.Start()
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
		cmd := exec.Command(binaryPath, "--update", "--dry-run")
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		out, err := cmd.CombinedOutput()
		if err != nil && len(out) == 0 {
			return ""
		}
		return parseYtdlpUpdateVersion(string(out))

	case "gallery-dl":
		// gallery-dl --update --check không thực sự update
		// Output: "gallery-dl 1.28.1 is up-to-date" hoặc "Updating to version 1.29.0"
		cmd := exec.Command(binaryPath, "--update", "--check")
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		out, err := cmd.CombinedOutput()
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

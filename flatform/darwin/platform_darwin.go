package darwin

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
)

type Manager struct{}

func New() *Manager { return &Manager{} }

// ---------- PATH injection ----------

func (m *Manager) InjectBinDir() {
	// Ensure Homebrew bin is always in PATH when app is opened from Finder.
	// We want /opt/homebrew/bin to be FIRST for Apple Silicon users.
	// Prepending in this order: /usr/local/bin then /opt/homebrew/bin
	// results in /opt/homebrew/bin being at the very front.
	for _, d := range []string{"/usr/local/bin", "/opt/homebrew/bin"} {
		current := os.Getenv("PATH")
		if !strings.Contains(current, d) {
			os.Setenv("PATH", d+":"+current)
		}
	}
}

func (m *Manager) brewPath() string {
	if p, err := exec.LookPath("brew"); err == nil {
		return p
	}
	for _, p := range []string{"/opt/homebrew/bin/brew", "/usr/local/bin/brew"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// ---------- Tool paths ----------

func (m *Manager) GetBinaryPath(tool string) string {
	// 1. Try explicit system paths in preferred order
	for _, dir := range []string{"/opt/homebrew/bin", "/usr/local/bin", "/usr/bin"} {
		p := filepath.Join(dir, tool)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}

	// 2. Fallback to system PATH
	if p, err := exec.LookPath(tool); err == nil {
		return p
	}
	return ""
}

// ---------- Dependency management ----------

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
	bp := m.brewPath()
	if bp == "" {
		return fmt.Errorf("Homebrew not found — install from https://brew.sh")
	}
	cmd := exec.Command(bp, "install", name)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (m *Manager) InstallInstructions(tools []string) string {
	return fmt.Sprintf(
		"YTDown cần các công cụ sau:\n\n%s\n\nCài đặt qua Homebrew?",
		strings.Join(tools, "\n"),
	)
}

func (m *Manager) PackageManagerName() string { return "Homebrew" }

func (m *Manager) PackageManagerAvailable() bool { return m.brewPath() != "" }

func (m *Manager) GetLatestVersion(name string) string {
	bp := m.brewPath()
	if bp == "" {
		return ""
	}
	cmd := exec.Command(bp, "info", "--json=v1", name)
	cmd.Env = append(os.Environ(),
		"PATH=/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin",
	)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	var result []struct {
		Versions struct {
			Stable string `json:"stable"`
		} `json:"versions"`
	}
	if err := json.Unmarshal(out, &result); err != nil || len(result) == 0 {
		return ""
	}
	return result[0].Versions.Stable
}

func (m *Manager) UpgradeTool(name, binaryPath string) error {
	brewPath := "/opt/homebrew/bin/brew"
	if _, err := os.Stat(brewPath); os.IsNotExist(err) {
		brewPath = "/usr/local/bin/brew" // Intel Mac fallback
	}

	// ✅ FIX 2: Inject full PATH để brew chạy được khi mở từ Finder
	cmd := exec.Command(brewPath, "upgrade", name)
	cmd.Env = append(os.Environ(),
		"PATH=/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin",
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("brew upgrade %s failed: %s", name, string(output))
	}
	return nil
}

func (m *Manager) LaunchSetup() error {
	usr, _ := user.Current()
	scriptPath := filepath.Join(usr.HomeDir, ".config", "ytdown", "setup_env.sh")
	os.MkdirAll(filepath.Dir(scriptPath), 0755)

	script := `#!/bin/bash
set -e
for brew in /opt/homebrew/bin/brew /usr/local/bin/brew; do
  [ -f "$brew" ] && eval "$($brew shellenv)" && BREW_PATH="$brew" && break
done
if [ -z "${BREW_PATH:-}" ]; then
  /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
  for brew in /opt/homebrew/bin/brew /usr/local/bin/brew; do
    [ -f "$brew" ] && eval "$($brew shellenv)" && BREW_PATH="$brew" && break
  done
fi
$BREW_PATH install yt-dlp ffmpeg gallery-dl 2>/dev/null || $BREW_PATH upgrade yt-dlp ffmpeg gallery-dl
echo "Done! Closing in 3s..."; sleep 3; exit`

	os.WriteFile(scriptPath, []byte(script), 0755)
	appleScript := fmt.Sprintf(`tell application "Terminal" to do script "/bin/bash %s; exit"`, scriptPath)
	return exec.Command("osascript", "-e", appleScript).Run()
}

// ---------- Directories ----------

func (m *Manager) GetDownloadDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Downloads")
}

func (m *Manager) GetConfigDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "ytdown")
}

func (m *Manager) AppDataDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "Application Support", "YTDown")
}

// ---------- File system ----------

func (m *Manager) OpenFolder(path string) error {
	os.MkdirAll(path, 0755)
	return exec.Command("open", path).Start()
}

func (m *Manager) OpenFile(path string) error {
	return exec.Command("open", path).Start()
}

// ---------- OS / Update ----------

func (m *Manager) OSName() string { return "macOS" }

func (m *Manager) UpdateAssetSuffix() string { return ".dmg" }

func (m *Manager) InstallAppUpdate(downloadURL string, parentPID int) error {
	tmpDir, _ := os.MkdirTemp("", "ytdown-update-*")
	home, _ := os.UserHomeDir()
	targetApp := filepath.Join(home, "Applications", "YTDown.app")
	if _, err := os.Stat("/Applications/YTDown.app"); err == nil {
		targetApp = "/Applications/YTDown.app"
	}
	script := fmt.Sprintf(`#!/bin/sh
set -eu
PARENT_PID=%d; DMG=%q/update.dmg; MNT=%q/mount
while kill -0 "$PARENT_PID" 2>/dev/null; do sleep 1; done
mkdir -p "$MNT"
curl -L --fail -o "$DMG" %q
hdiutil attach "$DMG" -nobrowse -quiet -mountpoint "$MNT"
APP=$(find "$MNT" -maxdepth 1 -name '*.app' -print -quit)
rm -rf %q; ditto "$APP" %q
hdiutil detach "$MNT" -quiet || true
open %q`, parentPID, tmpDir, tmpDir, downloadURL, targetApp, targetApp, targetApp)

	scriptPath := filepath.Join(tmpDir, "update.sh")
	os.WriteFile(scriptPath, []byte(script), 0755)
	return exec.Command("sh", scriptPath).Start()
}

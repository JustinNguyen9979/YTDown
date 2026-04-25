package darwin

import (
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
	// Ensure Homebrew bin is always in PATH when app is opened from Finder
	for _, d := range []string{"/opt/homebrew/bin", "/usr/local/bin"} {
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
	if p, err := exec.LookPath(tool); err == nil {
		return p
	}
	for _, dir := range []string{"/opt/homebrew/bin", "/usr/local/bin", "/usr/bin"} {
		p := filepath.Join(dir, tool)
		if _, err := os.Stat(p); err == nil {
			return p
		}
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

func (m *Manager) UpgradeTool(name, binaryPath string) error {
	// Try self-update first
	var selfCmd *exec.Cmd
	switch name {
	case "yt-dlp":
		selfCmd = exec.Command(binaryPath, "-U")
	case "gallery-dl":
		selfCmd = exec.Command(binaryPath, "--update")
	}
	if selfCmd != nil {
		if out, err := selfCmd.CombinedOutput(); err == nil {
			_ = out
			return nil
		}
	}
	// Fallback: brew upgrade
	bp := m.brewPath()
	if bp == "" {
		return fmt.Errorf("Homebrew not found")
	}
	return exec.Command(bp, "upgrade", name).Run()
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

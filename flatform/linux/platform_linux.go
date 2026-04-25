package linux

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Manager struct {
	pm    string // "apt", "dnf", "pacman", "zypper"
	pmBin string // absolute path to package manager binary
}

func New() *Manager {
	m := &Manager{}
	for _, pm := range []struct{ name, bin string }{
		{"apt", "apt-get"}, {"dnf", "dnf"}, {"pacman", "pacman"}, {"zypper", "zypper"},
	} {
		if p, err := exec.LookPath(pm.bin); err == nil {
			m.pm, m.pmBin = pm.name, p
			break
		}
	}
	return m
}

func (m *Manager) InjectBinDir() {} // standard Linux paths are already in PATH

func (m *Manager) GetBinaryPath(tool string) string {
	if p, err := exec.LookPath(tool); err == nil {
		return p
	}
	home, _ := os.UserHomeDir()
	for _, dir := range []string{"/usr/bin", "/usr/local/bin", filepath.Join(home, ".local/bin")} {
		p := filepath.Join(dir, tool)
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
	// gallery-dl: almost never in distro repos → always use pip3
	if name == "gallery-dl" {
		return m.pipInstall(name)
	}
	if m.pmBin != "" {
		var cmd *exec.Cmd
		switch m.pm {
		case "apt":
			cmd = exec.Command("sudo", m.pmBin, "install", "-y", name)
		case "dnf":
			cmd = exec.Command("sudo", m.pmBin, "install", "-y", name)
		case "pacman":
			cmd = exec.Command("sudo", m.pmBin, "-S", "--noconfirm", name)
		case "zypper":
			cmd = exec.Command("sudo", m.pmBin, "install", "-y", name)
		}
		if cmd != nil {
			cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
			if err := cmd.Run(); err == nil {
				return nil
			}
		}
	}
	// Fallback: pip3 for yt-dlp
	if name == "yt-dlp" {
		return m.pipInstall(name)
	}
	return fmt.Errorf("could not install %s: no supported package manager found", name)
}

func (m *Manager) pipInstall(pkg string) error {
	pip := "pip3"
	if _, err := exec.LookPath(pip); err != nil {
		pip = "pip"
		if _, err := exec.LookPath(pip); err != nil {
			return fmt.Errorf("pip3/pip not found — please install %s manually", pkg)
		}
	}
	cmd := exec.Command(pip, "install", "--user", "--upgrade", pkg)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}

func (m *Manager) InstallInstructions(tools []string) string {
	pm := m.pm
	if pm == "" {
		pm = "your package manager"
	}
	return fmt.Sprintf(
		"YTDown cần các công cụ sau:\n\n%s\n\nCài đặt qua %s?\n(gallery-dl sẽ dùng pip3)",
		strings.Join(tools, "\n"), pm,
	)
}

func (m *Manager) PackageManagerName() string {
	if m.pm == "" {
		return "pip3"
	}
	return m.pm
}

func (m *Manager) PackageManagerAvailable() bool { return m.pmBin != "" }

func (m *Manager) UpgradeTool(name, binaryPath string) error {
	switch name {
	case "yt-dlp":
		if err := exec.Command(binaryPath, "-U").Run(); err == nil {
			return nil
		}
		return m.pipInstall("yt-dlp")
	case "gallery-dl":
		return m.pipInstall("gallery-dl")
	}
	if m.pmBin != "" {
		return exec.Command("sudo", m.pmBin, "upgrade", name).Run()
	}
	return fmt.Errorf("upgrade not supported for %s on this system", name)
}

func (m *Manager) LaunchSetup() error {
	script := `#!/bin/sh
echo "=== YTDown Setup ==="
if command -v apt-get >/dev/null; then sudo apt-get install -y ffmpeg yt-dlp
elif command -v dnf >/dev/null; then sudo dnf install -y ffmpeg yt-dlp
elif command -v pacman >/dev/null; then sudo pacman -S --noconfirm ffmpeg yt-dlp
fi
pip3 install --user --upgrade gallery-dl 2>/dev/null || pip install --user --upgrade gallery-dl
echo "Done! Closing in 3s..."; sleep 3`

	home, _ := os.UserHomeDir()
	scriptPath := filepath.Join(home, ".config", "ytdown", "setup.sh")
	os.MkdirAll(filepath.Dir(scriptPath), 0755)
	os.WriteFile(scriptPath, []byte(script), 0755)

	// Try common terminal emulators
	for _, term := range [][]string{
		{"gnome-terminal", "--", "bash", scriptPath},
		{"xterm", "-e", "bash " + scriptPath},
		{"konsole", "-e", "bash", scriptPath},
		{"xfce4-terminal", "-e", "bash " + scriptPath},
	} {
		if _, err := exec.LookPath(term[0]); err == nil {
			return exec.Command(term[0], term[1:]...).Start()
		}
	}
	return fmt.Errorf("no terminal emulator found — run setup manually: bash %s", scriptPath)
}

func (m *Manager) GetDownloadDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Downloads")
}

func (m *Manager) GetConfigDir() string {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "ytdown")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "ytdown")
}

func (m *Manager) AppDataDir() string {
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return filepath.Join(d, "YTDown")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "YTDown")
}

func (m *Manager) OpenFolder(path string) error { return exec.Command("xdg-open", path).Start() }
func (m *Manager) OpenFile(path string) error   { return exec.Command("xdg-open", path).Start() }
func (m *Manager) OSName() string               { return "Linux" }
func (m *Manager) UpdateAssetSuffix() string    { return ".AppImage" }

func (m *Manager) InstallAppUpdate(downloadURL string, parentPID int) error {
	execPath, _ := os.Executable()
	tmpDir, _ := os.MkdirTemp("", "ytdown-update-*")
	script := fmt.Sprintf(`#!/bin/sh
PARENT_PID=%d; NEW=%q/YTDown.AppImage; TARGET=%q
while kill -0 "$PARENT_PID" 2>/dev/null; do sleep 1; done
curl -L --fail -o "$NEW" %q
chmod +x "$NEW"; cp "$NEW" "$TARGET"
"$TARGET" &`, parentPID, tmpDir, execPath, downloadURL)

	scriptPath := filepath.Join(tmpDir, "update.sh")
	os.WriteFile(scriptPath, []byte(script), 0755)
	return exec.Command("sh", scriptPath).Start()
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

package linux

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Manager struct {
	pm    string // "apt", "dnf", "pacman", "zypper"
	pmBin string // absolute path to package manager binary
}

func New() *Manager {
	m := &Manager{}
	for _, pm := range []struct{ name, bin string }{
		{"apt", "apt-get"},
		{"dnf", "dnf"},
		{"pacman", "pacman"},
		{"zypper", "zypper"},
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
	if m.pmBin == "" {
		return fmt.Errorf("không tìm thấy package manager — vui lòng cài %s thủ công", name)
	}
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
	default:
		return fmt.Errorf("package manager không được hỗ trợ: %s", m.pm)
	}
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}

func (m *Manager) InstallInstructions(tools []string) string {
	pm := m.pm
	if pm == "" {
		pm = "package manager của hệ thống"
	}
	return fmt.Sprintf(
		"YTDown cần các công cụ sau:\n\n%s\n\nCài đặt qua %s?",
		strings.Join(tools, "\n"),
		pm,
	)
}

func (m *Manager) PackageManagerName() string {
	if m.pm == "" {
		return "unknown"
	}
	return m.pm
}

func (m *Manager) PackageManagerAvailable() bool { return m.pmBin != "" }

func (m *Manager) UpgradeTool(name, binaryPath string) error {
	if m.pmBin == "" {
		return fmt.Errorf("không tìm thấy package manager — vui lòng nâng cấp %s thủ công", name)
	}
	switch m.pm {
	case "apt":
		// --only-upgrade: chỉ upgrade nếu đã cài, không cài mới
		return exec.Command("sudo", m.pmBin, "install", "--only-upgrade", "-y", name).Run()
	case "dnf":
		return exec.Command("sudo", m.pmBin, "upgrade", "-y", name).Run()
	case "pacman":
		return exec.Command("sudo", m.pmBin, "-S", "--noconfirm", name).Run()
	case "zypper":
		return exec.Command("sudo", m.pmBin, "update", "-y", name).Run()
	}
	return fmt.Errorf("upgrade không được hỗ trợ cho %s trên hệ thống này", name)
}

func (m *Manager) LaunchSetup() error {
	script := `#!/bin/sh
echo "=== YTDown Setup ==="
if command -v apt-get >/dev/null; then
    sudo apt-get install -y ffmpeg yt-dlp gallery-dl
elif command -v dnf >/dev/null; then
    sudo dnf install -y ffmpeg yt-dlp gallery-dl
elif command -v pacman >/dev/null; then
    sudo pacman -S --noconfirm ffmpeg yt-dlp gallery-dl
elif command -v zypper >/dev/null; then
    sudo zypper install -y ffmpeg yt-dlp gallery-dl
else
    echo "Không tìm thấy package manager được hỗ trợ."
    echo "Vui lòng cài thủ công: ffmpeg, yt-dlp, gallery-dl"
fi
echo "Done! Closing in 3s..."; sleep 3`

	home, _ := os.UserHomeDir()
	scriptPath := filepath.Join(home, ".config", "ytdown", "setup.sh")
	os.MkdirAll(filepath.Dir(scriptPath), 0755)
	os.WriteFile(scriptPath, []byte(script), 0755)

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

func (m *Manager) GetLatestVersion(name string) string {
	if m.pm != "apt" {
		return ""
	}
	out, err := exec.Command("apt-cache", "policy", name).Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Candidate:") {
			v := strings.TrimSpace(strings.TrimPrefix(line, "Candidate:"))
			if v == "(none)" || v == "" {
				return ""
			}
			// ✅ Strip Debian revision suffix: "2026.3.17-1" → "2026.3.17"
			// Debian format: <upstream_version>-<debian_revision>
			if idx := strings.LastIndex(v, "-"); idx != -1 {
				v = v[:idx]
			}
			return v
		}
	}
	return ""
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

func (m *Manager) UpdateAssetSuffix() string { return "-Linux.deb" }

func (m *Manager) InstallAppUpdate(downloadURL string, parentPID int) error {
	execPath, _ := os.Executable()
	tmpDir, _ := os.MkdirTemp("", "ytdown-update-*")
	debPath := filepath.Join(tmpDir, "ytdown.deb")

	script := fmt.Sprintf(`#!/bin/sh
PARENT_PID=%d
DEB=%q
EXEC=%q

while kill -0 "$PARENT_PID" 2>/dev/null; do sleep 1; done

curl -L --fail -o "$DEB" %q || exit 1

# Dùng pkexec thay sudo để có GUI password prompt
if command -v pkexec >/dev/null 2>&1; then
    pkexec dpkg -i "$DEB"
elif command -v gksu >/dev/null 2>&1; then
    gksu dpkg -i "$DEB"
else
    sudo dpkg -i "$DEB"
fi

# Restart app sau khi cài xong
"$EXEC" &
rm -rf "$(dirname "$DEB")"
`, parentPID, debPath, execPath, downloadURL)

	scriptPath := filepath.Join(tmpDir, "update.sh")
	os.WriteFile(scriptPath, []byte(script), 0755)
	return exec.Command("sh", scriptPath).Start()
}

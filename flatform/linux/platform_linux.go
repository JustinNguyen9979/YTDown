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

	var shellCmd string
	switch m.pm {
	case "apt":
		// ✅ apt update trước để refresh cache
		shellCmd = fmt.Sprintf("%s update -qq && %s install --only-upgrade -y %s",
			m.pmBin, m.pmBin, name)
	case "dnf":
		shellCmd = fmt.Sprintf("%s upgrade -y %s", m.pmBin, name)
	case "pacman":
		shellCmd = fmt.Sprintf("%s -S --noconfirm %s", m.pmBin, name)
	case "zypper":
		shellCmd = fmt.Sprintf("%s update -y %s", m.pmBin, name)
	default:
		return fmt.Errorf("upgrade không được hỗ trợ cho %s trên hệ thống này", name)
	}

	var cmd *exec.Cmd
	if _, err := exec.LookPath("pkexec"); err == nil {
		cmd = exec.Command("pkexec", "sh", "-c", shellCmd)
	} else {
		cmd = exec.Command("sudo", "sh", "-c", shellCmd)
	}

	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("upgrade %s thất bại: %w\n%s", name, err, strings.TrimSpace(string(out)))
	}
	return nil
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

	func (m *Manager) GetLatestAppVersion() string {
	return m.GetLatestVersion("ytdown")
	}

	func (m *Manager) GetDownloadDir() string {	home, _ := os.UserHomeDir()
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

func (m *Manager) OpenFolder(path string) error {
	os.MkdirAll(path, 0755)
	return exec.Command("xdg-open", path).Start()
}
func (m *Manager) OpenFile(path string) error   { return exec.Command("xdg-open", path).Start() }
func (m *Manager) OSName() string               { return "Linux" }

func (m *Manager) UpdateAssetSuffix() string { return "-Linux.deb" }

func (m *Manager) InstallAppUpdate(_ string, parentPID int) error {
	execPath, _ := os.Executable()

	script := fmt.Sprintf(`#!/bin/sh
PARENT_PID=%d
EXEC=%q

while kill -0 "$PARENT_PID" 2>/dev/null; do sleep 1; done

# ✅ apt update + upgrade, không cần curl/dpkg
if command -v pkexec >/dev/null 2>&1; then
    pkexec sh -c "apt-get update -qq && apt-get install --only-upgrade -y ytdown"
else
    sudo sh -c "apt-get update -qq && apt-get install --only-upgrade -y ytdown"
fi

"$EXEC" &
`, parentPID, execPath)

	tmpFile, _ := os.CreateTemp("", "ytdown-appupdate-*.sh")
	tmpFile.WriteString(script)
	tmpFile.Close()
	os.Chmod(tmpFile.Name(), 0755)
	return exec.Command("sh", tmpFile.Name()).Start()
}

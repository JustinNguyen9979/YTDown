//go:build linux

package linux

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

type LinuxManager struct{}

func New() *LinuxManager { return &LinuxManager{} }

func (m *LinuxManager) OSName() string { return "linux" }

func (m *LinuxManager) CheckDependencies() ([]string, error) {
	tools := []string{"ffmpeg", "yt-dlp", "gallery-dl"}
	var missing []string
	for _, tool := range tools {
		if _, err := exec.LookPath(tool); err != nil {
			missing = append(missing, tool)
		}
	}
	return missing, nil
}

// InstallDependency thử apt → dnf → pacman
func (m *LinuxManager) InstallDependency(name string) error {
	managers := []struct {
		bin  string
		args []string
	}{
		{"apt-get", []string{"install", "-y", name}},
		{"dnf", []string{"install", "-y", name}},
		{"pacman", []string{"-S", "--noconfirm", name}},
	}
	for _, pm := range managers {
		if _, err := exec.LookPath(pm.bin); err == nil {
			cmd := exec.Command(pm.bin, pm.args...)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			return cmd.Run()
		}
	}
	return fmt.Errorf("không tìm được package manager để cài %s", name)
}

func (m *LinuxManager) GetDownloadDir() string {
	if xdg := os.Getenv("XDG_DOWNLOAD_DIR"); xdg != "" {
		return xdg
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Downloads")
}

func (m *LinuxManager) GetConfigDir() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "ytdown")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "ytdown")
}

func (m *LinuxManager) OpenFolder(path string) error {
	return exec.Command("xdg-open", path).Start()
}

func (m *LinuxManager) GetBinaryPath(tool string) string {
	if p, err := exec.LookPath(tool); err == nil {
		return p
	}
	return tool
}

//go:build darwin

package darwin

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type DarwinManager struct{}

func New() *DarwinManager { return &DarwinManager{} }

func (m *DarwinManager) OSName() string { return "darwin" }

func (m *DarwinManager) CheckDependencies() ([]string, error) {
	tools := []string{"ffmpeg", "yt-dlp", "gallery-dl"}
	var missing []string
	for _, tool := range tools {
		if _, err := exec.LookPath(tool); err != nil {
			missing = append(missing, tool)
		}
	}
	return missing, nil
}

func (m *DarwinManager) InstallDependency(name string) error {
	cmd := exec.Command("brew", "install", name)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (m *DarwinManager) GetDownloadDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Downloads")
}

func (m *DarwinManager) GetConfigDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "Application Support", "YTDown")
}

func (m *DarwinManager) OpenFolder(path string) error {
	return exec.Command("open", path).Start()
}

func (m *DarwinManager) GetBinaryPath(tool string) string {
	paths := []string{
		fmt.Sprintf("/opt/homebrew/bin/%s", tool), // Apple Silicon
		fmt.Sprintf("/usr/local/bin/%s", tool),    // Intel
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if p, err := exec.LookPath(tool); err == nil {
		return p
	}
	return strings.ToLower(tool)
}

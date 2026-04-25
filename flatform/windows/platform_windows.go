//go:build windows

package windows

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type WindowsManager struct {
	binDir string // %APPDATA%\YTDown\bin – chứa binary bundle
}

func New() *WindowsManager {
	appData := os.Getenv("APPDATA")
	return &WindowsManager{
		binDir: filepath.Join(appData, "YTDown", "bin"),
	}
}

func (m *WindowsManager) OSName() string { return "windows" }

func (m *WindowsManager) CheckDependencies() ([]string, error) {
	tools := []string{"ffmpeg", "yt-dlp", "gallery-dl"}
	var missing []string
	for _, tool := range tools {
		// Kiểm tra binary bundle trước
		bundled := filepath.Join(m.binDir, tool+".exe")
		if _, err := os.Stat(bundled); err == nil {
			continue
		}
		if _, err := exec.LookPath(tool); err != nil {
			missing = append(missing, tool)
		}
	}
	return missing, nil
}

func (m *WindowsManager) InstallDependency(name string) error {
	os.MkdirAll(m.binDir, 0755)
	wingetMap := map[string]string{
		"ffmpeg":     "Gyan.FFmpeg",
		"yt-dlp":     "yt-dlp.yt-dlp",
		"gallery-dl": "mikf.gallery-dl",
	}
	if pkgID, ok := wingetMap[name]; ok {
		cmd := exec.Command("winget", "install", "--id", pkgID, "-e", "--silent")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}
	return fmt.Errorf("không biết cách cài %s trên Windows", name)
}

func (m *WindowsManager) GetDownloadDir() string {
	return filepath.Join(os.Getenv("USERPROFILE"), "Downloads")
}

func (m *WindowsManager) GetConfigDir() string {
	return filepath.Join(os.Getenv("APPDATA"), "YTDown")
}

func (m *WindowsManager) OpenFolder(path string) error {
	return exec.Command("explorer", path).Start()
}

func (m *WindowsManager) GetBinaryPath(tool string) string {
	bundled := filepath.Join(m.binDir, tool+".exe")
	if _, err := os.Stat(bundled); err == nil {
		return bundled
	}
	if p, err := exec.LookPath(tool); err == nil {
		return p
	}
	return strings.ToLower(tool) + ".exe"
}

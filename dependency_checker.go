package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	platform "ytdown/flatform"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type DependencyStatus struct {
	Name      string `json:"name"`
	Installed bool   `json:"installed"`
	Version   string `json:"version"`
	Error     string `json:"error"`
}

type DependencyCheckResult struct {
	AllInstalled bool               `json:"allInstalled"`
	Dependencies []DependencyStatus `json:"dependencies"`
	MissingTools []string           `json:"missingTools"`
	ErrorMessage string             `json:"errorMessage"`
}

var requiredDependencies = []string{"ffmpeg", "yt-dlp", "gallery-dl"}

// CheckDependencies checks if all required tools are installed
func (a *App) CheckDependencies() DependencyCheckResult {
	missing, _ := a.pm.CheckDependencies()
	result := DependencyCheckResult{
		AllInstalled: len(missing) == 0,
		MissingTools: missing,
	}
	for _, tool := range []string{"ffmpeg", "yt-dlp", "gallery-dl"} {
		path := a.pm.GetBinaryPath(tool)
		status := DependencyStatus{Name: tool, Installed: path != ""}
		if path != "" {
			status.Version = getToolVersion(tool, path)
		} else {
			status.Error = "not found"
		}
		result.Dependencies = append(result.Dependencies, status)
	}
	if !result.AllInstalled {
		result.ErrorMessage = fmt.Sprintf(
			"Missing: %s", strings.Join(missing, ", "),
		)
	}
	return result
}

// InstallDependencies installs missing dependencies and returns success status and error message if any
func (a *App) InstallDependencies(tools []string) (bool, string) {
	for _, tool := range tools {
		if err := a.pm.InstallDependency(tool); err != nil {
			return false, fmt.Sprintf("Failed to install %s: %v", tool, err)
		}
	}
	return true, ""
}

// GetBrewInstallStatus kept for frontend compatibility
func (a *App) GetBrewInstallStatus() map[string]interface{} {
	return a.GetPackageManagerStatus()
}

// GetPackageManagerStatus replaces the old macOS-only GetBrewInstallStatus
func (a *App) GetPackageManagerStatus() map[string]interface{} {
	return map[string]interface{}{
		"name":      a.pm.PackageManagerName(),
		"available": a.pm.PackageManagerAvailable(),
		"os":        a.pm.OSName(),
	}
}

// checkTool checks if a tool is installed and returns its version
func checkTool(toolName string) DependencyStatus {
	status := DependencyStatus{Name: toolName}

	// Bước 1: Thử LookPath (hoạt động khi mở từ Terminal)
	if path, err := exec.LookPath(toolName); err == nil {
		status.Installed = true
		status.Version = getToolVersion(toolName, path)
		return status
	}

	// Bước 2: Fallback check các đường dẫn Homebrew thường gặp
	// (khi mở từ Finder, $PATH không có /opt/homebrew/bin)
	commonPaths := []string{
		"/opt/homebrew/bin/" + toolName, // Apple Silicon (M1/M2/M3)
		"/usr/local/bin/" + toolName,    // Intel Mac
		"/usr/bin/" + toolName,
	}

	for _, p := range commonPaths {
		if _, err := os.Stat(p); err == nil {
			status.Installed = true
			status.Version = getToolVersion(toolName, p)
			return status
		}
	}

	// Không tìm thấy ở đâu cả
	status.Installed = false
	status.Error = "not found in PATH or Homebrew directories"
	return status
}

// getToolVersion retrieves the version of a tool
func getToolVersion(toolName string, toolPath string) string {
	var cmd *exec.Cmd

	switch toolName {
	case "ffmpeg":
		cmd = platform.Command(toolPath, "-version")
	case "yt-dlp":
		cmd = platform.Command(toolPath, "--version")
	case "gallery-dl":
		cmd = platform.Command(toolPath, "--version")
	default:
		return "unknown"
	}

	output, err := cmd.Output()
	if err != nil {
		return "unknown"
	}

	// Extract first line which usually contains version
	lines := strings.Split(string(output), "\n")
	if len(lines) > 0 {
		version := strings.TrimSpace(lines[0])
		if version == "" {
			return "unknown"
		}
		fields := strings.Fields(version)

		switch toolName {
		case "ffmpeg":
			// Output: "ffmpeg version 7.0.2 Copyright..."
			// fields: ["ffmpeg", "version", "7.0.2", ...]
			if len(fields) >= 3 && fields[1] == "version" {
				return fields[2]
			}
		case "gallery-dl":
			// Output: "gallery-dl 1.28.1"
			// fields: ["gallery-dl", "1.28.1"]
			if len(fields) >= 2 {
				return fields[1]
			}
		default:
			// yt-dlp output: "2024.03.10" — trả về trực tiếp
			if len(fields) > 0 {
				return fields[0]
			}
		}
	}

	return "unknown"
}

// isBrewInstalled checks if Homebrew is installed
func isBrewInstalled() bool {
	if _, err := exec.LookPath("brew"); err == nil {
		return true
	}
	// Fallback khi mở từ Finder
	for _, p := range []string{"/opt/homebrew/bin/brew", "/usr/local/bin/brew"} {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}

func (a *App) PromptToInstallDependencies() (bool, string) {
	check := a.CheckDependencies()
	if check.AllInstalled {
		return true, ""
	}
	confirmed, _ := runtime.MessageDialog(a.ctx, runtime.MessageDialogOptions{
		Type:    runtime.QuestionDialog,
		Title:   "Install Dependencies",
		Message: a.pm.InstallInstructions(check.MissingTools),
	})
	if confirmed != "Yes" {
		return false, "User declined"
	}
	return a.InstallDependencies(check.MissingTools)
}

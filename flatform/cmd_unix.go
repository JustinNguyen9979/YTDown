//go:build !windows

package platform

import (
	"context"
	"os/exec"
)

// HideConsoleWindow is a no-op on non-Windows platforms.
func HideConsoleWindow(cmd *exec.Cmd) {
	// No-op
}

// Command creates a new exec.Cmd and applies HideConsoleWindow.
func Command(name string, arg ...string) *exec.Cmd {
	cmd := exec.Command(name, arg...)
	HideConsoleWindow(cmd)
	return cmd
}

// CommandContext creates a new exec.Cmd with context and applies HideConsoleWindow.
func CommandContext(ctx context.Context, name string, arg ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, arg...)
	HideConsoleWindow(cmd)
	return cmd
}

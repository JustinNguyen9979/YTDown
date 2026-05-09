//go:build windows

package platform

import (
	"context"
	"os/exec"
	"syscall"
)

// HideConsoleWindow ensures that a command does not pop up a console window on Windows.
func HideConsoleWindow(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
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

//go:build windows

package platform

import "github.com/JustinNguyen9979/YTDown/platform/windows"

func newOSManager() Manager { return windows.New() }

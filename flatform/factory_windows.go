// flatform/factory_windows.go
//go:build windows

package platform

import "ytdown/flatform/windows"

func newOSManager() Manager { return windows.New() }

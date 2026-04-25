//go:build linux

package platform

import "github.com/JustinNguyen9979/YTDown/platform/linux"

func newOSManager() Manager { return linux.New() }

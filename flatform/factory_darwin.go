//go:build darwin

package platform

import "github.com/JustinNguyen9979/YTDown/platform/darwin"

func newOSManager() Manager { return darwin.New() }

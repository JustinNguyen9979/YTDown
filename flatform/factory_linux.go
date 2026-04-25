// flatform/factory_linux.go
//go:build linux

package platform

import "ytdown/flatform/linux"

func newOSManager() Manager { return linux.New() }

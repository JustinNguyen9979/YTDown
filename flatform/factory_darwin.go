// flatform/factory_darwin.go
//go:build darwin

package platform

import "ytdown/flatform/darwin"

func newOSManager() Manager { return darwin.New() }

package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
        appName = "YTDown"
)

type AppUpdateInfo struct {
        Current   string `json:"current"`
        Latest    string `json:"latest"`
        Available bool   `json:"available"`
}

func (a *App) GetAppUpdateInfo() AppUpdateInfo {
        info := AppUpdateInfo{Current: Version}

        latest := a.pm.GetLatestAppVersion()
	if latest == "" {
		return info
	}

	latest = normalizeReleaseVersion(latest)
	info.Latest = latest

	// Check if latest version is newer than current
	if info.Current != "" && latest != "" && compareDateVersions(latest, info.Current) > 0 {
		info.Available = true
	}
	return info
}

func (a *App) InstallAppUpdate() error {
	info := a.GetAppUpdateInfo()
	if !info.Available {
		return fmt.Errorf("no newer update available")
	}

	osName := a.pm.OSName()

	if err := a.pm.InstallAppUpdate("", os.Getpid()); err != nil {
		return err
	}

	runtime.EventsEmit(a.ctx, "app-update-started", map[string]interface{}{
		"version": info.Latest,
		"os":      osName,
	})

	go func() {
		time.Sleep(300 * time.Millisecond)
		runtime.Quit(a.ctx)
	}()
	return nil
}

func normalizeReleaseVersion(version string) string {
	return strings.TrimPrefix(strings.TrimSpace(version), "v")
}

func compareDateVersions(a, b string) int {
	// "2026.4.13"   → [2026, 4, 13, 0]  (patch mặc định = 0)
	// "2026.4.13.1" → [2026, 4, 13, 1]
	// "2026.4.13.2" → [2026, 4, 13, 2]
	parse := func(v string) [4]int {
		var out [4]int // patch mặc định = 0
		parts := strings.Split(v, ".")
		for i := 0; i < len(parts) && i < 4; i++ {
			n, _ := strconv.Atoi(parts[i])
			out[i] = n
		}
		return out
	}

	av := parse(a)
	bv := parse(b)
	for i := 0; i < 4; i++ {
		if av[i] > bv[i] {
			return 1
		}
		if av[i] < bv[i] {
			return -1
		}
	}
	return 0
}

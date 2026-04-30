package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	appReleaseRepoAPI = "https://api.github.com/repos/JustinNguyen9979/YTDown/releases/latest"
	appReleasePageURL = "https://github.com/JustinNguyen9979/YTDown/releases/latest"
	appName           = "YTDown"
)

type AppUpdateInfo struct {
	Current      string `json:"current"`
	Latest       string `json:"latest"`
	Available    bool   `json:"available"`
	ReleaseURL   string `json:"releaseUrl"`
	DownloadURL  string `json:"downloadUrl"`
	AssetName    string `json:"assetName"`
	ReleaseNotes string `json:"releaseNotes"`
}

type githubReleaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type githubRelease struct {
	TagName string               `json:"tag_name"`
	Body    string               `json:"body"`
	HTMLURL string               `json:"html_url"`
	Assets  []githubReleaseAsset `json:"assets"`
}

func (a *App) GetAppUpdateInfo() AppUpdateInfo {
	info := AppUpdateInfo{Current: Version, ReleaseURL: appReleasePageURL}
	release, err := fetchLatestAppRelease()
	if err != nil {
		return info
	}
	latest := normalizeReleaseVersion(release.TagName)
	info.Latest = latest
	info.ReleaseURL = chooseNonEmpty(release.HTMLURL, appReleasePageURL)
	info.ReleaseNotes = strings.TrimSpace(release.Body)

	// Use platform suffix to find the right asset
	suffix := a.pm.UpdateAssetSuffix()
	for i := range release.Assets {
		if strings.HasSuffix(strings.ToLower(release.Assets[i].Name), suffix) {
			info.AssetName = release.Assets[i].Name
			info.DownloadURL = release.Assets[i].BrowserDownloadURL
			break
		}
	}
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

	if a.pm.OSName() == "Linux" {
		if err := a.pm.InstallAppUpdate("", os.Getpid()); err != nil {
			return err
		}
		runtime.EventsEmit(a.ctx, "app-update-started", map[string]interface{}{"version": info.Latest})
		go func() {
			time.Sleep(300 * time.Millisecond)
			runtime.Quit(a.ctx)
		}()
		return nil
	}

	// macOS: Tải DMG, gắn vào, copy app ra, rồi mở app mới
	if info.DownloadURL == "" {
		return fmt.Errorf("no release asset found for %s", a.pm.OSName())
	}
	if err := a.pm.InstallAppUpdate(info.DownloadURL, os.Getpid()); err != nil {
		return err
	}
	runtime.EventsEmit(a.ctx, "app-update-started", map[string]interface{}{"version": info.Latest})
	go func() {
		time.Sleep(300 * time.Millisecond)
		runtime.Quit(a.ctx)
	}()
	return nil
}

func fetchLatestAppRelease() (*githubRelease, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, appReleaseRepoAPI, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", appName+"/"+Version)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github releases API returned HTTP %d", resp.StatusCode)
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, err
	}
	return &release, nil
}

func chooseDMGAsset(assets []githubReleaseAsset) *githubReleaseAsset {
	for i := range assets {
		if strings.HasSuffix(strings.ToLower(assets[i].Name), ".dmg") {
			return &assets[i]
		}
	}
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

func currentAppBundlePath() (string, error) {
	execPath, err := os.Executable()
	if err != nil {
		return "", err
	}

	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		return "", err
	}

	marker := ".app/Contents/MacOS/"
	idx := strings.Index(execPath, marker)
	if idx == -1 {
		return "", fmt.Errorf("current executable is not running inside an app bundle")
	}

	return execPath[:idx+len(".app")], nil
}

func preferredInstallPath() (string, error) {
	currentApp, err := currentAppBundlePath()
	if err == nil && isDirWritable(filepath.Dir(currentApp)) {
		return currentApp, nil
	}

	applicationsPath := filepath.Join("/Applications", appName+".app")
	if isDirWritable("/Applications") {
		return applicationsPath, nil
	}

	if err == nil {
		return currentApp, nil
	}

	return "", err
}

func isDirWritable(dir string) bool {
	testFile := filepath.Join(dir, "."+appName+".update-test")
	if err := os.WriteFile(testFile, []byte("ok"), 0o644); err != nil {
		return false
	}
	_ = os.Remove(testFile)
	return true
}

func writeAppUpdaterScript(downloadURL, targetApp string) (string, error) {
	tmpDir, err := os.MkdirTemp("", "ytdown-app-update-*")
	if err != nil {
		return "", fmt.Errorf("failed to create updater temp dir: %w", err)
	}

	scriptPath := filepath.Join(tmpDir, "update.sh")
	script := fmt.Sprintf(`#!/bin/sh
set -eu

DOWNLOAD_URL=%q
TARGET_APP=%q
PARENT_PID=%d
TMP_DIR=%q
DMG_PATH="$TMP_DIR/update.dmg"
MOUNT_DIR="$TMP_DIR/mount"

cleanup() {
  hdiutil detach "$MOUNT_DIR" -quiet >/dev/null 2>&1 || true
  rm -rf "$TMP_DIR"
}
trap cleanup EXIT

while kill -0 "$PARENT_PID" >/dev/null 2>&1; do
  sleep 1
done

mkdir -p "$MOUNT_DIR"
curl -L --fail --silent --show-error -o "$DMG_PATH" "$DOWNLOAD_URL"
hdiutil attach "$DMG_PATH" -nobrowse -quiet -mountpoint "$MOUNT_DIR"

SOURCE_APP=$(find "$MOUNT_DIR" -maxdepth 1 -name '*.app' -print -quit)
if [ -z "$SOURCE_APP" ]; then
  echo "No .app found in mounted DMG" >&2
  exit 1
fi

mkdir -p "$(dirname "$TARGET_APP")"
rm -rf "$TARGET_APP"
ditto "$SOURCE_APP" "$TARGET_APP"
open "$TARGET_APP"
`, downloadURL, targetApp, os.Getpid(), tmpDir)

	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		return "", fmt.Errorf("failed to write updater script: %w", err)
	}

	return scriptPath, nil
}

func chooseNonEmpty(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

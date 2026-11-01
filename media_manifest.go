package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

var mediaManifestMu sync.Mutex
var videoOutputTemplates = map[string]string{}

func setVideoOutputTemplate(dir, template string) {
	mediaManifestMu.Lock()
	defer mediaManifestMu.Unlock()
	videoOutputTemplates[dir] = template
}

func takeVideoOutputTemplate(dir string) string {
	mediaManifestMu.Lock()
	defer mediaManifestMu.Unlock()
	template := videoOutputTemplates[dir]
	delete(videoOutputTemplates, dir)
	return template
}

func nextCreatorFolder(root string) string {
	for n := 1; ; n++ {
		candidate := filepath.Join(root, fmt.Sprintf("creator%d", n))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
}

func prepareVideoMediaFolder(savePath, creator string) (string, string, func(), error) {
	mediaManifestMu.Lock()
	defer mediaManifestMu.Unlock()
	creator = sanitizeFolderName(creator)
	if creator == "" || creator == "." {
		creatorDir := nextCreatorFolder(savePath)
		creator = filepath.Base(creatorDir)
	}
	dir := filepath.Join(savePath, creator)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", nil, err
	}
	n := nextVideoNumber(dir)
	reservation := filepath.Join(dir, fmt.Sprintf(".ytdown-video-%d.reserve", n))
	f, err := os.OpenFile(reservation, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", "", nil, err
	}
	f.Close()
	template := filepath.Join(dir, fmt.Sprintf("video-%d.%%(ext)s", n))
	videoOutputTemplates[dir] = template
	return dir, fmt.Sprintf("video-%d", n), func() { _ = os.Remove(reservation) }, nil
}

func captionFileRecord(dir, fileBase, title, caption string) error {
	return appendCaptionRecord(dir, fileBase, title, caption)
}

func cleanupEmptyMediaDir(dir string) {
	if dir == "" {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		return
	}
	_ = os.Remove(dir)
}

func cleanupFailedVideoOutput(dir, fileBase string) {
	if dir == "" || fileBase == "" {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), fileBase+".") || strings.HasPrefix(entry.Name(), ".ytdown-") {
			_ = os.Remove(filepath.Join(dir, entry.Name()))
		}
	}
}

func appendLatestVideoCaption(root, creator, title, caption string, since time.Time) error {
	creatorDir := filepath.Join(root, mediaCreator(creator))
	var latest string
	var latestTime time.Time
	err := filepath.WalkDir(creatorDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		switch ext {
		case ".mp4", ".mkv", ".webm", ".mov", ".avi":
		default:
			return nil
		}
		info, statErr := entry.Info()
		if statErr == nil && !info.ModTime().Before(since) && (latest == "" || info.ModTime().After(latestTime)) {
			latest, latestTime = path, info.ModTime()
		}
		return nil
	})
	if err != nil || latest == "" {
		return err
	}
	return appendCaptionRecord(filepath.Dir(latest), filepath.Base(latest), title, caption)
}

func mediaCreator(name string) string {
	name = sanitizeFolderName(name)
	if name == "" || name == "." {
		return "creator1"
	}
	return name
}

func mediaTitle(name string) string {
	name = sanitizeFolderName(name)
	if name == "" || name == "." {
		return "untitled"
	}
	return name
}

func nextVideoNumber(dir string) int {
	max := 0
	entries, _ := os.ReadDir(dir)
	re := regexp.MustCompile(`^video-([0-9]+)\.`)
	for _, entry := range entries {
		m := re.FindStringSubmatch(strings.ToLower(entry.Name()))
		if len(m) == 2 {
			var n int
			fmt.Sscanf(m[1], "%d", &n)
			if n > max {
				max = n
			}
		}
	}
	return max + 1
}

func reserveVideoOutput(dir string) (string, func(), error) {
	mediaManifestMu.Lock()
	defer mediaManifestMu.Unlock()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", nil, err
	}
	n := nextVideoNumber(dir)
	reservation := filepath.Join(dir, fmt.Sprintf(".ytdown-video-%d.reserve", n))
	f, err := os.OpenFile(reservation, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", nil, err
	}
	f.Close()
	return filepath.Join(dir, fmt.Sprintf("video-%d.%%(ext)s", n)), func() { _ = os.Remove(reservation) }, nil
}

func appendCaptionRecord(dir, files, title, caption string) error {
	if dir == "" {
		return nil
	}
	mediaManifestMu.Lock()
	defer mediaManifestMu.Unlock()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, "caption.txt")
	data, _ := os.ReadFile(path)
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "- "+files+" | ") {
			return nil
		}
	}
	line := fmt.Sprintf("- %s | %s | %s\n", files, title, caption)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(line)
	return err
}

func imageCaptionFiles(paths []string) string {
	copyPaths := append([]string(nil), paths...)
	sort.Strings(copyPaths)
	return strings.Join(copyPaths, ", ")
}

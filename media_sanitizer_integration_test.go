package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSanitizeDownloadedVideoIntegration(t *testing.T) {
	source := os.Getenv("YTDOWN_SANITIZE_SAMPLE")
	if source == "" {
		t.Skip("set YTDOWN_SANITIZE_SAMPLE to run the FFmpeg integration test")
	}
	input, err := os.ReadFile(source)
	if err != nil {
		t.Fatalf("read sample: %v", err)
	}
	path := filepath.Join(t.TempDir(), "sample.mp4")
	if err := os.WriteFile(path, input, 0o600); err != nil {
		t.Fatalf("write sample copy: %v", err)
	}

	if err := sanitizeDownloadedVideo(context.Background(), path); err != nil {
		t.Fatalf("sanitizeDownloadedVideo() error = %v", err)
	}
	cleaned, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read sanitized sample: %v", err)
	}
	if bytes.Contains(bytes.ToLower(cleaned), []byte("xiaohongshu")) {
		t.Fatal("sanitized sample still contains Xiaohongshu encoder data")
	}

	ffprobePath := getResourcePath("ffprobe")
	cmd := exec.Command(ffprobePath, "-v", "error", "-show_format", "-show_streams", "-of", "json", path)
	probe, err := cmd.Output()
	if err != nil {
		t.Fatalf("probe sanitized sample: %v", err)
	}
	if !bytes.Contains(probe, []byte(`"codec_name": "h264"`)) || !bytes.Contains(probe, []byte(`"codec_name": "aac"`)) {
		t.Fatalf("sanitized sample lost expected streams: %s", probe)
	}
	if strings.Contains(strings.ToLower(string(probe)), "lavf59.14.100") {
		t.Fatalf("sanitized sample retained original container encoder: %s", probe)
	}

	cmd = exec.Command(ffprobePath, "-v", "error", "-select_streams", "v:0", "-read_intervals", "%+#1", "-show_frames", "-show_entries", "frame=side_data_list", "-of", "json", path)
	frames, err := cmd.Output()
	if err != nil {
		t.Fatalf("probe sanitized frame data: %v", err)
	}
	if bytes.Contains(frames, []byte("User Data Unregistered SEI")) {
		t.Fatalf("sanitized sample still contains unregistered SEI: %s", frames)
	}
}

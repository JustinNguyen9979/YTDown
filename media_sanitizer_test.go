package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestVideoBitstreamFilter(t *testing.T) {
	tests := []struct {
		codec string
		want  string
	}{
		{codec: "h264", want: "filter_units=remove_types=6"},
		{codec: "avc1", want: "filter_units=remove_types=6"},
		{codec: "hevc", want: "filter_units=remove_types=39|40"},
		{codec: "h265", want: "filter_units=remove_types=39|40"},
		{codec: "vp9", want: ""},
		{codec: "av1", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.codec, func(t *testing.T) {
			if got := videoBitstreamFilter(tt.codec); got != tt.want {
				t.Fatalf("videoBitstreamFilter(%q) = %q, want %q", tt.codec, got, tt.want)
			}
		})
	}
}

func TestBuildMediaSanitizeArgsStripsMetadataAndSEIWithoutReencoding(t *testing.T) {
	got := buildMediaSanitizeArgs("input.mp4", "output.mp4", "h264")
	want := []string{
		"-hide_banner", "-loglevel", "error", "-i", "input.mp4",
		"-map", "0", "-map_metadata", "-1", "-map_chapters", "-1",
		"-c", "copy", "-bsf:v", "filter_units=remove_types=6",
		"-fflags", "+bitexact", "-flags:v", "+bitexact", "-flags:a", "+bitexact",
		"-metadata", "encoder=", "-y", "output.mp4",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildMediaSanitizeArgs() = %#v\nwant %#v", got, want)
	}
}

func TestBuildMediaSanitizeArgsKeepsOtherCodecsAndStillStripsMetadata(t *testing.T) {
	got := buildMediaSanitizeArgs("input.webm", "output.webm", "vp9")
	for i, arg := range got {
		if arg == "-bsf:v" || (i > 0 && got[i-1] == "-bsf:v") {
			t.Fatalf("VP9 args unexpectedly contain a video bitstream filter: %#v", got)
		}
	}
	if !containsArg(got, "-map_metadata") || !containsArg(got, "-map_chapters") || !containsArg(got, "copy") {
		t.Fatalf("VP9 args do not preserve streams while removing metadata: %#v", got)
	}
}

func TestReplaceFileSafely(t *testing.T) {
	dir := t.TempDir()
	original := filepath.Join(dir, "video.mp4")
	replacement := filepath.Join(dir, "clean.mp4")
	if err := os.WriteFile(original, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(replacement, []byte("clean"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := replaceFileSafely(original, replacement); err != nil {
		t.Fatalf("replaceFileSafely() error = %v", err)
	}
	got, err := os.ReadFile(original)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "clean" {
		t.Fatalf("original content = %q, want clean", got)
	}
	if _, err := os.Stat(replacement); !os.IsNotExist(err) {
		t.Fatalf("replacement still exists or stat failed: %v", err)
	}
	backups, err := filepath.Glob(filepath.Join(dir, ".ytdown-original-*"))
	if err != nil || len(backups) != 0 {
		t.Fatalf("backup files = %#v, err = %v", backups, err)
	}
}

func TestIsSanitizableVideo(t *testing.T) {
	for _, path := range []string{"video.mp4", "clip.MOV", "movie.mkv", "asset.webm", "old.avi", "stream.flv"} {
		if !isSanitizableVideo(path) {
			t.Fatalf("isSanitizableVideo(%q) = false", path)
		}
	}
	for _, path := range []string{"audio.mp3", "sound.m4a", "image.png", "caption.txt"} {
		if isSanitizableVideo(path) {
			t.Fatalf("isSanitizableVideo(%q) = true", path)
		}
	}
}

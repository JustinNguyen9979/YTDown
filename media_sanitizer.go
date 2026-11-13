package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	platform "ytdown/flatform"
)

type mediaProbe struct {
	Streams []struct {
		CodecName string `json:"codec_name"`
		CodecType string `json:"codec_type"`
	} `json:"streams"`
}

func isSanitizableVideo(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp4", ".mov", ".mkv", ".webm", ".avi", ".flv", ".m4v", ".ts", ".m2ts":
		return true
	default:
		return false
	}
}

func videoBitstreamFilter(codec string) string {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "h264", "avc", "avc1":
		return "filter_units=remove_types=6"
	case "hevc", "h265", "hev1", "hvc1":
		return "filter_units=remove_types=39|40"
	default:
		return ""
	}
}

func buildMediaSanitizeArgs(input, output, videoCodec string) []string {
	args := []string{
		"-hide_banner", "-loglevel", "error", "-i", input,
		"-map", "0", "-map_metadata", "-1", "-map_chapters", "-1",
		"-c", "copy",
	}
	if filter := videoBitstreamFilter(videoCodec); filter != "" {
		args = append(args, "-bsf:v", filter)
	}
	args = append(args,
		"-fflags", "+bitexact", "-flags:v", "+bitexact", "-flags:a", "+bitexact",
		"-metadata", "encoder=", "-y", output,
	)
	return args
}

func probeVideoCodec(ctx context.Context, ffprobePath, path string) (string, error) {
	cmd := platform.CommandContext(ctx, ffprobePath,
		"-v", "error", "-show_streams", "-of", "json", path,
	)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("probe media streams: %w", err)
	}
	var probe mediaProbe
	if err := json.Unmarshal(output, &probe); err != nil {
		return "", fmt.Errorf("parse media streams: %w", err)
	}
	for _, stream := range probe.Streams {
		if stream.CodecType == "video" {
			return stream.CodecName, nil
		}
	}
	return "", fmt.Errorf("media file has no video stream")
}

func replaceFileSafely(originalPath, replacementPath string) error {
	backupFile, err := os.CreateTemp(filepath.Dir(originalPath), ".ytdown-original-*")
	if err != nil {
		return fmt.Errorf("reserve original media backup: %w", err)
	}
	backupPath := backupFile.Name()
	if err := backupFile.Close(); err != nil {
		_ = os.Remove(backupPath)
		return fmt.Errorf("close original media backup: %w", err)
	}
	_ = os.Remove(backupPath)

	if err := os.Rename(originalPath, backupPath); err != nil {
		return fmt.Errorf("backup original media: %w", err)
	}
	if err := os.Rename(replacementPath, originalPath); err != nil {
		if rollbackErr := os.Rename(backupPath, originalPath); rollbackErr != nil {
			return fmt.Errorf("replace downloaded media: %v; restore original media: %w", err, rollbackErr)
		}
		return fmt.Errorf("replace downloaded media: %w", err)
	}
	if err := os.Remove(backupPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove original media backup: %w", err)
	}
	return nil
}

func sanitizeDownloadedVideo(ctx context.Context, path string) error {
	if !isSanitizableVideo(path) {
		return nil
	}
	ffmpegPath := getResourcePath("ffmpeg")
	ffprobePath := getResourcePath("ffprobe")
	if ffmpegPath == "" || ffprobePath == "" {
		return fmt.Errorf("ffmpeg and ffprobe are required to sanitize downloaded video")
	}
	codec, err := probeVideoCodec(ctx, ffprobePath, path)
	if err != nil {
		return err
	}

	ext := filepath.Ext(path)
	tempFile, err := os.CreateTemp(filepath.Dir(path), ".ytdown-clean-*"+ext)
	if err != nil {
		return fmt.Errorf("create sanitized media file: %w", err)
	}
	tempPath := tempFile.Name()
	if err := tempFile.Close(); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("close sanitized media file: %w", err)
	}
	_ = os.Remove(tempPath) // FFmpeg creates the final temp output itself.
	defer os.Remove(tempPath)

	cmd := platform.CommandContext(ctx, ffmpegPath, buildMediaSanitizeArgs(path, tempPath, codec)...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("sanitize downloaded video: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	info, err := os.Stat(tempPath)
	if err != nil {
		return fmt.Errorf("sanitized media output is missing: %w", err)
	}
	if info.Size() == 0 {
		return fmt.Errorf("sanitized media output is empty")
	}
	if _, err := probeVideoCodec(ctx, ffprobePath, tempPath); err != nil {
		return fmt.Errorf("verify sanitized media output: %w", err)
	}

	originalMode := os.FileMode(0o644)
	if originalInfo, statErr := os.Stat(path); statErr == nil {
		originalMode = originalInfo.Mode().Perm()
	}
	if err := os.Chmod(tempPath, originalMode); err != nil {
		return fmt.Errorf("preserve sanitized media permissions: %w", err)
	}
	if err := replaceFileSafely(path, tempPath); err != nil {
		return err
	}
	return nil
}

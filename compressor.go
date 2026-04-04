package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// CompressionOptions stores settings for compression
type CompressionOptions struct {
	Type           string `json:"type"`     // "video" or "image"
	Quality        string `json:"quality"`  // "low", "medium", "high", "custom"
	CustomQuality  int    `json:"customQuality"` // 1-100
	UseSlowPreset  bool   `json:"useSlowPreset"`
	Format         string `json:"format"`   // "mp4", "webp", "jpg", "png", etc.
	SavePath       string `json:"savePath"`
}

// CompressFile handles single file compression
func CompressFile(ctx context.Context, inputPath string, options CompressionOptions, index int) error {
	ffmpegPath := getResourcePath("ffmpeg")
	if ffmpegPath == "" {
		return fmt.Errorf("ffmpeg not found")
	}

	// Create 'Compressed' directory in the save path
	outputDir := filepath.Join(options.SavePath, "Compressed")
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return err
	}

	// Prepare output filename
	filename := filepath.Base(inputPath)
	ext := filepath.Ext(filename)
	nameWithoutExt := strings.TrimSuffix(filename, ext)
	
	outputExt := ext
	if options.Format != "original" && options.Format != "" {
		outputExt = "." + options.Format
	}
	
	outputPath := filepath.Join(outputDir, nameWithoutExt+"_compressed"+outputExt)

	var args []string
	if options.Type == "video" {
		args = buildVideoCompressArgs(inputPath, outputPath, options)
	} else {
		args = buildImageCompressArgs(inputPath, outputPath, options)
	}

	cmd := exec.Command(ffmpegPath, args...)
	
	// Capture stderr to diagnose issues
	var stderr strings.Builder
	cmd.Stderr = &stderr

	// We can't easily get percentage from FFmpeg for compression without complex parsing,
	// so we'll just report "Processing" and then "Done".
	runtime.EventsEmit(ctx, "compression-progress", map[string]interface{}{
		"index":   index,
		"status":  "compressing",
		"message": "Compressing...",
	})

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("compression failed: %v\nDetails: %s", err, stderr.String())
	}

	runtime.EventsEmit(ctx, "compression-progress", map[string]interface{}{
		"index":   index,
		"status":  "done",
		"message": "Done",
	})

	return nil
}

func buildVideoCompressArgs(input, output string, options CompressionOptions) []string {
	crf := "28"
	preset := "medium"
	if options.UseSlowPreset {
		preset = "slow"
	}

	if options.Quality == "custom" {
		// Map 1-100 (Human) to 51-18 (CRF)
		// 100 -> 18 (Best), 1 -> 51 (Worst)
		val := 51 - (options.CustomQuality * (51 - 18) / 100)
		crf = fmt.Sprintf("%d", val)
	} else {
		switch options.Quality {
		case "low":
			crf = "35"
			if options.UseSlowPreset { preset = "slow" }
		case "medium":
			crf = "30"
			if options.UseSlowPreset { preset = "slow" }
		case "high":
			crf = "25"
			if options.UseSlowPreset { preset = "slow" }
		}
	}

	return []string{
		"-i", input,
		"-vcodec", "libx264",
		"-crf", crf,
		"-preset", preset,
		"-pix_fmt", "yuv420p",
		"-movflags", "+faststart",
		"-acodec", "aac",
		"-b:a", "128k",
		"-y",
		output,
	}
}

func buildImageCompressArgs(input, output string, options CompressionOptions) []string {
	// For image output, we want to ensure we only take 1 frame if input is video.
	// But if input is already an image, -frames:v 1 is sometimes problematic depending on ffmpeg version.
	
	args := []string{"-i", input}
	
	// If it's a video being converted to image, we MUST use -frames:v 1
	// We'll check extension to guess.
	ext := strings.ToLower(filepath.Ext(input))
	isVideo := false
	videoExts := []string{".mp4", ".mkv", ".avi", ".mov", ".wmv", ".flv", ".webm"}
	for _, ve := range videoExts {
		if ext == ve {
			isVideo = true
			break
		}
	}

	if isVideo {
		args = append(args, "-frames:v", "1")
	}

	format := strings.ToLower(options.Format)
	qValue := "80"
	jpegQ := "5"

	if options.Quality == "custom" {
		qValue = fmt.Sprintf("%d", options.CustomQuality)
		jVal := 32 - (options.CustomQuality * 31 / 100)
		if jVal < 1 { jVal = 1 }
		if jVal > 31 { jVal = 31 }
		jpegQ = fmt.Sprintf("%d", jVal)
	} else {
		switch options.Quality {
		case "low":
			qValue = "50"
			jpegQ = "12"
		case "medium":
			qValue = "75"
			jpegQ = "5"
		case "high":
			qValue = "95"
			jpegQ = "2"
		}
	}

	if format == "webp" {
		args = append(args, "-c:v", "libwebp", "-q:v", qValue, "-preset", "picture")
	} else if format == "jpg" || format == "jpeg" {
		args = append(args, "-q:v", jpegQ)
	} else if format == "png" {
		args = append(args, "-compression_level", "9")
	}

	args = append(args, "-y", output)
	return args
}

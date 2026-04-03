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
	Type     string `json:"type"`     // "video" or "image"
	Quality  string `json:"quality"`  // "low", "medium", "high"
	Format   string `json:"format"`   // "mp4", "webp", "jpg", "png", etc.
	SavePath string `json:"savePath"`
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
		args = buildVideoCompressArgs(inputPath, outputPath, options.Quality)
	} else {
		args = buildImageCompressArgs(inputPath, outputPath, options.Quality, options.Format)
	}

	cmd := exec.Command(ffmpegPath, args...)
	
	// We can't easily get percentage from FFmpeg for compression without complex parsing,
	// so we'll just report "Processing" and then "Done".
	runtime.EventsEmit(ctx, "compression-progress", map[string]interface{}{
		"index":   index,
		"status":  "compressing",
		"message": "Compressing...",
	})

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("compression failed: %v", err)
	}

	runtime.EventsEmit(ctx, "compression-progress", map[string]interface{}{
		"index":   index,
		"status":  "done",
		"message": "Done",
	})

	return nil
}

func buildVideoCompressArgs(input, output, quality string) []string {
	// CRF (Constant Rate Factor): 0-51, lower is better quality. 
	// 23 is default, 18 is visually lossless, 28 is still good but much smaller.
	crf := "23"
	preset := "medium"

	switch quality {
	case "low":
		crf = "32" // Smaller file, lower quality
		preset = "faster"
	case "medium":
		crf = "26" // Good balance
		preset = "medium"
	case "high":
		crf = "20" // High quality, larger file
		preset = "slow"
	}

	return []string{
		"-i", input,
		"-vcodec", "libx264",
		"-crf", crf,
		"-preset", preset,
		"-acodec", "aac",
		"-b:a", "128k",
		"-y", // Overwrite output if exists
		output,
	}
}

func buildImageCompressArgs(input, output, quality, format string) []string {
	args := []string{"-i", input}

	// Basic quality mapping for images
	qValue := "80"
	switch quality {
	case "low":
		qValue = "50"
	case "medium":
		qValue = "75"
	case "high":
		qValue = "95"
	}

	if strings.ToLower(format) == "webp" {
		args = append(args, "-c:v", "libwebp", "-q:v", qValue)
	} else if strings.ToLower(format) == "jpg" || strings.ToLower(format) == "jpeg" {
		args = append(args, "-q:v", "2") // FFmpeg uses 1-31 for JPEG, lower is better. 2 is very high quality.
		// For JPEG, we might want to use scale for quality if not using q:v correctly
	}

	args = append(args, "-y", output)
	return args
}

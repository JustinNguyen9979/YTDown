package main

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func jpegScanData(data []byte) []byte {
	for i := 2; i+4 <= len(data); {
		if data[i] != 0xff {
			return nil
		}
		marker := data[i+1]
		if marker == 0xda {
			return data[i:]
		}
		if marker >= 0xd0 && marker <= 0xd9 || marker == 0x01 {
			i += 2
			continue
		}
		length := int(binary.BigEndian.Uint16(data[i+2 : i+4]))
		if length < 2 || i+2+length > len(data) {
			return nil
		}
		i += 2 + length
	}
	return nil
}

func TestSanitizeJPEGPreservesPixelsAndOrientationWhileReplacingMetadata(t *testing.T) {
	source := os.Getenv("YTDOWN_IMAGE_SAMPLE")
	if source == "" {
		t.Skip("set YTDOWN_IMAGE_SAMPLE to run the JPEG integration fixture")
	}
	original, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	stamp := time.Date(2026, 11, 13, 23, 59, 1, 0, time.FixedZone("ICT", 7*3600))
	cleaned, err := sanitizeJPEG(original, stamp)
	if err != nil {
		t.Fatalf("sanitizeJPEG() error = %v", err)
	}
	if !bytes.Equal(jpegScanData(original), jpegScanData(cleaned)) {
		t.Fatal("JPEG compressed pixel scan changed")
	}
	if got := jpegOrientation(cleaned); got != 8 {
		t.Fatalf("orientation = %d, want 8", got)
	}
	for _, forbidden := range [][]byte{
		[]byte("NIKON CORPORATION"), []byte("NIKON D5200"), []byte("Ver.1.03"),
		[]byte("2026:07:20 17:24:56"), []byte("AF-S DX VR Zoom-Nikkor"),
	} {
		if bytes.Contains(cleaned, forbidden) {
			t.Fatalf("cleaned JPEG still contains %q", forbidden)
		}
	}
	for _, required := range [][]byte{[]byte("YTDown"), []byte("2026-11-13T23:59:01+07:00")} {
		if !bytes.Contains(cleaned, required) {
			t.Fatalf("cleaned JPEG missing %q", required)
		}
	}
}

func TestSanitizeDownloadedImageReplacesFileAndSetsRealDownloadTime(t *testing.T) {
	source := os.Getenv("YTDOWN_IMAGE_SAMPLE")
	if source == "" {
		t.Skip("set YTDOWN_IMAGE_SAMPLE to run the JPEG integration fixture")
	}
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "photo.JPG")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	stamp := time.Date(2026, 11, 13, 23, 59, 1, 0, time.Local)
	if err := sanitizeDownloadedImage(path, stamp); err != nil {
		t.Fatalf("sanitizeDownloadedImage() error = %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.ModTime().Unix() != stamp.Unix() {
		t.Fatalf("mtime = %v, want %v", info.ModTime(), stamp)
	}
}

func TestPNGMetadataRemovalKeepsColorAndAnimationChunks(t *testing.T) {
	data := append([]byte(nil), pngSignature...)
	data = append(data, pngChunk("IHDR", make([]byte, 13))...)
	data = append(data, pngChunk("iCCP", []byte("profile"))...)
	data = append(data, pngChunk("acTL", make([]byte, 8))...)
	data = append(data, pngChunk("eXIf", []byte("camera"))...)
	data = append(data, pngChunk("tEXt", []byte("platform=rednote"))...)
	data = append(data, pngChunk("tIME", make([]byte, 7))...)
	data = append(data, pngChunk("IDAT", []byte{1, 2, 3})...)
	data = append(data, pngChunk("IEND", nil)...)

	cleaned, err := sanitizePNG(data, time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	for _, kept := range [][]byte{[]byte("iCCP"), []byte("acTL"), []byte("YTDown")} {
		if !bytes.Contains(cleaned, kept) {
			t.Fatalf("cleaned PNG missing %q", kept)
		}
	}
	for _, removed := range [][]byte{[]byte("eXIf"), []byte("platform=rednote"), []byte("tIME")} {
		if bytes.Contains(cleaned, removed) {
			t.Fatalf("cleaned PNG retained %q", removed)
		}
	}
}

func TestIsSanitizableImage(t *testing.T) {
	for _, path := range []string{"a.jpg", "b.JPEG", "c.png", "d.WEBP"} {
		if !isSanitizableImage(path) {
			t.Fatalf("isSanitizableImage(%q) = false", path)
		}
	}
	for _, path := range []string{"a.mp4", "b.gif", "c.txt"} {
		if isSanitizableImage(path) {
			t.Fatalf("isSanitizableImage(%q) = true", path)
		}
	}
}

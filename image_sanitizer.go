package main

import (
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"fmt"
	"hash/crc32"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var pngSignature = []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}

func isSanitizableImage(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg", ".png", ".webp":
		return true
	default:
		return false
	}
}

func safeXMP(downloadedAt time.Time) []byte {
	stamp := downloadedAt.Format(time.RFC3339)
	type description struct {
		XMLName      xml.Name `xml:"rdf:Description"`
		RDF          string   `xml:"xmlns:rdf,attr"`
		XMP          string   `xml:"xmlns:xmp,attr"`
		CreatorTool  string   `xml:"xmp:CreatorTool,attr"`
		ModifyDate   string   `xml:"xmp:ModifyDate,attr"`
		MetadataDate string   `xml:"xmp:MetadataDate,attr"`
	}
	body, _ := xml.Marshal(description{
		RDF:          "http://www.w3.org/1999/02/22-rdf-syntax-ns#",
		XMP:          "http://ns.adobe.com/xap/1.0/",
		CreatorTool:  "YTDown",
		ModifyDate:   stamp,
		MetadataDate: stamp,
	})
	return []byte(`<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?><x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF>` + string(body) + `</rdf:RDF></x:xmpmeta><?xpacket end="w"?>`)
}

func tiffOrientation(tiff []byte) uint16 {
	if len(tiff) < 8 {
		return 1
	}
	var order binary.ByteOrder
	switch string(tiff[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 1
	}
	if order.Uint16(tiff[2:4]) != 42 {
		return 1
	}
	offset := int(order.Uint32(tiff[4:8]))
	if offset < 0 || offset+2 > len(tiff) {
		return 1
	}
	count := int(order.Uint16(tiff[offset : offset+2]))
	for i := 0; i < count; i++ {
		entry := offset + 2 + i*12
		if entry+12 > len(tiff) {
			return 1
		}
		if order.Uint16(tiff[entry:entry+2]) == 0x0112 && order.Uint16(tiff[entry+2:entry+4]) == 3 {
			value := order.Uint16(tiff[entry+8 : entry+10])
			if value >= 1 && value <= 8 {
				return value
			}
		}
	}
	return 1
}

func minimalJPEGOrientationMarker(orientation uint16) []byte {
	if orientation <= 1 || orientation > 8 {
		return nil
	}
	tiff := make([]byte, 26)
	copy(tiff[:4], []byte{'I', 'I', 42, 0})
	binary.LittleEndian.PutUint32(tiff[4:8], 8)
	binary.LittleEndian.PutUint16(tiff[8:10], 1)
	binary.LittleEndian.PutUint16(tiff[10:12], 0x0112)
	binary.LittleEndian.PutUint16(tiff[12:14], 3)
	binary.LittleEndian.PutUint32(tiff[14:18], 1)
	binary.LittleEndian.PutUint16(tiff[18:20], orientation)
	payload := append([]byte("Exif\x00\x00"), tiff...)
	segment := []byte{0xff, 0xe1, 0, 0}
	binary.BigEndian.PutUint16(segment[2:4], uint16(len(payload)+2))
	return append(segment, payload...)
}

func jpegOrientation(data []byte) uint16 {
	for pos := 2; pos+4 <= len(data) && data[pos] == 0xff; {
		marker := data[pos+1]
		if marker == 0xda || marker == 0xd9 {
			break
		}
		length := int(binary.BigEndian.Uint16(data[pos+2 : pos+4]))
		end := pos + 2 + length
		if length < 2 || end > len(data) {
			break
		}
		payload := data[pos+4 : end]
		if marker == 0xe1 && len(payload) >= 6 && bytes.Equal(payload[:6], []byte("Exif\x00\x00")) {
			return tiffOrientation(payload[6:])
		}
		pos = end
	}
	return 1
}

func sanitizeJPEG(data []byte, downloadedAt time.Time) ([]byte, error) {
	if len(data) < 4 || !bytes.Equal(data[:2], []byte{0xff, 0xd8}) {
		return nil, fmt.Errorf("invalid JPEG signature")
	}
	orientation := jpegOrientation(data)
	out := append([]byte(nil), data[:2]...)
	inserted := false
	for pos := 2; pos < len(data); {
		if data[pos] != 0xff {
			return nil, fmt.Errorf("invalid JPEG marker at offset %d", pos)
		}
		start := pos
		for pos < len(data) && data[pos] == 0xff {
			pos++
		}
		if pos >= len(data) {
			return nil, fmt.Errorf("truncated JPEG marker")
		}
		marker := data[pos]
		pos++
		if marker == 0xda { // Start of scan: preserve all compressed image data.
			if !inserted {
				out = append(out, minimalJPEGOrientationMarker(orientation)...)
				out = append(out, jpegXMPMarker(downloadedAt)...)
				inserted = true
			}
			out = append(out, data[start:]...)
			return out, nil
		}
		if marker == 0xd9 {
			out = append(out, data[start:pos]...)
			return out, nil
		}
		if marker >= 0xd0 && marker <= 0xd7 || marker == 0x01 {
			out = append(out, data[start:pos]...)
			continue
		}
		if pos+2 > len(data) {
			return nil, fmt.Errorf("truncated JPEG segment length")
		}
		length := int(binary.BigEndian.Uint16(data[pos : pos+2]))
		if length < 2 || pos+length > len(data) {
			return nil, fmt.Errorf("invalid JPEG segment length")
		}
		end := pos + length
		// Preserve JFIF, real ICC color profiles and Adobe color transform.
		// APP2 may also contain MPF thumbnails/auxiliary images, so keep it only
		// when its payload has the ICC_PROFILE signature.
		isICC := marker == 0xe2 && bytes.HasPrefix(data[pos+2:end], []byte("ICC_PROFILE\x00"))
		keep := marker == 0xe0 || isICC || marker == 0xee || marker < 0xe0
		if keep {
			out = append(out, data[start:end]...)
		}
		pos = end
	}
	return nil, fmt.Errorf("JPEG has no image scan")
}

func jpegXMPMarker(downloadedAt time.Time) []byte {
	payload := append([]byte("http://ns.adobe.com/xap/1.0/\x00"), safeXMP(downloadedAt)...)
	segment := []byte{0xff, 0xe1, 0, 0}
	binary.BigEndian.PutUint16(segment[2:4], uint16(len(payload)+2))
	return append(segment, payload...)
}

func pngChunk(kind string, payload []byte) []byte {
	chunk := make([]byte, 12+len(payload))
	binary.BigEndian.PutUint32(chunk[:4], uint32(len(payload)))
	copy(chunk[4:8], kind)
	copy(chunk[8:8+len(payload)], payload)
	binary.BigEndian.PutUint32(chunk[8+len(payload):], crc32.ChecksumIEEE(chunk[4:8+len(payload)]))
	return chunk
}

func sanitizePNG(data []byte, downloadedAt time.Time) ([]byte, error) {
	if len(data) < len(pngSignature) || !bytes.Equal(data[:8], pngSignature) {
		return nil, fmt.Errorf("invalid PNG signature")
	}
	out := append([]byte(nil), pngSignature...)
	inserted := false
	for pos := 8; pos+12 <= len(data); {
		length := int(binary.BigEndian.Uint32(data[pos : pos+4]))
		end := pos + 12 + length
		if length < 0 || end > len(data) {
			return nil, fmt.Errorf("invalid PNG chunk length")
		}
		kind := string(data[pos+4 : pos+8])
		if kind == "IDAT" && !inserted {
			payload := append([]byte("XML:com.adobe.xmp\x00\x00\x00\x00\x00"), safeXMP(downloadedAt)...)
			out = append(out, pngChunk("iTXt", payload)...)
			inserted = true
		}
		// Remove old text, EXIF, timestamps and C2PA/JUMBF. Keep color,
		// transparency and animation chunks byte-for-byte.
		remove := kind == "eXIf" || kind == "tEXt" || kind == "zTXt" || kind == "iTXt" || kind == "tIME" || kind == "caBX"
		if !remove {
			out = append(out, data[pos:end]...)
		}
		pos = end
		if kind == "IEND" {
			return out, nil
		}
	}
	return nil, fmt.Errorf("PNG has no IEND chunk")
}

func webPChunk(kind string, payload []byte) []byte {
	chunk := make([]byte, 8+len(payload)+(len(payload)&1))
	copy(chunk[:4], kind)
	binary.LittleEndian.PutUint32(chunk[4:8], uint32(len(payload)))
	copy(chunk[8:], payload)
	return chunk
}

func sanitizeWebP(data []byte, downloadedAt time.Time) ([]byte, error) {
	if len(data) < 12 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
		return nil, fmt.Errorf("invalid WebP signature")
	}
	out := append([]byte(nil), data[:12]...)
	hasVP8X := false
	for pos := 12; pos+8 <= len(data); {
		length := int(binary.LittleEndian.Uint32(data[pos+4 : pos+8]))
		end := pos + 8 + length + (length & 1)
		if length < 0 || end > len(data) {
			return nil, fmt.Errorf("invalid WebP chunk length")
		}
		kind := string(data[pos : pos+4])
		payload := append([]byte(nil), data[pos+8:pos+8+length]...)
		switch kind {
		case "EXIF", "XMP ", "C2PA", "JUMB":
			// Replace old metadata below; remove embedded provenance chunks.
		case "VP8X":
			hasVP8X = true
			if len(payload) > 0 {
				payload[0] &^= 0x08 // Clear EXIF flag.
				payload[0] |= 0x04  // Set XMP flag.
			}
			out = append(out, webPChunk(kind, payload)...)
		default:
			out = append(out, data[pos:end]...)
		}
		pos = end
	}
	// Simple WebP files have no VP8X extended header. Avoid constructing one
	// without decoding dimensions; they remain metadata-free except file times.
	if hasVP8X {
		out = append(out, webPChunk("XMP ", safeXMP(downloadedAt))...)
	}
	binary.LittleEndian.PutUint32(out[4:8], uint32(len(out)-8))
	return out, nil
}

func sanitizeDownloadedImage(path string, downloadedAt time.Time) error {
	if !isSanitizableImage(path) {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read downloaded image: %w", err)
	}
	var cleaned []byte
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg":
		cleaned, err = sanitizeJPEG(data, downloadedAt)
	case ".png":
		cleaned, err = sanitizePNG(data, downloadedAt)
	case ".webp":
		cleaned, err = sanitizeWebP(data, downloadedAt)
	}
	if err != nil {
		return err
	}
	if strings.EqualFold(filepath.Ext(path), ".webp") {
		if len(cleaned) < 12 || string(cleaned[:4]) != "RIFF" || string(cleaned[8:12]) != "WEBP" || int(binary.LittleEndian.Uint32(cleaned[4:8])) != len(cleaned)-8 {
			return fmt.Errorf("verify sanitized WebP: invalid RIFF structure")
		}
	} else if _, _, err := image.DecodeConfig(bytes.NewReader(cleaned)); err != nil {
		return fmt.Errorf("verify sanitized image: %w", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".ytdown-image-*")
	if err != nil {
		return fmt.Errorf("create sanitized image: %w", err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if _, err := temp.Write(cleaned); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write sanitized image: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close sanitized image: %w", err)
	}
	if err := os.Chmod(tempPath, info.Mode().Perm()); err != nil {
		return err
	}
	if err := replaceFileSafely(path, tempPath); err != nil {
		return err
	}
	// Filesystem timestamps reflect the real download/processing time too.
	return os.Chtimes(path, downloadedAt, downloadedAt)
}

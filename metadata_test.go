package main

import "testing"

func TestMetadataStringFallbacks(t *testing.T) {
	data := map[string]interface{}{
		"description": "",
		"caption":     "Facebook caption",
		"uploader_id": "creator-id",
	}
	if got := metadataString(data, "description", "caption"); got != "Facebook caption" {
		t.Fatalf("caption fallback = %q, want %q", got, "Facebook caption")
	}
	if got := metadataString(data, "uploader", "uploader_id"); got != "creator-id" {
		t.Fatalf("creator fallback = %q, want %q", got, "creator-id")
	}
}

func TestMetadataDeepStringFacebookStory(t *testing.T) {
	data := map[string]interface{}{
		"creation_story": map[string]interface{}{
			"message": map[string]interface{}{
				"text": "Nested Facebook caption",
			},
		},
	}
	if got := metadataDeepString(data, "creation_story", "message", "body"); got != "Nested Facebook caption" {
		t.Fatalf("nested caption = %q, want %q", got, "Nested Facebook caption")
	}
}

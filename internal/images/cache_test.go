package images

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestImageCache(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "image_cache_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	cache := NewImageCache(tmpDir)
	ctx := context.Background()

	t.Run("Save and Get Image", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "image/png")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("fake-image-data"))
		}))
		defer server.Close()

		guildID := "guild1"
		charID := "char1"
		url := server.URL

		path, err := cache.SaveImage(ctx, guildID, charID, url)
		if err != nil {
			t.Fatalf("SaveImage failed: %v", err)
		}

		if !strings.HasPrefix(path, tmpDir) {
			t.Errorf("Expected path to be in tmpDir, got %s", path)
		}

		gotPath, err := cache.GetImage(guildID, charID)
		if err != nil {
			t.Fatalf("GetImage failed: %v", err)
		}
		if gotPath != path {
			t.Errorf("expected path %s, got %s", path, gotPath)
		}
	})

	t.Run("Delete Image", func(t *testing.T) {
		guildID := "guild_del"
		charID := "char_del"

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "image/jpeg")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("fake-jpg-data"))
		}))
		defer server.Close()

		path, _ := cache.SaveImage(ctx, guildID, charID, server.URL)

		err := cache.DeleteImage(guildID, charID)
		if err != nil {
			t.Fatalf("DeleteImage failed: %v", err)
		}

		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("Image file should have been deleted: %s", path)
		}
	})

	t.Run("Invalid Protocol", func(t *testing.T) {
		_, err := cache.SaveImage(ctx, "g", "c", "ftp://invalid")
		if err == nil {
			t.Error("expected error for invalid protocol")
		}
	})

	t.Run("ImageToBase64", func(t *testing.T) {
		guildID := "guild_b64"
		charID := "char_b64"

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "image/webp")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("webp-data"))
		}))
		defer server.Close()

		path, _ := cache.SaveImage(ctx, guildID, charID, server.URL)

		b64, err := cache.ImageToBase64(ctx, path)
		if err != nil {
			t.Fatalf("ImageToBase64 failed: %v", err)
		}
		if !strings.HasPrefix(b64, "data:image/webp;base64,") {
			t.Errorf("unexpected base64 prefix: %s", b64)
		}
	})
}

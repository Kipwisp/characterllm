package images

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"characterllm/internal/safehttp"
	"characterllm/internal/search"
)

func TestNewImageClient(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "image_client_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	provider := &search.SearXNGProvider{URL: "http://localhost:8080"}
	client := NewImageClient(provider, tmpDir)

	if client == nil {
		t.Fatal("NewImageClient returned nil")
	}
	if client.GetCache() == nil {
		t.Error("Client cache is nil")
	}
}

func TestClientSaveImage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "image_save_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	client := &Client{Cache: NewImageCache(tmpDir), Fetcher: safehttp.NewFetcher()}
	// The local test servers bind to loopback http, which the safehttp
	// policy rejects; bypass the check for the download mechanics tests.
	client.Fetcher.Validate = func(ctx context.Context, raw string) (string, string, error) {
		return raw, "localhost", nil
	}
	ctx := context.Background()

	serve := func(t *testing.T, body []byte) *httptest.Server {
		t.Helper()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "image/png")
			w.Write(body)
		}))
		t.Cleanup(server.Close)
		return server
	}

	t.Run("Saves Downloaded Image", func(t *testing.T) {
		path, err := client.SaveImage(ctx, "g", "c", serve(t, pngBytes).URL)
		if err != nil {
			t.Fatalf("SaveImage failed: %v", err)
		}
		if !strings.HasSuffix(path, ".png") {
			t.Errorf("expected .png extension, got %s", path)
		}
		got, err := client.GetImage("g", "c")
		if err != nil || got != path {
			t.Errorf("GetImage mismatch: %v (%s vs %s)", err, got, path)
		}
	})

	t.Run("Redirect Rejected", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/final" {
				w.Write(pngBytes)
				return
			}
			http.Redirect(w, r, "/final", http.StatusFound)
		}))
		t.Cleanup(server.Close)

		if _, err := client.SaveImage(ctx, "g", "c", server.URL+"/"); err == nil || !strings.Contains(err.Error(), "redirect") {
			t.Errorf("expected redirect rejection, got %v", err)
		}
	})

	t.Run("Default Policy Rejects Non-HTTPS", func(t *testing.T) {
		client.Fetcher.Validate = safehttp.Validate
		defer func() {
			client.Fetcher.Validate = func(ctx context.Context, raw string) (string, string, error) {
				return raw, "localhost", nil
			}
		}()

		if _, err := client.SaveImage(ctx, "g", "c", serve(t, pngBytes).URL); err == nil || !strings.Contains(err.Error(), "https only") {
			t.Errorf("expected non-https rejection, got %v", err)
		}
	})

	t.Run("Oversized Rejected", func(t *testing.T) {
		big := append(pngBytes, make([]byte, maxDownloadBytes+1-len(pngBytes))...)
		if _, err := client.SaveImage(ctx, "g", "c", serve(t, big).URL); err == nil || !strings.Contains(err.Error(), "maximum size") {
			t.Errorf("expected size rejection, got %v", err)
		}
	})

	t.Run("Non-Image Content Rejected", func(t *testing.T) {
		if _, err := client.SaveImage(ctx, "g", "c", serve(t, []byte("hello world, definitely not an image")).URL); err == nil || !strings.Contains(err.Error(), "recognized image") {
			t.Errorf("expected magic-byte rejection, got %v", err)
		}
	})
}

func TestClientImageToDataURI(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "image_duri_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	client := &Client{Cache: NewImageCache(tmpDir), Fetcher: safehttp.NewFetcher()}
	client.Fetcher.Validate = func(ctx context.Context, raw string) (string, string, error) {
		return raw, "localhost", nil
	}
	ctx := context.Background()

	t.Run("Returns Processed Data URI", func(t *testing.T) {
		src := image.NewRGBA(image.Rect(0, 0, 800, 600))
		for y := 0; y < 600; y++ {
			for x := 0; x < 800; x++ {
				src.Set(x, y, color.RGBA{uint8(x * 255 / 799), uint8(y * 255 / 599), 128, 255})
			}
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, src); err != nil {
			t.Fatal(err)
		}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "image/png")
			w.Write(buf.Bytes())
		}))
		t.Cleanup(server.Close)

		duri, err := client.ImageToDataURI(ctx, server.URL)
		if err != nil {
			t.Fatalf("ImageToDataURI failed: %v", err)
		}
		if !strings.HasPrefix(duri, "data:image/jpeg;base64,") {
			t.Errorf("expected jpeg data URI for opaque image, got prefix %q", duri[:20])
		}

		// The payload must decode to the downscaled dimensions.
		payload := duri[strings.Index(duri, ",")+1:]
		cfg, err := decodeBase64Image(t, payload)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Width != maxImageEdge || cfg.Height != 384 {
			t.Errorf("expected %dx384, got %dx%d", maxImageEdge, cfg.Width, cfg.Height)
		}
	})

	t.Run("Non-Image Rejected", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("not an image"))
		}))
		t.Cleanup(server.Close)

		if _, err := client.ImageToDataURI(ctx, server.URL); err == nil || !strings.Contains(err.Error(), "recognized image") {
			t.Errorf("expected magic-byte rejection, got %v", err)
		}
	})
}

func decodeBase64Image(t *testing.T, b64 string) (image.Config, error) {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return image.Config{}, err
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	return cfg, err
}

func TestClientImageToBase64(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "image_b64_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	webp := append(append(append([]byte("RIFF"), 0, 0, 0, 0), "WEBP"...), "fake-webp"...)
	path := filepath.Join(tmpDir, "guild1_char1.webp")
	if err := os.WriteFile(path, webp, 0644); err != nil {
		t.Fatal(err)
	}

	client := &Client{}
	b64, err := client.ImageToBase64(context.Background(), path)
	if err != nil {
		t.Fatalf("ImageToBase64 failed: %v", err)
	}
	if !strings.HasPrefix(b64, "data:image/webp;base64,") {
		t.Errorf("unexpected base64 prefix: %s", b64)
	}

	if _, err := client.ImageToBase64(context.Background(), filepath.Join(tmpDir, "missing.webp")); err == nil {
		t.Error("expected error for missing file")
	}
}

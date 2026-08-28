package images

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"

	"characterllm/internal/safehttp"
)

func TestClientComposeRow(t *testing.T) {
	solid := func(w, h int) image.Image {
		return image.NewRGBA(image.Rect(0, 0, w, h))
	}

	client := &Client{Cache: NewImageCache(t.TempDir()), Fetcher: safehttp.NewFetcher()}
	client.Fetcher.Validate = func(ctx context.Context, raw string) (string, string, error) {
		return raw, "localhost", nil
	}
	ctx := context.Background()

	serveImage := func(t *testing.T, body []byte) *httptest.Server {
		t.Helper()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "image/png")
			w.Write(body)
		}))
		t.Cleanup(server.Close)
		return server
	}

	ok1 := serveImage(t, encodePNG(t, solid(100, 50)))
	ok2 := serveImage(t, encodePNG(t, solid(200, 200)))
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	t.Cleanup(bad.Close)

	t.Run("Tiles Fetched Images In Row Order", func(t *testing.T) {
		row, included, err := client.ComposeRow(ctx, []string{ok1.URL, bad.URL, ok2.URL}, 5)
		if err != nil {
			t.Fatalf("ComposeRow failed: %v", err)
		}
		if len(included) != 2 || included[0] != ok1.URL || included[1] != ok2.URL {
			t.Errorf("unexpected included urls: %v", included)
		}
		img, err := png.Decode(bytes.NewReader(row))
		if err != nil {
			t.Fatalf("row is not a valid PNG: %v", err)
		}
		// Corner and gutter pixels must be fully transparent.
		alpha := func(x, y int) uint8 {
			return img.At(x, y).(color.NRGBA).A
		}
		if a := alpha(0, 0); a != 0 {
			t.Errorf("top-left corner alpha = %d, want 0", a)
		}
		if a := alpha(rowCellSize, 0); a != 0 {
			t.Errorf("gutter alpha = %d, want 0", a)
		}
		wantW, wantH := 2*rowCellSize+rowGutter, rowCellSize
		if b := img.Bounds(); b.Dx() != wantW || b.Dy() != wantH {
			t.Errorf("row dimensions = %dx%d, want %dx%d", b.Dx(), b.Dy(), wantW, wantH)
		}
	})

	t.Run("Stops At Limit", func(t *testing.T) {
		ok3 := serveImage(t, encodePNG(t, solid(120, 80)))
		row, included, err := client.ComposeRow(ctx, []string{ok1.URL, ok2.URL, ok3.URL}, 2)
		if err != nil {
			t.Fatalf("ComposeRow failed: %v", err)
		}
		if len(included) != 2 || included[0] != ok1.URL || included[1] != ok2.URL {
			t.Errorf("expected the first two urls, got %v", included)
		}
		img, err := png.Decode(bytes.NewReader(row))
		if err != nil {
			t.Fatalf("row is not a valid PNG: %v", err)
		}
		if b := img.Bounds(); b.Dx() != 2*rowCellSize+rowGutter {
			t.Errorf("row width = %d, want %d", b.Dx(), 2*rowCellSize+rowGutter)
		}
	})

	t.Run("All Fetches Fail", func(t *testing.T) {
		if _, included, err := client.ComposeRow(ctx, []string{bad.URL, bad.URL}, 5); err == nil {
			t.Error("expected error when no images can be fetched")
		} else if len(included) != 0 {
			t.Errorf("expected no included urls, got %v", included)
		}
	})
}

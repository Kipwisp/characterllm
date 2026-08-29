package images

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"testing"
)

// testMaxImageEdge is the long-edge cap used by tests that build a Client or
// call processImage directly (the config layer normally guarantees the value).
const testMaxImageEdge = 512

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func decodeProcessed(t *testing.T, data []byte) image.Config {
	t.Helper()
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("processed output does not decode: %v", err)
	}
	return cfg
}

func TestProcessImage(t *testing.T) {
	t.Run("Opaque Photo Scaled to JPEG", func(t *testing.T) {
		src := image.NewRGBA(image.Rect(0, 0, 800, 600))
		for y := 0; y < 600; y++ {
			for x := 0; x < 800; x++ {
				// Smooth gradient
				src.Set(x, y, color.RGBA{uint8(x * 255 / 799), uint8(y * 255 / 599), 128, 255})
			}
		}

		out, ext, err := processImage(encodePNG(t, src), ".png", testMaxImageEdge)
		if err != nil {
			t.Fatalf("processImage failed: %v", err)
		}
		if ext != ".jpg" {
			t.Fatalf("expected .jpg, got %s", ext)
		}
		cfg := decodeProcessed(t, out)
		if cfg.Width != testMaxImageEdge || cfg.Height != 384 {
			t.Errorf("expected %dx384, got %dx%d", testMaxImageEdge, cfg.Width, cfg.Height)
		}
		if len(out) >= 256<<10 {
			t.Errorf("scaled 512px JPEG should be tiny, got %d bytes", len(out))
		}
	})

	t.Run("Custom Cap", func(t *testing.T) {
		src := image.NewRGBA(image.Rect(0, 0, 800, 600))
		for y := 0; y < 600; y++ {
			for x := 0; x < 800; x++ {
				src.Set(x, y, color.RGBA{uint8(x * 255 / 799), uint8(y * 255 / 599), 128, 255})
			}
		}

		out, ext, err := processImage(encodePNG(t, src), ".png", 200)
		if err != nil {
			t.Fatalf("processImage failed: %v", err)
		}
		if ext != ".jpg" {
			t.Fatalf("expected .jpg, got %s", ext)
		}
		cfg := decodeProcessed(t, out)
		if cfg.Width != 200 || cfg.Height != 150 {
			t.Errorf("expected 200x150, got %dx%d", cfg.Width, cfg.Height)
		}
	})

	t.Run("Transparent Image Kept as PNG", func(t *testing.T) {
		src := image.NewNRGBA(image.Rect(0, 0, 800, 600))
		for y := 0; y < 600; y++ {
			for x := 0; x < 800; x++ {
				src.SetNRGBA(x, y, color.NRGBA{200, 50, 50, 255})
			}
		}
		src.SetNRGBA(400, 300, color.NRGBA{0, 0, 0, 0})

		out, ext, err := processImage(encodePNG(t, src), ".png", testMaxImageEdge)
		if err != nil {
			t.Fatalf("processImage failed: %v", err)
		}
		if ext != ".png" {
			t.Fatalf("expected .png, got %s", ext)
		}
		cfg := decodeProcessed(t, out)
		if cfg.Width != testMaxImageEdge || cfg.Height != 384 {
			t.Errorf("expected %dx384, got %dx%d", testMaxImageEdge, cfg.Width, cfg.Height)
		}
		img, _, err := image.Decode(bytes.NewReader(out))
		if err != nil {
			t.Fatal(err)
		}
		if !hasTransparency(img.(*image.NRGBA)) {
			t.Error("transparency should have survived the round trip")
		}
	})

	t.Run("Small Image Not Upscaled", func(t *testing.T) {
		src := image.NewRGBA(image.Rect(0, 0, 100, 80))
		for y := 0; y < 80; y++ {
			for x := 0; x < 100; x++ {
				src.Set(x, y, color.RGBA{10, 200, 30, 255})
			}
		}

		out, ext, err := processImage(encodePNG(t, src), ".png", testMaxImageEdge)
		if err != nil {
			t.Fatalf("processImage failed: %v", err)
		}
		if ext != ".jpg" {
			t.Fatalf("expected .jpg, got %s", ext)
		}
		cfg := decodeProcessed(t, out)
		if cfg.Width != 100 || cfg.Height != 80 {
			t.Errorf("expected 100x80, got %dx%d", cfg.Width, cfg.Height)
		}
	})

	t.Run("Animated GIF Reduced to First Frame", func(t *testing.T) {
		palette := color.Palette{color.RGBA{255, 0, 0, 255}, color.RGBA{0, 255, 0, 255}}
		first := image.NewPaletted(image.Rect(0, 0, 64, 64), palette)
		for x := 0; x < 64; x++ {
			first.SetColorIndex(x, x, 0)
		}
		second := image.NewPaletted(image.Rect(0, 0, 64, 64), palette)
		var buf bytes.Buffer
		if err := gif.EncodeAll(&buf, &gif.GIF{Image: []*image.Paletted{first, second}, Delay: []int{100, 100}}); err != nil {
			t.Fatal(err)
		}
		src := buf.Bytes()

		out, ext, err := processImage(src, ".gif", testMaxImageEdge)
		if err != nil {
			t.Fatalf("processImage failed: %v", err)
		}
		if ext == ".gif" {
			t.Fatalf("animated gif must be re-encoded as a still, got %s", ext)
		}
		cfg := decodeProcessed(t, out)
		if cfg.Width != 64 || cfg.Height != 64 {
			t.Errorf("expected the 64x64 first frame, got %dx%d", cfg.Width, cfg.Height)
		}
	})

	t.Run("Undecodable Content Rejected", func(t *testing.T) {
		if _, _, err := processImage(pngBytes, ".png", testMaxImageEdge); err == nil {
			t.Error("expected error for undecodable content")
		}
	})
}

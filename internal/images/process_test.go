package images

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"testing"
)

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

		out, ext := processImage(encodePNG(t, src), ".png")
		if ext != ".jpg" {
			t.Fatalf("expected .jpg, got %s", ext)
		}
		cfg := decodeProcessed(t, out)
		if cfg.Width != maxImageEdge || cfg.Height != 384 {
			t.Errorf("expected %dx384, got %dx%d", maxImageEdge, cfg.Width, cfg.Height)
		}
		if len(out) >= 256<<10 {
			t.Errorf("scaled 512px JPEG should be tiny, got %d bytes", len(out))
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

		out, ext := processImage(encodePNG(t, src), ".png")
		if ext != ".png" {
			t.Fatalf("expected .png, got %s", ext)
		}
		cfg := decodeProcessed(t, out)
		if cfg.Width != maxImageEdge || cfg.Height != 384 {
			t.Errorf("expected %dx384, got %dx%d", maxImageEdge, cfg.Width, cfg.Height)
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

		out, ext := processImage(encodePNG(t, src), ".png")
		if ext != ".jpg" {
			t.Fatalf("expected .jpg, got %s", ext)
		}
		cfg := decodeProcessed(t, out)
		if cfg.Width != 100 || cfg.Height != 80 {
			t.Errorf("expected 100x80, got %dx%d", cfg.Width, cfg.Height)
		}
	})

	t.Run("Animated GIF Passed Through", func(t *testing.T) {
		palette := color.Palette{color.Black, color.White}
		frames := make([]*image.Paletted, 2)
		for i := range frames {
			frames[i] = image.NewPaletted(image.Rect(0, 0, 16, 16), palette)
		}
		var buf bytes.Buffer
		if err := gif.EncodeAll(&buf, &gif.GIF{Image: frames, Delay: []int{100, 100}}); err != nil {
			t.Fatal(err)
		}
		src := buf.Bytes()

		out, ext := processImage(src, ".gif")
		if ext != ".gif" {
			t.Fatalf("expected .gif, got %s", ext)
		}
		if !bytes.Equal(out, src) {
			t.Error("animated gif should pass through unchanged")
		}
	})

	t.Run("Undecodable Content Passed Through", func(t *testing.T) {
		out, ext := processImage(pngBytes, ".png")
		if ext != ".png" {
			t.Fatalf("expected .png, got %s", ext)
		}
		if !bytes.Equal(out, pngBytes) {
			t.Error("undecodable content should pass through unchanged")
		}
	})
}

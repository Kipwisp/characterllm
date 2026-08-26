package images

import (
	"bytes"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"

	"golang.org/x/image/draw"
	"golang.org/x/image/webp"
)

const (
	// maxImageEdge caps the long edge of cached images.
	maxImageEdge = 512

	// jpegQuality is the re-encode quality for opaque images.
	jpegQuality = 90
)

func init() {
	image.RegisterFormat("webp", "WEBP", webp.Decode, webp.DecodeConfig)
}

var (
	pngMagic  = []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}
	jpegMagic = []byte{0xFF, 0xD8, 0xFF}
	gifMagic  = []byte("GIF8")
	riffMagic = []byte("RIFF")
	webpMagic = []byte("WEBP")
)

// sniffImageType identifies an image by its magic bytes and returns the
// corresponding file extension.
func sniffImageType(b []byte) (string, bool) {
	switch {
	case len(b) >= len(pngMagic) && bytes.HasPrefix(b, pngMagic):
		return ".png", true
	case len(b) >= len(jpegMagic) && bytes.HasPrefix(b, jpegMagic):
		return ".jpg", true
	case len(b) >= len(gifMagic) && bytes.HasPrefix(b, gifMagic):
		return ".gif", true
	case len(b) >= 12 && bytes.HasPrefix(b, riffMagic) && bytes.Equal(b[8:12], webpMagic):
		return ".webp", true
	}
	return "", false
}

// processImage downscales and re-encodes image bytes for avatar use: the long
// edge is capped at maxImageEdge, opaque images are re-encoded as JPEG and
// images with transparency as PNG. Animated GIFs and content that cannot be decoded are returned unchanged with the sniffed extension.
func processImage(data []byte, ext string) ([]byte, string) {
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return data, ext
	}
	if format == "gif" {
		g, err := gif.DecodeAll(bytes.NewReader(data))
		if err == nil && len(g.Image) > 1 {
			return data, ext
		}
	}

	scaled := scaleDown(img, maxImageEdge)

	var buf bytes.Buffer
	if hasTransparency(scaled) {
		if err := png.Encode(&buf, scaled); err != nil {
			return data, ext
		}
		return buf.Bytes(), ".png"
	}
	if err := jpeg.Encode(&buf, scaled, &jpeg.Options{Quality: jpegQuality}); err != nil {
		return data, ext
	}
	return buf.Bytes(), ".jpg"
}

// scaleDown returns img drawn into an NRGBA canvas whose long edge is at most
// maxEdge, preserving aspect ratio.
func scaleDown(img image.Image, maxEdge int) *image.NRGBA {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	sw, sh := w, h
	if w > maxEdge || h > maxEdge {
		if w >= h {
			sw = maxEdge
			sh = h * maxEdge / w
		} else {
			sh = maxEdge
			sw = w * maxEdge / h
		}
		if sw < 1 {
			sw = 1
		}
		if sh < 1 {
			sh = 1
		}
	}
	dst := image.NewNRGBA(image.Rect(0, 0, sw, sh))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Over, nil)
	return dst
}

// hasTransparency reports whether any pixel of an NRGBA image has a reduced
// alpha value.
func hasTransparency(img *image.NRGBA) bool {
	pix := img.Pix
	for i := 3; i < len(pix); i += 4 {
		if pix[i] != 255 {
			return true
		}
	}
	return false
}

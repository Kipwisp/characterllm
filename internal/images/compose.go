package images

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"

	"golang.org/x/image/draw"
)

const (
	// rowCellSize is the edge length of each uniform tile in an options row.
	rowCellSize = 512

	// rowGutter is the transparent gap between adjacent tiles in an options row.
	rowGutter = 16
)

// FetchCandidates downloads each url under the safehttp policy and, for up to
// limit successfully processed images, returns in row order: the processed
// image as a data URI, the url that made it in, plus a single horizontal row
// PNG tiling the images for display (urls that fail to fetch do not count
// against the limit).
func (c *Client) FetchCandidates(ctx context.Context, urls []string, limit int) ([]string, []string, []byte, error) {
	var dataURIs, included []string
	var tiles []image.Image
	for _, url := range urls {
		if limit > 0 && len(tiles) == limit {
			break
		}
		data, ext, err := c.fetchImage(ctx, url)
		if err != nil {
			continue
		}
		img, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			continue
		}
		tiles = append(tiles, img)
		included = append(included, url)
		dataURIs = append(dataURIs, fmt.Sprintf("data:%s;base64,%s", mimeForExt(ext), base64.StdEncoding.EncodeToString(data)))
	}
	if len(tiles) == 0 {
		return nil, nil, nil, fmt.Errorf("no images could be fetched")
	}
	row, err := composeRowFromImages(tiles)
	if err != nil {
		return nil, nil, nil, err
	}
	return dataURIs, included, row, nil
}

// composeRowFromImages tiles the images left to right into a single
// horizontal row on a transparent background and returns it as PNG.
func composeRowFromImages(tiles []image.Image) ([]byte, error) {
	row := image.NewNRGBA(image.Rect(0, 0, len(tiles)*rowCellSize+(len(tiles)-1)*rowGutter, rowCellSize))
	x := 0
	for _, img := range tiles {
		tile := image.NewNRGBA(image.Rect(0, 0, rowCellSize, rowCellSize))
		b := img.Bounds()
		sw, sh := b.Dx(), b.Dy()
		if sw > rowCellSize || sh > rowCellSize {
			if sw >= sh {
				sw, sh = rowCellSize, sh*rowCellSize/sw
			} else {
				sh, sw = rowCellSize, sw*rowCellSize/sh
			}
		}
		ox, oy := (rowCellSize-sw)/2, (rowCellSize-sh)/2
		draw.CatmullRom.Scale(tile, image.Rect(ox, oy, ox+sw, oy+sh), img, b, draw.Over, nil)
		draw.Draw(row, image.Rect(x, 0, x+rowCellSize, rowCellSize), tile, image.Point{}, draw.Over)
		x += rowCellSize + rowGutter
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, row); err != nil {
		return nil, fmt.Errorf("encode row image: %w", err)
	}
	return buf.Bytes(), nil
}

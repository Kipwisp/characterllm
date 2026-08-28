package images

import (
	"bytes"
	"context"
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

// ComposeRow downloads each url under the safehttp policy and tiles the
// successfully decoded images left to right into a single horizontal row,
// stopping once limit images have been fetched (urls that fail to fetch do not
// count against the limit). It returns the row encoded as PNG and the urls that
// made it in, in roworder.
func (c *Client) ComposeRow(ctx context.Context, urls []string, limit int) ([]byte, []string, error) {
	var tiles []image.Image
	var included []string
	for _, url := range urls {
		if limit > 0 && len(tiles) == limit {
			break
		}
		data, _, err := c.fetchImage(ctx, url)
		if err != nil {
			continue
		}
		img, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			continue
		}
		tiles = append(tiles, img)
		included = append(included, url)
	}
	if len(tiles) == 0 {
		return nil, nil, fmt.Errorf("no images could be fetched")
	}

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
		return nil, nil, fmt.Errorf("encode row image: %w", err)
	}
	return buf.Bytes(), included, nil
}

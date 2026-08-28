package images

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"characterllm/internal/safehttp"
	"characterllm/internal/search"
)

// ImageClient defines the interface for searching and caching character images.
type ImageClient interface {
	SearchImages(ctx context.Context, query string, limit int) ([]search.Image, error)
	SaveImage(ctx context.Context, guildID, characterID, url string) (string, error)
	GetImage(guildID, characterID string) (string, error)
	ImageToBase64(ctx context.Context, path string) (string, error)
	// ImageToDataURI downloads the image at url, processes it, and returns it
	// as a data URI suitable for a vision model. Nothing is written to disk.
	ImageToDataURI(ctx context.Context, url string) (string, error)
	// ComposeRow downloads each url, tiles up to limit successfully decoded
	// images into a single horizontal row on a transparent background (urls
	// that fail to fetch do not count against the limit), and returns the
	// PNG-encoded row plus the urls that made it in, in row order.
	ComposeRow(ctx context.Context, urls []string, limit int) ([]byte, []string, error)
	// DeleteImage removes a character's cached image from disk.
	DeleteImage(guildID, characterID string) error
	GetCache() *ImageCache
}

// maxDownloadBytes caps image downloads to bound disk and memory usage. It
// matches Discord's 10 MB avatar upload limit so legitimate avatar photos
// are not rejected.
const maxDownloadBytes = 10 << 20 // 10 MiB

// Client orchestrates image search, download, processing, and caching.
type Client struct {
	Provider search.ImageSearchProvider
	Cache    *ImageCache
	Fetcher  *safehttp.Fetcher
}

// DeleteImage removes a character's cached image from disk.
func (c *Client) DeleteImage(guildID, characterID string) error {
	return c.Cache.DeleteImage(guildID, characterID)
}

// GetCache returns the image cache.
func (c *Client) GetCache() *ImageCache {
	return c.Cache
}

// SearchImages performs an image search using the configured provider.
func (c *Client) SearchImages(ctx context.Context, query string, limit int) ([]search.Image, error) {
	return c.Provider.SearchImages(ctx, query, limit)
}

// SaveImage downloads an image from the given URL (via the safehttp policy),
// downscales and re-encodes it, stores it in the local cache under the
// guild/character key, and returns the local path.
func (c *Client) SaveImage(ctx context.Context, guildID, characterID, url string) (string, error) {
	data, ext, err := c.fetchImage(ctx, url)
	if err != nil {
		return "", err
	}
	return c.Cache.Save(guildID, characterID, ext, data)
}

// ImageToDataURI downloads the image at url (via the safehttp policy),
// processes it, and returns it as a base64 data URI.
func (c *Client) ImageToDataURI(ctx context.Context, url string) (string, error) {
	data, ext, err := c.fetchImage(ctx, url)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("data:%s;base64,%s", mimeForExt(ext), base64.StdEncoding.EncodeToString(data)), nil
}

// fetchImage downloads an image under the safehttp policy, enforces the size
// cap, verifies the magic bytes, and processes the content. It returns the
// processed bytes and the resulting file extension.
func (c *Client) fetchImage(ctx context.Context, url string) ([]byte, string, error) {
	resp, err := c.Fetcher.Get(ctx, url)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	if resp.ContentLength > maxDownloadBytes {
		return nil, "", fmt.Errorf("image exceeds maximum size of %d bytes", maxDownloadBytes)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxDownloadBytes+1))
	if err != nil {
		return nil, "", err
	}
	if int64(len(data)) > maxDownloadBytes {
		return nil, "", fmt.Errorf("image exceeds maximum size of %d bytes", maxDownloadBytes)
	}

	ext, ok := sniffImageType(data)
	if !ok {
		return nil, "", fmt.Errorf("downloaded content is not a recognized image")
	}
	processed, pext := processImage(data, ext)
	return processed, pext, nil
}

// GetImage returns the local path of the cached image for a guild and character.
func (c *Client) GetImage(guildID, characterID string) (string, error) {
	return c.Cache.GetImage(guildID, characterID)
}

// ImageToBase64 reads an image file and returns it as a Base64 data URI, with
// the MIME type derived from the file extension.
func (c *Client) ImageToBase64(ctx context.Context, path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("data:%s;base64,%s", mimeForExt(filepath.Ext(path)), base64.StdEncoding.EncodeToString(data)), nil
}

// mimeForExt maps an image file extension to its MIME type, defaulting to PNG.
func mimeForExt(ext string) string {
	switch ext {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	}
	return "image/png"
}

// NewImageClient is a factory that returns the configured image client.
func NewImageClient(provider search.ImageSearchProvider, cacheDir string) ImageClient {
	return &Client{
		Provider: provider,
		Cache:    NewImageCache(cacheDir),
		Fetcher:  safehttp.NewFetcher(),
	}
}

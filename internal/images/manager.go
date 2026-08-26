package images

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"characterllm/internal/search"
	"characterllm/internal/safehttp"
)

// ImageClient defines the interface for searching and caching character images.
type ImageClient interface {
	SearchImages(ctx context.Context, query string, limit int) ([]search.Image, error)
	SaveImage(ctx context.Context, guildID, characterID, url string) (string, error)
	GetImage(guildID, characterID string) (string, error)
	ImageToBase64(ctx context.Context, path string) (string, error)
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
// guild/character key, and returns the local path. The content must carry a
// recognized image signature.
func (c *Client) SaveImage(ctx context.Context, guildID, characterID, url string) (string, error) {
	resp, err := c.Fetcher.Get(ctx, url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.ContentLength > maxDownloadBytes {
		return "", fmt.Errorf("image exceeds maximum size of %d bytes", maxDownloadBytes)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxDownloadBytes+1))
	if err != nil {
		return "", err
	}
	if int64(len(data)) > maxDownloadBytes {
		return "", fmt.Errorf("image exceeds maximum size of %d bytes", maxDownloadBytes)
	}

	ext, ok := sniffImageType(data)
	if !ok {
		return "", fmt.Errorf("downloaded content is not a recognized image")
	}
	data, ext = processImage(data, ext)

	return c.Cache.Save(guildID, characterID, ext, data)
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

	ext := filepath.Ext(path)
	contentType := "image/png"
	switch ext {
	case ".jpg", ".jpeg":
		contentType = "image/jpeg"
	case ".gif":
		contentType = "image/gif"
	case ".webp":
		contentType = "image/webp"
	}

	encoded := base64.StdEncoding.EncodeToString(data)
	return fmt.Sprintf("data:%s;base64,%s", contentType, encoded), nil
}

// NewImageClient is a factory that returns the configured image client.
func NewImageClient(provider search.ImageSearchProvider, cacheDir string) ImageClient {
	return &Client{
		Provider: provider,
		Cache:    NewImageCache(cacheDir),
		Fetcher:  safehttp.NewFetcher(),
	}
}

package images

import (
	"context"

	"characterllm/internal/search"
)

// ImageClient defines the interface for searching and caching character images.
type ImageClient interface {
	SearchImages(ctx context.Context, query string, limit int) ([]search.Image, error)
	SaveImage(ctx context.Context, guildID, characterID, url string) (string, error)
	ImageToBase64(ctx context.Context, path string) (string, error)
	GetCache() *ImageCache
}

// Client handles image search and caching.
type Client struct {
	Provider search.ImageSearchProvider
	Cache    *ImageCache
}

// GetCache returns the image cache.
func (c *Client) GetCache() *ImageCache {
	return c.Cache
}

// SearchImages performs an image search using the configured provider.
func (c *Client) SearchImages(ctx context.Context, query string, limit int) ([]search.Image, error) {
	return c.Provider.SearchImages(ctx, query, limit)
}

// SaveImage caches an image and returns the local path.
func (c *Client) SaveImage(ctx context.Context, guildID, characterID, url string) (string, error) {
	return c.Cache.SaveImage(ctx, guildID, characterID, url)
}

// ImageToBase64 converts a cached image to a Base64 data URI.
func (c *Client) ImageToBase64(ctx context.Context, path string) (string, error) {
	return c.Cache.ImageToBase64(ctx, path)
}

// NewImageClient is a factory that returns the configured image client.
func NewImageClient(provider search.ImageSearchProvider, cacheDir string) ImageClient {
	return &Client{
		Provider: provider,
		Cache:    NewImageCache(cacheDir),
	}
}

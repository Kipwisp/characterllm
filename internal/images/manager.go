package images

import (
	"fmt"
	"strings"

	"characterllm/internal/config"
	"characterllm/internal/search"
)

// Client handles image search and caching.
type Client struct {
	Provider search.ImageSearchProvider
	Cache    *ImageCache
}

// NewImageClient is a factory that returns the configured image client.
func NewImageClient(cfg *config.Config) (*Client, error) {
	var provider search.ImageSearchProvider
	switch strings.ToLower(cfg.Images.Provider) {
	case "searxng":
		provider = search.NewSearXNGProvider(cfg.Images.SearXNGURL)
	default:
		return nil, fmt.Errorf("unsupported image provider: %s", cfg.Images.Provider)
	}

	return &Client{
		Provider: provider,
		Cache:    NewImageCache(cfg.Images.CacheDir),
	}, nil
}

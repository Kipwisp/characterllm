// Package search provides tools for performing general web searches.
package search

import (
	"context"
	"fmt"
	"strings"
)

// Provider names.
const ProviderSearXNG = "searxng"

// NewProvider creates a search provider based on the provided name.
func NewProvider(name string, url string) (SearchProvider, ImageSearchProvider, error) {
	switch strings.ToLower(name) {
	case ProviderSearXNG:
		if url == "" {
			return nil, nil, fmt.Errorf("SearXNG URL is required for provider %s", ProviderSearXNG)
		}
		p := NewSearXNGProvider(url)
		return p, p, nil
	default:
		return nil, nil, fmt.Errorf("unsupported search provider: %s", name)
	}
}

// SearchResult represents a single result from a web search.
type SearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"content"`
}

// Image represents a single image result from a search provider.
type Image struct {
	URL    string `json:"url"`
	ImgSrc string `json:"img_src"`
	Title  string `json:"title"`
}

// SearchProvider defines the interface for web search implementations.
type SearchProvider interface {
	Search(ctx context.Context, query string, limit int) ([]SearchResult, error)
}

// ImageSearchProvider defines the interface for image search implementations.
type ImageSearchProvider interface {
	SearchImages(ctx context.Context, query string, limit int) ([]Image, error)
}

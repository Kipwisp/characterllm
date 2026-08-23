// Package search provides tools for performing general web searches.
package search

import (
	"context"
)

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

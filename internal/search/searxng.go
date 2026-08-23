package search

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"characterllm/internal/logger"
)

type searxngApiResponse struct {
	Results []SearchResult `json:"results"`
}

type searxngImageApiResponse struct {
	Results []Image `json:"results"`
}

// SearXNGProvider handles requests to a SearXNG instance.
type SearXNGProvider struct {
	URL string
}

// NewSearXNGProvider creates a new provider for interacting with a SearXNG instance.
func NewSearXNGProvider(url string) *SearXNGProvider {
	return &SearXNGProvider{URL: url}
}

// Search performs a web search using SearXNG and returns a list of search results.
func (p *SearXNGProvider) Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	if p.URL == "" {
		return nil, fmt.Errorf("SearXNG URL is not configured")
	}

	params := url.Values{}
	params.Add("q", query)
	params.Add("format", "json")
	params.Add("engines", "google,bing,duckduckgo")

	fullURL := fmt.Sprintf("%s/search?%s", strings.TrimSuffix(p.URL, "/"), params.Encode())
	req, err := http.NewRequestWithContext(ctx, "GET", fullURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create SearXNG request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to request SearXNG: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("SearXNG returned non-OK status: %d", resp.StatusCode)
	}

	var apiResp searxngApiResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("failed to decode SearXNG response: %v", err)
	}

	if len(apiResp.Results) > limit {
		return apiResp.Results[:limit], nil
	}
	return apiResp.Results, nil
}

// SearchImages performs an image search using SearXNG and returns a list of image results.
func (p *SearXNGProvider) SearchImages(ctx context.Context, query string, limit int) ([]Image, error) {
	if p.URL == "" {
		return nil, fmt.Errorf("SearXNG URL is not configured")
	}

	params := url.Values{}
	params.Add("q", query)
	params.Add("categories", "images")
	params.Add("format", "json")

	fullURL := fmt.Sprintf("%s/search?%s", strings.TrimSuffix(p.URL, "/"), params.Encode())
	req, err := http.NewRequestWithContext(ctx, "GET", fullURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create SearXNG request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to request SearXNG: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("SearXNG returned non-OK status: %d", resp.StatusCode)
	}

	var apiResp searxngImageApiResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("failed to decode SearXNG response: %v", err)
	}

	var filtered []Image
	logger.FromContext(ctx).Debug("SearXNG returned images", "count", len(apiResp.Results))
	for _, img := range apiResp.Results {
		url := img.URL
		if img.ImgSrc != "" {
			url = img.ImgSrc
		}

		if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
			img.URL = url
			filtered = append(filtered, img)
		}
	}

	if len(filtered) > limit {
		return filtered[:limit], nil
	}
	return filtered, nil
}

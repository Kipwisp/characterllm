package mocks

import (
	"context"
	"fmt"

	"characterllm/internal/images"
	"characterllm/internal/search"
)

// MockImageClient is a configurable test double for images.ImageClient.
type MockImageClient struct {
	SearchImagesFn   func(ctx context.Context, query string, limit int) ([]search.Image, error)
	SaveImageFn      func(ctx context.Context, guildID, characterID, url string) (string, error)
	GetImageFn       func(guildID, characterID string) (string, error)
	ImageToBase64Fn  func(ctx context.Context, path string) (string, error)
	ImageToDataURIFn func(ctx context.Context, url string) (string, error)
	GetCacheFn       func() *images.ImageCache
}

func (m *MockImageClient) SearchImages(ctx context.Context, query string, limit int) ([]search.Image, error) {
	if m.SearchImagesFn == nil {
		return nil, nil
	}
	return m.SearchImagesFn(ctx, query, limit)
}

func (m *MockImageClient) SaveImage(ctx context.Context, guildID, characterID, url string) (string, error) {
	if m.SaveImageFn == nil {
		return "", nil
	}
	return m.SaveImageFn(ctx, guildID, characterID, url)
}

func (m *MockImageClient) GetImage(guildID, characterID string) (string, error) {
	if m.GetImageFn == nil {
		return "", fmt.Errorf("no cached image")
	}
	return m.GetImageFn(guildID, characterID)
}

func (m *MockImageClient) ImageToBase64(ctx context.Context, path string) (string, error) {
	if m.ImageToBase64Fn == nil {
		return "", nil
	}
	return m.ImageToBase64Fn(ctx, path)
}

func (m *MockImageClient) ImageToDataURI(ctx context.Context, url string) (string, error) {
	if m.ImageToDataURIFn == nil {
		return "", nil
	}
	return m.ImageToDataURIFn(ctx, url)
}

func (m *MockImageClient) GetCache() *images.ImageCache {
	if m.GetCacheFn == nil {
		return nil
	}
	return m.GetCacheFn()
}

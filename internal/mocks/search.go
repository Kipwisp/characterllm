package mocks

import (
	"context"

	"characterllm/internal/search"
)

// MockSearchProvider is a configurable test double for search.SearchProvider.
// When limit is greater than zero and smaller than the number of results, only
// the first limit results are returned.
type MockSearchProvider struct {
	Results []search.SearchResult
	Err     error
}

func (m *MockSearchProvider) Search(ctx context.Context, query string, limit int) ([]search.SearchResult, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	if limit > 0 && len(m.Results) > limit {
		return m.Results[:limit], nil
	}
	return m.Results, nil
}

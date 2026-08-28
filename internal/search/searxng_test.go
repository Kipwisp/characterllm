package search

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSearXNGProvider_Search(t *testing.T) {
	tests := []struct {
		name          string
		serverHandler http.HandlerFunc
		query         string
		limit         int
		expectedCount int
		wantErr       bool
	}{
		{
			name: "search_success",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("q") != "test query" {
					t.Errorf("expected query 'test query', got %s", r.URL.Query().Get("q"))
				}
				if r.URL.Query().Get("engines") != "google,duckduckgo" {
					t.Errorf("expected engines 'google,duckduckgo', got %s", r.URL.Query().Get("engines"))
				}
				if r.URL.Query().Get("categories") != "web" {
					t.Errorf("expected categories 'web', got %s", r.URL.Query().Get("categories"))
				}
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(searxngApiResponse{
					Results: []SearchResult{
						{Title: "Res 1", URL: "url1", Snippet: "Snippet 1"},
						{Title: "Res 2", URL: "url2", Snippet: "Snippet 2"},
						{Title: "Res 3", URL: "url3", Snippet: "Snippet 3"},
					},
				})
			},
			query:         "test query",
			limit:         2,
			expectedCount: 2,
			wantErr:       false,
		},
		{
			name: "search_server_error",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			query:   "test query",
			limit:   2,
			wantErr: true,
		},
		{
			name: "search_invalid_json",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("invalid json"))
			},
			query:   "test query",
			limit:   2,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.serverHandler)
			defer server.Close()

			provider := NewSearXNGProvider(server.URL, "google,duckduckgo")
			results, err := provider.Search(context.Background(), tt.query, tt.limit)

			if (err != nil) != tt.wantErr {
				t.Errorf("Search() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && len(results) != tt.expectedCount {
				t.Errorf("expected %d results, got %d", tt.expectedCount, len(results))
			}
		})
	}
}

func TestSearXNGProvider_SearchImages(t *testing.T) {
	tests := []struct {
		name          string
		serverHandler http.HandlerFunc
		query         string
		limit         int
		expectedCount int
		wantErr       bool
	}{
		{
			name: "images_success",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(searxngImageApiResponse{
					Results: []Image{
						{URL: "http://img1.com", ImgSrc: "http://src1.com", Title: "Img 1"},
						{URL: "http://img2.com", ImgSrc: "", Title: "Img 2"},
						{URL: "invalid", ImgSrc: "invalid", Title: "Img 3"},
						{URL: "http://img4.com", ImgSrc: "http://src4.com", Title: "Img 4"},
					},
				})
			},
			query:         "test images",
			limit:         2,
			expectedCount: 2,
			wantErr:       false,
		},
		{
			name: "images_server_error",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			query:   "test images",
			limit:   2,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.serverHandler)
			defer server.Close()

			provider := NewSearXNGProvider(server.URL, "")
			images, err := provider.SearchImages(context.Background(), tt.query, tt.limit)

			if (err != nil) != tt.wantErr {
				t.Errorf("SearchImages() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && len(images) != tt.expectedCount {
				t.Errorf("expected %d images, got %d", tt.expectedCount, len(images))
			}
		})
	}
}

func TestSearXNGProvider_NoURL(t *testing.T) {
	provider := NewSearXNGProvider("", "")
	_, err := provider.Search(context.Background(), "query", 1)
	if err == nil {
		t.Error("expected error when URL is empty")
	}

	_, err = provider.SearchImages(context.Background(), "query", 1)
	if err == nil {
		t.Error("expected error when URL is empty")
	}
}

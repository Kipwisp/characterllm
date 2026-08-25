package images

import (
	"os"
	"testing"

	"characterllm/internal/search"
)

func TestNewImageClient(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "image_client_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	provider := &search.SearXNGProvider{URL: "http://localhost:8080"}
	client := NewImageClient(provider, tmpDir)

	if client == nil {
		t.Fatal("NewImageClient returned nil")
	}
	if client.GetCache() == nil {
		t.Error("Client cache is nil")
	}
}

package images

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"characterllm/internal/logger"
)

// ImageCache handles local storage of images.
type ImageCache struct {
	Dir string
}

// NewImageCache creates a new ImageCache and ensures the cache directory exists.
func NewImageCache(dir string) *ImageCache {
	if err := os.MkdirAll(dir, 0755); err != nil {
		logger.FromContext(context.Background()).Error("failed to create image cache dir", "dir", dir, "error", err)
	}
	return &ImageCache{Dir: dir}
}

// SaveImage downloads an image from the given URL and saves it to the local cache using a composite key of guild and character.
// It returns the local file path to the saved image.
func (c *ImageCache) SaveImage(ctx context.Context, guildID, characterID, urlStr string) (string, error) {
	if !strings.HasPrefix(urlStr, "http://") && !strings.HasPrefix(urlStr, "https://") {
		return "", fmt.Errorf("invalid image URL protocol: %s", urlStr)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
	if err != nil {
		return "", err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to download image: status %d", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	ext := ".png"
	if strings.Contains(contentType, "jpeg") || strings.Contains(contentType, "jpg") {
		ext = ".jpg"
	} else if strings.Contains(contentType, "gif") {
		ext = ".gif"
	} else if strings.Contains(contentType, "webp") {
		ext = ".webp"
	}

	filename := fmt.Sprintf("%s_%s%s", guildID, characterID, ext)
	path := filepath.Join(c.Dir, filename)

	out, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer out.Close()

	if _, err := io.Copy(out, resp.Body); err != nil {
		return "", err
	}

	return path, nil
}

// GetImage retrieves the local path of the cached image associated with the guild and character.
func (c *ImageCache) GetImage(guildID, characterID string) (string, error) {
	files, err := os.ReadDir(c.Dir)
	if err != nil {
		return "", err
	}

	prefix := fmt.Sprintf("%s_%s.", guildID, characterID)
	for _, f := range files {
		if strings.HasPrefix(f.Name(), prefix) {
			return filepath.Join(c.Dir, f.Name()), nil
		}
	}

	return "", fmt.Errorf("no cached image found for guild %s, character %s", guildID, characterID)
}

// DeleteImage removes the cached image associated with the guild and character from the local filesystem.
func (c *ImageCache) DeleteImage(guildID, characterID string) error {
	files, err := os.ReadDir(c.Dir)
	if err != nil {
		return err
	}

	prefix := fmt.Sprintf("%s_%s.", guildID, characterID)
	for _, f := range files {
		if strings.HasPrefix(f.Name(), prefix) {
			if err := os.Remove(filepath.Join(c.Dir, f.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

// ImageToBase64 reads an image file from the provided path and returns it as a Base64 encoded Data URI.
func (c *ImageCache) ImageToBase64(ctx context.Context, path string) (string, error) {
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

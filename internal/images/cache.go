package images

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"characterllm/internal/logger"
)

// ImageCache is the on-disk store for images: write, read, delete.
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

// Save writes the image bytes to the cache under a composite key of guild and
// character and returns the local file path. Any previous file for the same
// key with a different extension is removed so prefix lookups stay
// unambiguous.
func (c *ImageCache) Save(guildID, characterID, ext string, data []byte) (string, error) {
	filename := fmt.Sprintf("%s_%s%s", guildID, safeName(characterID), ext)
	path := filepath.Join(c.Dir, filename)

	out, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer out.Close()

	if _, err := out.Write(data); err != nil {
		os.Remove(path)
		return "", err
	}

	prefix := fmt.Sprintf("%s_%s.", guildID, safeName(characterID))
	if files, err := os.ReadDir(c.Dir); err == nil {
		for _, f := range files {
			if f.Name() != filename && strings.HasPrefix(f.Name(), prefix) {
				os.Remove(filepath.Join(c.Dir, f.Name()))
			}
		}
	}

	return path, nil
}

// GetImage retrieves the local path of the cached image associated with the guild and character.
func (c *ImageCache) GetImage(guildID, characterID string) (string, error) {
	files, err := os.ReadDir(c.Dir)
	if err != nil {
		return "", err
	}

	prefix := fmt.Sprintf("%s_%s.", guildID, safeName(characterID))
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

	prefix := fmt.Sprintf("%s_%s.", guildID, safeName(characterID))
	for _, f := range files {
		if strings.HasPrefix(f.Name(), prefix) {
			if err := os.Remove(filepath.Join(c.Dir, f.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

// safeName restricts a string to filename-safe characters so it can never
// introduce path separators.
func safeName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}

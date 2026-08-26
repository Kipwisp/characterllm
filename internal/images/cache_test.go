package images

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Real magic bytes so the sniff check passes; the remainder is inert filler
// that fails to decode, exercising the pass-through path.
var pngBytes = append([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}, []byte("fake-png")...)

func TestImageCache(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "image_cache_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	cache := NewImageCache(tmpDir)

	t.Run("Save and Get Image", func(t *testing.T) {
		guildID := "guild1"
		charID := "char1"

		path, err := cache.Save(guildID, charID, ".png", pngBytes)
		if err != nil {
			t.Fatalf("Save failed: %v", err)
		}

		rel, err := filepath.Rel(tmpDir, path)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			t.Fatalf("path escaped the cache dir: %s", path)
		}
		if !strings.HasSuffix(path, ".png") {
			t.Errorf("expected .png extension, got %s", path)
		}

		gotPath, err := cache.GetImage(guildID, charID)
		if err != nil {
			t.Fatalf("GetImage failed: %v", err)
		}
		if gotPath != path {
			t.Errorf("expected path %s, got %s", gotPath, path)
		}

		stored, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile failed: %v", err)
		}
		if string(stored) != string(pngBytes) {
			t.Error("stored bytes do not match saved bytes")
		}
	})

	t.Run("Delete Image", func(t *testing.T) {
		guildID := "guild_del"
		charID := "char_del"

		path, _ := cache.Save(guildID, charID, ".png", pngBytes)

		if err := cache.DeleteImage(guildID, charID); err != nil {
			t.Fatalf("DeleteImage failed: %v", err)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("Image file should have been deleted: %s", path)
		}
	})

	t.Run("Path Traversal in Character ID Contained", func(t *testing.T) {
		path, err := cache.Save("g", "../../evil", ".png", pngBytes)
		if err != nil {
			t.Fatalf("Save failed: %v", err)
		}
		rel, err := filepath.Rel(tmpDir, path)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			t.Fatalf("path escaped the cache dir: %s", path)
		}
	})

	t.Run("Save Replaces Stale File With Different Extension", func(t *testing.T) {
		first, err := cache.Save("g", "c", ".jpg", pngBytes)
		if err != nil {
			t.Fatalf("Save failed: %v", err)
		}
		second, err := cache.Save("g", "c", ".png", pngBytes)
		if err != nil {
			t.Fatalf("Save failed: %v", err)
		}

		if _, err := os.Stat(first); !os.IsNotExist(err) {
			t.Errorf("stale file should have been removed: %s", first)
		}
		got, err := cache.GetImage("g", "c")
		if err != nil {
			t.Fatalf("GetImage failed: %v", err)
		}
		if got != second {
			t.Errorf("expected %s, got %s", second, got)
		}
	})

	t.Run("Evicts Oldest Files When Cache Exceeds Limit", func(t *testing.T) {
		dir, err := os.MkdirTemp("", "image_evict_test")
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(dir)
		c := NewImageCache(dir)

		size := int64(len(pngBytes))
		// Room for the incoming file plus one existing file, so the oldest
		// of the two pre-existing files must go.
		maxCacheBytes = size*2 + 1
		defer func() { maxCacheBytes = 512 << 20 }()

		old, mid := time.Now().Add(-2*time.Hour), time.Now().Add(-time.Hour)
		for name, mod := range map[string]time.Time{"1_old.png": old, "2_mid.png": mid} {
			p := filepath.Join(dir, name)
			if err := os.WriteFile(p, pngBytes, 0644); err != nil {
				t.Fatal(err)
			}
			if err := os.Chtimes(p, mod, mod); err != nil {
				t.Fatal(err)
			}
		}

		if _, err := c.Save("g", "new", ".png", pngBytes); err != nil {
			t.Fatalf("Save failed: %v", err)
		}

		if _, err := os.Stat(filepath.Join(dir, "1_old.png")); !os.IsNotExist(err) {
			t.Error("oldest file should have been evicted")
		}
		if _, err := os.Stat(filepath.Join(dir, "2_mid.png")); err != nil {
			t.Errorf("newer file should have been kept: %v", err)
		}
	})
}

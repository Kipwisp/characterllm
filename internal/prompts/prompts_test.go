package prompts

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTempPrompt(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoad(t *testing.T) {
	system := writeTempPrompt(t, "system.md", "sys")
	compaction := writeTempPrompt(t, "compaction.md", "cmp")
	synthesis := writeTempPrompt(t, "synthesis.md", "syn")
	analyzer := writeTempPrompt(t, "analyzer.md", "ana")

	set, err := Load(system, compaction, synthesis, analyzer)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if set.System != "sys" || set.Compaction != "cmp" || set.Synthesis != "syn" || set.Analyzer != "ana" {
		t.Errorf("unexpected prompt set: %+v", set)
	}
}

func TestLoad_MissingFile(t *testing.T) {
	system := writeTempPrompt(t, "system.md", "sys")

	_, err := Load(system, "/nonexistent/compaction.md", system, system)
	if err == nil {
		t.Fatal("expected error for missing prompt file")
	}
}

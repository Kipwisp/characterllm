// Package prompts loads and caches prompt template files in memory.
package prompts

import (
	"fmt"
	"os"
)

// Set holds all prompt templates used by the bot, cached for the process lifetime.
type Set struct {
	System     string
	Compaction string
	Synthesis  string
	Analyzer   string
}

// Load reads every prompt file into memory.
func Load(systemPath, compactionPath, synthesisPath, analyzerPath string) (*Set, error) {
	set := &Set{}
	entries := []struct {
		name string
		path string
		dst  *string
	}{
		{"system", systemPath, &set.System},
		{"compaction", compactionPath, &set.Compaction},
		{"synthesis", synthesisPath, &set.Synthesis},
		{"analyzer", analyzerPath, &set.Analyzer},
	}

	for _, e := range entries {
		data, err := os.ReadFile(e.path)
		if err != nil {
			return nil, fmt.Errorf("failed to load %s prompt from %s: %w", e.name, e.path, err)
		}
		*e.dst = string(data)
	}

	return set, nil
}

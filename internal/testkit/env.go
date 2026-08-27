// Package testkit provides shared disposable test fixtures: a real session
// manager on a temporary SQLite database, an audit logger on a temporary
// directory, and a config pointed at temporary prompt files.
package testkit

import (
	"os"
	"path/filepath"
	"testing"

	"characterllm/internal/audit"
	"characterllm/internal/config"
	"characterllm/internal/mocks"
	"characterllm/internal/prompts"
	"characterllm/internal/session"
)

// Env is a disposable test environment. All temporary files are cleaned up
// automatically when the test ends.
type Env struct {
	Session *session.Manager
	Audit   *audit.AuditLogger
	Config  *config.Config
	Prompts *prompts.Set
	LLM     *mocks.MockLLMClient
	DBPath  string
}

func NewEnv(t *testing.T) *Env {
	t.Helper()

	tmpDb, err := os.CreateTemp(t.TempDir(), "testkit*.db")
	if err != nil {
		t.Fatal(err)
	}
	tmpDbName := tmpDb.Name()
	tmpDb.Close()

	sm, err := session.NewManager(tmpDbName, "Default Prompt")
	if err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		LLM: config.LLMConfig{
			Model:               "test-model",
			MaxContext:          4096,
			CompactionThreshold: 0.8,
			RecentMemoryWindow:  10,
			SummaryMaxTokens:    1024,
			MaxImages:           2,
		},
		Images: config.ImageConfig{
			Provider:   "searxng",
			SearXNGURL: "http://localhost:8080",
		},
		Prompts: config.PromptConfig{
			AnalyzerPath:  writeTempPrompt(t, "analyzer.md", "dummy analyzer prompt"),
			SynthesisPath: writeTempPrompt(t, "synthesis.md", "dummy synthesis prompt"),
		},
	}

	return &Env{
		Session: sm,
		Audit:   audit.NewAuditLogger(t.TempDir(), true),
		Config:  cfg,
		Prompts: &prompts.Set{
			System:     "{{CHARACTER_DETAILS}} is a helpful bot.{{SUMMARY_CONTEXT}}",
			Compaction: "Summarize the following: {{LENGTH_LIMIT}}",
		},
		LLM:    &mocks.MockLLMClient{},
		DBPath: tmpDbName,
	}
}

func writeTempPrompt(t *testing.T, name, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

package conversation

import (
	"context"
	"fmt"
	"os"
	"testing"

	"characterllm/internal/audit"
	"characterllm/internal/config"
	"characterllm/internal/mocks"
	"characterllm/internal/prompts"
	"characterllm/internal/session"
)

type mockLLMClient = mocks.MockLLMClient

const (
	testGuildID = "guild1"
	testThread  = ""
)

type fixtures struct {
	cfg   *config.Config
	llm   *mockLLMClient
	sm    *session.Manager
	audit *audit.AuditLogger
	ps    *prompts.Set
}

// setupConversation builds the shared test fixtures: a temp SQLite session
// manager with one active character, a mock LLM client, and the prompt set.
func setupConversation(t *testing.T) *fixtures {
	t.Helper()

	tmpDb, err := os.CreateTemp("", "conversation_test*.db")
	if err != nil {
		t.Fatal(err)
	}
	tmpDbName := tmpDb.Name()
	tmpDb.Close()
	t.Cleanup(func() { os.Remove(tmpDbName) })

	sm, err := session.NewManager(tmpDbName, "Default Prompt")
	if err != nil {
		t.Fatal(err)
	}

	tmpLogDir, err := os.MkdirTemp("", "audit_test*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(tmpLogDir) })

	ctx := context.Background()
	if err := sm.SaveCharacterCard(ctx, testGuildID, &session.CharacterCard{
		CharacterID: "char1",
		DisplayName: "TestChar",
		Description: "A test character",
	}, []string{}); err != nil {
		t.Fatal(err)
	}
	if err := sm.SetActiveCharacter(ctx, testGuildID, "char1"); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		LLM: config.LLMConfig{
			Model:               "gpt-4",
			MaxContext:          4096,
			CompactionThreshold: 0.8,
			RecentMemoryWindow:  10,
			SummaryMaxTokens:    1024,
		},
	}

	return &fixtures{
		cfg:   cfg,
		llm:   &mockLLMClient{},
		sm:    sm,
		audit: audit.NewAuditLogger(tmpLogDir),
		ps: &prompts.Set{
			System:     "[CHARACTER_DETAILS] is a helpful bot.[SUMMARY_CONTEXT]",
			Compaction: "Summarize the following: [LENGTH_LIMIT]",
		},
	}
}

// saveCurrent saves the current user message, mirroring the handler which
// persists the incoming message before prompt assembly (Build's precondition).
func (f *fixtures) saveCurrent(t *testing.T) {
	t.Helper()
	if err := f.sm.SaveMessage(context.Background(), testGuildID, testThread, "user", "current"); err != nil {
		t.Fatal(err)
	}
}

// seed saves n user messages into the conversation.
func (f *fixtures) seed(t *testing.T, n int, format string) {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < n; i++ {
		msg := ""
		if format != "" {
			msg = fmt.Sprintf(format, i)
		}
		if err := f.sm.SaveMessage(ctx, "guild1", "", "user", msg); err != nil {
			t.Fatal(err)
		}
	}
}

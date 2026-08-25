package commands

import (
	"os"
	"testing"

	"characterllm/internal/audit"
	"characterllm/internal/config"
	"characterllm/internal/session"
)

func setupCommandTest(t *testing.T) (*mockCommandContext, *mockDiscordSession, string) {
	tmpDb, err := os.CreateTemp("", "cmd_session_test*.db")
	if err != nil {
		t.Fatal(err)
	}
	tmpDbName := tmpDb.Name()
	tmpDb.Close()

	sm, err := session.NewManager(tmpDbName, "test prompt")
	if err != nil {
		t.Fatal(err)
	}

	tmpLogDir, err := os.MkdirTemp("", "cmd_audit_test*")
	if err != nil {
		t.Fatal(err)
	}

	llmMock := &mockLLMClient{}

	tmpPromptDir, err := os.MkdirTemp("", "cmd_prompt_test*")
	if err != nil {
		t.Fatal(err)
	}

	analyzerPath := tmpPromptDir + "/analyzer.md"
	os.WriteFile(analyzerPath, []byte("dummy analyzer prompt"), 0644)
	synthesisPath := tmpPromptDir + "/synthesis.md"
	os.WriteFile(synthesisPath, []byte("dummy synthesis prompt"), 0644)

	cfg := &config.Config{
		LLM: config.LLMConfig{
			Model: "test-model",
		},
		Prompts: config.PromptConfig{
			AnalyzerPath:  analyzerPath,
			SynthesisPath: synthesisPath,
		},
		Images: config.ImageConfig{
			Provider: "searxng",
		},
	}

	auditLogger := audit.NewAuditLogger(tmpLogDir)

	ctx := &mockCommandContext{
		Session: sm,
		LLM:     llmMock,
		Config:  cfg,
		Audit:   auditLogger,
	}

	return ctx, &mockDiscordSession{}, tmpDbName
}

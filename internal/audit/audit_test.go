package audit

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"characterllm/internal/llm"
)

func TestLogConversation(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "audit_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	auditLogger := NewAuditLogger(tmpDir)
	ctx := context.Background()
	guildID := "guild123"
	charID := "char456"
	prompt := "Hello character!"
	reasoning := "User is greeting me."
	response := "Hello there!"
	reqID := "req789"
	history := []llm.Message{
		{Role: "user", Content: "Hi"},
		{Role: "assistant", Content: "Hello!"},
	}
	latency := 100 * time.Millisecond

	auditLogger.LogConversation(ctx, guildID, charID, prompt, reasoning, response, history, latency, reqID)

	filename := filepath.Join(tmpDir, guildID+"_"+charID+".log")
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		t.Fatalf("Log file not created: %s", filename)
	}

	content, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}

	contentStr := string(content)
	expectedParts := []string{
		"REQUEST_ID: " + reqID,
		"GUILD: " + guildID,
		"CHAR: " + charID,
		"LATENCY: 100ms",
		"HISTORY:",
		"[user] Hi",
		"[assistant] Hello!",
		"PROMPT: " + prompt,
		"REASONING:\n" + reasoning,
		"RESPONSE:\n" + response,
	}

	for _, part := range expectedParts {
		if !strings.Contains(contentStr, part) {
			t.Errorf("Log content missing expected part: %q\nContent: %s", part, contentStr)
		}
	}
}

func TestLogConversationNoReasoning(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "audit_test_no_reasoning")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	auditLogger := NewAuditLogger(tmpDir)
	ctx := context.Background()
	guildID := "guild123"
	charID := "char456"
	prompt := "Hello character!"
	reasoning := ""
	response := "Hello there!"
	reqID := "req789"
	history := []llm.Message{}
	latency := 100 * time.Millisecond

	auditLogger.LogConversation(ctx, guildID, charID, prompt, reasoning, response, history, latency, reqID)

	filename := filepath.Join(tmpDir, guildID+"_"+charID+".log")
	content, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(string(content), "REASONING:") {
		t.Error("Log content should not contain REASONING section when reasoning is empty")
	}
}

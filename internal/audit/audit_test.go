package audit

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func logTurn(t *testing.T, a *AuditLogger, guildID, threadID, charID string, turn Turn) {
	t.Helper()
	a.Log(context.Background(), guildID, threadID, charID, "req789", turn)
}

func readLog(t *testing.T, dir, name string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("log file not readable: %v", err)
	}
	return string(content)
}

func TestLogTurn(t *testing.T) {
	tmpDir := t.TempDir()
	a := NewAuditLogger(tmpDir, true)

	logTurn(t, a, "guild123", "", "char456", Turn{
		Kind:      KindChat,
		Model:     "test-model",
		Latency:   100 * time.Millisecond,
		System:    "You are a helpful character.\nStay in persona.",
		Prompt:    "Alice: Hello character!",
		Reasoning: "User is greeting me.",
		Response:  "Hello there!\nHow can I help?",
	})

	content := readLog(t, tmpDir, "guild123_char456.log")

	expectedParts := []string{
		"kind=chat",
		"req=req789",
		"model=test-model",
		"100ms",
		"system prompt:",
		"    You are a helpful character.",
		"    Stay in persona.",
		"prompt:",
		"    Alice: Hello character!",
		"reasoning:",
		"    User is greeting me.",
		"response:",
		"    Hello there!",
		"    How can I help?",
	}
	for _, part := range expectedParts {
		if !strings.Contains(content, part) {
			t.Errorf("log content missing expected part: %q\nContent:\n%s", part, content)
		}
	}
}

func TestLogTurnOmitsEmptySections(t *testing.T) {
	tmpDir := t.TempDir()
	a := NewAuditLogger(tmpDir, true)

	logTurn(t, a, "guild123", "", "char456", Turn{
		Kind:     KindChat,
		Latency:  100 * time.Millisecond,
		Prompt:   "Hello",
		Response: "Hi!",
	})

	content := readLog(t, tmpDir, "guild123_char456.log")

	for _, section := range []string{"reasoning:", "system prompt:", "model="} {
		if strings.Contains(content, section) {
			t.Errorf("log content should not contain %q section:\n%s", section, content)
		}
	}
}

func TestLogTurnThread(t *testing.T) {
	tmpDir := t.TempDir()
	a := NewAuditLogger(tmpDir, true)

	logTurn(t, a, "guild123", "thread999", "char456", Turn{
		Kind:     KindChat,
		Latency:  100 * time.Millisecond,
		Prompt:   "Hello",
		Response: "Hi!",
	})

	content := readLog(t, tmpDir, "guild123_char456_thread999.log")
	if !strings.Contains(content, "thread=thread999") {
		t.Errorf("log content missing thread in header:\n%s", content)
	}
}

func TestSystemPromptRecordedOnceUntilChanged(t *testing.T) {
	tmpDir := t.TempDir()
	a := NewAuditLogger(tmpDir, true)

	turn := Turn{Kind: KindChat, Latency: time.Millisecond, System: "v1 system prompt", Prompt: "p", Response: "r"}
	logTurn(t, a, "guild123", "", "char456", turn)
	logTurn(t, a, "guild123", "", "char456", turn)

	content := readLog(t, tmpDir, "guild123_char456.log")
	if got := strings.Count(content, "system prompt:"); got != 1 {
		t.Fatalf("expected system prompt recorded once, got %d:\n%s", got, content)
	}

	turn.System = "v2 system prompt"
	logTurn(t, a, "guild123", "", "char456", turn)

	content = readLog(t, tmpDir, "guild123_char456.log")
	if got := strings.Count(content, "system prompt:"); got != 2 {
		t.Fatalf("expected system prompt re-recorded after change, got %d:\n%s", got, content)
	}
	if !strings.Contains(content, "v2 system prompt") {
		t.Errorf("log content missing changed system prompt:\n%s", content)
	}
}

func TestLogDisabled(t *testing.T) {
	tmpDir := t.TempDir()
	a := NewAuditLogger(tmpDir, false)

	logTurn(t, a, "guild123", "", "char456", Turn{
		Kind:     KindChat,
		Latency:  time.Millisecond,
		Prompt:   "Hello",
		Response: "Hi!",
	})

	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("expected no log files when disabled, got %d", len(entries))
	}
}

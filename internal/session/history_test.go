package session

import (
	"characterllm/internal/llm"
	"context"
	"os"
	"testing"
)

func TestChatHistoryOperations(t *testing.T) {
	m, tmpFile := setupManager(t)
	defer os.Remove(tmpFile)
	defer m.Close()
	ctx := context.Background()

	guildID := "guild1"
	charID := "char1"
	threadID := "thread1"

	// Setup active character
	err := m.SetActiveCharacter(ctx, guildID, charID)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("Save and Get History", func(t *testing.T) {
		msgs := []struct {
			role    llm.Role
			content string
		}{
			{llm.RoleUser, "Hi"},
			{llm.RoleAssistant, "Hello!"},
			{llm.RoleUser, "How are you?"},
		}

		for _, msg := range msgs {
			err := m.SaveMessage(ctx, guildID, threadID, msg.role, msg.content)
			if err != nil {
				t.Fatalf("SaveMessage failed: %v", err)
			}
		}

		history, err := m.GetHistory(ctx, guildID, threadID, 10, 0)
		if err != nil {
			t.Fatalf("GetHistory failed: %v", err)
		}
		if len(history) != 3 {
			t.Errorf("Expected 3 messages, got %d", len(history))
		}
		if history[0].Text() != "Hi" {
			t.Errorf("Expected first message 'Hi', got %s", history[0].Text())
		}
	})

	t.Run("History Count", func(t *testing.T) {
		count, err := m.GetHistoryCount(ctx, guildID, threadID)
		if err != nil {
			t.Fatalf("GetHistoryCount failed: %v", err)
		}
		if count != 3 {
			t.Errorf("Expected count 3, got %d", count)
		}
	})

	t.Run("Get Oldest Messages", func(t *testing.T) {
		oldest, err := m.GetHistory(ctx, guildID, threadID, 2, 0)
		if err != nil {
			t.Fatalf("GetHistory failed: %v", err)
		}
		if len(oldest) != 2 {
			t.Errorf("Expected 2 oldest messages, got %d", len(oldest))
		}
		if oldest[0].Text() != "Hi" {
			t.Errorf("Expected first oldest message 'Hi', got %s", oldest[0].Text())
		}
	})

	t.Run("Clear History", func(t *testing.T) {
		err := m.ClearHistory(ctx, guildID, threadID)
		if err != nil {
			t.Fatalf("ClearHistory failed: %v", err)
		}
		count, err := m.GetHistoryCount(ctx, guildID, threadID)
		if err != nil {
			t.Fatalf("GetHistoryCount failed: %v", err)
		}
		if count != 0 {
			t.Errorf("Expected count 0 after clear, got %d", count)
		}
	})
}

func TestResolveImageNotes(t *testing.T) {
	m, tmpFile := setupManager(t)
	defer os.Remove(tmpFile)
	defer m.Close()
	ctx := context.Background()

	guildID := "guild1"
	charID := "char1"
	m.SaveCharacterCard(ctx, guildID, &CharacterCard{CharacterID: charID, DisplayName: "Test"})
	m.SetActiveCharacter(ctx, guildID, charID)

	lastUserContent := func(t *testing.T, threadID string) string {
		t.Helper()
		history, err := m.GetHistory(ctx, guildID, threadID, 10, 0)
		if err != nil {
			t.Fatalf("GetHistory failed: %v", err)
		}
		if len(history) == 0 {
			t.Fatal("expected a user row")
		}
		return history[len(history)-1].Text()
	}

	t.Run("resolves markers to notes in order", func(t *testing.T) {
		m.SaveMessage(ctx, guildID, "t1", llm.RoleUser, "header\nAlice: look\n"+ImageMarker(1)+"\nBob: hi\n"+ImageMarker(2)+"\nfooter")
		if _, err := m.ResolveImageNotes(ctx, guildID, "t1", []string{"a dog", "a harbor"}); err != nil {
			t.Fatalf("ResolveImageNotes failed: %v", err)
		}
		want := "header\nAlice: look\n[Image: a dog]\nBob: hi\n[Image: a harbor]\nfooter"
		if got := lastUserContent(t, "t1"); got != want {
			t.Errorf("unexpected row:\n%s\nwant:\n%s", got, want)
		}
	})

	t.Run("keeps a placeholder for markers without a note", func(t *testing.T) {
		m.SaveMessage(ctx, guildID, "t2", llm.RoleUser, "header\nAlice: look\n"+ImageMarker(1)+"\nBob: hi\n"+ImageMarker(2)+"\nfooter")
		if _, err := m.ResolveImageNotes(ctx, guildID, "t2", []string{"a dog"}); err != nil {
			t.Fatalf("ResolveImageNotes failed: %v", err)
		}
		want := "header\nAlice: look\n[Image: a dog]\nBob: hi\n[Image: no description]\nfooter"
		if got := lastUserContent(t, "t2"); got != want {
			t.Errorf("unexpected row:\n%s\nwant:\n%s", got, want)
		}
	})

	t.Run("keeps placeholders when the model returned none", func(t *testing.T) {
		m.SaveMessage(ctx, guildID, "t3", llm.RoleUser, "header\nAlice: look\n"+ImageMarker(1)+"\nfooter")
		if _, err := m.ResolveImageNotes(ctx, guildID, "t3", nil); err != nil {
			t.Fatalf("ResolveImageNotes failed: %v", err)
		}
		want := "header\nAlice: look\n[Image: no description]\nfooter"
		if got := lastUserContent(t, "t3"); got != want {
			t.Errorf("unexpected row:\n%s\nwant:\n%s", got, want)
		}
	})

	t.Run("appends surplus notes at the end", func(t *testing.T) {
		m.SaveMessage(ctx, guildID, "t4", llm.RoleUser, "header\nAlice: look\n"+ImageMarker(1)+"\nfooter")
		if _, err := m.ResolveImageNotes(ctx, guildID, "t4", []string{"a dog", "a harbor"}); err != nil {
			t.Fatalf("ResolveImageNotes failed: %v", err)
		}
		want := "header\nAlice: look\n[Image: a dog]\nfooter\n[Image: a harbor]"
		if got := lastUserContent(t, "t4"); got != want {
			t.Errorf("unexpected row:\n%s\nwant:\n%s", got, want)
		}
	})

	t.Run("no-op without markers", func(t *testing.T) {
		m.SaveMessage(ctx, guildID, "t5", llm.RoleUser, "plain transcript")
		if _, err := m.ResolveImageNotes(ctx, guildID, "t5", []string{"a dog"}); err != nil {
			t.Fatalf("ResolveImageNotes failed: %v", err)
		}
		if got := lastUserContent(t, "t5"); got != "plain transcript" {
			t.Errorf("expected the row unchanged, got %q", got)
		}
	})

	t.Run("errors when no user message exists", func(t *testing.T) {
		if _, err := m.ResolveImageNotes(ctx, "empty-guild", "", []string{"a dog"}); err == nil {
			t.Error("expected an error when no user message exists")
		}
	})
}

func TestGetLastCharacterMessage(t *testing.T) {
	m, tmpFile := setupManager(t)
	defer os.Remove(tmpFile)
	defer m.Close()
	ctx := context.Background()

	guildID := "guild1"
	charID := "char1"
	m.SaveCharacterCard(ctx, guildID, &CharacterCard{CharacterID: charID, DisplayName: "Test"})
	m.SetActiveCharacter(ctx, guildID, charID)

	t.Run("no history returns false", func(t *testing.T) {
		if _, ok := m.GetLastCharacterMessage(ctx, guildID, charID, "1"); ok {
			t.Error("expected no history to report false")
		}
	})

	t.Run("scoped to the named character and thread", func(t *testing.T) {
		m.SaveMessage(ctx, guildID, "1", llm.RoleUser, "a1")
		m.SaveMessage(ctx, guildID, "1", llm.RoleAssistant, "a2")
		m.SaveMessage(ctx, guildID, "2", llm.RoleUser, "b1")
		m.SaveMessage(ctx, guildID, "2", llm.RoleAssistant, "b2")

		got, ok := m.GetLastCharacterMessage(ctx, guildID, charID, "1")
		if !ok || got != "a2" {
			t.Errorf("expected last character message %q from thread 1, got %q (ok=%v)", "a2", got, ok)
		}
		got, ok = m.GetLastCharacterMessage(ctx, guildID, charID, "2")
		if !ok || got != "b2" {
			t.Errorf("expected last character message %q from thread 2, got %q (ok=%v)", "b2", got, ok)
		}

		m.SaveMessage(ctx, guildID, "1", llm.RoleAssistant, "a3")
		got, ok = m.GetLastCharacterMessage(ctx, guildID, charID, "1")
		if !ok || got != "a3" {
			t.Errorf("expected last character message %q from thread 1, got %q (ok=%v)", "a3", got, ok)
		}

		m.SetActiveCharacter(ctx, guildID, "char2")
		m.SaveMessage(ctx, guildID, "3", llm.RoleAssistant, "c1")

		// char1's history is unaffected by char2's new thread.
		got, ok = m.GetLastCharacterMessage(ctx, guildID, charID, "1")
		if !ok || got != "a3" {
			t.Errorf("expected char1's last character message %q, got %q (ok=%v)", "a3", got, ok)
		}
		got, ok = m.GetLastCharacterMessage(ctx, guildID, "char2", "3")
		if !ok || got != "c1" {
			t.Errorf("expected char2's last character message %q, got %q (ok=%v)", "c1", got, ok)
		}
	})

	t.Run("skips user messages", func(t *testing.T) {
		m.SaveMessage(ctx, guildID, "1", llm.RoleUser, "a4")
		got, ok := m.GetLastCharacterMessage(ctx, guildID, charID, "1")
		if !ok || got != "a3" {
			t.Errorf("expected the user's newer message to be skipped, got %q (ok=%v)", got, ok)
		}
	})
}

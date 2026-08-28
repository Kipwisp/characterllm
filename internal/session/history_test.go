package session

import (
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
			role    string
			content string
		}{
			{"user", "Hi"},
			{"assistant", "Hello!"},
			{"user", "How are you?"},
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
		if history[0].Content != "Hi" {
			t.Errorf("Expected first message 'Hi', got %s", history[0].Content)
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
		if oldest[0].Content != "Hi" {
			t.Errorf("Expected first oldest message 'Hi', got %s", oldest[0].Content)
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

func TestAppendToLastUserMessage(t *testing.T) {
	m, tmpFile := setupManager(t)
	defer os.Remove(tmpFile)
	defer m.Close()
	ctx := context.Background()

	guildID := "guild1"
	charID := "char1"
	m.SaveCharacterCard(ctx, guildID, &CharacterCard{CharacterID: charID, DisplayName: "Test"})
	m.SetActiveCharacter(ctx, guildID, charID)

	t.Run("appends to most recent user message only", func(t *testing.T) {
		m.SaveMessage(ctx, guildID, "", "user", "first")
		m.SaveMessage(ctx, guildID, "", "assistant", "reply")
		m.SaveMessage(ctx, guildID, "", "user", "second")

		if err := m.AppendToLastUserMessage(ctx, guildID, "", "\n[Image: a dog]"); err != nil {
			t.Fatalf("AppendToLastUserMessage failed: %v", err)
		}

		history, err := m.GetHistory(ctx, guildID, "", 10, 0)
		if err != nil {
			t.Fatalf("GetHistory failed: %v", err)
		}
		want := []string{"first", "reply", "second\n[Image: a dog]"}
		if len(history) != len(want) {
			t.Fatalf("expected %d rows, got %d", len(want), len(history))
		}
		for i, w := range want {
			if history[i].Content != w {
				t.Errorf("row %d: expected %q, got %q", i, w, history[i].Content)
			}
		}
	})

	t.Run("errors when no user message exists", func(t *testing.T) {
		if err := m.AppendToLastUserMessage(ctx, "empty-guild", "", "\n[Image: x]"); err == nil {
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
		m.SaveMessage(ctx, guildID, "1", "user", "a1")
		m.SaveMessage(ctx, guildID, "1", "assistant", "a2")
		m.SaveMessage(ctx, guildID, "2", "user", "b1")
		m.SaveMessage(ctx, guildID, "2", "assistant", "b2")

		got, ok := m.GetLastCharacterMessage(ctx, guildID, charID, "1")
		if !ok || got != "a2" {
			t.Errorf("expected last character message %q from thread 1, got %q (ok=%v)", "a2", got, ok)
		}
		got, ok = m.GetLastCharacterMessage(ctx, guildID, charID, "2")
		if !ok || got != "b2" {
			t.Errorf("expected last character message %q from thread 2, got %q (ok=%v)", "b2", got, ok)
		}

		m.SaveMessage(ctx, guildID, "1", "assistant", "a3")
		got, ok = m.GetLastCharacterMessage(ctx, guildID, charID, "1")
		if !ok || got != "a3" {
			t.Errorf("expected last character message %q from thread 1, got %q (ok=%v)", "a3", got, ok)
		}

		m.SetActiveCharacter(ctx, guildID, "char2")
		m.SaveMessage(ctx, guildID, "3", "assistant", "c1")

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
		m.SaveMessage(ctx, guildID, "1", "user", "a4")
		got, ok := m.GetLastCharacterMessage(ctx, guildID, charID, "1")
		if !ok || got != "a3" {
			t.Errorf("expected the user's newer message to be skipped, got %q (ok=%v)", got, ok)
		}
	})
}

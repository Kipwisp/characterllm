package session

import (
	"context"
	"os"
	"testing"
)

func setupManager(t *testing.T) (*Manager, string) {
	tmpFile, err := os.CreateTemp("", "session_test*.db")
	if err != nil {
		t.Fatal(err)
	}
	tmpFileName := tmpFile.Name()
	tmpFile.Close()

	m, err := NewManager(tmpFileName, "Default Prompt")
	if err != nil {
		t.Fatal(err)
	}
	return m, tmpFileName
}

func TestCharacterCardOperations(t *testing.T) {
	m, tmpFile := setupManager(t)
	defer os.Remove(tmpFile)
	defer m.Close()
	ctx := context.Background()

	guildID := "guild1"
	card := &CharacterCard{
		CharacterID:  "char1",
		OfficialName: "Official Name",
		DisplayName:  "Display Name",
		Description:  "Character Description",
		Series:       "Series Name",
	}
	t.Run("Save and Get Character Card", func(t *testing.T) {
		err := m.SaveCharacterCard(ctx, guildID, card)
		if err != nil {
			t.Fatalf("SaveCharacterCard failed: %v", err)
		}

		got, err := m.GetCharacterCard(ctx, guildID, "char1")
		if err != nil {
			t.Fatalf("GetCharacterCard failed: %v", err)
		}
		if got == nil || got.OfficialName != card.OfficialName {
			t.Errorf("Expected %s, got %v", card.OfficialName, got)
		}
	})

	t.Run("Get Guild Characters", func(t *testing.T) {
		cards, err := m.GetGuildCharacters(ctx, guildID)
		if err != nil {
			t.Fatalf("GetGuildCharacters failed: %v", err)
		}
		if len(cards) != 1 {
			t.Errorf("Expected 1 card, got %d", len(cards))
		}
	})

	t.Run("Get Non-existent Card", func(t *testing.T) {
		got, err := m.GetCharacterCard(ctx, guildID, "nonexistent")
		if err != nil {
			t.Fatalf("GetCharacterCard failed: %v", err)
		}
		if got != nil {
			t.Errorf("Expected nil for non-existent card, got %v", got)
		}
	})
}

func TestCountCharacterThreads(t *testing.T) {
	m, tmpFile := setupManager(t)
	defer os.Remove(tmpFile)
	defer m.Close()
	ctx := context.Background()

	m.SaveCharacterCard(ctx, "guild1", &CharacterCard{CharacterID: "char1", DisplayName: "A"})
	m.SaveCharacterCard(ctx, "guild1", &CharacterCard{CharacterID: "char2", DisplayName: "B"})
	m.SetActiveCharacter(ctx, "guild1", "char2")

	// Legacy history (no threads rows yet) counts as zero until promoted.
	m.SaveMessage(ctx, "guild1", "thread-a", "user", "for char2")
	count, err := m.CountCharacterThreads(ctx, "guild1", "char1")
	if err != nil || count != 0 {
		t.Errorf("expected 0 for char1, got %d (err %v)", count, err)
	}
	count, err = m.CountCharacterThreads(ctx, "guild1", "char2")
	if err != nil || count != 0 {
		t.Errorf("expected 0 before promotion for char2, got %d (err %v)", count, err)
	}

	// Promotion adds the default thread; created threads are counted.
	if err := m.EnsureDefaultThread(ctx, "guild1", "char2"); err != nil {
		t.Fatalf("EnsureDefaultThread failed: %v", err)
	}
	count, err = m.CountCharacterThreads(ctx, "guild1", "char2")
	if err != nil || count != 1 {
		t.Errorf("expected 1 after promotion for char2, got %d (err %v)", count, err)
	}
	if _, err := m.CreateThread(ctx, "guild1", "char2", "Side quest"); err != nil {
		t.Fatalf("CreateThread failed: %v", err)
	}
	count, err = m.CountCharacterThreads(ctx, "guild1", "char2")
	if err != nil || count != 2 {
		t.Errorf("expected 2 for char2, got %d (err %v)", count, err)
	}

	// Counts the named character, not just the active one.
	count, err = m.CountCharacterThreads(ctx, "guild1", "char1")
	if err != nil || count != 0 {
		t.Errorf("expected 0 for char1, got %d (err %v)", count, err)
	}
}

func TestDeleteCharacterCard(t *testing.T) {
	m, tmpFile := setupManager(t)
	defer os.Remove(tmpFile)
	defer m.Close()
	ctx := context.Background()

	guildID := "guild1"
	m.SaveCharacterCard(ctx, guildID, &CharacterCard{
		CharacterID: "char1",
		DisplayName: "Display Name",
	})
	m.SaveMessage(ctx, guildID, "", "user", "hello")
	m.SetActiveCharacter(ctx, guildID, "char1")
	m.PruneAndSummarize(ctx, guildID, "", "rolling summary", 0)

	if err := m.DeleteCharacterCard(ctx, guildID, "char1"); err != nil {
		t.Fatalf("DeleteCharacterCard failed: %v", err)
	}

	if card, err := m.GetCharacterCard(ctx, guildID, "char1"); err != nil || card != nil {
		t.Errorf("expected card to be gone, got %v (err %v)", card, err)
	}
	if history, err := m.GetHistory(ctx, guildID, "", 10, 0); err != nil || len(history) != 0 {
		t.Errorf("expected history to be gone, got %v (err %v)", history, err)
	}
	if summary, err := m.GetSummary(ctx, guildID, ""); err != nil || summary != "" {
		t.Errorf("expected summary to be gone, got %q (err %v)", summary, err)
	}

	// Deleting an unknown character is a no-op.
	if err := m.DeleteCharacterCard(ctx, guildID, "nosuchchar"); err != nil {
		t.Errorf("deleting unknown character should be a no-op, got %v", err)
	}
}

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

func TestActiveCharacterAndDetails(t *testing.T) {
	m, tmpFile := setupManager(t)
	defer os.Remove(tmpFile)
	defer m.Close()
	ctx := context.Background()

	guildID := "guild1"
	charID := "char1"

	card := &CharacterCard{
		CharacterID: charID,
		DisplayName: "Display Name",
		Description: "Character Description",
	}
	m.SaveCharacterCard(ctx, guildID, card)
	m.SetActiveCharacter(ctx, guildID, charID)

	details, err := m.GetCharacterDetails(ctx, guildID)
	if err != nil {
		t.Fatalf("GetCharacterDetails failed: %v", err)
	}
	if details == nil || details.CharacterID != charID || details.DisplayName != "Display Name" {
		t.Errorf("Unexpected character details: %v", details)
	}
}

func TestImageCandidates(t *testing.T) {
	m, tmpFile := setupManager(t)
	defer os.Remove(tmpFile)
	defer m.Close()
	ctx := context.Background()

	guildID := "guild1"
	urls := []string{"url1", "url2", "url3"}

	err := m.SaveImageCandidates(ctx, guildID, urls)
	if err != nil {
		t.Fatal(err)
	}

	got, err := m.GetImageCandidates(ctx, guildID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0] != "url1" {
		t.Errorf("Unexpected candidates: %v", got)
	}

	err = m.ClearImageCandidates(ctx, guildID)
	if err != nil {
		t.Fatal(err)
	}

	got, err = m.GetImageCandidates(ctx, guildID)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("Expected nil candidates after clear, got %v", got)
	}
}

func TestPruneAndSummarize(t *testing.T) {
	m, tmpFile := setupManager(t)
	defer os.Remove(tmpFile)
	defer m.Close()
	ctx := context.Background()

	guildID := "guild1"
	charID := "char1"
	threadID := "thread1"

	if err := m.SetActiveCharacter(ctx, guildID, charID); err != nil {
		t.Fatal(err)
	}

	msgs := []struct {
		role    string
		content string
	}{
		{"user", "one"},
		{"assistant", "two"},
		{"user", "three"},
		{"assistant", "four"},
		{"user", "five"},
	}
	for _, msg := range msgs {
		if err := m.SaveMessage(ctx, guildID, threadID, msg.role, msg.content); err != nil {
			t.Fatalf("SaveMessage failed: %v", err)
		}
	}

	t.Run("Prune Boundary", func(t *testing.T) {
		if err := m.PruneAndSummarize(ctx, guildID, threadID, "summary of one and two", 2); err != nil {
			t.Fatalf("PruneAndSummarize failed: %v", err)
		}

		history, err := m.GetHistory(ctx, guildID, threadID, 10, 0)
		if err != nil {
			t.Fatalf("GetHistory failed: %v", err)
		}
		if len(history) != 3 {
			t.Fatalf("Expected 3 remaining messages, got %d", len(history))
		}
		if history[0].Content != "three" {
			t.Errorf("Expected first remaining message 'three', got %s", history[0].Content)
		}

		count, err := m.GetHistoryCount(ctx, guildID, threadID)
		if err != nil {
			t.Fatalf("GetHistoryCount failed: %v", err)
		}
		if count != 3 {
			t.Errorf("Expected count 3, got %d", count)
		}
	})

	t.Run("Summary Stored Separately", func(t *testing.T) {
		summary, err := m.GetSummary(ctx, guildID, threadID)
		if err != nil {
			t.Fatalf("GetSummary failed: %v", err)
		}
		if summary != "summary of one and two" {
			t.Errorf("Unexpected summary: %s", summary)
		}
	})

	t.Run("Summary Upsert", func(t *testing.T) {
		if err := m.PruneAndSummarize(ctx, guildID, threadID, "rolled summary", 2); err != nil {
			t.Fatalf("second PruneAndSummarize failed: %v", err)
		}

		summary, err := m.GetSummary(ctx, guildID, threadID)
		if err != nil {
			t.Fatalf("GetSummary failed: %v", err)
		}
		if summary != "rolled summary" {
			t.Errorf("Expected upserted summary, got %s", summary)
		}

		history, err := m.GetHistory(ctx, guildID, threadID, 10, 0)
		if err != nil {
			t.Fatalf("GetHistory failed: %v", err)
		}
		if len(history) != 1 || history[0].Content != "five" {
			t.Errorf("Expected only 'five' remaining, got %v", history)
		}
	})

	t.Run("Clear Removes Summary", func(t *testing.T) {
		if err := m.ClearHistory(ctx, guildID, threadID); err != nil {
			t.Fatalf("ClearHistory failed: %v", err)
		}
		summary, err := m.GetSummary(ctx, guildID, threadID)
		if err != nil {
			t.Fatalf("GetSummary failed: %v", err)
		}
		if summary != "" {
			t.Errorf("Expected empty summary after clear, got %s", summary)
		}
	})
}

func TestGetSummaryEmpty(t *testing.T) {
	m, tmpFile := setupManager(t)
	defer os.Remove(tmpFile)
	defer m.Close()
	ctx := context.Background()

	guildID := "guild1"
	charID := "char1"

	if err := m.SetActiveCharacter(ctx, guildID, charID); err != nil {
		t.Fatal(err)
	}

	summary, err := m.GetSummary(ctx, guildID, "")
	if err != nil {
		t.Fatalf("GetSummary failed: %v", err)
	}
	if summary != "" {
		t.Errorf("Expected empty summary, got %s", summary)
	}
}

func TestPruneAndSummarize_ZeroDeletionsPreservesHistory(t *testing.T) {
	m, tmpFile := setupManager(t)
	defer os.Remove(tmpFile)
	defer m.Close()
	ctx := context.Background()

	guildID := "guild1"
	charID := "char1"
	if err := m.SetActiveCharacter(ctx, guildID, charID); err != nil {
		t.Fatal(err)
	}

	for _, content := range []string{"one", "two", "three"} {
		if err := m.SaveMessage(ctx, guildID, "", "user", content); err != nil {
			t.Fatalf("SaveMessage failed: %v", err)
		}
	}

	// A summary-only upsert (no pruning) must not delete any history.
	if err := m.PruneAndSummarize(ctx, guildID, "", "summary only", 0); err != nil {
		t.Fatalf("PruneAndSummarize failed: %v", err)
	}

	count, err := m.GetHistoryCount(ctx, guildID, "")
	if err != nil {
		t.Fatalf("GetHistoryCount failed: %v", err)
	}
	if count != 3 {
		t.Errorf("Expected all 3 messages preserved, got %d", count)
	}

	history, err := m.GetHistory(ctx, guildID, "", 10, 0)
	if err != nil {
		t.Fatalf("GetHistory failed: %v", err)
	}
	if len(history) != 3 || history[0].Content != "one" {
		t.Errorf("Expected original history intact, got %v", history)
	}

	summary, err := m.GetSummary(ctx, guildID, "")
	if err != nil {
		t.Fatalf("GetSummary failed: %v", err)
	}
	if summary != "summary only" {
		t.Errorf("Expected summary to be stored, got %q", summary)
	}
}

func TestSetCharacterImage(t *testing.T) {
	m, tmpFile := setupManager(t)
	defer os.Remove(tmpFile)
	defer m.Close()
	ctx := context.Background()

	guildID := "guild1"
	m.SaveCharacterCard(ctx, guildID, &CharacterCard{CharacterID: "char1", DisplayName: "C"})

	if err := m.SetCharacterImage(ctx, guildID, "char1", "http://img.example/c.png"); err != nil {
		t.Fatalf("SetCharacterImage failed: %v", err)
	}

	card, err := m.GetCharacterCard(ctx, guildID, "char1")
	if err != nil {
		t.Fatalf("GetCharacterCard failed: %v", err)
	}
	if card.ImageURL != "http://img.example/c.png" {
		t.Errorf("expected image URL on card, got %q", card.ImageURL)
	}

	if err := m.SetCharacterImage(ctx, guildID, "missing", "http://img.example/x.png"); err == nil {
		t.Error("expected error for unknown character")
	}
}

func TestCharacterDetailsIncludesImageURL(t *testing.T) {
	m, tmpFile := setupManager(t)
	defer os.Remove(tmpFile)
	defer m.Close()
	ctx := context.Background()

	guildID := "guild1"
	m.SaveCharacterCard(ctx, guildID, &CharacterCard{CharacterID: "char1", DisplayName: "C"})
	m.SetCharacterImage(ctx, guildID, "char1", "http://img.example/c.png")
	m.SetActiveCharacter(ctx, guildID, "char1")

	details, err := m.GetCharacterDetails(ctx, guildID)
	if err != nil {
		t.Fatalf("GetCharacterDetails failed: %v", err)
	}
	if details.ImageURL != "http://img.example/c.png" {
		t.Errorf("expected image URL in details, got %q", details.ImageURL)
	}
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

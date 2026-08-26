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
		SourceURL:    "http://source.com",
		Modifiers:    "mod1, mod2",
		Scenario:     "A dark forest",
	}
	aliases := []string{"alias1", "alias2"}

	t.Run("Save and Get Character Card", func(t *testing.T) {
		err := m.SaveCharacterCard(ctx, guildID, card, aliases)
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

	t.Run("Get Card by Alias", func(t *testing.T) {
		got, err := m.GetCardByAlias(ctx, guildID, "alias1")
		if err != nil {
			t.Fatalf("GetCardByAlias failed: %v", err)
		}
		if got == nil || got.CharacterID != "char1" {
			t.Errorf("Expected char1, got %v", got)
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
			err := m.SaveMessage(ctx, guildID, threadID, msg.role, msg.content, 0)
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
	m.SaveCharacterCard(ctx, guildID, card, []string{})
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
		tokens  int
	}{
		{"user", "one", 10},
		{"assistant", "two", 20},
		{"user", "three", 30},
		{"assistant", "four", 40},
		{"user", "five", 50},
	}
	for _, msg := range msgs {
		if err := m.SaveMessage(ctx, guildID, threadID, msg.role, msg.content, msg.tokens); err != nil {
			t.Fatalf("SaveMessage failed: %v", err)
		}
	}

	t.Run("Prune Boundary", func(t *testing.T) {
		if err := m.PruneAndSummarize(ctx, guildID, threadID, "summary of one and two", 2, 15); err != nil {
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

	t.Run("Stats Arithmetic", func(t *testing.T) {
		total, err := m.GetTotalTokens(ctx, guildID, threadID)
		if err != nil {
			t.Fatalf("GetTotalTokens failed: %v", err)
		}
		// 150 saved - 30 pruned + 15 summary
		if total != 135 {
			t.Errorf("Expected 135 total tokens, got %d", total)
		}
	})

	t.Run("Summary Upsert", func(t *testing.T) {
		if err := m.PruneAndSummarize(ctx, guildID, threadID, "rolled summary", 2, 25); err != nil {
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

		total, err := m.GetTotalTokens(ctx, guildID, threadID)
		if err != nil {
			t.Fatalf("GetTotalTokens failed: %v", err)
		}
		// 135 - 70 pruned + 25 summary
		if total != 90 {
			t.Errorf("Expected 90 total tokens, got %d", total)
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
		total, err := m.GetTotalTokens(ctx, guildID, threadID)
		if err != nil {
			t.Fatalf("GetTotalTokens failed: %v", err)
		}
		if total != 0 {
			t.Errorf("Expected 0 total tokens after clear, got %d", total)
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

	for i, content := range []string{"one", "two", "three"} {
		if err := m.SaveMessage(ctx, guildID, "", "user", content, 10*i+1); err != nil {
			t.Fatalf("SaveMessage failed: %v", err)
		}
	}

	// A summary-only upsert (no pruning) must not delete any history.
	if err := m.PruneAndSummarize(ctx, guildID, "", "summary only", 0, 15); err != nil {
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
	m.SaveCharacterCard(ctx, guildID, &CharacterCard{CharacterID: "char1", DisplayName: "C"}, nil)

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
	m.SaveCharacterCard(ctx, guildID, &CharacterCard{CharacterID: "char1", DisplayName: "C"}, nil)
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


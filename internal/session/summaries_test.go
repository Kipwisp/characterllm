package session

import (
	"context"
	"os"
	"testing"
)

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

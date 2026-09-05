package session

import (
	"characterllm/internal/llm"
	"context"
	"errors"
	"os"
	"testing"

	_ "modernc.org/sqlite"
)

func TestEnsureDefaultThread(t *testing.T) {
	m, tmpFile := setupManager(t)
	defer os.Remove(tmpFile)
	defer m.Close()
	ctx := context.Background()

	guildID := "guild1"
	charID := "char1"
	m.SaveCharacterCard(ctx, guildID, &CharacterCard{CharacterID: charID, DisplayName: "Test"})

	t.Run("creates the default thread and points active at it", func(t *testing.T) {
		if err := m.EnsureDefaultThread(ctx, guildID, charID); err != nil {
			t.Fatalf("EnsureDefaultThread failed: %v", err)
		}
		threads, err := m.ListThreads(ctx, guildID, charID)
		if err != nil || len(threads) != 1 {
			t.Fatalf("expected 1 thread, got %d (err %v)", len(threads), err)
		}
		if threads[0].ThreadID != "1" || threads[0].Name != "Thread 1" || !threads[0].Active {
			t.Errorf("unexpected default thread: %+v", threads[0])
		}
		active, err := m.GetActiveThreadID(ctx, guildID, charID)
		if err != nil || active != "1" {
			t.Errorf("expected active thread %q, got %q (err %v)", "1", active, err)
		}
	})

	t.Run("idempotent", func(t *testing.T) {
		if err := m.EnsureDefaultThread(ctx, guildID, charID); err != nil {
			t.Fatalf("second EnsureDefaultThread failed: %v", err)
		}
		count, err := m.CountCharacterThreads(ctx, guildID, charID)
		if err != nil || count != 1 {
			t.Errorf("expected still 1 thread, got %d (err %v)", count, err)
		}
	})

	t.Run("does not steal an existing active pointer", func(t *testing.T) {
		thread, err := m.CreateThread(ctx, guildID, charID, "Side quest")
		if err != nil {
			t.Fatalf("CreateThread failed: %v", err)
		}
		if err := m.EnsureDefaultThread(ctx, guildID, charID); err != nil {
			t.Fatalf("EnsureDefaultThread failed: %v", err)
		}
		active, err := m.GetActiveThreadID(ctx, guildID, charID)
		if err != nil || active != thread.ThreadID {
			t.Errorf("expected active thread %q, got %q (err %v)", thread.ThreadID, active, err)
		}
	})

	t.Run("unknown character is a no-op", func(t *testing.T) {
		if err := m.EnsureDefaultThread(ctx, guildID, "ghost"); err != nil {
			t.Fatalf("EnsureDefaultThread for unknown character failed: %v", err)
		}
		if threads, err := m.ListThreads(ctx, guildID, "ghost"); err != nil || len(threads) != 0 {
			t.Errorf("expected no threads for unknown character, got %d (err %v)", len(threads), err)
		}
	})
}

func TestCreateThread(t *testing.T) {
	m, tmpFile := setupManager(t)
	defer os.Remove(tmpFile)
	defer m.Close()
	ctx := context.Background()

	guildID := "guild1"
	charID := "char1"
	m.SaveCharacterCard(ctx, guildID, &CharacterCard{CharacterID: charID, DisplayName: "Test"})
	if err := m.EnsureDefaultThread(ctx, guildID, charID); err != nil {
		t.Fatalf("EnsureDefaultThread failed: %v", err)
	}

	t.Run("mints sequential IDs after the default thread", func(t *testing.T) {
		first, err := m.CreateThread(ctx, guildID, charID, "Alpha")
		if err != nil || first.ThreadID != "2" {
			t.Fatalf("expected first ID 2, got %q (err %v)", first.ThreadID, err)
		}
		second, err := m.CreateThread(ctx, guildID, charID, "Beta")
		if err != nil || second.ThreadID != "3" {
			t.Fatalf("expected second ID 3, got %q (err %v)", second.ThreadID, err)
		}
	})

	t.Run("rejects duplicate names", func(t *testing.T) {
		_, err := m.CreateThread(ctx, guildID, charID, "Alpha")
		if !errors.Is(err, ErrThreadNameTaken) {
			t.Errorf("expected ErrThreadNameTaken, got %v", err)
		}
	})

	t.Run("reuses a deleted ID to fill the gap", func(t *testing.T) {
		cleared, err := m.DeleteThread(ctx, guildID, charID, "3")
		if err != nil || cleared {
			t.Fatalf("expected a plain delete, got cleared=%v (err %v)", cleared, err)
		}
		next, err := m.CreateThread(ctx, guildID, charID, "Gamma")
		if err != nil || next.ThreadID != "3" {
			t.Fatalf("expected gap-filled ID 3, got %q (err %v)", next.ThreadID, err)
		}
	})

	t.Run("new thread becomes active", func(t *testing.T) {
		active, err := m.GetActiveThreadID(ctx, guildID, charID)
		if err != nil || active != "3" {
			t.Errorf("expected active thread 3, got %q (err %v)", active, err)
		}
		threads, err := m.ListThreads(ctx, guildID, charID)
		if err != nil {
			t.Fatalf("ListThreads failed: %v", err)
		}
		if len(threads) == 0 || !threads[0].Active || threads[0].ThreadID != "3" {
			t.Errorf("expected newest thread first and active, got %+v", threads)
		}
	})

	t.Run("scoped per character", func(t *testing.T) {
		m.SaveCharacterCard(ctx, guildID, &CharacterCard{CharacterID: "char2", DisplayName: "Other"})
		if err := m.EnsureDefaultThread(ctx, guildID, "char2"); err != nil {
			t.Fatalf("EnsureDefaultThread failed: %v", err)
		}
		other, err := m.CreateThread(ctx, guildID, "char2", "Alpha")
		if err != nil || other.ThreadID != "2" {
			t.Fatalf("expected char2's first ID 2 (no cross-character clash), got %q (err %v)", other.ThreadID, err)
		}
	})
}

func TestSetActiveThread(t *testing.T) {
	m, tmpFile := setupManager(t)
	defer os.Remove(tmpFile)
	defer m.Close()
	ctx := context.Background()

	guildID := "guild1"
	charID := "char1"
	m.SaveCharacterCard(ctx, guildID, &CharacterCard{CharacterID: charID, DisplayName: "Test"})
	if err := m.EnsureDefaultThread(ctx, guildID, charID); err != nil {
		t.Fatalf("EnsureDefaultThread failed: %v", err)
	}
	if _, err := m.CreateThread(ctx, guildID, charID, "Alpha"); err != nil {
		t.Fatalf("CreateThread failed: %v", err)
	}

	if err := m.SetActiveThread(ctx, guildID, charID, "1"); err != nil {
		t.Fatalf("SetActiveThread failed: %v", err)
	}
	active, err := m.GetActiveThreadID(ctx, guildID, charID)
	if err != nil || active != "1" {
		t.Errorf("expected active thread %q, got %q (err %v)", "1", active, err)
	}

	if err := m.SetActiveThread(ctx, guildID, charID, "999"); err == nil {
		t.Error("expected an error switching to a nonexistent thread")
	}
}

func TestDeleteThread(t *testing.T) {
	m, tmpFile := setupManager(t)
	defer os.Remove(tmpFile)
	defer m.Close()
	ctx := context.Background()

	guildID := "guild1"
	charID := "char1"
	m.SaveCharacterCard(ctx, guildID, &CharacterCard{CharacterID: charID, DisplayName: "Test"})
	m.SetActiveCharacter(ctx, guildID, charID)
	if err := m.EnsureDefaultThread(ctx, guildID, charID); err != nil {
		t.Fatalf("EnsureDefaultThread failed: %v", err)
	}
	second, err := m.CreateThread(ctx, guildID, charID, "Alpha")
	if err != nil {
		t.Fatalf("CreateThread failed: %v", err)
	}
	third, err := m.CreateThread(ctx, guildID, charID, "Beta")
	if err != nil {
		t.Fatalf("CreateThread failed: %v", err)
	}

	t.Run("deleting a non-last thread removes it and its history", func(t *testing.T) {
		m.SaveMessage(ctx, guildID, second.ThreadID, llm.RoleUser, "hello")
		m.SaveMessage(ctx, guildID, third.ThreadID, llm.RoleUser, "stay")
		cleared, err := m.DeleteThread(ctx, guildID, charID, second.ThreadID)
		if err != nil || cleared {
			t.Fatalf("expected a plain delete, got cleared=%v (err %v)", cleared, err)
		}
		if th, _ := m.GetThread(ctx, guildID, charID, second.ThreadID); th != nil {
			t.Errorf("expected thread %s to be gone", second.ThreadID)
		}
		count, _ := m.GetHistoryCount(ctx, guildID, second.ThreadID)
		if count != 0 {
			t.Errorf("expected deleted thread history to be empty, got %d", count)
		}
		if count, _ := m.GetHistoryCount(ctx, guildID, third.ThreadID); count != 1 {
			t.Errorf("expected surviving thread history untouched, got %d", count)
		}
	})

	t.Run("deleting the active thread hands the pointer to the most recent survivor", func(t *testing.T) {
		active, err := m.GetActiveThreadID(ctx, guildID, charID)
		if err != nil || active != third.ThreadID {
			t.Fatalf("expected active thread %s, got %q (err %v)", third.ThreadID, active, err)
		}
		cleared, err := m.DeleteThread(ctx, guildID, charID, third.ThreadID)
		if err != nil || cleared {
			t.Fatalf("expected a plain delete, got cleared=%v (err %v)", cleared, err)
		}
		active, err = m.GetActiveThreadID(ctx, guildID, charID)
		if err != nil || active != "1" {
			t.Errorf("expected active thread handed back to default, got %q (err %v)", active, err)
		}
	})

	t.Run("deleting the last thread only clears it", func(t *testing.T) {
		m.SaveMessage(ctx, guildID, "1", llm.RoleUser, "legacy")
		cleared, err := m.DeleteThread(ctx, guildID, charID, "1")
		if err != nil || !cleared {
			t.Fatalf("expected a clear, got cleared=%v (err %v)", cleared, err)
		}
		count, _ := m.CountCharacterThreads(ctx, guildID, charID)
		if count != 1 {
			t.Errorf("expected the last thread to survive, got %d threads", count)
		}
		if th, _ := m.GetThread(ctx, guildID, charID, "1"); th == nil {
			t.Error("expected the last thread row to survive")
		}
		if count, _ := m.GetHistoryCount(ctx, guildID, "1"); count != 0 {
			t.Errorf("expected the cleared thread history to be empty, got %d", count)
		}
		active, _ := m.GetActiveThreadID(ctx, guildID, charID)
		if active != "1" {
			t.Errorf("expected the cleared thread to stay active, got %q", active)
		}
	})
}

func TestDeleteCharacterCardRemovesThreads(t *testing.T) {
	m, tmpFile := setupManager(t)
	defer os.Remove(tmpFile)
	defer m.Close()
	ctx := context.Background()

	guildID := "guild1"
	charID := "char1"
	m.SaveCharacterCard(ctx, guildID, &CharacterCard{CharacterID: charID, DisplayName: "Test"})
	if err := m.EnsureDefaultThread(ctx, guildID, charID); err != nil {
		t.Fatalf("EnsureDefaultThread failed: %v", err)
	}
	if _, err := m.CreateThread(ctx, guildID, charID, "Alpha"); err != nil {
		t.Fatalf("CreateThread failed: %v", err)
	}

	if err := m.DeleteCharacterCard(ctx, guildID, charID); err != nil {
		t.Fatalf("DeleteCharacterCard failed: %v", err)
	}
	count, err := m.CountCharacterThreads(ctx, guildID, charID)
	if err != nil || count != 0 {
		t.Errorf("expected no threads after card deletion, got %d (err %v)", count, err)
	}
}

func TestGetCharacterDetailsIncludesActiveThread(t *testing.T) {
	m, tmpFile := setupManager(t)
	defer os.Remove(tmpFile)
	defer m.Close()
	ctx := context.Background()

	guildID := "guild1"
	charID := "char1"
	m.SaveCharacterCard(ctx, guildID, &CharacterCard{CharacterID: charID, DisplayName: "Test"})
	m.SetActiveCharacter(ctx, guildID, charID)
	if err := m.EnsureDefaultThread(ctx, guildID, charID); err != nil {
		t.Fatalf("EnsureDefaultThread failed: %v", err)
	}
	thread, err := m.CreateThread(ctx, guildID, charID, "Alpha")
	if err != nil {
		t.Fatalf("CreateThread failed: %v", err)
	}

	details, err := m.GetCharacterDetails(ctx, guildID)
	if err != nil || details == nil {
		t.Fatalf("GetCharacterDetails failed: %v", err)
	}
	if details.ActiveThreadID != thread.ThreadID {
		t.Errorf("expected active thread %q in details, got %q", thread.ThreadID, details.ActiveThreadID)
	}
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
	m.SaveMessage(ctx, "guild1", "thread-a", llm.RoleUser, "for char2")
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

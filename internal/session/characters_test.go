package session

import (
	"context"
	"os"
	"testing"
)

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

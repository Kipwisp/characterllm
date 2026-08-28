package session

import (
	"context"
	"os"
	"testing"
)

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

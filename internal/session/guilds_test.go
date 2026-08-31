package session

import (
	"context"
	"os"
	"testing"
)

func TestAmbientChannelOperations(t *testing.T) {
	m, tmpFile := setupManager(t)
	defer os.Remove(tmpFile)
	defer m.Close()
	ctx := context.Background()

	guildID := "guild1"
	otherGuild := "guild2"

	t.Run("unset returns empty", func(t *testing.T) {
		channels, err := m.GetAmbientChannels(ctx, guildID)
		if err != nil {
			t.Fatalf("GetAmbientChannels failed: %v", err)
		}
		if len(channels) != 0 {
			t.Errorf("expected no ambient channels, got %v", channels)
		}
	})

	t.Run("add and get", func(t *testing.T) {
		if err := m.AddAmbientChannel(ctx, guildID, "chan1"); err != nil {
			t.Fatalf("AddAmbientChannel failed: %v", err)
		}
		channels, err := m.GetAmbientChannels(ctx, guildID)
		if err != nil {
			t.Fatalf("GetAmbientChannels failed: %v", err)
		}
		if len(channels) != 1 || channels[0] != "chan1" {
			t.Errorf("expected [chan1], got %v", channels)
		}
	})

	t.Run("add rejects an empty channel ID", func(t *testing.T) {
		if err := m.AddAmbientChannel(ctx, guildID, ""); err == nil {
			t.Error("expected an error adding an empty channel ID")
		}
	})

	t.Run("add does not touch other guilds", func(t *testing.T) {
		channels, err := m.GetAmbientChannels(ctx, otherGuild)
		if err != nil {
			t.Fatalf("GetAmbientChannels failed: %v", err)
		}
		if len(channels) != 0 {
			t.Errorf("expected no ambient channels for the other guild, got %v", channels)
		}
	})

	t.Run("duplicate add is a no-op", func(t *testing.T) {
		if err := m.AddAmbientChannel(ctx, guildID, "chan1"); err != nil {
			t.Fatalf("AddAmbientChannel failed: %v", err)
		}
		channels, err := m.GetAmbientChannels(ctx, guildID)
		if err != nil {
			t.Fatalf("GetAmbientChannels failed: %v", err)
		}
		if len(channels) != 1 || channels[0] != "chan1" {
			t.Errorf("expected [chan1], got %v", channels)
		}
	})

	t.Run("adds accumulate and sort", func(t *testing.T) {
		if err := m.AddAmbientChannel(ctx, guildID, "chan3"); err != nil {
			t.Fatalf("AddAmbientChannel failed: %v", err)
		}
		if err := m.AddAmbientChannel(ctx, guildID, "chan2"); err != nil {
			t.Fatalf("AddAmbientChannel failed: %v", err)
		}
		channels, err := m.GetAmbientChannels(ctx, guildID)
		if err != nil {
			t.Fatalf("GetAmbientChannels failed: %v", err)
		}
		want := []string{"chan1", "chan2", "chan3"}
		if len(channels) != len(want) {
			t.Fatalf("expected %v, got %v", want, channels)
		}
		for i := range want {
			if channels[i] != want[i] {
				t.Fatalf("expected %v, got %v", want, channels)
			}
		}
	})

	t.Run("remove one", func(t *testing.T) {
		if err := m.RemoveAmbientChannel(ctx, guildID, "chan2"); err != nil {
			t.Fatalf("RemoveAmbientChannel failed: %v", err)
		}
		channels, err := m.GetAmbientChannels(ctx, guildID)
		if err != nil {
			t.Fatalf("GetAmbientChannels failed: %v", err)
		}
		if len(channels) != 2 || channels[0] != "chan1" || channels[1] != "chan3" {
			t.Errorf("expected [chan1 chan3], got %v", channels)
		}
	})

	t.Run("removing a non-member is a no-op", func(t *testing.T) {
		if err := m.RemoveAmbientChannel(ctx, guildID, "ghost"); err != nil {
			t.Fatalf("RemoveAmbientChannel failed: %v", err)
		}
		channels, _ := m.GetAmbientChannels(ctx, guildID)
		if len(channels) != 2 {
			t.Errorf("expected the set to be unchanged, got %v", channels)
		}
	})

	t.Run("remove does not touch other guilds", func(t *testing.T) {
		if err := m.AddAmbientChannel(ctx, otherGuild, "chan1"); err != nil {
			t.Fatalf("AddAmbientChannel failed: %v", err)
		}
		if err := m.RemoveAmbientChannel(ctx, guildID, "chan1"); err != nil {
			t.Fatalf("RemoveAmbientChannel failed: %v", err)
		}
		channels, _ := m.GetAmbientChannels(ctx, otherGuild)
		if len(channels) != 1 || channels[0] != "chan1" {
			t.Errorf("expected the other guild's channel to survive, got %v", channels)
		}
	})

	t.Run("clear", func(t *testing.T) {
		if err := m.ClearAmbientChannels(ctx, otherGuild); err != nil {
			t.Fatalf("ClearAmbientChannels failed: %v", err)
		}
		channels, err := m.GetAmbientChannels(ctx, otherGuild)
		if err != nil {
			t.Fatalf("GetAmbientChannels failed: %v", err)
		}
		if len(channels) != 0 {
			t.Errorf("expected the set cleared, got %v", channels)
		}
		if channels, _ := m.GetAmbientChannels(ctx, guildID); len(channels) != 1 || channels[0] != "chan3" {
			t.Errorf("expected the other guild's set to survive, got %v", channels)
		}
	})
}

func TestListAmbientChannels(t *testing.T) {
	m, tmpFile := setupManager(t)
	defer os.Remove(tmpFile)
	defer m.Close()
	ctx := context.Background()

	t.Run("empty when none set", func(t *testing.T) {
		channels, err := m.ListAmbientChannels(ctx)
		if err != nil {
			t.Fatalf("ListAmbientChannels failed: %v", err)
		}
		if len(channels) != 0 {
			t.Errorf("expected no ambient channels, got %v", channels)
		}
	})

	t.Run("lists only set guilds with sorted channels", func(t *testing.T) {
		if err := m.AddAmbientChannel(ctx, "guild1", "chan1"); err != nil {
			t.Fatalf("AddAmbientChannel failed: %v", err)
		}
		if err := m.AddAmbientChannel(ctx, "guild1", "chan0"); err != nil {
			t.Fatalf("AddAmbientChannel failed: %v", err)
		}
		if err := m.AddAmbientChannel(ctx, "guild2", "chan2"); err != nil {
			t.Fatalf("AddAmbientChannel failed: %v", err)
		}

		channels, err := m.ListAmbientChannels(ctx)
		if err != nil {
			t.Fatalf("ListAmbientChannels failed: %v", err)
		}
		if len(channels) != 2 {
			t.Fatalf("expected 2 guilds, got %v", channels)
		}
		if got := channels["guild1"]; len(got) != 2 || got[0] != "chan0" || got[1] != "chan1" {
			t.Errorf("expected guild1 -> [chan0 chan1], got %v", got)
		}
		if got := channels["guild2"]; len(got) != 1 || got[0] != "chan2" {
			t.Errorf("expected guild2 -> [chan2], got %v", got)
		}
	})

	t.Run("guild with an emptied set is omitted", func(t *testing.T) {
		if err := m.AddAmbientChannel(ctx, "guild3", "chan3"); err != nil {
			t.Fatalf("AddAmbientChannel failed: %v", err)
		}
		if err := m.ClearAmbientChannels(ctx, "guild3"); err != nil {
			t.Fatalf("ClearAmbientChannels failed: %v", err)
		}
		channels, err := m.ListAmbientChannels(ctx)
		if err != nil {
			t.Fatalf("ListAmbientChannels failed: %v", err)
		}
		if _, ok := channels["guild3"]; ok {
			t.Errorf("expected guild3 to be absent after clear, got %v", channels)
		}
	})
}

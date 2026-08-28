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
		channel, err := m.GetAmbientChannel(ctx, guildID)
		if err != nil {
			t.Fatalf("GetAmbientChannel failed: %v", err)
		}
		if channel != "" {
			t.Errorf("expected empty ambient channel, got %q", channel)
		}
	})

	t.Run("set and get", func(t *testing.T) {
		if err := m.SetAmbientChannel(ctx, guildID, "chan1"); err != nil {
			t.Fatalf("SetAmbientChannel failed: %v", err)
		}
		channel, err := m.GetAmbientChannel(ctx, guildID)
		if err != nil {
			t.Fatalf("GetAmbientChannel failed: %v", err)
		}
		if channel != "chan1" {
			t.Errorf("expected chan1, got %q", channel)
		}
	})

	t.Run("set does not touch other guilds", func(t *testing.T) {
		channel, err := m.GetAmbientChannel(ctx, otherGuild)
		if err != nil {
			t.Fatalf("GetAmbientChannel failed: %v", err)
		}
		if channel != "" {
			t.Errorf("expected empty ambient channel, got %q", channel)
		}
	})

	t.Run("update replaces", func(t *testing.T) {
		if err := m.SetAmbientChannel(ctx, guildID, "chan2"); err != nil {
			t.Fatalf("SetAmbientChannel failed: %v", err)
		}
		channel, err := m.GetAmbientChannel(ctx, guildID)
		if err != nil {
			t.Fatalf("GetAmbientChannel failed: %v", err)
		}
		if channel != "chan2" {
			t.Errorf("expected chan2, got %q", channel)
		}
	})

	t.Run("clear", func(t *testing.T) {
		if err := m.SetAmbientChannel(ctx, guildID, ""); err != nil {
			t.Fatalf("SetAmbientChannel failed: %v", err)
		}
		channel, err := m.GetAmbientChannel(ctx, guildID)
		if err != nil {
			t.Fatalf("GetAmbientChannel failed: %v", err)
		}
		if channel != "" {
			t.Errorf("expected empty ambient channel after clear, got %q", channel)
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

	t.Run("lists only set guilds", func(t *testing.T) {
		if err := m.SetAmbientChannel(ctx, "guild1", "chan1"); err != nil {
			t.Fatalf("SetAmbientChannel failed: %v", err)
		}
		if err := m.SetAmbientChannel(ctx, "guild2", "chan2"); err != nil {
			t.Fatalf("SetAmbientChannel failed: %v", err)
		}
		if err := m.SetAmbientChannel(ctx, "guild3", ""); err != nil {
			t.Fatalf("SetAmbientChannel failed: %v", err)
		}

		channels, err := m.ListAmbientChannels(ctx)
		if err != nil {
			t.Fatalf("ListAmbientChannels failed: %v", err)
		}
		if len(channels) != 2 {
			t.Fatalf("expected 2 ambient channels, got %v", channels)
		}
		if channels["guild1"] != "chan1" || channels["guild2"] != "chan2" {
			t.Errorf("unexpected ambient channels: %v", channels)
		}
	})
}

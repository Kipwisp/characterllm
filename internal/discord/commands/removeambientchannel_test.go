package commands

import (
	"context"
	"os"
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestRemoveAmbientChannelCmd_Execute(t *testing.T) {
	deps, s, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)
	ctx := context.Background()
	guildID := "guild1"
	cmd := &removeAmbientChannelCmd{session: deps.Session}

	s.GuildChannelsFn = func(guildID string) ([]*discordgo.Channel, error) {
		return []*discordgo.Channel{
			{ID: "chan-invoke", Name: "invoke", Type: discordgo.ChannelTypeGuildText},
			{ID: "chan2", Name: "lobby", Type: discordgo.ChannelTypeGuildText},
			{ID: "chan3", Name: "gossip", Type: discordgo.ChannelTypeGuildText},
		}, nil
	}

	var content string
	s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
		content = response.Data.Content
		return nil
	}

	t.Run("removing from an empty set notes none set", func(t *testing.T) {
		if err := cmd.Execute(ctx, s, newAmbientInteraction("removeambientchannel")); err != nil {
			t.Fatalf("Execute failed: %v", err)
		}
		if content != "No ambient channels are set for this guild." {
			t.Errorf("unexpected reply %q", content)
		}
	})

	t.Run("remove all from an empty set notes none set", func(t *testing.T) {
		if err := cmd.Execute(ctx, s, newAmbientInteraction("removeambientchannel", stringOption("channel", ambientRemoveAllKey))); err != nil {
			t.Fatalf("Execute failed: %v", err)
		}
		if content != "No ambient channels are set for this guild." {
			t.Errorf("unexpected reply %q", content)
		}
	})

	if err := deps.Session.AddAmbientChannel(ctx, guildID, "chan-invoke"); err != nil {
		t.Fatalf("AddAmbientChannel failed: %v", err)
	}
	if err := deps.Session.AddAmbientChannel(ctx, guildID, "chan2"); err != nil {
		t.Fatalf("AddAmbientChannel failed: %v", err)
	}
	if err := deps.Session.AddAmbientChannel(ctx, guildID, "chan3"); err != nil {
		t.Fatalf("AddAmbientChannel failed: %v", err)
	}

	t.Run("removing a non-member is a note", func(t *testing.T) {
		if err := cmd.Execute(ctx, s, newAmbientInteraction("removeambientchannel", stringOption("channel", "ghost"))); err != nil {
			t.Fatalf("Execute failed: %v", err)
		}
		got, _ := deps.Session.GetAmbientChannels(ctx, guildID)
		if len(got) != 3 {
			t.Fatalf("expected the set unchanged, got %v", got)
		}
		if content != "ghost is not an ambient channel." {
			t.Errorf("unexpected reply %q", content)
		}
	})

	t.Run("defaults to the invoking channel", func(t *testing.T) {
		if err := cmd.Execute(ctx, s, newAmbientInteraction("removeambientchannel")); err != nil {
			t.Fatalf("Execute failed: %v", err)
		}
		got, err := deps.Session.GetAmbientChannels(ctx, guildID)
		if err != nil || len(got) != 2 || got[0] != "chan2" || got[1] != "chan3" {
			t.Fatalf("expected [chan2 chan3], got %v (err %v)", got, err)
		}
		if content != "#invoke removed from ambient channels." {
			t.Errorf("unexpected reply %q", content)
		}
	})

	t.Run("removes via option", func(t *testing.T) {
		if err := cmd.Execute(ctx, s, newAmbientInteraction("removeambientchannel", stringOption("channel", "chan2"))); err != nil {
			t.Fatalf("Execute failed: %v", err)
		}
		got, err := deps.Session.GetAmbientChannels(ctx, guildID)
		if err != nil || len(got) != 1 || got[0] != "chan3" {
			t.Fatalf("expected [chan3], got %v (err %v)", got, err)
		}
		if content != "#lobby removed from ambient channels." {
			t.Errorf("unexpected reply %q", content)
		}
	})

	t.Run("removing the last member reports removed", func(t *testing.T) {
		if err := cmd.Execute(ctx, s, newAmbientInteraction("removeambientchannel", stringOption("channel", "chan3"))); err != nil {
			t.Fatalf("Execute failed: %v", err)
		}
		got, err := deps.Session.GetAmbientChannels(ctx, guildID)
		if err != nil || len(got) != 0 {
			t.Fatalf("expected the set empty, got %v (err %v)", got, err)
		}
		if content != "#gossip removed from ambient channels." {
			t.Errorf("unexpected reply %q", content)
		}
	})

	if err := deps.Session.AddAmbientChannel(ctx, guildID, "chan2"); err != nil {
		t.Fatalf("AddAmbientChannel failed: %v", err)
	}
	if err := deps.Session.AddAmbientChannel(ctx, guildID, "chan3"); err != nil {
		t.Fatalf("AddAmbientChannel failed: %v", err)
	}

	t.Run("remove all", func(t *testing.T) {
		if err := cmd.Execute(ctx, s, newAmbientInteraction("removeambientchannel", stringOption("channel", ambientRemoveAllKey))); err != nil {
			t.Fatalf("Execute failed: %v", err)
		}
		got, err := deps.Session.GetAmbientChannels(ctx, guildID)
		if err != nil || len(got) != 0 {
			t.Fatalf("expected the set empty, got %v (err %v)", got, err)
		}
		if content != "Ambient channels cleared — I'll stop speaking on my own." {
			t.Errorf("unexpected reply %q", content)
		}
	})
}

func TestAutocompleteRemoveAmbientChannels(t *testing.T) {
	deps, _, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)
	sm := deps.Session

	s := &mockDiscordSession{
		GuildChannelsFn: func(guildID string) ([]*discordgo.Channel, error) {
			return []*discordgo.Channel{
				{ID: "c1", Name: "alpha", Type: discordgo.ChannelTypeGuildText},
				{ID: "c2", Name: "bravo", Type: discordgo.ChannelTypeGuildText},
				{ID: "c3", Name: "voice", Type: discordgo.ChannelTypeGuildVoice},
			}, nil
		},
	}

	t.Run("no choices while the set is empty", func(t *testing.T) {
		if choices := autocompleteRemoveAmbientChannels(context.Background(), sm, s, "guild1", ""); len(choices) != 0 {
			t.Errorf("expected no choices, got %v", choices)
		}
	})

	if err := sm.AddAmbientChannel(context.Background(), "guild1", "c2"); err != nil {
		t.Fatalf("AddAmbientChannel failed: %v", err)
	}
	if err := sm.AddAmbientChannel(context.Background(), "guild1", "c3"); err != nil {
		t.Fatalf("AddAmbientChannel failed: %v", err)
	}

	t.Run("offers remove all and the set members only", func(t *testing.T) {
		choices := autocompleteRemoveAmbientChannels(context.Background(), sm, s, "guild1", "")
		if len(choices) != 3 {
			t.Fatalf("expected 3 choices, got %d", len(choices))
		}
		if choices[0].Name != "Remove all" || choices[0].Value != "all" {
			t.Errorf("unexpected leading choice %v", choices[0])
		}
		if choices[1].Name != "#bravo" || choices[1].Value != "c2" {
			t.Errorf("unexpected choice %v", choices[1])
		}
		if choices[2].Name != "#voice" || choices[2].Value != "c3" {
			t.Errorf("expected non-text channels excluded, got %v", choices[2])
		}
	})

	t.Run("filters members and the remove all label", func(t *testing.T) {
		choices := autocompleteRemoveAmbientChannels(context.Background(), sm, s, "guild1", "br")
		if len(choices) != 1 {
			t.Fatalf("expected 1 choice, got %d", len(choices))
		}
		if choices[0].Name != "#bravo" {
			t.Errorf("unexpected choice %v", choices[0])
		}
		choices = autocompleteRemoveAmbientChannels(context.Background(), sm, s, "guild1", "remove")
		if len(choices) != 1 || choices[0].Value != "all" {
			t.Errorf("expected only the Remove all choice, got %v", choices)
		}
	})
}

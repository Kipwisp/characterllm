package commands

import (
	"context"
	"os"
	"testing"

	"github.com/bwmarrin/discordgo"
)

func newAmbientInteraction(name string, opts ...*discordgo.ApplicationCommandInteractionDataOption) *discordgo.InteractionCreate {
	i := &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			Type: discordgo.InteractionApplicationCommand,
			Data: discordgo.ApplicationCommandInteractionData{
				Name:    name,
				Options: opts,
			},
		},
	}
	i.GuildID = "guild1"
	i.ChannelID = "chan-invoke"
	return i
}

func TestAddAmbientChannelCmd_Execute(t *testing.T) {
	deps, s, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)
	ctx := context.Background()
	guildID := "guild1"
	cmd := &addAmbientChannelCmd{session: deps.Session}

	s.GuildChannelsFn = func(guildID string) ([]*discordgo.Channel, error) {
		return []*discordgo.Channel{
			{ID: "chan-invoke", Name: "invoke", Type: discordgo.ChannelTypeGuildText},
			{ID: "chan2", Name: "lobby", Type: discordgo.ChannelTypeGuildText},
		}, nil
	}

	var content string
	s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
		content = response.Data.Content
		return nil
	}

	t.Run("defaults to the invoking channel", func(t *testing.T) {
		if err := cmd.Execute(ctx, s, newAmbientInteraction("addambientchannel")); err != nil {
			t.Fatalf("Execute failed: %v", err)
		}
		got, err := deps.Session.GetAmbientChannels(ctx, guildID)
		if err != nil || len(got) != 1 || got[0] != "chan-invoke" {
			t.Fatalf("expected ambient channel chan-invoke, got %v (err %v)", got, err)
		}
		if content != "#invoke added to ambient channels — I'll occasionally speak on my own there, as the active character." {
			t.Errorf("unexpected reply %q", content)
		}
	})

	t.Run("adds via option", func(t *testing.T) {
		if err := cmd.Execute(ctx, s, newAmbientInteraction("addambientchannel", stringOption("channel", "chan2"))); err != nil {
			t.Fatalf("Execute failed: %v", err)
		}
		got, err := deps.Session.GetAmbientChannels(ctx, guildID)
		if err != nil || len(got) != 2 || got[0] != "chan-invoke" || got[1] != "chan2" {
			t.Fatalf("expected both channels, got %v (err %v)", got, err)
		}
		if content != "#lobby added to ambient channels — I'll occasionally speak on my own there, as the active character." {
			t.Errorf("unexpected reply %q", content)
		}
	})

	t.Run("re-adding a member notes it is already set", func(t *testing.T) {
		content = ""
		if err := cmd.Execute(ctx, s, newAmbientInteraction("addambientchannel", stringOption("channel", "chan2"))); err != nil {
			t.Fatalf("Execute failed: %v", err)
		}
		got, err := deps.Session.GetAmbientChannels(ctx, guildID)
		if err != nil || len(got) != 2 {
			t.Fatalf("expected the set unchanged, got %v (err %v)", got, err)
		}
		if content != "#lobby is already an ambient channel." {
			t.Errorf("unexpected reply %q", content)
		}
	})
}

func TestAutocompleteAmbientChannels(t *testing.T) {
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

	t.Run("lists text channels only while none is set", func(t *testing.T) {
		choices := autocompleteAmbientChannels(context.Background(), sm, s, "guild1", "")
		if len(choices) != 2 {
			t.Fatalf("expected 2 choices, got %d", len(choices))
		}
		if choices[0].Name != "#alpha" || choices[0].Value != "c1" {
			t.Errorf("unexpected choice %v", choices[0])
		}
		if choices[1].Name != "#bravo" || choices[1].Value != "c2" {
			t.Errorf("unexpected choice %v", choices[1])
		}
	})

	t.Run("filters by query", func(t *testing.T) {
		choices := autocompleteAmbientChannels(context.Background(), sm, s, "guild1", "br")
		if len(choices) != 1 {
			t.Fatalf("expected 1 choice, got %d", len(choices))
		}
		if choices[0].Name != "#bravo" {
			t.Errorf("unexpected choice %v", choices[0])
		}
	})

	t.Run("marks set channels active", func(t *testing.T) {
		if err := sm.AddAmbientChannel(context.Background(), "guild1", "c2"); err != nil {
			t.Fatalf("AddAmbientChannel failed: %v", err)
		}
		choices := autocompleteAmbientChannels(context.Background(), sm, s, "guild1", "")
		if len(choices) != 2 {
			t.Fatalf("expected 2 choices, got %d", len(choices))
		}
		if choices[0].Name != "#alpha" {
			t.Errorf("unexpected name for the non-member channel %q", choices[0].Name)
		}
		if choices[1].Name != "#bravo (active)" {
			t.Errorf("expected the member channel marked active, got %q", choices[1].Name)
		}
	})
}

func TestNew_RegistersAmbientChannelCmds(t *testing.T) {
	deps, _, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	deps.Config.Ambient.Enabled = true
	reg := New(Deps{Session: deps.Session, LLM: deps.LLM, Audit: deps.Audit, Config: deps.Config})
	names := map[string]bool{}
	for _, d := range reg.Definitions() {
		names[d.Name] = true
	}
	if !names["addambientchannel"] || !names["removeambientchannel"] {
		t.Errorf("expected both ambient commands registered when ambient is enabled, got %v", names)
	}

	deps.Config.Ambient.Enabled = false
	reg = New(Deps{Session: deps.Session, LLM: deps.LLM, Audit: deps.Audit, Config: deps.Config})
	names = map[string]bool{}
	for _, d := range reg.Definitions() {
		names[d.Name] = true
	}
	if names["addambientchannel"] || names["removeambientchannel"] {
		t.Errorf("expected no ambient commands registered when ambient is disabled, got %v", names)
	}
}

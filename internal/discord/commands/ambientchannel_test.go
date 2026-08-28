package commands

import (
	"context"
	"os"
	"testing"

	"github.com/bwmarrin/discordgo"
)

func newAmbientInteraction(opts ...*discordgo.ApplicationCommandInteractionDataOption) *discordgo.InteractionCreate {
	i := &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			Type: discordgo.InteractionApplicationCommand,
			Data: discordgo.ApplicationCommandInteractionData{
				Name:    "setambientchannel",
				Options: opts,
			},
		},
	}
	i.GuildID = "guild1"
	i.ChannelID = "chan-invoke"
	return i
}

func TestSetAmbientChannelCmd_Execute(t *testing.T) {
	deps, s, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)
	ctx := context.Background()
	guildID := "guild1"
	cmd := &ambientChannelCmd{session: deps.Session}

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
		if err := cmd.Execute(ctx, s, newAmbientInteraction()); err != nil {
			t.Fatalf("Execute failed: %v", err)
		}
		got, err := deps.Session.GetAmbientChannel(ctx, guildID)
		if err != nil || got != "chan-invoke" {
			t.Fatalf("expected ambient channel chan-invoke, got %q (err %v)", got, err)
		}
		if content != "I'll occasionally speak on my own in #invoke, as the active character." {
			t.Errorf("unexpected reply %q", content)
		}
	})

	t.Run("sets via option", func(t *testing.T) {
		if err := cmd.Execute(ctx, s, newAmbientInteraction(stringOption("channel", "chan2"))); err != nil {
			t.Fatalf("Execute failed: %v", err)
		}
		got, err := deps.Session.GetAmbientChannel(ctx, guildID)
		if err != nil || got != "chan2" {
			t.Fatalf("expected ambient channel chan2, got %q (err %v)", got, err)
		}
		if content != "I'll occasionally speak on my own in #lobby, as the active character." {
			t.Errorf("unexpected reply %q", content)
		}
	})

	t.Run("clears via the clear choice", func(t *testing.T) {
		if err := cmd.Execute(ctx, s, newAmbientInteraction(stringOption("channel", ambientClearKey))); err != nil {
			t.Fatalf("Execute failed: %v", err)
		}
		got, err := deps.Session.GetAmbientChannel(ctx, guildID)
		if err != nil || got != "" {
			t.Fatalf("expected ambient channel cleared, got %q (err %v)", got, err)
		}
		if content != "Ambient channel cleared — I'll stop speaking on my own." {
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

	t.Run("marks the current channel active and offers clear", func(t *testing.T) {
		if err := sm.SetAmbientChannel(context.Background(), "guild1", "c2"); err != nil {
			t.Fatalf("SetAmbientChannel failed: %v", err)
		}
		choices := autocompleteAmbientChannels(context.Background(), sm, s, "guild1", "")
		var alpha, bravo, clear *discordgo.ApplicationCommandOptionChoice
		for _, c := range choices {
			switch c.Value {
			case "c1":
				alpha = c
			case "c2":
				bravo = c
			case ambientClearKey:
				clear = c
			}
		}
		if alpha == nil || bravo == nil || clear == nil {
			t.Fatalf("expected both channels and the clear choice, got %v", choices)
		}
		if clear.Name != ambientClearLabel {
			t.Errorf("unexpected clear choice %v", clear)
		}
		if alpha.Name != "#alpha" {
			t.Errorf("unexpected name for the non-current channel %q", alpha.Name)
		}
		if bravo.Name != "#bravo (active)" {
			t.Errorf("expected the current channel marked active, got %q", bravo.Name)
		}
	})
}

func TestNew_RegistersAmbientChannelCmd(t *testing.T) {
	deps, _, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	deps.Config.Ambient.Enabled = true
	reg := New(Deps{Session: deps.Session, LLM: deps.LLM, Audit: deps.Audit, Config: deps.Config})
	names := map[string]bool{}
	for _, d := range reg.Definitions() {
		names[d.Name] = true
	}
	if !names["setambientchannel"] {
		t.Error("expected setambientchannel to be registered when ambient is enabled")
	}

	deps.Config.Ambient.Enabled = false
	reg = New(Deps{Session: deps.Session, LLM: deps.LLM, Audit: deps.Audit, Config: deps.Config})
	names = map[string]bool{}
	for _, d := range reg.Definitions() {
		names[d.Name] = true
	}
	if names["setambientchannel"] {
		t.Error("expected setambientchannel to be absent when ambient is disabled")
	}
}

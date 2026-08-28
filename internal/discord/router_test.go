package discord

import (
	"context"
	"os"
	"testing"
	"time"

	"characterllm/internal/discord/commands"
	"characterllm/internal/responses"
	"characterllm/internal/session"
	"characterllm/internal/testkit"

	"github.com/bwmarrin/discordgo"
)

func setupRouter(t *testing.T, llm *mockLLMClient) (*Router, *mockDiscordSession, *session.Manager, string) {
	t.Helper()

	env := testkit.NewEnv(t)

	registry := commands.New(commands.Deps{
		Session: env.Session,
		LLM:     llm,
	})

	return &Router{CommandRegistry: registry}, &mockDiscordSession{}, env.Session, env.DBPath
}

func TestInteractionCreate_DispatchesSlashCommand(t *testing.T) {
	llm := &mockLLMClient{
		PingFn: func(ctx context.Context) (time.Duration, error) {
			return 42 * time.Millisecond, nil
		},
	}
	r, s, _, dbPath := setupRouter(t, llm)
	defer os.Remove(dbPath)

	var capturedEmbed *discordgo.MessageEmbed
	s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
		if len(response.Data.Embeds) != 1 {
			t.Fatalf("expected 1 embed, got %d", len(response.Data.Embeds))
		}
		capturedEmbed = response.Data.Embeds[0]
		return nil
	}

	i := &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			Type: discordgo.InteractionApplicationCommand,
			Data: discordgo.ApplicationCommandInteractionData{Name: "status"},
		},
	}
	i.GuildID = "guild1"

	r.handleInteraction(s, i)

	if capturedEmbed == nil || capturedEmbed.Title != responses.Status.Title {
		t.Errorf("unexpected response: %+v", capturedEmbed)
	}
	if len(capturedEmbed.Fields) != 2 || capturedEmbed.Fields[0].Value != responses.Status.Online ||
		capturedEmbed.Fields[1].Value != (42*time.Millisecond).String() {
		t.Errorf("unexpected embed fields: %+v", capturedEmbed.Fields)
	}
}

func TestInteractionCreate_UnknownCommand(t *testing.T) {
	r, s, _, dbPath := setupRouter(t, &mockLLMClient{})
	defer os.Remove(dbPath)

	responded := false
	s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
		responded = true
		return nil
	}

	i := &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			Type: discordgo.InteractionApplicationCommand,
			Data: discordgo.ApplicationCommandInteractionData{Name: "nosuchcommand"},
		},
	}
	i.GuildID = "guild1"

	r.handleInteraction(s, i)

	if responded {
		t.Error("expected no response for unknown command")
	}
}

func TestInteractionCreate_IgnoresNonSlashTypes(t *testing.T) {
	r, s, _, dbPath := setupRouter(t, &mockLLMClient{})
	defer os.Remove(dbPath)

	responded := false
	s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
		responded = true
		return nil
	}

	i := &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			Type: discordgo.InteractionMessageComponent,
			Data: discordgo.ApplicationCommandInteractionData{Name: "status"},
		},
	}
	i.GuildID = "guild1"

	r.handleInteraction(s, i)

	if responded {
		t.Error("expected non-slash interaction to be ignored")
	}
}

func TestComponentCreate_IgnoresUnknownCustomID(t *testing.T) {
	r, s, _, dbPath := setupRouter(t, &mockLLMClient{})
	defer os.Remove(dbPath)

	responded := false
	s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
		responded = true
		return nil
	}

	i := &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			Type: discordgo.InteractionMessageComponent,
			Data: discordgo.MessageComponentInteractionData{
				CustomID: "unrelated",
			},
		},
	}
	i.GuildID = "guild1"

	r.handleComponent(s, i)

	if responded {
		t.Error("expected unknown custom id to be ignored")
	}
}

func TestComponentCreate_IgnoresNonComponentTypes(t *testing.T) {
	r, s, _, dbPath := setupRouter(t, &mockLLMClient{})
	defer os.Remove(dbPath)

	responded := false
	s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
		responded = true
		return nil
	}

	i := &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			Type: discordgo.InteractionApplicationCommand,
			Data: discordgo.ApplicationCommandInteractionData{Name: "status"},
		},
	}
	i.GuildID = "guild1"

	r.handleComponent(s, i)

	if responded {
		t.Error("expected non-component interaction to be ignored")
	}
}

func TestInteractionCreate_DispatchesAutocomplete(t *testing.T) {
	r, s, sm, dbPath := setupRouter(t, &mockLLMClient{})
	defer os.Remove(dbPath)

	sm.SaveCharacterCard(context.Background(), "guild1", &session.CharacterCard{
		CharacterID: "miles-morales-ca8da118",
		DisplayName: "Miles Morales",
	})

	var choices []*discordgo.ApplicationCommandOptionChoice
	s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
		if response.Type != discordgo.InteractionApplicationCommandAutocompleteResult {
			t.Errorf("expected autocomplete response type, got %v", response.Type)
		}
		choices = response.Data.Choices
		return nil
	}

	i := &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			Type: discordgo.InteractionApplicationCommandAutocomplete,
			Data: discordgo.ApplicationCommandInteractionData{
				Name: "deletecharacter",
				Options: []*discordgo.ApplicationCommandInteractionDataOption{
					{Name: "name", Value: "miles", Type: discordgo.ApplicationCommandOptionString, Focused: true},
				},
			},
		},
	}
	i.GuildID = "guild1"

	r.handleInteraction(s, i)

	if len(choices) != 1 || choices[0].Value != "miles-morales-ca8da118" {
		t.Errorf("expected character suggestion, got %v", choices)
	}
}

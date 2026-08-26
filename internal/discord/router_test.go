package discord

import (
	"context"
	"fmt"
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

	var content string
	s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
		content = response.Data.Content
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

	if content != fmt.Sprintf(responses.Status.Online, 42*time.Millisecond) {
		t.Errorf("unexpected response: %q", content)
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

func TestComponentCreate_RoutesSelectCard(t *testing.T) {
	r, s, sm, dbPath := setupRouter(t, &mockLLMClient{})
	defer os.Remove(dbPath)

	sm.SaveCharacterCard(context.Background(), "guild1", &session.CharacterCard{
		CharacterID: "char1",
		DisplayName: "Test Character",
	}, nil)

	var content string
	s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
		content = response.Data.Content
		return nil
	}

	i := &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			Type: discordgo.InteractionMessageComponent,
			Data: discordgo.MessageComponentInteractionData{
				CustomID: "select_character_card",
				Values:   []string{"char1"},
			},
		},
	}
	i.GuildID = "guild1"

	r.handleComponent(s, i)

	if content != fmt.Sprintf(responses.ListCharacters.SetSuccess, "Test Character") {
		t.Errorf("unexpected response: %q", content)
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

package commands

import (
	"context"
	"errors"
	"os"
	"testing"

	"characterllm/internal/session"

	"github.com/bwmarrin/discordgo"
)

func TestRegistry_UnknownCommand(t *testing.T) {
	r := New(Deps{})
	err := r.Execute(context.Background(), "nosuchcmd", &mockDiscordSession{}, &discordgo.InteractionCreate{})
	if !errors.Is(err, ErrUnknownCommand) {
		t.Fatalf("expected ErrUnknownCommand, got %v", err)
	}
}

func TestRegistry_Definitions(t *testing.T) {
	r := New(Deps{})
	defs := r.Definitions()
	if len(defs) != 5 {
		t.Fatalf("expected 5 command definitions, got %d", len(defs))
	}
	names := map[string]bool{}
	for _, d := range defs {
		names[d.Name] = true
	}
	for _, want := range []string{"resetchat", "status", "listcharacters", "setavatar", "setcharacter"} {
		if !names[want] {
			t.Errorf("missing command definition %q", want)
		}
	}
}

func TestRegistry_Execute_DispatchesByName(t *testing.T) {
	cmdCtx, s, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	responded := false
	s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
		responded = true
		return nil
	}

	r := New(Deps{
		Session: cmdCtx.Session,
		LLM:     cmdCtx.LLM,
	})
	i := &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			Type: discordgo.InteractionApplicationCommand,
		},
	}
	i.GuildID = "guild1"

	if err := r.Execute(context.Background(), "status", s, i); err != nil {
		t.Fatalf("Execute(status) returned error: %v", err)
	}
	if !responded {
		t.Fatal("expected InteractionRespond to be called for status command")
	}
}

func TestRegistry_HandleComponent_RoutesByCustomID(t *testing.T) {
	cmdCtx, s, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	cmdCtx.Session.SaveCharacterCard(context.Background(), "guild1", &session.CharacterCard{
		CharacterID: "char1",
		DisplayName: "Test Character",
	}, nil)

	responded := 0
	s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
		responded++
		return nil
	}

	r := New(Deps{
		Session:     cmdCtx.Session,
		ImageClient: cmdCtx.ImageClient,
	})

	// select_character_card routes to listCharacters.handleSelectCard
	i := &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			Type: discordgo.InteractionMessageComponent,
			Data: discordgo.MessageComponentInteractionData{
				CustomID: selectCharacterCardID,
				Values:   []string{"char1"},
			},
		},
	}
	i.GuildID = "guild1"
	r.HandleComponent(context.Background(), s, i)
	if responded != 1 {
		t.Fatal("expected select_character_card to respond")
	}

	// pagination prefix routes to listCharacters.handlePagination
	i2 := &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			Type: discordgo.InteractionMessageComponent,
			Data: discordgo.MessageComponentInteractionData{
				CustomID: listPaginationID("next", 0),
			},
		},
	}
	i2.GuildID = "guild1"
	r.HandleComponent(context.Background(), s, i2)
	if responded != 2 {
		t.Fatal("expected pagination component to respond")
	}

	// unknown custom id is ignored
	i3 := &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			Type: discordgo.InteractionMessageComponent,
			Data: discordgo.MessageComponentInteractionData{
				CustomID: "something_else",
			},
		},
	}
	i3.GuildID = "guild1"
	r.HandleComponent(context.Background(), s, i3)
	if responded != 2 {
		t.Fatal("expected unknown custom id to be ignored")
	}
}

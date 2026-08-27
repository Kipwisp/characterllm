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
	if len(defs) != 8 {
		t.Fatalf("expected 8 command definitions, got %d", len(defs))
	}
	names := map[string]bool{}
	for _, d := range defs {
		names[d.Name] = true
	}
	for _, want := range []string{"resetchat", "status", "setcharacter", "viewcharacterdetails", "createcharacter", "deletecharacter", "editcharacter", "setavatar"} {
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
	})

	responded := 0
	s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
		responded++
		return nil
	}

	r := New(Deps{
		Session:     cmdCtx.Session,
		ImageClient: cmdCtx.ImageClient,
	})

	// delete_confirm_ routes to the deletion handler.
	i := &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			Type: discordgo.InteractionMessageComponent,
			Data: discordgo.MessageComponentInteractionData{
				CustomID: deleteConfirmID("char1"),
			},
		},
	}
	i.GuildID = "guild1"
	r.HandleComponent(context.Background(), s, i)
	if responded != 1 {
		t.Fatal("expected delete_confirm to respond")
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
	if responded != 1 {
		t.Fatal("expected unknown custom id to be ignored")
	}
}

func TestRegistry_HandleAutocomplete_SuggestsCharacters(t *testing.T) {
	cmdCtx, s, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	cmdCtx.Session.SaveCharacterCard(context.Background(), "guild1", &session.CharacterCard{
		CharacterID: "miles-morales-ca8da118",
		DisplayName: "Miles Morales",
	})
	cmdCtx.Session.SaveCharacterCard(context.Background(), "guild1", &session.CharacterCard{
		CharacterID: "peter-parker-00000001",
		DisplayName: "Peter Parker",
	})

	var choices []*discordgo.ApplicationCommandOptionChoice
	s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
		if response.Type != discordgo.InteractionApplicationCommandAutocompleteResult {
			t.Errorf("expected autocomplete response type, got %v", response.Type)
		}
		choices = response.Data.Choices
		return nil
	}

	r := New(Deps{Session: cmdCtx.Session})

	i := &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			Type: discordgo.InteractionApplicationCommandAutocomplete,
			Data: discordgo.ApplicationCommandInteractionData{
				Name: "setcharacter",
				Options: []*discordgo.ApplicationCommandInteractionDataOption{
					{Name: "name", Value: "mil", Type: discordgo.ApplicationCommandOptionString, Focused: true},
				},
			},
		},
	}
	i.GuildID = "guild1"
	r.HandleAutocomplete(context.Background(), s, i)

	if len(choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(choices))
	}
	if choices[0].Name != "Miles Morales miles-morales-ca8da118" || choices[0].Value != "miles-morales-ca8da118" {
		t.Errorf("unexpected choice: %q = %q", choices[0].Name, choices[0].Value)
	}

	// No matches yields only the placeholder for setcharacter (no current).
	choices = nil
	i.Interaction.Data.(discordgo.ApplicationCommandInteractionData).Options[0].Value = "zzz"
	r.HandleAutocomplete(context.Background(), s, i)
	if len(choices) != 1 || choices[0].Value != "none" {
		t.Errorf("expected placeholder choice only, got %v", choices)
	}

	// viewcharacterdetails offers the current suggestion on no matches.
	iview := &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			Type: discordgo.InteractionApplicationCommandAutocomplete,
			Data: discordgo.ApplicationCommandInteractionData{
				Name: "viewcharacterdetails",
				Options: []*discordgo.ApplicationCommandInteractionDataOption{
					{Name: "name", Value: "zzz", Type: discordgo.ApplicationCommandOptionString, Focused: true},
				},
			},
		},
	}
	iview.GuildID = "guild1"
	choices = nil
	r.HandleAutocomplete(context.Background(), s, iview)
	if len(choices) != 2 || choices[0].Value != currentCardName || choices[1].Value != "none" {
		t.Errorf("expected current + placeholder choices, got %v", choices)
	}

	// deletecharacter also offers the current suggestion on no matches.
	idel := &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			Type: discordgo.InteractionApplicationCommandAutocomplete,
			Data: discordgo.ApplicationCommandInteractionData{
				Name: "deletecharacter",
				Options: []*discordgo.ApplicationCommandInteractionDataOption{
					{Name: "name", Value: "zzz", Type: discordgo.ApplicationCommandOptionString, Focused: true},
				},
			},
		},
	}
	idel.GuildID = "guild1"
	choices = nil
	r.HandleAutocomplete(context.Background(), s, idel)
	if len(choices) != 2 || choices[0].Value != currentCardName || choices[1].Value != "none" {
		t.Errorf("expected current + placeholder choices for deletecharacter, got %v", choices)
	}

	// editcharacter also offers the current suggestion on no matches.
	iedit := &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			Type: discordgo.InteractionApplicationCommandAutocomplete,
			Data: discordgo.ApplicationCommandInteractionData{
				Name: "editcharacter",
				Options: []*discordgo.ApplicationCommandInteractionDataOption{
					{Name: "name", Value: "zzz", Type: discordgo.ApplicationCommandOptionString, Focused: true},
				},
			},
		},
	}
	iedit.GuildID = "guild1"
	choices = nil
	r.HandleAutocomplete(context.Background(), s, iedit)
	if len(choices) != 2 || choices[0].Value != currentCardName || choices[1].Value != "none" {
		t.Errorf("expected current + placeholder choices for editcharacter, got %v", choices)
	}

	// setcharacter does not offer the current suggestion.
	iset := &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			Type: discordgo.InteractionApplicationCommandAutocomplete,
			Data: discordgo.ApplicationCommandInteractionData{
				Name: "setcharacter",
				Options: []*discordgo.ApplicationCommandInteractionDataOption{
					{Name: "name", Value: "zzz", Type: discordgo.ApplicationCommandOptionString, Focused: true},
				},
			},
		},
	}
	iset.GuildID = "guild1"
	choices = nil
	r.HandleAutocomplete(context.Background(), s, iset)
	if len(choices) != 1 || choices[0].Value != "none" {
		t.Errorf("expected placeholder choice only for setcharacter, got %v", choices)
	}

	// Non-focused interactions are ignored.
	choices = nil
	opt := i.Interaction.Data.(discordgo.ApplicationCommandInteractionData).Options[0]
	opt.Value = "mil"
	opt.Focused = false
	r.HandleAutocomplete(context.Background(), s, i)
	if choices != nil {
		t.Errorf("expected no response for non-focused option, got %v", choices)
	}
}

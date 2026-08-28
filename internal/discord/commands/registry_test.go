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
	if len(defs) != 11 {
		t.Fatalf("expected 11 command definitions, got %d", len(defs))
	}
	names := map[string]bool{}
	for _, d := range defs {
		names[d.Name] = true
	}
	for _, want := range []string{"clearthread", "newthread", "setthread", "deletethread", "status", "setcharacter", "viewcharacterdetails", "createcharacter", "deletecharacter", "editcharacter", "setavatar"} {
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
	cmdCtx.Session.SetActiveCharacter(context.Background(), "guild1", "miles-morales-ca8da118")

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
	if choices[0].Name != "Miles Morales miles-morales-ca8da118 (active)" || choices[0].Value != "miles-morales-ca8da118" {
		t.Errorf("unexpected choice: %q = %q", choices[0].Name, choices[0].Value)
	}

	// No matches yields only the placeholder.
	choices = nil
	i.Interaction.Data.(discordgo.ApplicationCommandInteractionData).Options[0].Value = "zzz"
	r.HandleAutocomplete(context.Background(), s, i)
	if len(choices) != 1 || choices[0].Value != "none" {
		t.Errorf("expected placeholder choice only, got %v", choices)
	}

	// viewcharacterdetails offers the current choice plus the placeholder on
	// no matches.
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
		t.Errorf("expected current and placeholder choices, got %v", choices)
	}

	// deletecharacter falls back to the placeholder on no matches too.
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
		t.Errorf("expected current and placeholder choices for deletecharacter, got %v", choices)
	}

	// editcharacter falls back to the placeholder on no matches too.
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
		t.Errorf("expected current and placeholder choices for editcharacter, got %v", choices)
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

func TestRegistry_HandleAutocomplete_SuggestsThreads(t *testing.T) {
	cmdCtx, s, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	ctx := context.Background()
	guildID := "guild1"
	cmdCtx.Session.SaveCharacterCard(ctx, guildID, &session.CharacterCard{
		CharacterID: "miles-morales-ca8da118",
		DisplayName: "Miles Morales",
	})
	cmdCtx.Session.SetActiveCharacter(ctx, guildID, "miles-morales-ca8da118")
	if err := cmdCtx.Session.EnsureDefaultThread(ctx, guildID, "miles-morales-ca8da118"); err != nil {
		t.Fatalf("EnsureDefaultThread failed: %v", err)
	}
	if _, err := cmdCtx.Session.CreateThread(ctx, guildID, "miles-morales-ca8da118", "Side quest"); err != nil {
		t.Fatalf("CreateThread failed: %v", err)
	}

	var choices []*discordgo.ApplicationCommandOptionChoice
	s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
		if response.Type != discordgo.InteractionApplicationCommandAutocompleteResult {
			t.Errorf("expected autocomplete response type, got %v", response.Type)
		}
		choices = response.Data.Choices
		return nil
	}

	r := New(Deps{Session: cmdCtx.Session})

	newInteraction := func(name, value string) *discordgo.InteractionCreate {
		i := &discordgo.InteractionCreate{
			Interaction: &discordgo.Interaction{
				Type: discordgo.InteractionApplicationCommandAutocomplete,
				Data: discordgo.ApplicationCommandInteractionData{
					Name: name,
					Options: []*discordgo.ApplicationCommandInteractionDataOption{
						{Name: "thread", Value: value, Type: discordgo.ApplicationCommandOptionString, Focused: true},
					},
				},
			},
		}
		i.GuildID = guildID
		return i
	}

	t.Run("no active character offers a placeholder", func(t *testing.T) {
		i := newInteraction("setthread", "")
		i.GuildID = "empty-guild"
		r.HandleAutocomplete(ctx, s, i)
		if len(choices) != 1 || choices[0].Value != "none" {
			t.Fatalf("expected placeholder choice, got %v", choices)
		}
	})

	t.Run("lists threads with the active one marked", func(t *testing.T) {
		choices = nil
		r.HandleAutocomplete(ctx, s, newInteraction("setthread", "side"))
		if len(choices) != 1 {
			t.Fatalf("expected 1 choice, got %v", choices)
		}
		if choices[0].Value != "2" || choices[0].Name != "Side quest (active)" {
			t.Errorf("unexpected choice %v", choices[0])
		}
	})

	t.Run("default thread is a regular choice", func(t *testing.T) {
		choices = nil
		r.HandleAutocomplete(ctx, s, newInteraction("setthread", "thread"))
		found := false
		for _, c := range choices {
			if c.Value == "1" && c.Name == "Thread 1" {
				found = true
			}
		}
		if !found {
			t.Errorf("expected the default thread choice, got %v", choices)
		}
	})

	t.Run("current suggestion on matching query", func(t *testing.T) {
		choices = nil
		r.HandleAutocomplete(ctx, s, newInteraction("deletethread", "current"))
		found := false
		for _, c := range choices {
			if c.Value == "current" && c.Name == "Current (active thread)" {
				found = true
			}
		}
		if !found {
			t.Errorf("expected the current-thread suggestion, got %v", choices)
		}
	})

	t.Run("setthread never offers current", func(t *testing.T) {
		choices = nil
		r.HandleAutocomplete(ctx, s, newInteraction("setthread", "current"))
		for _, c := range choices {
			if c.Value == "current" {
				t.Errorf("expected no current choice for setthread, got %v", choices)
				break
			}
		}
	})
}

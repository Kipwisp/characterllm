package commands

import (
	"context"
	"os"
	"strings"
	"testing"

	"characterllm/internal/session"

	"github.com/bwmarrin/discordgo"
)

func newSetInteraction(guildID, name string) *discordgo.InteractionCreate {
	i := &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			Type: discordgo.InteractionApplicationCommand,
			Data: discordgo.ApplicationCommandInteractionData{
				Name: "setcharacter",
				Options: []*discordgo.ApplicationCommandInteractionDataOption{
					{Name: "name", Value: name, Type: discordgo.ApplicationCommandOptionString},
				},
			},
		},
	}
	i.GuildID = guildID
	return i
}

func TestSetCharacterCmd_ActivateByDisplayName(t *testing.T) {
	cmdCtx, s, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	guildID := "guild1"
	charID := "char1"
	cmdCtx.Session.SaveCharacterCard(context.Background(), guildID, &session.CharacterCard{
		CharacterID:  charID,
		DisplayName:  "Miles Morales",
		OfficialName: "Miles Morales",
	})

	var capturedContent string
	s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
		capturedContent = response.Data.Content
		return nil
	}
	s.GuildMemberNicknameFn = func(guildID string, member string, nickname string) error {
		return nil
	}

	cmd := &setCharacterCmd{session: cmdCtx.Session, imageClient: cmdCtx.ImageClient}
	if err := cmd.Execute(context.Background(), s, newSetInteraction(guildID, "miles morales")); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !strings.Contains(capturedContent, "Character set to **Miles Morales**!") {
		t.Errorf("Expected set success message, got %q", capturedContent)
	}
	active, err := cmdCtx.Session.GetCharacterDetails(context.Background(), guildID)
	if err != nil {
		t.Fatalf("GetCharacterDetails failed: %v", err)
	}
	if active == nil || active.CharacterID != charID {
		t.Errorf("Expected active character %s, got %v", charID, active)
	}
}

func TestSetCharacterCmd_ActivateByCharacterID(t *testing.T) {
	cmdCtx, s, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	guildID := "guild1"
	cmdCtx.Session.SaveCharacterCard(context.Background(), guildID, &session.CharacterCard{
		CharacterID: "miles-morales-ca8da118",
		DisplayName: "Miles Morales",
	})

	var capturedContent string
	s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
		capturedContent = response.Data.Content
		return nil
	}

	cmd := &setCharacterCmd{session: cmdCtx.Session, imageClient: cmdCtx.ImageClient}
	if err := cmd.Execute(context.Background(), s, newSetInteraction(guildID, "miles-morales-ca8da118")); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !strings.Contains(capturedContent, "Character set to **Miles Morales**!") {
		t.Errorf("Expected set success message, got %q", capturedContent)
	}
}

func TestSetCharacterCmd_ActivateNotFound(t *testing.T) {
	cmdCtx, s, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	var capturedContent string
	s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
		capturedContent = response.Data.Content
		return nil
	}

	cmd := &setCharacterCmd{session: cmdCtx.Session, imageClient: cmdCtx.ImageClient}
	if err := cmd.Execute(context.Background(), s, newSetInteraction("guild1", "Unknown Hero")); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !strings.Contains(capturedContent, "/createcharacter") {
		t.Errorf("Expected suggestion to create the character, got %q", capturedContent)
	}
}

func TestSetCharacterCmd_ActivateAmbiguous(t *testing.T) {
	cmdCtx, s, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	guildID := "guild1"
	for _, id := range []string{"char1", "char2"} {
		cmdCtx.Session.SaveCharacterCard(context.Background(), guildID, &session.CharacterCard{
			CharacterID:  id,
			DisplayName:  "Twin",
			OfficialName: "Twin One",
		})
		cmdCtx.Session.SaveCharacterCard(context.Background(), guildID, &session.CharacterCard{
			CharacterID:  id + "x",
			DisplayName:  "Twin",
			OfficialName: "Twin Two",
		})
	}

	var capturedContent string
	s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
		capturedContent = response.Data.Content
		return nil
	}

	cmd := &setCharacterCmd{session: cmdCtx.Session, imageClient: cmdCtx.ImageClient}
	if err := cmd.Execute(context.Background(), s, newSetInteraction(guildID, "twin")); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !strings.Contains(capturedContent, "Multiple saved characters match") {
		t.Errorf("Expected ambiguity message, got %q", capturedContent)
	}
	if !strings.Contains(capturedContent, "Twin (char1)") || !strings.Contains(capturedContent, "Twin (char2x)") {
		t.Errorf("Expected candidates annotated with their IDs, got %q", capturedContent)
	}
}

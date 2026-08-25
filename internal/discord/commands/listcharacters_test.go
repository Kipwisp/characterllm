package commands

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"characterllm/internal/session"
	"github.com/bwmarrin/discordgo"
)

func TestListCharactersCmd_Empty(t *testing.T) {
	cmdCtx, s, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	respondCalled := false
	s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
		respondCalled = true
		return nil
	}

	i := &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{},
	}
	i.GuildID = "guild1"

	cmd := &listCharactersCmd{}
	err := cmd.Execute(context.Background(), cmdCtx, s, i)

	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !respondCalled {
		t.Error("InteractionRespond was not called")
	}
}

func TestListCharactersCmd_WithCharacters(t *testing.T) {
	cmdCtx, s, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	guildID := "guild1"
	sm := cmdCtx.Session
	sm.SaveCharacterCard(context.Background(), guildID, &session.CharacterCard{
		CharacterID: "char1",
		DisplayName: "Character 1",
	}, []string{})

	respondCalled := false
	s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
		respondCalled = true
		if len(response.Data.Components) == 0 {
			t.Error("Expected components (select menu), got none")
		}
		return nil
	}

	i := &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{},
	}
	i.GuildID = guildID

	cmd := &listCharactersCmd{}
	err := cmd.Execute(context.Background(), cmdCtx, s, i)

	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !respondCalled {
		t.Error("InteractionRespond was not called")
	}
}

func TestHandleSelectCharacterCard_Success(t *testing.T) {
	cmdCtx, s, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	guildID := "guild1"
	charID := "char1"
	sm := cmdCtx.Session
	sm.SaveCharacterCard(context.Background(), guildID, &session.CharacterCard{
		CharacterID: charID,
		DisplayName: "Character 1",
	}, []string{})

	i := &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			Type: discordgo.InteractionMessageComponent,
		},
	}
	i.Interaction.Data = discordgo.MessageComponentInteractionData{
		Values: []string{charID},
	}
	i.GuildID = guildID

	respondCalled := false
	s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
		respondCalled = true
		return nil
	}
	s.GuildMemberNicknameFn = func(guildID string, member string, nickname string) error {
		return nil
	}

	HandleSelectCharacterCard(context.Background(), cmdCtx, s, i)

	if !respondCalled {
		t.Error("InteractionRespond was not called")
	}

	active, err := sm.GetCharacterDetails(context.Background(), guildID)
	if err != nil {
		t.Fatalf("GetCharacterDetails failed: %v", err)
	}
	if active == nil || active.CharacterID != charID {
		t.Errorf("Expected active character %s, got %v", charID, active)
	}
}

func TestListCharactersCmd_PaginationButtons(t *testing.T) {
	cmdCtx, s, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	guildID := "guild1"
	sm := cmdCtx.Session
	for i := 0; i < 26; i++ {
		sm.SaveCharacterCard(context.Background(), guildID, &session.CharacterCard{
			CharacterID: fmt.Sprintf("char%d", i),
			DisplayName: fmt.Sprintf("Character %d", i),
		}, []string{})
	}

	var capturedComponents []discordgo.MessageComponent
	s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
		capturedComponents = response.Data.Components
		return nil
	}

	i := &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{},
	}
	i.GuildID = guildID

	cmd := &listCharactersCmd{}
	err := cmd.Execute(context.Background(), cmdCtx, s, i)

	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	foundNextButton := false
	for _, row := range capturedComponents {
		if actionRow, ok := row.(discordgo.ActionsRow); ok {
			for _, comp := range actionRow.Components {
				if button, ok := comp.(discordgo.Button); ok && button.CustomID == "list_char_next_0" {
					foundNextButton = true
					break
				}
			}
		}
	}

	if !foundNextButton {
		t.Error("Expected 'Next' button for 26 characters on page 0, but none found")
	}
}

func TestHandleListCharactersPagination(t *testing.T) {
	cmdCtx, s, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	guildID := "guild1"
	sm := cmdCtx.Session
	for i := 0; i < 26; i++ {
		sm.SaveCharacterCard(context.Background(), guildID, &session.CharacterCard{
			CharacterID: fmt.Sprintf("char%d", i),
			DisplayName: fmt.Sprintf("Character %d", i),
		}, []string{})
	}

	i := &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			Type: discordgo.InteractionMessageComponent,
		},
	}
	i.Interaction.Data = discordgo.MessageComponentInteractionData{
		CustomID: "list_char_next_0",
	}
	i.GuildID = guildID

	var capturedContent string
	s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
		capturedContent = response.Data.Content
		return nil
	}

	HandleListCharactersPagination(context.Background(), cmdCtx, s, i)

	if capturedContent == "" {
		t.Error("Expected response content for pagination, got empty string")
	}
	if !strings.Contains(capturedContent, "Page 2") {
		t.Errorf("Expected response to indicate Page 2, got %q", capturedContent)
	}
}

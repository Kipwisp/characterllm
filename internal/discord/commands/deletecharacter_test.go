package commands

import (
	"characterllm/internal/llm"
	"context"
	"os"
	"strings"
	"testing"

	"characterllm/internal/session"

	"github.com/bwmarrin/discordgo"
)

func newDeleteInteraction(guildID, name string) *discordgo.InteractionCreate {
	i := &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			Type: discordgo.InteractionApplicationCommand,
			Data: discordgo.ApplicationCommandInteractionData{
				Name: "deletecharacter",
				Options: []*discordgo.ApplicationCommandInteractionDataOption{
					{Name: "name", Value: name, Type: discordgo.ApplicationCommandOptionString},
				},
			},
		},
	}
	i.GuildID = guildID
	return i
}

func newComponentInteraction(guildID, customID string) *discordgo.InteractionCreate {
	i := &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			ID:   "interaction1",
			Type: discordgo.InteractionMessageComponent,
			Data: discordgo.MessageComponentInteractionData{
				CustomID: customID,
			},
		},
	}
	i.GuildID = guildID
	return i
}

func TestDeleteCharacterCmd_ConfirmPrompt(t *testing.T) {
	cmdCtx, s, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	guildID := "guild1"
	cmdCtx.Session.SaveCharacterCard(context.Background(), guildID, &session.CharacterCard{
		CharacterID: "char1",
		DisplayName: "Miles Morales",
	})

	var capturedEmbed *discordgo.MessageEmbed
	var components []discordgo.MessageComponent
	s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
		if len(response.Data.Embeds) > 0 {
			capturedEmbed = response.Data.Embeds[0]
		}
		components = response.Data.Components
		return nil
	}

	cmdCtx.ImageClient = &mockImageClient{}
	cmd := &deleteCharacterCmd{session: cmdCtx.Session, imageClient: cmdCtx.ImageClient}
	if err := cmd.Execute(context.Background(), s, newDeleteInteraction(guildID, "char1")); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if capturedEmbed == nil {
		t.Fatal("Expected the confirmation to be an embed")
	}
	if capturedEmbed.Title != "Miles Morales" {
		t.Errorf("embed title = %q", capturedEmbed.Title)
	}
	if !strings.Contains(capturedEmbed.Description, "Miles Morales") || !strings.Contains(capturedEmbed.Description, "chat threads") {
		t.Errorf("Expected the warning in the embed description, got %q", capturedEmbed.Description)
	}

	confirmID, cancelID := false, false
	for _, row := range components {
		actionRow, ok := row.(discordgo.ActionsRow)
		if !ok {
			continue
		}
		for _, comp := range actionRow.Components {
			button, ok := comp.(discordgo.Button)
			if !ok {
				continue
			}
			if button.CustomID == deleteConfirmID("char1") {
				confirmID = true
			}
			if button.CustomID == deleteCancelID("char1") {
				cancelID = true
			}
		}
	}
	if !confirmID || !cancelID {
		t.Errorf("Expected confirm and cancel buttons, got components %v", components)
	}
}

func TestDeleteCharacterCmd_Confirm(t *testing.T) {
	cmdCtx, s, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	guildID := "guild1"
	sm := cmdCtx.Session
	sm.SaveCharacterCard(context.Background(), guildID, &session.CharacterCard{
		CharacterID: "char1",
		DisplayName: "Miles Morales",
	})
	sm.SetActiveCharacter(context.Background(), guildID, "char1")
	sm.SaveMessage(context.Background(), guildID, "", llm.RoleUser, "hello")
	sm.SaveMessage(context.Background(), guildID, "", llm.RoleAssistant, "hi")

	deletedImage := false
	cmdCtx.ImageClient = &mockImageClient{
		DeleteImageFn: func(guildID, characterID string) error {
			deletedImage = true
			return nil
		},
	}

	var capturedContent string
	s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
		capturedContent = response.Data.Content
		return nil
	}
	nicknameReset := false
	s.GuildMemberNicknameFn = func(guildID string, member string, nickname string) error {
		if nickname == "" {
			nicknameReset = true
		}
		return nil
	}

	cmd := &deleteCharacterCmd{session: sm, imageClient: cmdCtx.ImageClient}
	cmd.handleDeleteConfirm(context.Background(), s, newComponentInteraction(guildID, deleteConfirmID("char1")))

	if !strings.Contains(capturedContent, "Deleted **Miles Morales** (1 threads)") {
		t.Errorf("Expected deletion confirmation with thread count, got %q", capturedContent)
	}
	if card, err := sm.GetCharacterCard(context.Background(), guildID, "char1"); err != nil || card != nil {
		t.Errorf("Expected card to be deleted, got %v (err %v)", card, err)
	}
	if !deletedImage {
		t.Error("Expected cached image deletion")
	}
	if !nicknameReset {
		t.Error("Expected bot nickname reset for a deleted active character")
	}
	if details, err := sm.GetCharacterDetails(context.Background(), guildID); err == nil && details != nil && details.CharacterID != "" {
		t.Errorf("Expected active character cleared, got %v", details)
	}
}

func TestDeleteCharacterCmd_Cancel(t *testing.T) {
	cmdCtx, s, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	guildID := "guild1"
	sm := cmdCtx.Session
	sm.SaveCharacterCard(context.Background(), guildID, &session.CharacterCard{
		CharacterID: "char1",
		DisplayName: "Miles Morales",
	})

	var capturedContent string
	var capturedEmbeds []*discordgo.MessageEmbed
	var capturedAttachments *[]*discordgo.MessageAttachment
	s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
		capturedContent = response.Data.Content
		capturedEmbeds = response.Data.Embeds
		capturedAttachments = response.Data.Attachments
		return nil
	}

	cmd := &deleteCharacterCmd{session: sm, imageClient: cmdCtx.ImageClient}
	cmd.handleDeleteCancel(context.Background(), s, newComponentInteraction(guildID, deleteCancelID("char1")))

	if !strings.Contains(capturedContent, "cancelled") {
		t.Errorf("Expected cancellation message, got %q", capturedContent)
	}
	// The confirmation embed (and its avatar) must be stripped, not left
	// next to the cancellation text.
	if capturedEmbeds == nil || len(capturedEmbeds) != 0 {
		t.Errorf("cancel must clear the confirmation embed, got %+v", capturedEmbeds)
	}
	if capturedAttachments == nil || len(*capturedAttachments) != 0 {
		t.Errorf("cancel must clear the avatar attachment, got %+v", capturedAttachments)
	}
	if card, err := sm.GetCharacterCard(context.Background(), guildID, "char1"); err != nil || card == nil {
		t.Errorf("Expected card to survive cancellation, got %v (err %v)", card, err)
	}
}

func TestDeleteCharacterCmd_NotFound(t *testing.T) {
	cmdCtx, s, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	var capturedContent string
	s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
		capturedContent = response.Data.Content
		return nil
	}

	cmd := &deleteCharacterCmd{session: cmdCtx.Session, imageClient: cmdCtx.ImageClient}
	if err := cmd.Execute(context.Background(), s, newDeleteInteraction("guild1", "Nobody")); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !strings.Contains(capturedContent, "/createcharacter") {
		t.Errorf("Expected suggestion to create the character, got %q", capturedContent)
	}
}

func TestDeleteCharacterCmd_CurrentKey(t *testing.T) {
	cmdCtx, s, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	guildID := "guild1"
	cmdCtx.Session.SaveCharacterCard(context.Background(), guildID, &session.CharacterCard{
		CharacterID: "active-char",
		DisplayName: "Geralt of Rivia",
	})
	cmdCtx.Session.SetActiveCharacter(context.Background(), guildID, "active-char")

	var capturedTitle string
	s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
		if len(response.Data.Embeds) > 0 {
			capturedTitle = response.Data.Embeds[0].Title
		}
		return nil
	}

	cmdCtx.ImageClient = &mockImageClient{}
	cmd := &deleteCharacterCmd{session: cmdCtx.Session, imageClient: cmdCtx.ImageClient}
	if err := cmd.Execute(context.Background(), s, newDeleteInteraction(guildID, "current")); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if capturedTitle != "Geralt of Rivia" {
		t.Errorf("Expected confirmation naming the active character, got %q", capturedTitle)
	}
}

func TestDeleteCharacterCmd_CurrentKeyNoActive(t *testing.T) {
	cmdCtx, s, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	var capturedContent string
	var ephemeral bool
	s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
		capturedContent = response.Data.Content
		ephemeral = response.Data.Flags&discordgo.MessageFlagsEphemeral != 0
		return nil
	}

	cmd := &deleteCharacterCmd{session: cmdCtx.Session, imageClient: cmdCtx.ImageClient}
	if err := cmd.Execute(context.Background(), s, newDeleteInteraction("guild1", "current")); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !ephemeral || !strings.Contains(capturedContent, "No saved character cards") {
		t.Errorf("Expected ephemeral empty response, got %q (ephemeral=%v)", capturedContent, ephemeral)
	}
}

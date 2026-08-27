package commands

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"characterllm/internal/research"
	"characterllm/internal/session"

	"github.com/bwmarrin/discordgo"
)

const editTestSpec = "### Identity & Temperament\nCold and questioning.\n\n### Appearance\n- **Species/Origin**: Human\n\n### Voice & Habits\nSlow cadence, dry wit.\n\n### Example Dialogue\n<START>\nUser: Hello\nCharacter: You again.\n"

func newEditInteraction(guildID, name string, extra ...*discordgo.ApplicationCommandInteractionDataOption) *discordgo.InteractionCreate {
	opts := []*discordgo.ApplicationCommandInteractionDataOption{
		{Name: "name", Value: name, Type: discordgo.ApplicationCommandOptionString},
	}
	opts = append(opts, extra...)
	i := &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			Type: discordgo.InteractionApplicationCommand,
			Data: discordgo.ApplicationCommandInteractionData{
				Name:    "editcharacter",
				Options: opts,
			},
		},
	}
	i.GuildID = guildID
	i.ChannelID = "channel1"
	return i
}

func stringOption(name, value string) *discordgo.ApplicationCommandInteractionDataOption {
	return &discordgo.ApplicationCommandInteractionDataOption{Name: name, Value: value, Type: discordgo.ApplicationCommandOptionString}
}

// editButtonIDs pulls the Accept/Reject button custom IDs off a components tree.
func editButtonIDs(components []discordgo.MessageComponent) (acceptID, rejectID string) {
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
			switch {
			case strings.HasPrefix(button.CustomID, editAcceptPrefix):
				acceptID = button.CustomID
			case strings.HasPrefix(button.CustomID, editRejectPrefix):
				rejectID = button.CustomID
			}
		}
	}
	return
}

// editSnapshot captures one webhook edit so tests can inspect the sequence
// (rewriting ack, preview, final result).
type editSnapshot struct {
	content  string
	embeds   []*discordgo.MessageEmbed
	acceptID string
	rejectID string
}

func snapshotEdit(edit *discordgo.WebhookEdit) editSnapshot {
	snap := editSnapshot{}
	if edit.Content != nil {
		snap.content = *edit.Content
	}
	if edit.Embeds != nil {
		snap.embeds = *edit.Embeds
	}
	if edit.Components != nil {
		snap.acceptID, snap.rejectID = editButtonIDs(*edit.Components)
	}
	return snap
}

func TestEditCharacterCmd_SimpleSections(t *testing.T) {
	cmdCtx, s, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	guildID := "guild1"
	sm := cmdCtx.Session
	sm.SaveCharacterCard(context.Background(), guildID, &session.CharacterCard{
		CharacterID: "char1",
		DisplayName: "Miles Morales",
		Series:      "Marvel",
		Description: editTestSpec,
	})

	var capturedContent string
	s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
		capturedContent = response.Data.Content
		return nil
	}

	cmd := &editCharacterCmd{session: sm, imageClient: cmdCtx.ImageClient, synthesizer: &mockSynthesizer{}, audit: cmdCtx.Audit}

	// One section per invocation.
	if err := cmd.Execute(context.Background(), s, newEditInteraction(guildID, "char1",
		stringOption("section", "official_name"),
		stringOption("instruction", "Miles G. Morales"),
	)); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !strings.Contains(capturedContent, "Updated **Miles Morales**: official_name") {
		t.Errorf("Expected update summary, got %q", capturedContent)
	}
	if err := cmd.Execute(context.Background(), s, newEditInteraction(guildID, "char1",
		stringOption("section", "display_name"),
		stringOption("instruction", "Miles"),
	)); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !strings.Contains(capturedContent, "Updated **Miles**: display_name") {
		t.Errorf("Expected update summary, got %q", capturedContent)
	}
	if err := cmd.Execute(context.Background(), s, newEditInteraction(guildID, "char1",
		stringOption("section", "series"),
		stringOption("instruction", "Spider-Verse"),
	)); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !strings.Contains(capturedContent, "Updated **Miles**: series") {
		t.Errorf("Expected series update summary, got %q", capturedContent)
	}
	card, err := sm.GetCharacterCard(context.Background(), guildID, "char1")
	if err != nil || card == nil {
		t.Fatalf("GetCharacterCard failed: %v", err)
	}
	if card.OfficialName != "Miles G. Morales" || card.DisplayName != "Miles" || card.Series != "Spider-Verse" {
		t.Errorf("Unexpected card after edits: %+v", card)
	}
	// The persona spec must be untouched by simple section edits.
	if card.Description != editTestSpec {
		t.Errorf("Simple edits must not touch the persona spec:\n%s", card.Description)
	}
}

func TestEditCharacterCmd_SectionValidation(t *testing.T) {
	cmdCtx, s, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	guildID := "guild1"
	sm := cmdCtx.Session
	sm.SaveCharacterCard(context.Background(), guildID, &session.CharacterCard{
		CharacterID: "char1",
		DisplayName: "Miles Morales",
	})

	var capturedContent string
	s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
		capturedContent = response.Data.Content
		return nil
	}

	cmd := &editCharacterCmd{session: sm, imageClient: cmdCtx.ImageClient, synthesizer: &mockSynthesizer{}, audit: cmdCtx.Audit}

	// A section without an instruction is rejected.
	if err := cmd.Execute(context.Background(), s, newEditInteraction(guildID, "char1", stringOption("section", "series"))); err == nil {
		t.Error("Expected error for section without instruction")
	}
	if !strings.Contains(capturedContent, "instruction") {
		t.Errorf("Expected instruction guidance, got %q", capturedContent)
	}

	// An unknown section is rejected.
	if err := cmd.Execute(context.Background(), s, newEditInteraction(guildID, "char1",
		stringOption("section", "image"),
		stringOption("instruction", "x"),
	)); err == nil {
		t.Error("Expected error for unknown section")
	}
	if !strings.Contains(capturedContent, "image") {
		t.Errorf("Expected unknown-section message, got %q", capturedContent)
	}

	card, _ := sm.GetCharacterCard(context.Background(), guildID, "char1")
	if card.Series != "" || card.DisplayName != "Miles Morales" {
		t.Errorf("Card must be untouched by rejected edits, got %+v", card)
	}
}

func TestEditCharacterCmd_SectionRewrite(t *testing.T) {
	cmdCtx, s, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	guildID := "guild1"
	sm := cmdCtx.Session
	sm.SaveCharacterCard(context.Background(), guildID, &session.CharacterCard{
		CharacterID: "char1",
		DisplayName: "Miles Morales",
		Description: editTestSpec,
	})

	var capturedReq research.SectionRewriteRequest
	mockSynth := &mockSynthesizer{
		RewriteSectionFn: func(ctx context.Context, req research.SectionRewriteRequest) (*research.SectionRewriteResult, error) {
			capturedReq = req
			return &research.SectionRewriteResult{
				Body:      "Fast cadence, warm wit.\n\nSome extra header noise",
				Prompt:    "captured prompt",
				Reasoning: "rewrote it",
			}, nil
		},
	}

	var edits []editSnapshot
	var finalContent string
	var finalEmbeds []*discordgo.MessageEmbed
	s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
		if response.Type == discordgo.InteractionResponseUpdateMessage && response.Data != nil {
			finalContent = response.Data.Content
			finalEmbeds = response.Data.Embeds
		}
		return nil
	}
	s.InteractionResponseEditFn = func(interaction *discordgo.Interaction, edit *discordgo.WebhookEdit) (*discordgo.Message, error) {
		edits = append(edits, snapshotEdit(edit))
		return nil, nil
	}

	cmd := &editCharacterCmd{session: sm, imageClient: cmdCtx.ImageClient, synthesizer: mockSynth, audit: cmdCtx.Audit}
	err := cmd.Execute(context.Background(), s, newEditInteraction(guildID, "char1",
		stringOption("section", "voice"),
		stringOption("instruction", "make him sound warmer"),
	))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// The rewrite runs immediately and is shown as a preview the user can
	// accept or reject; the card must not change yet.
	if len(edits) < 1 {
		t.Fatalf("Expected a preview edit, got %d", len(edits))
	}
	preview := edits[0]
	if !strings.Contains(preview.content, "proposed") {
		t.Errorf("Expected a proposal message, got %q", preview.content)
	}
	if preview.acceptID == "" || preview.rejectID == "" {
		t.Fatalf("Expected Accept and Reject buttons, got ids %q %q", preview.acceptID, preview.rejectID)
	}
	if len(preview.embeds) != 1 || preview.embeds[0].Title != research.SectionVoice {
		t.Fatalf("Expected a preview embed titled %q, got %+v", research.SectionVoice, preview.embeds)
	}
	if !strings.Contains(preview.embeds[0].Description, "__Fast__") || !strings.Contains(preview.embeds[0].Description, "__warm__") {
		t.Errorf("Expected changed words underlined in the preview embed, got %q", preview.embeds[0].Description)
	}
	if strings.Contains(preview.embeds[0].Description, "__cadence") {
		t.Errorf("unchanged words must not be underlined: %q", preview.embeds[0].Description)
	}
	card, _ := sm.GetCharacterCard(context.Background(), guildID, "char1")
	if card.Description != editTestSpec {
		t.Errorf("card must be unchanged before Accept:\n%s", card.Description)
	}

	cmd.handleEditAccept(context.Background(), s, newComponentInteraction(guildID, preview.acceptID))

	// Accepting updates the preview into the final confirmation.
	if !strings.Contains(finalContent, "Updated **Miles Morales**: ") {
		t.Errorf("Expected section update summary in final message, got %q", finalContent)
	}
	if len(finalEmbeds) != 1 || finalEmbeds[0].Title != research.SectionVoice {
		t.Fatalf("Expected a section embed on the final message, got %+v", finalEmbeds)
	}
	if !strings.Contains(finalEmbeds[0].Description, "__Fast__") || !strings.Contains(finalEmbeds[0].Description, "~~Slow~~") {
		t.Errorf("Expected the marked-up section body in the final embed, got %q", finalEmbeds[0].Description)
	}
	if strings.Contains(finalContent, "Fast cadence") {
		t.Errorf("section body must not be in the message content: %q", finalContent)
	}

	// The command supplies the full card context to the synthesizer.
	if capturedReq.Section != research.SectionVoice {
		t.Errorf("Section = %q, want %q", capturedReq.Section, research.SectionVoice)
	}
	if capturedReq.CurrentBody != "Slow cadence, dry wit." {
		t.Errorf("CurrentBody = %q", capturedReq.CurrentBody)
	}
	if capturedReq.Instruction != "make him sound warmer" {
		t.Errorf("Instruction = %q", capturedReq.Instruction)
	}
	if capturedReq.Spec != editTestSpec {
		t.Errorf("Spec must be the full persona spec, got %q", capturedReq.Spec)
	}

	card, _ = sm.GetCharacterCard(context.Background(), guildID, "char1")
	body, ok := research.ExtractSection(card.Description, research.SectionVoice)
	if !ok || body != "Fast cadence, warm wit.\n\nSome extra header noise" {
		t.Errorf("Voice section not rewritten: got %q (ok=%v)", body, ok)
	}
	// Neighbors survive byte-for-byte.
	for _, section := range []string{research.SectionIdentity, research.SectionAppearance, research.SectionDialogue} {
		if b, ok := research.ExtractSection(card.Description, section); !ok {
			t.Errorf("section %s lost after rewrite", section)
		} else if strings.Contains(b, "warm wit") && section != research.SectionVoice {
			t.Errorf("section %s contaminated: %q", section, b)
		}
	}
	// A section without an instruction is rejected before confirmation.
	var missingContent string
	s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
		if response.Data != nil {
			missingContent = response.Data.Content
		}
		return nil
	}
	if err := cmd.Execute(context.Background(), s, newEditInteraction(guildID, "char1", stringOption("section", "voice"))); err == nil {
		t.Error("Expected error for section without instruction")
	}
	if !strings.Contains(missingContent, "instruction") {
		t.Errorf("Expected instruction guidance, got %q", missingContent)
	}
}

func TestEditCharacterCmd_ScenarioSection(t *testing.T) {
	cmdCtx, s, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	guildID := "guild1"
	sm := cmdCtx.Session
	spec := editTestSpec + "\n\n### Scenario\nA dark forest.\n"
	sm.SaveCharacterCard(context.Background(), guildID, &session.CharacterCard{
		CharacterID: "char1",
		DisplayName: "Miles Morales",
		Description: spec,
	})

	var capturedReq research.SectionRewriteRequest
	mockSynth := &mockSynthesizer{
		RewriteSectionFn: func(ctx context.Context, req research.SectionRewriteRequest) (*research.SectionRewriteResult, error) {
			capturedReq = req
			return &research.SectionRewriteResult{Body: "A rainy rooftop, 1989.", Reasoning: "ok"}, nil
		},
	}

	var edits []editSnapshot
	s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
		return nil
	}
	s.InteractionResponseEditFn = func(interaction *discordgo.Interaction, edit *discordgo.WebhookEdit) (*discordgo.Message, error) {
		edits = append(edits, snapshotEdit(edit))
		return nil, nil
	}

	cmd := &editCharacterCmd{session: sm, imageClient: cmdCtx.ImageClient, synthesizer: mockSynth, audit: cmdCtx.Audit}
	err := cmd.Execute(context.Background(), s, newEditInteraction(guildID, "char1",
		stringOption("section", "scenario"),
		stringOption("instruction", "move it to a rooftop"),
	))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if len(edits) < 1 || edits[0].acceptID == "" {
		t.Fatalf("Expected a preview with an Accept button, got %d edits", len(edits))
	}
	cmd.handleEditAccept(context.Background(), s, newComponentInteraction(guildID, edits[0].acceptID))

	if capturedReq.Section != research.SectionScenario {
		t.Errorf("Section = %q, want %q", capturedReq.Section, research.SectionScenario)
	}
	if capturedReq.CurrentBody != "A dark forest." {
		t.Errorf("existing scenario must be supplied to the synthesizer, got %q", capturedReq.CurrentBody)
	}

	card, _ := sm.GetCharacterCard(context.Background(), guildID, "char1")
	body, ok := research.ExtractSection(card.Description, research.SectionScenario)
	if !ok || body != "A rainy rooftop, 1989." {
		t.Errorf("Scenario section not updated in the spec: got %q (ok=%v)", body, ok)
	}
}

func TestEditCharacterCmd_MissingInput(t *testing.T) {
	cmdCtx, s, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	guildID := "guild1"
	sm := cmdCtx.Session
	sm.SaveCharacterCard(context.Background(), guildID, &session.CharacterCard{
		CharacterID: "char1",
		DisplayName: "Miles Morales",
	})

	var capturedContent string
	s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
		capturedContent = response.Data.Content
		return nil
	}

	cmd := &editCharacterCmd{session: sm, imageClient: cmdCtx.ImageClient, synthesizer: &mockSynthesizer{}, audit: cmdCtx.Audit}

	if err := cmd.Execute(context.Background(), s, newEditInteraction(guildID, "char1")); err == nil {
		t.Error("Expected error when nothing is given")
	}
	if !strings.Contains(capturedContent, "section") || !strings.Contains(capturedContent, "instruction") {
		t.Errorf("Expected guidance message, got %q", capturedContent)
	}

	// A section without an instruction is rejected before the LLM is called.
	capturedContent = ""
	if err := cmd.Execute(context.Background(), s, newEditInteraction(guildID, "char1", stringOption("section", "voice"))); err == nil {
		t.Error("Expected error for section without instruction")
	}
	if !strings.Contains(capturedContent, "instruction") {
		t.Errorf("Expected instruction guidance, got %q", capturedContent)
	}
}

func TestEditCharacterCmd_CurrentKey(t *testing.T) {
	cmdCtx, s, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	guildID := "guild1"
	cmdCtx.Session.SaveCharacterCard(context.Background(), guildID, &session.CharacterCard{
		CharacterID: "active-char",
		DisplayName: "Geralt of Rivia",
	})
	cmdCtx.Session.SetActiveCharacter(context.Background(), guildID, "active-char")

	var capturedContent string
	s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
		capturedContent = response.Data.Content
		return nil
	}

	cmd := &editCharacterCmd{session: cmdCtx.Session, imageClient: cmdCtx.ImageClient, synthesizer: &mockSynthesizer{}, audit: cmdCtx.Audit}
	if err := cmd.Execute(context.Background(), s, newEditInteraction(guildID, "current", stringOption("section", "display_name"), stringOption("instruction", "Geralt"))); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !strings.Contains(capturedContent, "Updated **Geralt**") {
		t.Errorf("Expected update summary for the active character, got %q", capturedContent)
	}
	card, _ := cmdCtx.Session.GetCharacterCard(context.Background(), guildID, "active-char")
	if card == nil || card.DisplayName != "Geralt" {
		t.Errorf("Expected active card renamed, got %+v", card)
	}
}

func TestEditCharacterCmd_CurrentKeyNoActive(t *testing.T) {
	cmdCtx, s, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	var capturedContent string
	var ephemeral bool
	s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
		capturedContent = response.Data.Content
		ephemeral = response.Data.Flags&discordgo.MessageFlagsEphemeral != 0
		return nil
	}

	cmd := &editCharacterCmd{session: cmdCtx.Session, imageClient: cmdCtx.ImageClient, synthesizer: &mockSynthesizer{}, audit: cmdCtx.Audit}
	if err := cmd.Execute(context.Background(), s, newEditInteraction("guild1", "current", stringOption("section", "display_name"), stringOption("instruction", "X"))); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !ephemeral || !strings.Contains(capturedContent, "No saved character cards") {
		t.Errorf("Expected ephemeral empty response, got %q (ephemeral=%v)", capturedContent, ephemeral)
	}
}

func TestEditCharacterCmd_GeneralRewrite(t *testing.T) {
	cmdCtx, s, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	guildID := "guild1"
	sm := cmdCtx.Session
	sm.SaveCharacterCard(context.Background(), guildID, &session.CharacterCard{
		CharacterID: "char1",
		DisplayName: "Miles Morales",
		Description: editTestSpec,
	})

	avatarPath := t.TempDir() + "/avatar.png"
	if err := os.WriteFile(avatarPath, []byte("png"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	cmdCtx.ImageClient = &mockImageClient{
		GetImageFn: func(g, c string) (string, error) { return avatarPath, nil },
	}
	newSpec := strings.Replace(editTestSpec, "Cold and questioning.", "Perpetually upbeat.", 1)
	var capturedReq research.SectionRewriteRequest
	mockSynth := &mockSynthesizer{
		RewriteSectionFn: func(ctx context.Context, req research.SectionRewriteRequest) (*research.SectionRewriteResult, error) {
			capturedReq = req
			return &research.SectionRewriteResult{Body: newSpec, Reasoning: "ok"}, nil
		},
	}

	var edits []editSnapshot
	var finalContent string
	var finalEmbeds []*discordgo.MessageEmbed
	var finalAttachments []*discordgo.MessageAttachment
	s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
		if response.Type == discordgo.InteractionResponseUpdateMessage && response.Data != nil {
			finalContent = response.Data.Content
			finalEmbeds = response.Data.Embeds
			if response.Data.Attachments != nil {
				finalAttachments = *response.Data.Attachments
			}
		}
		return nil
	}
	// The preview edit is answered with a message whose avatar attachment is
	// then expected to be re-referenced by the Accept response.
	s.InteractionResponseEditFn = func(interaction *discordgo.Interaction, edit *discordgo.WebhookEdit) (*discordgo.Message, error) {
		edits = append(edits, snapshotEdit(edit))
		if len(edits) == 1 && len(edit.Files) > 0 {
			return &discordgo.Message{ID: "msg1", Attachments: []*discordgo.MessageAttachment{{ID: "avatar-att"}}}, nil
		}
		return nil, nil
	}

	cmd := &editCharacterCmd{session: sm, imageClient: cmdCtx.ImageClient, synthesizer: mockSynth, audit: cmdCtx.Audit}
	err := cmd.Execute(context.Background(), s, newEditInteraction(guildID, "char1",
		stringOption("section", "general"),
		stringOption("instruction", "he is always happy"),
	))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// The proposed persona is previewed before it is saved.
	if len(edits) < 1 || edits[0].acceptID == "" {
		t.Fatalf("Expected a preview with an Accept button, got %d edits", len(edits))
	}
	if !embedContains(edits[0].embeds, "Perpetually upbeat") {
		t.Fatalf("Expected the proposed spec with underlined changes in the preview embeds, got %+v", edits[0].embeds)
	}
	if !embedHasTitle(edits[0].embeds, "Identity & Temperament") {
		t.Errorf("expected per-section preview embeds, got %+v", edits[0].embeds)
	}
	card, _ := sm.GetCharacterCard(context.Background(), guildID, "char1")
	if card.Description != editTestSpec {
		t.Errorf("card must be unchanged before Accept")
	}

	cmd.handleEditAccept(context.Background(), s, newComponentInteraction(guildID, edits[0].acceptID))

	if !capturedReq.WholePersona || capturedReq.Section != "general" {
		t.Errorf("expected a whole-persona request, got %+v", capturedReq)
	}
	if capturedReq.Instruction != "he is always happy" || capturedReq.Spec != editTestSpec {
		t.Errorf("request context wrong: %+v", capturedReq)
	}

	card, _ = sm.GetCharacterCard(context.Background(), guildID, "char1")
	if card.Description != newSpec {
		t.Errorf("whole spec not replaced:\n%s", card.Description)
	}
	// Accepting updates the preview into the confirmation through the
	// button's own interaction callback, keeping the card embed and
	// re-referencing the avatar attachment so the thumbnail survives.
	if !strings.Contains(finalContent, "Updated **Miles Morales**: whole persona") {
		t.Errorf("expected whole-persona confirmation, got %q", finalContent)
	}
	if len(finalEmbeds) < 2 {
		t.Fatalf("expected the card embeds (identity + sections) on the final response, got %+v", finalEmbeds)
	}
	if finalEmbeds[0].Title != "Miles Morales" {
		t.Errorf("card embed title = %q", finalEmbeds[0].Title)
	}
	if !embedContains(finalEmbeds, "Perpetually upbeat") {
		t.Errorf("rewritten spec missing from the section embeds: %+v", finalEmbeds)
	}
	if finalEmbeds[0].Thumbnail == nil || finalEmbeds[0].Thumbnail.URL != "attachment://avatar.png" {
		t.Errorf("card embed must keep the avatar thumbnail, got %+v", finalEmbeds[0].Thumbnail)
	}
	if len(finalAttachments) != 1 || finalAttachments[0].ID != "avatar-att" {
		t.Errorf("Accept response must re-reference the preview's avatar attachment, got %+v", finalAttachments)
	}
}

func TestEditCharacterCmd_GeneralRewriteFailure(t *testing.T) {
	cmdCtx, s, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	guildID := "guild1"
	sm := cmdCtx.Session
	sm.SaveCharacterCard(context.Background(), guildID, &session.CharacterCard{
		CharacterID: "char1",
		DisplayName: "Miles Morales",
		Description: editTestSpec,
	})

	mockSynth := &mockSynthesizer{
		RewriteSectionFn: func(ctx context.Context, req research.SectionRewriteRequest) (*research.SectionRewriteResult, error) {
			return nil, fmt.Errorf("model dropped the Appearance section")
		},
	}

	var errorContent string
	s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
		return nil
	}
	s.InteractionResponseEditFn = func(interaction *discordgo.Interaction, edit *discordgo.WebhookEdit) (*discordgo.Message, error) {
		if edit.Content != nil {
			errorContent = *edit.Content
		}
		return nil, nil
	}

	cmd := &editCharacterCmd{session: sm, imageClient: cmdCtx.ImageClient, synthesizer: mockSynth, audit: cmdCtx.Audit}
	if err := cmd.Execute(context.Background(), s, newEditInteraction(guildID, "char1",
		stringOption("section", "general"),
		stringOption("instruction", "he is always happy"),
	)); err == nil {
		t.Fatal("Expected error for failed whole-persona rewrite")
	}
	if !strings.Contains(errorContent, "couldn't update") {
		t.Errorf("Expected the error to be shown, got %q", errorContent)
	}

	card, _ := sm.GetCharacterCard(context.Background(), guildID, "char1")
	if card.Description != editTestSpec {
		t.Errorf("failed rewrite must not modify the spec:\n%s", card.Description)
	}
}

func TestEditCharacterCmd_EditReject(t *testing.T) {
	cmdCtx, s, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	guildID := "guild1"
	sm := cmdCtx.Session
	sm.SaveCharacterCard(context.Background(), guildID, &session.CharacterCard{
		CharacterID: "char1",
		DisplayName: "Miles Morales",
		Description: editTestSpec,
	})

	mockSynth := &mockSynthesizer{
		RewriteSectionFn: func(ctx context.Context, req research.SectionRewriteRequest) (*research.SectionRewriteResult, error) {
			return &research.SectionRewriteResult{Body: "Warm cadence.", Reasoning: "ok"}, nil
		},
	}

	var rejectID string
	s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
		return nil
	}
	s.InteractionResponseEditFn = func(interaction *discordgo.Interaction, edit *discordgo.WebhookEdit) (*discordgo.Message, error) {
		if edit.Components != nil {
			_, rejectID = editButtonIDs(*edit.Components)
		}
		return nil, nil
	}

	cmd := &editCharacterCmd{session: sm, imageClient: cmdCtx.ImageClient, synthesizer: mockSynth, audit: cmdCtx.Audit}
	if err := cmd.Execute(context.Background(), s, newEditInteraction(guildID, "char1",
		stringOption("section", "voice"),
		stringOption("instruction", "make him sound warmer"),
	)); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if rejectID == "" {
		t.Fatal("Expected a Reject button on the preview")
	}

	var rejectedContent string
	var rejectedEmbeds []*discordgo.MessageEmbed
	var rejectedAttachments *[]*discordgo.MessageAttachment
	s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
		if response.Data != nil {
			rejectedContent = response.Data.Content
			rejectedEmbeds = response.Data.Embeds
			rejectedAttachments = response.Data.Attachments
		}
		return nil
	}
	cmd.handleEditReject(context.Background(), s, newComponentInteraction(guildID, rejectID))
	if !strings.Contains(rejectedContent, "rejected") {
		t.Errorf("Expected rejection message, got %q", rejectedContent)
	}
	// The preview embed must be stripped, not left next to the rejection text.
	if rejectedEmbeds == nil || len(rejectedEmbeds) != 0 {
		t.Errorf("reject must clear the preview embed, got %+v", rejectedEmbeds)
	}
	if rejectedAttachments == nil || len(*rejectedAttachments) != 0 {
		t.Errorf("reject must clear the preview attachment, got %+v", rejectedAttachments)
	}

	card, _ := sm.GetCharacterCard(context.Background(), guildID, "char1")
	if card.Description != editTestSpec {
		t.Errorf("rejected edit must not modify the spec")
	}
}

func TestEditCharacterCmd_EditConfirmExpired(t *testing.T) {
	cmdCtx, s, dbPath := setupCommandTest(t)
	defer os.Remove(dbPath)

	guildID := "guild1"
	sm := cmdCtx.Session
	sm.SaveCharacterCard(context.Background(), guildID, &session.CharacterCard{
		CharacterID: "char1",
		DisplayName: "Miles Morales",
		Description: editTestSpec,
	})

	var capturedContent string
	s.InteractionRespondFn = func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
		if response.Data != nil {
			capturedContent = response.Data.Content
		}
		return nil
	}

	cmd := &editCharacterCmd{session: sm, imageClient: cmdCtx.ImageClient, synthesizer: &mockSynthesizer{}, audit: cmdCtx.Audit}
	cmd.handleEditAccept(context.Background(), s, newComponentInteraction(guildID, editAcceptPrefix+"unknown-token"))
	if !strings.Contains(capturedContent, "no longer available") {
		t.Errorf("Expected expired message, got %q", capturedContent)
	}

	card, _ := sm.GetCharacterCard(context.Background(), guildID, "char1")
	if card.Description != editTestSpec {
		t.Errorf("expired proposal must not modify the spec")
	}
}

package commands

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"characterllm/internal/audit"
	"characterllm/internal/images"
	"characterllm/internal/logger"
	"characterllm/internal/research"
	"characterllm/internal/responses"
	"characterllm/internal/session"
	"characterllm/internal/utils"

	"github.com/bwmarrin/discordgo"
	"github.com/google/uuid"
)

const (
	editAcceptPrefix = "editchar_accept_"
	editRejectPrefix = "editchar_reject_"

	// interactionTokenTTL is how long Discord keeps an interaction token
	// valid; the preview's ack can only be edited through it.
	interactionTokenTTL = 15 * time.Minute
)

// Section option keys for /editcharacter
const (
	sectionKeyOfficialName = "official_name"
	sectionKeyDisplayName  = "display_name"
	sectionKeySeries       = "series"
	sectionKeyGeneral      = "general"

	sectionKeyIdentity   = "identity"
	sectionKeyAppearance = "appearance"
	sectionKeyVoice      = "voice"
	sectionKeyDialogue   = "dialogue"
	sectionKeyScenario   = "scenario"
	sectionKeyGreeting   = "greeting"
)

// editSectionChoices maps the persona-section option keys to the persona
// specification section constants.
var editSectionChoices = map[string]string{
	sectionKeyIdentity:   research.SectionIdentity,
	sectionKeyAppearance: research.SectionAppearance,
	sectionKeyVoice:      research.SectionVoice,
	sectionKeyDialogue:   research.SectionDialogue,
	sectionKeyScenario:   research.SectionScenario,
	sectionKeyGreeting:   research.SectionGreeting,
}

type editCharacterCmd struct {
	session     *session.Manager
	imageClient images.ImageClient
	synthesizer research.Synthesizer
	audit       *audit.AuditLogger

	pendingMu    sync.Mutex
	pendingEdits map[string]*pendingEdit
}

// pendingEdit is a generated-but-unaccepted LLM edit, held between the
// preview and the Accept/Reject click.
type pendingEdit struct {
	characterID      string
	section          string
	body             string
	prompt           string
	reasoning        string
	latency          time.Duration
	avatarAttachment string
	// cardMessageIDs holds the plain channel messages of a multi-message
	// whole-persona preview (in send order); empty for single-message
	// previews, whose preview is the ack message itself.
	cardMessageIDs []string
	// orig is the original command interaction: its token (valid 15
	// minutes) is the only route left to the preview's ack message, which
	// is an interaction response. expiresAt is when that token dies.
	orig      *discordgo.Interaction
	expiresAt time.Time
}

// dropPending discards a generated proposal so its Accept/Reject buttons
// can never act on it.
func (c *editCharacterCmd) dropPending(token string) {
	c.pendingMu.Lock()
	delete(c.pendingEdits, token)
	c.pendingMu.Unlock()
}

func newEditToken() string {
	b := make([]byte, 8)
	rand.Read(b)
	return strings.ToLower(hex.EncodeToString(b))
}

// Definition returns the Discord application command definition for editing a saved character.
func (c *editCharacterCmd) Definition() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        "editcharacter",
		Description: "Edit a saved character's fields, or propose an LLM rewrite of part (or all) of their persona.",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:         discordgo.ApplicationCommandOptionString,
				Name:         "name",
				Description:  "Character name to edit, or 'current' for the active character.",
				Required:     true,
				Autocomplete: true,
			},
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "section",
				Description: "What to edit.",
				Required:    true,
				Choices: []*discordgo.ApplicationCommandOptionChoice{
					{Name: "Official name", Value: sectionKeyOfficialName},
					{Name: "Display name", Value: sectionKeyDisplayName},
					{Name: "Series", Value: sectionKeySeries},
					{Name: "General (whole persona)", Value: sectionKeyGeneral},
					{Name: "Identity & Temperament", Value: sectionKeyIdentity},
					{Name: "Appearance", Value: sectionKeyAppearance},
					{Name: "Voice & Habits", Value: sectionKeyVoice},
					{Name: "Example Dialogue", Value: sectionKeyDialogue},
					{Name: "Scenario", Value: sectionKeyScenario},
					{Name: "Greeting", Value: sectionKeyGreeting},
				},
			},
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "instruction",
				Description: "New value for a field, or what to change in the persona.",
				Required:    true,
			},
		},
	}
}

// Execute applies exactly one change to the card: field sets apply
// immediately, LLM-based rewrites generate a proposal the user can accept
// or reject.
func (c *editCharacterCmd) Execute(ctx context.Context, s DiscordSession, i *discordgo.InteractionCreate) error {
	data := i.ApplicationCommandData()
	name := data.GetOption("name").StringValue()
	card, err := resolveNameOrCurrent(ctx, c.session, i.GuildID, name)
	if err != nil {
		return respondResolveError(ctx, s, i, err)
	}

	acked := false
	say := func(content string, flags discordgo.MessageFlags) {
		if acked {
			s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
				Content: utils.PtrString(content),
			})
			return
		}
		acked = true
		if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: content,
				Flags:   flags,
			},
		}); err != nil {
			logger.FromContext(ctx).Error("failed to acknowledge edit character command", "error", err, "guild_id", i.GuildID)
		}
	}

	sectionKey := optionValue(data, "section")
	instruction := optionValue(data, "instruction")
	if sectionKey == "" || instruction == "" {
		say(responses.EditCharacter.MissingInput, discordgo.MessageFlagsEphemeral)
		return fmt.Errorf("missing section or instruction")
	}

	switch sectionKey {
	case sectionKeyOfficialName:
		card.OfficialName = instruction
	case sectionKeyDisplayName:
		card.DisplayName = instruction
	case sectionKeySeries:
		card.Series = instruction
	default:
		return c.proposeRewrite(ctx, s, i, card, sectionKey, instruction, say)
	}

	if err := c.session.SaveCharacterCard(ctx, i.GuildID, card); err != nil {
		logger.FromContext(ctx).Error("failed to save edited character card", "error", err, "characterID", card.CharacterID, "guild_id", i.GuildID)
		say(responses.EditCharacter.Error, discordgo.MessageFlagsEphemeral)
		return err
	}

	// The display name is part of the bot's identity when the character is active.
	details, err := c.session.GetCharacterDetails(ctx, i.GuildID)
	if err == nil && details != nil && details.CharacterID == card.CharacterID {
		if err := SyncGuildIdentity(ctx, c.session, c.imageClient, s, i.GuildID); err != nil {
			logger.FromContext(ctx).Warn("failed to sync guild identity after edit", "error", err, "guild_id", i.GuildID)
		}
	}

	return s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: fmt.Sprintf(responses.EditCharacter.Updated, card.DisplayName, sectionKey),
		},
	})
}

// proposeRewrite runs the LLM rewrite immediately and shows the result as a
// proposal with Accept/Reject buttons; the card is only saved on Accept.
func (c *editCharacterCmd) proposeRewrite(ctx context.Context, s DiscordSession, i *discordgo.InteractionCreate, card *session.CharacterCard, sectionKey, instruction string, say func(string, discordgo.MessageFlags)) error {
	section := sectionKey
	label := "persona"
	if sectionKey != sectionKeyGeneral {
		mapped, ok := editSectionChoices[sectionKey]
		if !ok {
			say(fmt.Sprintf(responses.EditCharacter.SectionNotFound, sectionKey), discordgo.MessageFlagsEphemeral)
			return fmt.Errorf("unknown section %q", sectionKey)
		}
		section = mapped
		label = section
	}

	say(fmt.Sprintf(responses.EditCharacter.Rewriting, card.DisplayName, label), 0)

	current := currentSectionBody(card.Description, section)
	if section == sectionKeyGeneral {
		current = card.Description
	}

	start := time.Now()
	rewrite, err := c.synthesizer.RewriteSection(ctx, research.SectionRewriteRequest{
		DisplayName:  card.DisplayName,
		OfficialName: card.OfficialName,
		Series:       card.Series,
		Spec:         card.Description,
		Section:      section,
		CurrentBody:  current,
		Instruction:  instruction,
		WholePersona: section == sectionKeyGeneral,
	})
	latency := time.Since(start)
	if err != nil {
		logger.FromContext(ctx).Error("persona rewrite failed", "error", err, "character_id", card.CharacterID, "section", section)
		say(responses.EditCharacter.Error, discordgo.MessageFlagsEphemeral)
		return err
	}

	token := newEditToken()
	c.pendingMu.Lock()
	if c.pendingEdits == nil {
		c.pendingEdits = map[string]*pendingEdit{}
	}
	c.pendingEdits[token] = &pendingEdit{
		characterID: card.CharacterID,
		section:     section,
		body:        rewrite.Body,
		prompt:      rewrite.Prompt,
		reasoning:   rewrite.Reasoning,
		latency:     latency,
		orig:        i.Interaction,
		expiresAt:   time.Now().Add(interactionTokenTTL),
	}
	c.pendingMu.Unlock()

	previewContent := fmt.Sprintf(responses.EditCharacter.Propose, label, card.DisplayName)
	components := []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{Label: "Accept", Style: discordgo.SuccessButton, CustomID: editAcceptPrefix + token},
				discordgo.Button{Label: "Reject", Style: discordgo.DangerButton, CustomID: editRejectPrefix + token},
			},
		},
	}

	// The preview underlines or strikethroughs the words that changed so the
	// proposal is easy to scan.
	var marked string
	if section == sectionKeyGeneral {
		marked = markSpecChanges(current, rewrite.Body)
	} else {
		marked = markChanges(current, rewrite.Body)
	}

	var messages [][]*discordgo.MessageEmbed
	var files []*discordgo.File
	var closeFiles func()
	if section == sectionKeyGeneral {
		proposed := *card
		proposed.Description = marked
		messages, files, closeFiles = buildCharacterCardEmbed(c.imageClient, i.GuildID, &proposed)
		defer closeFiles()
	} else {
		messages = [][]*discordgo.MessageEmbed{{sectionBodyEmbed(section, marked)}}
	}

	if len(messages) == 1 {
		msg, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content:    utils.PtrString(previewContent),
			Embeds:     &messages[0],
			Files:      files,
			Components: &components,
		})
		if err != nil {
			logger.FromContext(ctx).Error("failed to show edit proposal", "error", err, "character_id", card.CharacterID)
			c.failProposal(ctx, s, i, card, token)
			return nil
		}
		c.pendingMu.Lock()
		if pending, ok := c.pendingEdits[token]; ok && msg != nil && len(msg.Attachments) > 0 {
			pending.avatarAttachment = msg.Attachments[0].ID
		}
		c.pendingMu.Unlock()
		return nil
	}

	// A multi-message preview
	if _, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Content:    utils.PtrString(previewContent),
		Components: nil,
	}); err != nil {
		logger.FromContext(ctx).Error("failed to show edit proposal", "error", err, "character_id", card.CharacterID)
		c.failProposal(ctx, s, i, card, token)
		return nil
	}

	var cardIDs []string
	for idx, messageEmbeds := range messages {
		send := &discordgo.MessageSend{Embeds: messageEmbeds}
		if idx == 0 {
			send.Files = files
		}
		if idx == len(messages)-1 {
			send.Components = components
		}
		sent, serr := s.ChannelMessageSendComplex(i.ChannelID, send)
		if serr != nil {
			logger.FromContext(ctx).Error("failed to send edit proposal message", "error", serr, "character_id", card.CharacterID)
			if len(cardIDs) == 0 {
				c.failProposal(ctx, s, i, card, token)
				return nil
			}
			// Move the buttons onto the last message that did go out so the
			// proposal can still be accepted or rejected.
			if _, eerr := s.ChannelMessageEditComplex(i.ChannelID, cardIDs[len(cardIDs)-1], &discordgo.MessageEdit{Components: &components}); eerr != nil {
				logger.FromContext(ctx).Error("failed to attach proposal buttons", "error", eerr, "character_id", card.CharacterID)
			}
			break
		}
		cardIDs = append(cardIDs, sent.ID)
	}

	c.pendingMu.Lock()
	if pending, ok := c.pendingEdits[token]; ok {
		pending.cardMessageIDs = cardIDs
	}
	c.pendingMu.Unlock()
	return nil
}

// failProposal discards an in-flight proposal and replaces the ack with an
// error message so the user is not left on "rewriting…".
func (c *editCharacterCmd) failProposal(ctx context.Context, s DiscordSession, i *discordgo.InteractionCreate, card *session.CharacterCard, token string) {
	c.dropPending(token)
	if _, ferr := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Content:     utils.PtrString(responses.EditCharacter.ProposalFailed),
		Embeds:      &[]*discordgo.MessageEmbed{},
		Attachments: &[]*discordgo.MessageAttachment{},
		Components:  nil,
	}); ferr != nil {
		logger.FromContext(ctx).Error("failed to report edit proposal failure", "error", ferr, "character_id", card.CharacterID)
	}
}

// handleEditAccept saves a generated proposal: it splices the new section
// into the spec (or replaces the whole spec for general edits), persists the
// card, and edits the preview message into the final confirmation.
func (c *editCharacterCmd) handleEditAccept(ctx context.Context, s DiscordSession, i *discordgo.InteractionCreate) {
	token := strings.TrimPrefix(i.MessageComponentData().CustomID, editAcceptPrefix)
	c.pendingMu.Lock()
	pending, ok := c.pendingEdits[token]
	delete(c.pendingEdits, token)
	c.pendingMu.Unlock()
	if !ok {
		c.respondExpired(ctx, s, i)
		return
	}

	if time.Now().After(pending.expiresAt) {
		c.respondExpiredEphemeral(ctx, s, i)
		c.deleteCardMessages(ctx, s, i, pending.cardMessageIDs)
		return
	}

	card, err := c.session.GetCharacterCard(ctx, i.GuildID, pending.characterID)
	if err != nil || card == nil {
		logger.FromContext(ctx).Error("failed to retrieve character for edit", "error", err, "characterID", pending.characterID, "guild_id", i.GuildID)
		c.respondExpired(ctx, s, i)
		c.deleteCardMessages(ctx, s, i, pending.cardMessageIDs)
		return
	}

	oldSpec := card.Description
	if pending.section == sectionKeyGeneral {
		card.Description = pending.body
	} else {
		newSpec, err := research.ReplaceSection(card.Description, pending.section, pending.body)
		if err != nil {
			logger.FromContext(ctx).Error("failed to replace section", "error", err, "section", pending.section)
			c.respondExpired(ctx, s, i)
			return
		}
		card.Description = newSpec
	}

	if err := c.session.SaveCharacterCard(ctx, i.GuildID, card); err != nil {
		logger.FromContext(ctx).Error("failed to save edited character card", "error", err, "characterID", pending.characterID, "guild_id", i.GuildID)
		c.respondExpired(ctx, s, i)
		return
	}
	c.audit.Log(ctx, i.GuildID, "", pending.characterID, uuid.New().String(), audit.Turn{
		Kind:      audit.KindEdit,
		Latency:   pending.latency,
		Prompt:    pending.prompt,
		Reasoning: pending.reasoning,
		Response:  pending.body,
	})

	if pending.section == sectionKeyGeneral {
		display := *card
		display.Description = markSpecChanges(oldSpec, pending.body)
		messages, files, closeFiles := buildCharacterCardEmbed(c.imageClient, i.GuildID, &display)
		defer closeFiles()

		if len(pending.cardMessageIDs) == 0 {
			data := &discordgo.InteractionResponseData{
				Content: fmt.Sprintf(responses.EditCharacter.Updated, card.DisplayName, "whole persona"),
				Embeds:  messages[0],
			}
			if pending.avatarAttachment != "" {
				data.Attachments = &[]*discordgo.MessageAttachment{{ID: pending.avatarAttachment}}
			}
			if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseUpdateMessage,
				Data: data,
			}); err != nil {
				logger.FromContext(ctx).Error("failed to update accepted persona preview", "error", err, "characterID", pending.characterID)
			}
			return
		}

		// Multi-message confirmation
		if _, err := s.InteractionResponseEdit(pending.orig, &discordgo.WebhookEdit{
			Content:    utils.PtrString(fmt.Sprintf(responses.EditCharacter.Updated, card.DisplayName, "whole persona")),
			Embeds:     &messages[0],
			Files:      files,
			Components: nil,
		}); err != nil {
			logger.FromContext(ctx).Error("failed to update accepted proposal ack", "error", err, "characterID", pending.characterID)
			c.respondExpiredEphemeral(ctx, s, i)
			c.deleteCardMessages(ctx, s, i, pending.cardMessageIDs)
			return
		}

		// Acknowledge the click with an invisible deferred update
		if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseDeferredMessageUpdate,
		}); err != nil {
			logger.FromContext(ctx).Error("failed to acknowledge accepted proposal", "error", err, "characterID", pending.characterID)
		}
		c.deleteCardMessages(ctx, s, i, pending.cardMessageIDs)
		return
	}

	marked := markChanges(currentSectionBody(oldSpec, pending.section), pending.body)
	sectionEmbeds := []*discordgo.MessageEmbed{sectionBodyEmbed(pending.section, marked)}
	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Content:    fmt.Sprintf(responses.EditCharacter.Updated, card.DisplayName, pending.section),
			Embeds:     sectionEmbeds,
			Components: nil,
		},
	}); err != nil {
		logger.FromContext(ctx).Error("failed to update accepted section preview", "error", err, "characterID", pending.characterID)
	}
}

// deleteCardMessages removes the card messages of a multi-message preview.
func (c *editCharacterCmd) deleteCardMessages(ctx context.Context, s DiscordSession, i *discordgo.InteractionCreate, messageIDs []string) {
	for _, id := range messageIDs {
		if err := s.ChannelMessageDelete(i.ChannelID, id); err != nil {
			logger.FromContext(ctx).Error("failed to delete proposal message", "error", err, "message_id", id)
		}
	}
}

// handleEditReject discards a generated proposal without saving.
func (c *editCharacterCmd) handleEditReject(ctx context.Context, s DiscordSession, i *discordgo.InteractionCreate) {
	token := strings.TrimPrefix(i.MessageComponentData().CustomID, editRejectPrefix)
	c.pendingMu.Lock()
	pending, ok := c.pendingEdits[token]
	delete(c.pendingEdits, token)
	c.pendingMu.Unlock()
	if ok {
		if time.Now().After(pending.expiresAt) {
			c.respondExpiredEphemeral(ctx, s, i)
			c.deleteCardMessages(ctx, s, i, pending.cardMessageIDs)
			return
		}
		if len(pending.cardMessageIDs) > 1 {
			// The ack becomes the rejection note and every card message is
			// deleted.
			if _, err := s.InteractionResponseEdit(pending.orig, &discordgo.WebhookEdit{
				Content:    utils.PtrString(responses.EditCharacter.Rejected),
				Embeds:     &[]*discordgo.MessageEmbed{},
				Components: nil,
			}); err != nil {
				logger.FromContext(ctx).Error("failed to update rejected proposal ack", "error", err, "guild_id", i.GuildID)
				c.respondExpiredEphemeral(ctx, s, i)
				c.deleteCardMessages(ctx, s, i, pending.cardMessageIDs)
				return
			}
			// Acknowledge the click with an invisible deferred update
			if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseDeferredMessageUpdate,
			}); err != nil {
				logger.FromContext(ctx).Error("failed to acknowledge rejected proposal", "error", err, "guild_id", i.GuildID)
			}
			c.deleteCardMessages(ctx, s, i, pending.cardMessageIDs)
			return
		}
	}
	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Content:     responses.EditCharacter.Rejected,
			Embeds:      []*discordgo.MessageEmbed{},
			Attachments: &[]*discordgo.MessageAttachment{},
			Components:  nil,
		},
	}); err != nil {
		logger.FromContext(ctx).Error("failed to update rejected section preview", "error", err, "guild_id", i.GuildID)
	}
}

func (c *editCharacterCmd) respondExpired(ctx context.Context, s DiscordSession, i *discordgo.InteractionCreate) {
	logger.FromContext(ctx).Warn("edit proposal no longer available", "guild_id", i.GuildID)
	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Content:     responses.EditCharacter.Expired,
			Embeds:      []*discordgo.MessageEmbed{},
			Attachments: &[]*discordgo.MessageAttachment{},
			Components:  nil,
		},
	}); err != nil {
		logger.FromContext(ctx).Error("failed to report expired edit proposal", "error", err, "guild_id", i.GuildID)
	}
}

// respondExpiredEphemeral notifies just the clicking user that the proposal
// is gone. Used when the proposal is known but its interaction token has
// expired, so the preview messages can no longer be edited in place.
func (c *editCharacterCmd) respondExpiredEphemeral(ctx context.Context, s DiscordSession, i *discordgo.InteractionCreate) {
	logger.FromContext(ctx).Warn("edit proposal token expired", "guild_id", i.GuildID)
	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: responses.EditCharacter.Expired,
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	}); err != nil {
		logger.FromContext(ctx).Error("failed to report expired edit proposal", "error", err, "guild_id", i.GuildID)
	}
}

// currentSectionBody returns the body of the given section, or "" for
// whole-persona rewrites and for sections not yet present in the spec.
func currentSectionBody(spec, section string) string {
	if section == sectionKeyGeneral {
		return ""
	}
	body, _ := research.ExtractSection(spec, section)
	return body
}

func optionValue(data discordgo.ApplicationCommandInteractionData, name string) string {
	opt := data.GetOption(name)
	if opt == nil {
		return ""
	}
	return strings.TrimSpace(opt.StringValue())
}

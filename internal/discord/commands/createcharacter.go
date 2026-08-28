package commands

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"
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
	// avatarSearchLimit is how many image candidates to search for.
	avatarSearchLimit = 10

	// avatarRowLimit is the maximum number of candidates shown in the row.
	avatarRowLimit = 5
)

type createCharacterCmd struct {
	session     *session.Manager
	imageClient images.ImageClient
	synthesizer research.Synthesizer
	audit       *audit.AuditLogger
}

// Definition returns the Discord application command definition for researching and creating a character persona card.
func (c *createCharacterCmd) Definition() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        "createcharacter",
		Description: "Generate a persona from a prompt, save them as a character card, and set them active.",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "description",
				Description: "Who to create. Example: 'Happy Barrett who is chilling on the beach'.",
				Required:    true,
			},
		},
	}
}

// Execute handles the process of researching, synthesizing, and saving a character persona.
func (c *createCharacterCmd) Execute(ctx context.Context, s DiscordSession, i *discordgo.InteractionCreate) error {
	userInput, err := c.parseDescriptionOption(s, i)
	if err != nil {
		return err
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: fmt.Sprintf(responses.SetCharacter.Creating, userInput),
		},
	})

	card, err := c.fetchAndSetupCharacter(ctx, s, i, userInput)
	if err != nil {
		return err
	}

	if err := ApplyCharacterAvatar(ctx, c.imageClient, s, i.GuildID, card.CharacterID, card.ImageURL); err != nil {
		logger.FromContext(ctx).Warn("failed to apply character avatar", "error", err, "guild_id", i.GuildID)
	}

	return c.searchAndProcessImages(ctx, s, i, card)
}

func (c *createCharacterCmd) parseDescriptionOption(s DiscordSession, i *discordgo.InteractionCreate) (string, error) {
	data := i.ApplicationCommandData()
	opts := make(map[string]*discordgo.ApplicationCommandInteractionDataOption)
	for _, o := range data.Options {
		opts[o.Name] = o
	}

	opt, ok := opts["description"]
	if !ok || strings.TrimSpace(opt.StringValue()) == "" {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: responses.SetCharacter.SetMissingPrompt,
			},
		})
		return "", fmt.Errorf("missing description option")
	}
	return opt.StringValue(), nil
}

func (c *createCharacterCmd) fetchAndSetupCharacter(ctx context.Context, s DiscordSession, i *discordgo.InteractionCreate, userInput string) (*session.CharacterCard, error) {

	synth := c.synthesizer

	startAnalysis := time.Now()
	analysis, rawResponse, analysisReasoning, err := synth.AnalyzeInput(ctx, userInput)
	analysisLatency := time.Since(startAnalysis)

	if err != nil {
		logger.FromContext(ctx).Error("input analysis failed", "error", err)
		s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: utils.PtrString(fmt.Sprintf(responses.CreateCharacter.AnalysisFailed, err)),
		})
		return nil, err
	}

	// Mint the character ID as early as possible for logging
	characterID := userInput
	if analysis.Status == research.AnalysisStatusOK {
		characterID = c.mintCharacterID(ctx, i.GuildID, analysis.OfficialName)
	}
	c.audit.Log(ctx, i.GuildID, "", characterID, uuid.New().String(), audit.Turn{
		Kind:      audit.KindAnalysis,
		Latency:   analysisLatency,
		Prompt:    userInput,
		Reasoning: analysisReasoning,
		Response:  rawResponse,
	})

	// Handle Analysis Status
	switch analysis.Status {
	case research.AnalysisStatusUnknown:
		s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: utils.PtrString(fmt.Sprintf(responses.CreateCharacter.Unknown, userInput)),
		})
		return nil, fmt.Errorf("character unknown: %s", userInput)
	case research.AnalysisStatusAmbiguous:
		s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: utils.PtrString(fmt.Sprintf(responses.CreateCharacter.Ambiguous, userInput, strings.Join(analysis.Ambiguities, "\n"))),
		})
		return nil, fmt.Errorf("character ambiguous: %s", userInput)
	case research.AnalysisStatusInjection:
		s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: utils.PtrString(responses.CreateCharacter.Injection),
		})
		return nil, fmt.Errorf("prompt injection detected: %s", userInput)
	case research.AnalysisStatusOK:
		// Continue to research and synthesis
	default:
		return nil, fmt.Errorf("unexpected analysis status: %s", analysis.Status)
	}

	// Research and Synthesize
	start := time.Now()
	res, err := synth.FetchCharacter(ctx, analysis)
	latency := time.Since(start)
	if err != nil {
		s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: utils.PtrString(fmt.Sprintf(responses.SetCharacter.NotFound, userInput, err)),
		})
		return nil, err
	}

	// Log the synthesis to the conversation audit trail
	c.audit.Log(ctx, i.GuildID, "", characterID, uuid.New().String(), audit.Turn{
		Kind:      audit.KindSynthesis,
		Latency:   latency,
		Prompt:    fmt.Sprintf("Request: %s\n\nResearch Dossier:\n%s", userInput, res.ResearchData),
		Reasoning: res.Reasoning,
		Response:  res.PersonaSpec,
	})

	// Handle Synthesis Status
	switch res.Status {
	case research.SynthesisStatusUnknown:
		s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: utils.PtrString(fmt.Sprintf(responses.CreateCharacter.Unknown, userInput)),
		})
		return nil, fmt.Errorf("character unknown: %s", userInput)
	case research.SynthesisStatusAmbiguous:
		s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: utils.PtrString(fmt.Sprintf(responses.CreateCharacter.Ambiguous, userInput, strings.Join(res.Ambiguities, "\n"))),
		})
		return nil, fmt.Errorf("character ambiguous: %s", userInput)
	case research.SynthesisStatusOK:
		// Continue to save
	default:
		return nil, fmt.Errorf("unexpected synthesis status: %s", res.Status)
	}

	// Save New Card
	newCard := &session.CharacterCard{
		CharacterID:  characterID,
		OfficialName: analysis.OfficialName,
		Series:       analysis.Series,
		DisplayName:  analysis.DisplayName,
		Description:  res.PersonaSpec,
	}

	if err := c.session.SaveCharacterCard(ctx, i.GuildID, newCard); err != nil {
		return nil, fmt.Errorf("failed to save character card %s: %w", characterID, err)
	}
	finalCard := newCard

	// Finalize setup: save prompt and update nickname immediately
	if err := c.session.SetActiveCharacter(ctx, i.GuildID, finalCard.CharacterID); err != nil {
		logger.FromContext(ctx).Error("failed to save system prompt", "error", err, "guild_id", i.GuildID)
	}
	if err := s.GuildMemberNickname(i.GuildID, "@me", finalCard.DisplayName); err != nil {
		logger.FromContext(ctx).Error("could not update bot nickname", "error", err, "guild_id", i.GuildID)
	}

	return finalCard, nil
}

func (c *createCharacterCmd) searchAndProcessImages(ctx context.Context, s DiscordSession, i *discordgo.InteractionCreate, card *session.CharacterCard) error {
	if c.imageClient == nil {
		logger.FromContext(ctx).Error("no image client available")
		return fmt.Errorf("no image client available")
	}
	imgResults, err := c.imageClient.SearchImages(ctx, fmt.Sprintf("%s profile picture", card.DisplayName), avatarSearchLimit)
	if err != nil {
		logger.FromContext(ctx).Warn("image search failed", "error", err)
		_, errEdit := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: utils.PtrString(formatCharacterSetMessage(card)),
		})
		if errEdit != nil {
			logger.FromContext(ctx).Error("error editing interaction response after search failure", "error", errEdit)
		}
		return err
	}

	if len(imgResults) == 0 {
		logger.FromContext(ctx).Info("no images found", "character", card.DisplayName)
		_, errEdit := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: utils.PtrString(formatCharacterSetMessage(card)),
		})
		if errEdit != nil {
			logger.FromContext(ctx).Error("error editing interaction response for empty results", "error", errEdit)
		}
		return nil
	}

	logger.FromContext(ctx).Info("found images", "count", len(imgResults), "character", card.DisplayName)

	var urls []string
	titles := make(map[string]string, len(imgResults))
	for _, img := range imgResults {
		urls = append(urls, img.URL)
		titles[img.URL] = img.Title
	}

	// Download the candidates and tile them into a single row image. We
	// Candidates that fail to fetch are skipped, so the row still fills
	// up to avatarRowLimit options.
	rowBytes, included, err := c.imageClient.ComposeRow(ctx, urls, avatarRowLimit)
	if err != nil || len(included) == 0 {
		logger.FromContext(ctx).Warn("no avatar options could be fetched", "error", err)
		_, errEdit := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: utils.PtrString(formatCharacterSetMessage(card)),
		})
		if errEdit != nil {
			logger.FromContext(ctx).Error("error editing interaction response after compose failure", "error", errEdit)
		}
		return nil
	}

	embeds := []*discordgo.MessageEmbed{
		{
			Title: "Avatar options",
			Image: &discordgo.MessageEmbedImage{
				URL: "attachment://avatar_options.png",
			},
		},
	}

	var options []discordgo.SelectMenuOption
	for idx, url := range included {
		options = append(options, discordgo.SelectMenuOption{
			Label:       fmt.Sprintf("Option %d", idx+1),
			Value:       strconv.Itoa(idx),
			Description: utils.TruncateString(titles[url], MaxSelectMenuDescriptionLength),
		})
	}

	// Save candidates to session to retrieve them in HandleComponent
	if err := c.session.SaveImageCandidates(ctx, i.GuildID, included); err != nil {
		logger.FromContext(ctx).Error("failed to save image candidates", "error", err, "guild_id", i.GuildID)
	}

	components := []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.SelectMenu{
					CustomID: setCharacterImageID,
					Options:  options,
				},
			},
		},
	}

	_, errEdit := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Content:    utils.PtrString(formatCharacterSetMessage(card) + "\n\n" + responses.CreateCharacter.SelectPicture),
		Embeds:     &embeds,
		Files:      []*discordgo.File{{Name: "avatar_options.png", ContentType: "image/png", Reader: bytes.NewReader(rowBytes)}},
		Components: &components,
	})
	if errEdit != nil {
		logger.FromContext(ctx).Error("error editing interaction response with select menu", "error", errEdit)
	}

	return nil
}

// handleImageSelection processes the user's selection of a profile picture for the character.
func (c *createCharacterCmd) handleImageSelection(ctx context.Context, s DiscordSession, i *discordgo.InteractionCreate) {
	if len(i.MessageComponentData().Values) == 0 {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: responses.SetCharacter.NoImageSelected,
			},
		})
		return
	}

	// Retrieve candidate images from session
	candidates, err := c.session.GetImageCandidates(ctx, i.GuildID)
	if err != nil {
		logger.FromContext(ctx).Error("failed to retrieve image candidates", "error", err, "guild_id", i.GuildID)
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: responses.SetCharacter.ImageExpired,
			},
		})
		return
	}
	if len(candidates) == 0 {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: responses.SetCharacter.ImageExpired,
			},
		})
		return
	}

	// The value is the index of the image in the candidates list
	idx, err := strconv.Atoi(i.MessageComponentData().Values[0])
	if err != nil || idx < 0 || idx >= len(candidates) {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: responses.SetCharacter.ImageInvalid,
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}
	selectedURL := candidates[idx]

	details, err := c.session.GetCharacterDetails(ctx, i.GuildID)
	if err != nil {
		logger.FromContext(ctx).Error("failed to get active character details", "error", err, "guild_id", i.GuildID)
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: responses.SetCharacter.ImageError,
			},
		})
		return
	}

	// Save image URL to the character card
	if err := c.session.SetCharacterImage(ctx, i.GuildID, details.CharacterID, selectedURL); err != nil {
		logger.FromContext(ctx).Error("failed to save character image", "error", err, "guild_id", i.GuildID)
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: responses.SetCharacter.ImageError,
			},
		})
		return
	}

	// Cache the image and convert to Base64
	if c.imageClient == nil {
		logger.FromContext(ctx).Error("no image client available")
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: responses.SetCharacter.ImageError,
			},
		})
		return
	}

	path, err := c.imageClient.SaveImage(ctx, i.GuildID, details.CharacterID, selectedURL)
	if err != nil {
		logger.FromContext(ctx).Error("failed to cache image", "error", err, "guild_id", i.GuildID, "character_id", details.CharacterID)
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: responses.SetCharacter.ImageError,
			},
		})
		return
	}

	dataURI, err := c.imageClient.ImageToBase64(ctx, path)
	if err != nil {
		logger.FromContext(ctx).Error("error converting image to Base64", "error", err)
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: responses.SetCharacter.ImageError,
			},
		})
		return
	}

	// Update guild-specific avatar
	err = s.UpdateGuildAvatar(i.GuildID, dataURI)
	if err != nil {
		logger.FromContext(ctx).Error("error updating guild avatar", "error", err)
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: responses.SetCharacter.AvatarError,
			},
		})
		return
	}

	// Clean up candidates
	if err := c.session.ClearImageCandidates(ctx, i.GuildID); err != nil {
		logger.FromContext(ctx).Error("failed to clear image candidates", "error", err, "guild_id", i.GuildID)
	}

	// Retrieve full character card for the final success message
	card, err := c.session.GetCharacterCard(ctx, i.GuildID, details.CharacterID)
	if err != nil || card == nil {
		logger.FromContext(ctx).Error("failed to get character card for success message", "error", err, "guild_id", i.GuildID)
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Content: responses.SetCharacter.SetFinalizationError,
			},
		})
		return
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Content:     formatCharacterSetMessage(card),
			Embeds:      nil,
			Attachments: &[]*discordgo.MessageAttachment{},
			Components:  nil,
		},
	})
}

func formatCharacterSetMessage(card *session.CharacterCard) string {
	message := fmt.Sprintf(responses.ListCharacters.SetSuccess, card.DisplayName)
	if card.OfficialName != "" {
		message += fmt.Sprintf(responses.ListCharacters.SetDetail, card.OfficialName)
	}
	return message
}

// mintCharacterID mints a guild-unique character ID from the official name.
func (c *createCharacterCmd) mintCharacterID(ctx context.Context, guildID, officialName string) string {
	taken := make(map[string]bool)
	if cards, err := c.session.GetGuildCharacters(ctx, guildID); err == nil {
		for _, card := range cards {
			taken[card.CharacterID] = true
		}
	}
	return utils.CreateCharacterSlug(officialName, taken)
}

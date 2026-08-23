package commands

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"characterllm/internal/images"
	"characterllm/internal/logger"
	"characterllm/internal/research"
	"characterllm/internal/responses"
	"characterllm/internal/search"
	"characterllm/internal/session"
	"characterllm/internal/utils"

	"github.com/bwmarrin/discordgo"
	"github.com/google/uuid"
)

type setCharacterCmd struct{}

// Definition returns the Discord application command definition for setting the character persona.
func (c *setCharacterCmd) Definition() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        "setcharacter",
		Description: "Set the persona using a prompt.",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "prompt",
				Description: "Prompt to generate a persona from.",
				Required:    true,
			},
		},
	}
}

// Execute handles the process of searching for a character and setting their persona.
func (c *setCharacterCmd) Execute(ctx context.Context, cmdCtx CommandContext, s *discordgo.Session, i *discordgo.InteractionCreate) error {
	userInput, err := c.parsePromptOption(s, i)
	if err != nil {
		return err
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: fmt.Sprintf(responses.MsgCharCreating, userInput),
		},
	})

	card, err := c.fetchAndSetupCharacter(ctx, cmdCtx, s, i, userInput)
	if err != nil {
		return err
	}

	return c.searchAndProcessImages(ctx, cmdCtx, s, i, card)
}

func (c *setCharacterCmd) parsePromptOption(s *discordgo.Session, i *discordgo.InteractionCreate) (string, error) {
	data := i.ApplicationCommandData()
	opts := make(map[string]*discordgo.ApplicationCommandInteractionDataOption)
	for _, o := range data.Options {
		opts[o.Name] = o
	}

	opt, ok := opts["prompt"]
	if !ok {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: responses.MsgSetCharMissingPrompt,
			},
		})
		return "", fmt.Errorf("missing prompt option")
	}
	return opt.StringValue(), nil
}

func (c *setCharacterCmd) fetchAndSetupCharacter(ctx context.Context, cmdCtx CommandContext, s *discordgo.Session, i *discordgo.InteractionCreate, userInput string) (*session.CharacterCard, error) {
	// 1. Resolve Alias First (Fast Path)
	if card, err := cmdCtx.GetSession().GetCardByAlias(ctx, i.GuildID, userInput); err == nil && card != nil {
		logger.FromContext(ctx).Info("found character by alias", "alias", userInput, "name", card.DisplayName)

		// Finalize setup for cached card
		if err := cmdCtx.GetSession().SetActiveCharacter(ctx, i.GuildID, card.CharacterID); err != nil {
			logger.FromContext(ctx).Error("failed to save system prompt", "error", err, "guild_id", i.GuildID)
		}
		if err := s.GuildMemberNickname(i.GuildID, "@me", card.DisplayName); err != nil {
			logger.FromContext(ctx).Error("could not update bot nickname", "error", err, "guild_id", i.GuildID)
		}

		return card, nil
	} else if err != nil {
		logger.FromContext(ctx).Error("error resolving alias", "error", err, "alias", userInput)
	}

	// 2. Analyze Input (Intent, Modifier, Injection, Nonsense)
	cfg := cmdCtx.GetConfig()
	sp := search.NewSearXNGProvider(cfg.Images.SearXNGURL)
	synth := research.NewSynthesizer(sp, cmdCtx.GetLLM(), cfg)

	startAnalysis := time.Now()
	analysis, rawResponse, analysisReasoning, err := synth.AnalyzeInput(ctx, userInput)
	analysisLatency := time.Since(startAnalysis)

	if err != nil {
		logger.FromContext(ctx).Error("input analysis failed", "error", err)
		s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: utils.PtrString(fmt.Sprintf("I had trouble understanding your request: %v", err)),
		})
		return nil, err
	}

	// Resolve the canonical slug as early as possible for logging
	characterID := userInput
	if analysis.Status == "OK" {
		characterID = utils.CreateCharacterSlug(analysis.OfficialName, analysis.Modifiers, analysis.ScenarioID)
	}
	cmdCtx.GetAudit().LogConversation(ctx, i.GuildID, characterID, fmt.Sprintf("Analyze Input: %s", userInput), analysisReasoning, rawResponse, nil, analysisLatency, uuid.New().String())

	// 3. Handle Analysis Status
	switch analysis.Status {
	case "UNKNOWN":
		s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: utils.PtrString(fmt.Sprintf("I couldn't find any reliable information on '%s'. Could you provide more details or the series they are from?", userInput)),
		})
		return nil, fmt.Errorf("character unknown: %s", userInput)
	case "AMBIGUOUS":
		s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: utils.PtrString(fmt.Sprintf("I found multiple characters named '%s':\n%s\nPlease be more specific!", userInput, strings.Join(analysis.Ambiguities, "\n"))),
		})
		return nil, fmt.Errorf("character ambiguous: %s", userInput)
	case "INJECTION":
		s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: utils.PtrString("Nice try! I'm not falling for that prompt injection. Please provide a valid character name."),
		})
		return nil, fmt.Errorf("prompt injection detected: %s", userInput)
	case "OK":
		// Continue to research and synthesis
	default:
		return nil, fmt.Errorf("unexpected analysis status: %s", analysis.Status)
	}

	// 4. Research and Synthesize
	start := time.Now()
	res, err := synth.FetchCharacter(ctx, analysis)
	latency := time.Since(start)
	if err != nil {
		s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: utils.PtrString(fmt.Sprintf(responses.MsgCharNotFound, userInput, err)),
		})
		return nil, err
	}

	// Log the synthesis to the conversation audit trail
	logPrompt := fmt.Sprintf("Request: %s\n\nResearch Dossier:\n%s", userInput, res.ResearchData)
	cmdCtx.GetAudit().LogConversation(ctx, i.GuildID, characterID, logPrompt, res.Reasoning, res.PersonaSpec, nil, latency, uuid.New().String())

	// 5. Handle Synthesis Status
	switch res.Status {
	case "UNKNOWN":
		s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: utils.PtrString(fmt.Sprintf("I couldn't find any reliable information on '%s'. Could you provide more details or the series they are from?", userInput)),
		})
		return nil, fmt.Errorf("character unknown: %s", userInput)
	case "AMBIGUOUS":
		s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: utils.PtrString(fmt.Sprintf("I found multiple characters named '%s':\n%s\nPlease be more specific!", userInput, strings.Join(res.Ambiguities, "\n"))),
		})
		return nil, fmt.Errorf("character ambiguous: %s", userInput)
	case "OK":
		// Continue to canonical check
	default:
		return nil, fmt.Errorf("unexpected synthesis status: %s", res.Status)
	}

	// 6. Canonical Check (Guild-Specific)
	card, err := cmdCtx.GetSession().GetCharacterCard(ctx, i.GuildID, characterID)
	if err != nil {
		return nil, err
	}

	var finalCard *session.CharacterCard
	if card != nil {
		logger.FromContext(ctx).Info("resolved to existing canonical card", "characterID", characterID, "series", analysis.Series)
		finalCard = card
	} else {
		// 7. Save New Card and Aliases
		newCard := &session.CharacterCard{
			CharacterID:  characterID,
			OfficialName: analysis.OfficialName,
			Series:       analysis.Series,
			DisplayName:  analysis.DisplayName,
			Description:  res.PersonaSpec,
			SourceURL:    "SearXNG", // Simplification for now
			Modifiers:    strings.Join(analysis.Modifiers, ", "),
			Scenario:     analysis.Scenario,
		}

		if err := cmdCtx.GetSession().SaveCharacterCard(ctx, i.GuildID, newCard, analysis.Aliases); err != nil {
			logger.FromContext(ctx).Error("failed to save canonical character card", "error", err, "characterID", characterID)
		}
		finalCard = newCard
	}

	// Finalize setup: save prompt and update nickname immediately
	if err := cmdCtx.GetSession().SetActiveCharacter(ctx, i.GuildID, finalCard.CharacterID); err != nil {
		logger.FromContext(ctx).Error("failed to save system prompt", "error", err, "guild_id", i.GuildID)
	}
	if err := s.GuildMemberNickname(i.GuildID, "@me", finalCard.DisplayName); err != nil {
		logger.FromContext(ctx).Error("could not update bot nickname", "error", err, "guild_id", i.GuildID)
	}

	return finalCard, nil
}

func (c *setCharacterCmd) searchAndProcessImages(ctx context.Context, cmdCtx CommandContext, s *discordgo.Session, i *discordgo.InteractionCreate, card *session.CharacterCard) error {
	imgClient, err := images.NewImageClient(cmdCtx.GetConfig())
	if err != nil {
		logger.FromContext(ctx).Error("failed to initialize image client", "error", err)
		return err
	}
	imgResults, err := imgClient.Provider.SearchImages(ctx, fmt.Sprintf("%s profile picture", card.DisplayName), 5)
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

	var embeds []*discordgo.MessageEmbed
	var options []discordgo.SelectMenuOption
	var urls []string
	for idx, img := range imgResults {
		urls = append(urls, img.URL)

		embeds = append(embeds, &discordgo.MessageEmbed{
			Title:       fmt.Sprintf("Option %d", idx+1),
			Description: img.Title,
			Image: &discordgo.MessageEmbedImage{
				URL: img.URL,
			},
		})

		options = append(options, discordgo.SelectMenuOption{
			Label:       fmt.Sprintf("Option %d", idx+1),
			Value:       strconv.Itoa(idx),
			Description: utils.TruncateString(img.Title, 100),
		})
	}

	// Save candidates to session to retrieve them in ComponentCreate
	if err := cmdCtx.GetSession().SaveImageCandidates(ctx, i.GuildID, urls); err != nil {
		logger.FromContext(ctx).Error("failed to save image candidates", "error", err, "guild_id", i.GuildID)
	}

	components := []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.SelectMenu{
					CustomID: "select_char_image",
					Options:  options,
				},
			},
		},
	}

	_, errEdit := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Content:    utils.PtrString(formatCharacterSetMessage(card) + "\n\nPlease select a profile picture from the options below:"),
		Embeds:     &embeds,
		Components: &components,
	})
	if errEdit != nil {
		logger.FromContext(ctx).Error("error editing interaction response with select menu", "error", errEdit)
	}

	return nil
}

// HandleSetCharacterImage processes the user's selection of a profile picture for the character.
func HandleSetCharacterImage(ctx context.Context, cmdCtx CommandContext, s *discordgo.Session, i *discordgo.InteractionCreate) {
	if len(i.MessageComponentData().Values) == 0 {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: responses.MsgNoImageSelected,
			},
		})
		return
	}

	// Retrieve candidate images from session
	candidates, err := cmdCtx.GetSession().GetImageCandidates(ctx, i.GuildID)
	if err != nil {
		logger.FromContext(ctx).Error("failed to retrieve image candidates", "error", err, "guild_id", i.GuildID)
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: responses.MsgCharImageExpired,
			},
		})
		return
	}
	if len(candidates) == 0 {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: responses.MsgCharImageExpired,
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
				Content: responses.MsgCharImageInvalid,
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}
	selectedURL := candidates[idx]

	// Save image URL to database
	if err := cmdCtx.GetSession().SetCharacterImage(ctx, i.GuildID, selectedURL); err != nil {
		logger.FromContext(ctx).Error("failed to save character image", "error", err, "guild_id", i.GuildID)
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: responses.MsgCharImageError,
			},
		})
		return
	}

	// Cache the image and convert to Base64
	imgClient, err := images.NewImageClient(cmdCtx.GetConfig())
	if err != nil {
		logger.FromContext(ctx).Error("failed to initialize image client", "error", err)
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: responses.MsgCharImageError,
			},
		})
		return
	}

	details, err := cmdCtx.GetSession().GetCharacterDetails(ctx, i.GuildID)
	if err != nil {
		logger.FromContext(ctx).Error("failed to get active character details", "error", err, "guild_id", i.GuildID)
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: responses.MsgCharImageError,
			},
		})
		return
	}

	path, err := imgClient.Cache.SaveImage(ctx, i.GuildID, details.CharacterID, selectedURL)
	if err != nil {
		logger.FromContext(ctx).Error("failed to cache image", "error", err, "guild_id", i.GuildID, "character_id", details.CharacterID)
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: responses.MsgCharImageError,
			},
		})
		return
	}

	dataURI, err := imgClient.Cache.ImageToBase64(ctx, path)
	if err != nil {
		logger.FromContext(ctx).Error("error converting image to Base64", "error", err)
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: responses.MsgCharImageError,
			},
		})
		return
	}

	// Update guild-specific avatar
	err = updateGuildAvatar(s, i.GuildID, dataURI)
	if err != nil {
		logger.FromContext(ctx).Error("error updating guild avatar", "error", err, "guild_id", i.GuildID)
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: responses.MsgCharAvatarError,
			},
		})
		return
	}

	// Clean up candidates
	if err := cmdCtx.GetSession().ClearImageCandidates(ctx, i.GuildID); err != nil {
		logger.FromContext(ctx).Error("failed to clear image candidates", "error", err, "guild_id", i.GuildID)
	}

	// Retrieve full character card for the final success message
	card, err := cmdCtx.GetSession().GetCharacterCard(ctx, i.GuildID, details.CharacterID)
	if err != nil || card == nil {
		logger.FromContext(ctx).Error("failed to get character card for success message", "error", err, "guild_id", i.GuildID)
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Content: responses.MsgCharSetFinalizationError,
			},
		})
		return
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Content:    formatCharacterSetMessage(card),
			Embeds:     nil,
			Components: nil,
		},
	})
}

func formatCharacterSetMessage(card *session.CharacterCard) string {
	message := fmt.Sprintf("Character set to **%s**!", card.DisplayName)
	if card.OfficialName != "" {
		message += fmt.Sprintf("\nCharacter: %s", card.OfficialName)
	}
	if card.Modifiers != "" {
		message += fmt.Sprintf("\nModifiers: %s", card.Modifiers)
	}
	if card.Scenario != "" {
		message += fmt.Sprintf("\nScenario: %s", card.Scenario)
	}
	return message
}

func init() {
	Register(&setCharacterCmd{})
}

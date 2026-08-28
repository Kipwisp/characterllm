package commands

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"characterllm/internal/audit"
	"characterllm/internal/config"
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
	config      *config.Config
}

// createResult carries everything the avatar finalization needs out of the
// research pipeline.
type createResult struct {
	card          *session.CharacterCard
	candidates    []string
	rowBytes      []byte
	titles        map[string]string
	avatarChoice  int
	pickAttempted bool
	greeting      string
}

// characterSetupMessage is the confirmation message shown once a character is
// created: the character's own greeting, falling back to the boilerplate
// "Character set to…" line when the greeting is missing or empty.
func characterSetupMessage(card *session.CharacterCard, greeting string) string {
	if strings.TrimSpace(greeting) != "" {
		return greeting
	}
	return fmt.Sprintf(responses.ListCharacters.SetSuccess, card.DisplayName)
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

	result, err := c.fetchAndSetupCharacter(ctx, s, i, userInput)
	if err != nil {
		return err
	}

	if err := ApplyCharacterAvatar(ctx, c.imageClient, s, i.GuildID, result.card.CharacterID, result.card.ImageURL); err != nil {
		logger.FromContext(ctx).Warn("failed to apply character avatar", "error", err, "guild_id", i.GuildID)
	}

	return c.finalizeAvatar(ctx, s, i, result)
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

func (c *createCharacterCmd) fetchAndSetupCharacter(ctx context.Context, s DiscordSession, i *discordgo.InteractionCreate, userInput string) (*createResult, error) {

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

	// Avatar candidates
	candidates, rowBytes, titles := c.fetchAvatarCandidates(ctx, analysis)

	var imageURIs []string
	if c.config.LLM.AvatarPick && len(candidates) > 0 {
		if c.config.LLM.Vision {
			imageURIs = []string{utils.PNGDataURI(rowBytes)}
		} else {
			logger.FromContext(ctx).Info("avatar model pick enabled but LLM vision is disabled; falling back to manual selection")
		}
	}

	// Research and Synthesize
	start := time.Now()
	res, err := synth.FetchCharacter(ctx, analysis, imageURIs, len(candidates))
	latency := time.Since(start)
	if err != nil {
		s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: utils.PtrString(fmt.Sprintf(responses.SetCharacter.NotFound, userInput, err)),
		})
		return nil, err
	}

	// Log the synthesis to the conversation audit trail
	synthesisPrompt := fmt.Sprintf("Request: %s\n\nResearch Dossier:\n%s", userInput, res.ResearchData)
	if len(candidates) > 0 {
		pick := "none"
		if len(imageURIs) > 0 && res.AvatarChoice > 0 {
			pick = fmt.Sprintf("%d of %d", res.AvatarChoice, len(candidates))
		}
		synthesisPrompt += fmt.Sprintf("\n\nAvatar candidates: %d (model pick: %s)", len(candidates), pick)
	}

	synthesisResponse := res.PersonaSpec
	if res.RawResponse != "" {
		synthesisResponse = res.RawResponse
	}
	c.audit.Log(ctx, i.GuildID, "", characterID, uuid.New().String(), audit.Turn{
		Kind:      audit.KindSynthesis,
		Latency:   latency,
		Prompt:    synthesisPrompt,
		Reasoning: res.Reasoning,
		Response:  synthesisResponse,
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

	greeting, _ := research.ExtractSection(res.PersonaSpec, research.SectionGreeting)

	return &createResult{
		card:          finalCard,
		candidates:    candidates,
		rowBytes:      rowBytes,
		titles:        titles,
		avatarChoice:  res.AvatarChoice,
		pickAttempted: len(imageURIs) > 0,
		greeting:      greeting,
	}, nil
}

// fetchAvatarCandidates searches for candidate profile pictures and composes
// them into a row. Any failure degrades to no candidates (no images on the
// synthesis call, no avatar menu), which matches the pre-feature behavior.
func (c *createCharacterCmd) fetchAvatarCandidates(ctx context.Context, analysis *research.AnalysisResult) ([]string, []byte, map[string]string) {
	if c.imageClient == nil {
		logger.FromContext(ctx).Error("no image client available")
		return nil, nil, nil
	}
	query := fmt.Sprintf("%s profile picture", analysis.DisplayName)
	if analysis.Series != "" {
		query = fmt.Sprintf("%s (%s) profile picture", analysis.DisplayName, analysis.Series)
	}
	imgResults, err := c.imageClient.SearchImages(ctx, query, avatarSearchLimit)
	if err != nil {
		logger.FromContext(ctx).Warn("image search failed", "error", err)
		return nil, nil, nil
	}

	if len(imgResults) == 0 {
		logger.FromContext(ctx).Info("no images found", "character", analysis.DisplayName)
		return nil, nil, nil
	}

	var urls []string
	titles := make(map[string]string, len(imgResults))
	for _, img := range imgResults {
		urls = append(urls, img.URL)
		titles[img.URL] = img.Title
	}

	// Download the candidates and tile them into a single row image.
	// Candidates that fail to fetch are skipped, so the row still fills
	// up to avatarRowLimit options.
	rowBytes, included, err := c.imageClient.ComposeRow(ctx, urls, avatarRowLimit)
	if err != nil || len(included) == 0 {
		logger.FromContext(ctx).Warn("no avatar options could be fetched", "error", err)
		return nil, nil, nil
	}

	logger.FromContext(ctx).Info("found images", "count", len(included), "character", analysis.DisplayName)
	return included, rowBytes, titles
}

// finalizeAvatar applies the model's avatar pick when it is valid, otherwise
// offers the candidates in the manual select menu.
func (c *createCharacterCmd) finalizeAvatar(ctx context.Context, s DiscordSession, i *discordgo.InteractionCreate, r *createResult) error {
	if r.isModelPickValid() {
		if err := c.applyAvatar(ctx, s, i.GuildID, r.card.CharacterID, r.candidates[r.avatarChoice-1]); err != nil {
			logger.FromContext(ctx).Warn("failed to apply model-picked avatar, falling back to manual selection", "error", err, "guild_id", i.GuildID, "character_id", r.card.CharacterID)
		} else {
			logger.FromContext(ctx).Info("model-picked avatar applied", "guild_id", i.GuildID, "character_id", r.card.CharacterID, "index", r.avatarChoice)
			_, errEdit := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
				Content: utils.PtrString(characterSetupMessage(r.card, r.greeting)),
			})
			if errEdit != nil {
				logger.FromContext(ctx).Error("error editing interaction response after avatar pick", "error", errEdit)
			}
			return nil
		}
	}
	return c.renderAvatarMenu(ctx, s, i, r, r.pickAttempted)
}

// isModelPickValid reports whether the model was shown candidates and picked
// one of them.
func (r *createResult) isModelPickValid() bool {
	return r.pickAttempted && r.avatarChoice >= 1 && r.avatarChoice <= len(r.candidates)
}

// renderAvatarMenu shows the candidate row with a manual select menu, or a
// plain confirmation message when no candidates are available.
func (c *createCharacterCmd) renderAvatarMenu(ctx context.Context, s DiscordSession, i *discordgo.InteractionCreate, r *createResult, pickFailed bool) error {
	if len(r.candidates) == 0 {
		_, errEdit := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: utils.PtrString(characterSetupMessage(r.card, r.greeting)),
		})
		if errEdit != nil {
			logger.FromContext(ctx).Error("error editing interaction response for empty results", "error", errEdit)
		}
		return nil
	}

	prompt := responses.CreateCharacter.SelectPicture
	if pickFailed {
		prompt = responses.CreateCharacter.PickFailed + " " + prompt
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
	for idx, url := range r.candidates {
		options = append(options, discordgo.SelectMenuOption{
			Label:       fmt.Sprintf("Option %d", idx+1),
			Value:       strconv.Itoa(idx),
			Description: utils.TruncateString(r.titles[url], MaxSelectMenuDescriptionLength),
		})
	}

	// Save candidates under a per-menu token
	menuToken := newComponentToken()
	if err := c.session.SaveImageCandidates(ctx, menuToken, r.candidates); err != nil {
		logger.FromContext(ctx).Error("failed to save image candidates", "error", err)
	}

	components := []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.SelectMenu{
					CustomID: setCharacterImagePrefix + menuToken,
					Options:  options,
				},
			},
		},
	}

	_, errEdit := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Content:    utils.PtrString(characterSetupMessage(r.card, r.greeting) + "\n\n" + prompt),
		Embeds:     &embeds,
		Files:      []*discordgo.File{{Name: "avatar_options.png", ContentType: "image/png", Reader: bytes.NewReader(r.rowBytes)}},
		Components: &components,
	})
	if errEdit != nil {
		logger.FromContext(ctx).Error("error editing interaction response with select menu", "error", errEdit)
	}

	return nil
}

// applyAvatar persists url as the character's avatar on the card and uploads
// it as the guild avatar.
func (c *createCharacterCmd) applyAvatar(ctx context.Context, s DiscordSession, guildID, characterID, url string) error {
	if err := c.session.SetCharacterImage(ctx, guildID, characterID, url); err != nil {
		return fmt.Errorf("failed to save character image: %w", err)
	}
	return ApplyCharacterAvatar(ctx, c.imageClient, s, guildID, characterID, url)
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
	menuToken := strings.TrimPrefix(i.MessageComponentData().CustomID, setCharacterImagePrefix)
	candidates, err := c.session.GetImageCandidates(ctx, menuToken)
	if err != nil {
		logger.FromContext(ctx).Error("failed to retrieve image candidates", "error", err, "menu_token", menuToken)
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

	if err := c.applyAvatar(ctx, s, i.GuildID, details.CharacterID, selectedURL); err != nil {
		logger.FromContext(ctx).Error("failed to apply selected avatar", "error", err, "guild_id", i.GuildID, "character_id", details.CharacterID)
		content := responses.SetCharacter.ImageError
		if errors.Is(err, errGuildAvatarUpdate) {
			content = responses.SetCharacter.AvatarError
		}
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: content,
			},
		})
		return
	}

	// Clean up candidates
	if err := c.session.ClearImageCandidates(ctx, menuToken); err != nil {
		logger.FromContext(ctx).Error("failed to clear image candidates", "error", err, "menu_token", menuToken)
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

	greeting, _ := research.ExtractSection(card.Description, research.SectionGreeting)
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Content:     characterSetupMessage(card, greeting),
			Embeds:      nil,
			Attachments: &[]*discordgo.MessageAttachment{},
			Components:  nil,
		},
	})
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

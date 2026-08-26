package commands

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"characterllm/internal/images"
	"characterllm/internal/logger"
	"characterllm/internal/responses"
	"characterllm/internal/session"
	"characterllm/internal/utils"

	"github.com/bwmarrin/discordgo"
)

type listCharactersCmd struct {
	session     *session.Manager
	imageClient images.ImageClient
}

// Definition returns the Discord application command definition for listing available character cards in a guild.
func (c *listCharactersCmd) Definition() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        "listcharacters",
		Description: "List all saved character cards for this guild.",
	}
}

// Execute handles the process of listing characters and providing a selection menu.
func (c *listCharactersCmd) Execute(ctx context.Context, s DiscordSession, i *discordgo.InteractionCreate) error {
	return c.render(ctx, s, i, 0, true)
}

func (c *listCharactersCmd) render(ctx context.Context, s DiscordSession, i *discordgo.InteractionCreate, page int, isInitialResponse bool) error {
	cards, err := c.session.GetGuildCharacters(ctx, i.GuildID)
	if err != nil {
		logger.FromContext(ctx).Error("failed to retrieve characters", "error", err, "guild_id", i.GuildID)
		return err
	}

	if len(cards) == 0 {
		content := responses.ListCharacters.Empty
		if isInitialResponse {
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{Content: content},
			})
		} else {
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseUpdateMessage,
				Data: &discordgo.InteractionResponseData{Content: content},
			})
		}
		return nil
	}

	pageSize := 25
	totalCards := len(cards)
	start := page * pageSize
	end := start + pageSize
	if end > totalCards {
		end = totalCards
	}

	if start >= totalCards {
		start = totalCards - pageSize
		if start < 0 {
			start = 0
		}
		page = start / pageSize
	}

	var options []discordgo.SelectMenuOption
	for _, card := range cards[start:end] {
		var details []string
		if card.Series != "" {
			details = append(details, fmt.Sprintf("Series: %s", card.Series))
		}
		if card.Modifiers != "" {
			details = append(details, fmt.Sprintf("Modifiers: %s", card.Modifiers))
		}
		if card.Scenario != "" {
			details = append(details, fmt.Sprintf("Scenario: %s", card.Scenario))
		}
		desc := strings.Join(details, " | ")

		options = append(options, discordgo.SelectMenuOption{
			Label:       card.DisplayName,
			Value:       card.CharacterID,
			Description: utils.TruncateString(desc, MaxSelectMenuDescriptionLength),
		})
	}

	components := []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.SelectMenu{
					CustomID: selectCharacterCardID,
					Options:  options,
				},
			},
		},
	}

	// Add Pagination Buttons
	var buttons []discordgo.MessageComponent
	if page > 0 {
		buttons = append(buttons, discordgo.Button{
			Label:    "Prev",
			Style:    discordgo.PrimaryButton,
			CustomID: listPaginationID("prev", page),
		})
	}
	if end < totalCards {
		buttons = append(buttons, discordgo.Button{
			Label:    "Next",
			Style:    discordgo.PrimaryButton,
			CustomID: listPaginationID("next", page),
		})
	}

	if len(buttons) > 0 {
		components = append(components, discordgo.ActionsRow{
			Components: buttons,
		})
	}

	content := fmt.Sprintf("%s (Page %d of **%d**)", responses.ListCharacters.SelectPrompt, page+1, (totalCards+pageSize-1)/pageSize)

	if isInitialResponse {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content:    content,
				Components: components,
			},
		})
	} else {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Content:    content,
				Components: components,
			},
		})
	}

	return nil
}

// handlePagination processes the "Next" and "Prev" buttons for the character list.
func (c *listCharactersCmd) handlePagination(ctx context.Context, s DiscordSession, i *discordgo.InteractionCreate) {
	rest := strings.TrimPrefix(i.MessageComponentData().CustomID, listPaginationPrefix)
	parts := strings.SplitN(rest, "_", 2)
	if len(parts) != 2 {
		return
	}

	direction := parts[0] // "prev" or "next"
	currentPage, err := strconv.Atoi(parts[1])
	if err != nil {
		return
	}

	newPage := currentPage
	if direction == "next" {
		newPage++
	} else if direction == "prev" {
		newPage--
	}

	if err := c.render(ctx, s, i, newPage, false); err != nil {
		logger.FromContext(ctx).Error("pagination failed", "error", err, "guild_id", i.GuildID)
	}
}

// handleSelectCard processes the user's selection of a character card.
func (c *listCharactersCmd) handleSelectCard(ctx context.Context, s DiscordSession, i *discordgo.InteractionCreate) {
	if len(i.MessageComponentData().Values) == 0 {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: responses.ListCharacters.NoSelected,
			},
		})
		return
	}

	characterID := i.MessageComponentData().Values[0]

	// Verify the card exists for this guild
	card, err := c.session.GetCharacterCard(ctx, i.GuildID, characterID)
	if err != nil || card == nil {
		logger.FromContext(ctx).Error("failed to retrieve selected character card", "error", err, "characterID", characterID, "guild_id", i.GuildID)
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: responses.ListCharacters.NotFound,
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	// Set as active character
	if err := c.session.SetActiveCharacter(ctx, i.GuildID, characterID); err != nil {
		logger.FromContext(ctx).Error("failed to set active character", "error", err, "guild_id", i.GuildID)
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: responses.ListCharacters.SetError,
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	// Sync bot identity (nickname + avatar) to the selected character
	if err := SyncGuildIdentity(ctx, c.session, c.imageClient, s, i.GuildID); err != nil {
		logger.FromContext(ctx).Warn("failed to sync guild identity", "error", err, "guild_id", i.GuildID)
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Content:    fmt.Sprintf(responses.ListCharacters.SetSuccess, card.DisplayName),
			Components: nil,
		},
	})
}

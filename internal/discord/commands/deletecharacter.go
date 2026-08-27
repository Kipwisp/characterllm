package commands

import (
	"context"
	"fmt"
	"strings"

	"characterllm/internal/images"
	"characterllm/internal/logger"
	"characterllm/internal/responses"
	"characterllm/internal/session"

	"github.com/bwmarrin/discordgo"
)

type deleteCharacterCmd struct {
	session     *session.Manager
	imageClient images.ImageClient
}

// Definition returns the Discord application command definition for deleting a saved character.
func (c *deleteCharacterCmd) Definition() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        "deletecharacter",
		Description: "Delete a saved character card and all its chat threads.",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:         discordgo.ApplicationCommandOptionString,
				Name:         "name",
				Description:  "Character name to delete, or 'current' for the active character.",
				Required:     true,
				Autocomplete: true,
			},
		},
	}
}

// Execute resolves the character and asks for confirmation before deleting.
func (c *deleteCharacterCmd) Execute(ctx context.Context, s DiscordSession, i *discordgo.InteractionCreate) error {
	name := i.ApplicationCommandData().GetOption("name").StringValue()
	card, err := resolveNameOrCurrent(ctx, c.session, i.GuildID, name)
	if err != nil {
		return respondResolveError(ctx, s, i, err)
	}

	components := []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{
					Label:    "Delete",
					Style:    discordgo.DangerButton,
					CustomID: deleteConfirmID(card.CharacterID),
				},
				discordgo.Button{
					Label:    "Cancel",
					Style:    discordgo.SecondaryButton,
					CustomID: deleteCancelID(card.CharacterID),
				},
			},
		},
	}

	count, err := c.session.CountCharacterThreads(ctx, i.GuildID, card.CharacterID)
	if err != nil {
		logger.FromContext(ctx).Warn("failed to count threads before deletion confirmation", "error", err, "characterID", card.CharacterID)
	}

	embed, files, closeFiles := characterAvatarEmbed(c.imageClient, i.GuildID, card)
	defer closeFiles()
	embed.Description = fmt.Sprintf(responses.DeleteCharacter.ConfirmPrompt, card.DisplayName, count)
	if card.OfficialName != "" {
		embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{Name: "Official name", Value: card.OfficialName, Inline: true})
	}
	if card.Series != "" {
		embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{Name: "Series", Value: card.Series, Inline: true})
	}
	embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{Name: "ID", Value: "`" + card.CharacterID + "`", Inline: true})

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Files:      files,
			Components: components,
		},
	})
	return nil
}

// handleDeleteConfirm deletes the character card, history, and cached
// image, clearing the active pointer if the character was active.
func (c *deleteCharacterCmd) handleDeleteConfirm(ctx context.Context, s DiscordSession, i *discordgo.InteractionCreate) {
	characterID := strings.TrimPrefix(i.MessageComponentData().CustomID, deleteConfirmPrefix)

	card, err := c.session.GetCharacterCard(ctx, i.GuildID, characterID)
	if err != nil || card == nil {
		logger.FromContext(ctx).Error("failed to retrieve character for deletion", "error", err, "characterID", characterID, "guild_id", i.GuildID)
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: responses.ListCharacters.NotFound,
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	details, err := c.session.GetCharacterDetails(ctx, i.GuildID)
	wasActive := err == nil && details != nil && details.CharacterID == characterID

	count, err := c.session.CountCharacterThreads(ctx, i.GuildID, characterID)
	if err != nil {
		logger.FromContext(ctx).Warn("failed to count threads before deletion", "error", err, "characterID", characterID)
	}

	if err := c.session.DeleteCharacterCard(ctx, i.GuildID, characterID); err != nil {
		logger.FromContext(ctx).Error("failed to delete character card", "error", err, "characterID", characterID, "guild_id", i.GuildID)
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Content:     responses.DeleteCharacter.Error,
				Embeds:      []*discordgo.MessageEmbed{},
				Attachments: &[]*discordgo.MessageAttachment{},
				Components:  nil,
			},
		})
		return
	}

	if c.imageClient != nil {
		if err := c.imageClient.DeleteImage(i.GuildID, characterID); err != nil {
			logger.FromContext(ctx).Warn("failed to delete cached character image", "error", err, "characterID", characterID)
		}
	}

	if wasActive {
		if err := c.session.SetActiveCharacter(ctx, i.GuildID, ""); err != nil {
			logger.FromContext(ctx).Warn("failed to clear active character after deletion", "error", err, "guild_id", i.GuildID)
		}
		if err := s.GuildMemberNickname(i.GuildID, "@me", ""); err != nil {
			logger.FromContext(ctx).Warn("failed to reset bot nickname after deletion", "error", err, "guild_id", i.GuildID)
		}
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Content:     fmt.Sprintf(responses.DeleteCharacter.Deleted, card.DisplayName, count),
			Embeds:      []*discordgo.MessageEmbed{},
			Attachments: &[]*discordgo.MessageAttachment{},
			Components:  nil,
		},
	})
}

// handleDeleteCancel backs out of a pending deletion.
func (c *deleteCharacterCmd) handleDeleteCancel(ctx context.Context, s DiscordSession, i *discordgo.InteractionCreate) {
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Content:     responses.DeleteCharacter.Cancelled,
			Embeds:      []*discordgo.MessageEmbed{},
			Attachments: &[]*discordgo.MessageAttachment{},
			Components:  nil,
		},
	})
}

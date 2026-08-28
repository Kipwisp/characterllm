package commands

import (
	"context"
	"fmt"
	"strings"

	"characterllm/internal/images"
	"characterllm/internal/logger"
	"characterllm/internal/research"
	"characterllm/internal/responses"
	"characterllm/internal/session"

	"github.com/bwmarrin/discordgo"
)

type setCharacterCmd struct {
	session     *session.Manager
	imageClient images.ImageClient
}

// Definition returns the Discord application command definition for setting the active character.
func (c *setCharacterCmd) Definition() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        "setcharacter",
		Description: "Set the active character.",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:         discordgo.ApplicationCommandOptionString,
				Name:         "name",
				Description:  "Character name to activate.",
				Required:     true,
				Autocomplete: true,
			},
		},
	}
}

// Execute sets the named character as active.
func (c *setCharacterCmd) Execute(ctx context.Context, s DiscordSession, i *discordgo.InteractionCreate) error {
	name := i.ApplicationCommandData().GetOption("name").StringValue()
	card, err := resolveCard(ctx, c.session, i.GuildID, name)
	if err != nil {
		return respondResolveError(ctx, s, i, err)
	}

	if err := c.session.SetActiveCharacter(ctx, i.GuildID, card.CharacterID); err != nil {
		logger.FromContext(ctx).Error("failed to set active character", "error", err, "guild_id", i.GuildID)
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: responses.ListCharacters.SetError,
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return err
	}

	if err := SyncGuildIdentity(ctx, c.session, c.imageClient, s, i.GuildID); err != nil {
		logger.FromContext(ctx).Warn("failed to sync guild identity", "error", err, "guild_id", i.GuildID)
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: c.characterSwitchMessage(ctx, i.GuildID, card),
		},
	})
	return nil
}

// characterSwitchMessage builds the message shown when a character is
// activated: the character's last line in its active thread, falling back
// to its greeting, then to the boilerplate confirmation.
func (c *setCharacterCmd) characterSwitchMessage(ctx context.Context, guildID string, card *session.CharacterCard) string {
	if err := c.session.EnsureDefaultThread(ctx, guildID, card.CharacterID); err != nil {
		logger.FromContext(ctx).Warn("failed to ensure default thread", "error", err, "guild_id", guildID)
	}
	threadID, _ := c.session.GetActiveThreadID(ctx, guildID, card.CharacterID)
	if last, ok := c.session.GetLastCharacterMessage(ctx, guildID, card.CharacterID, threadID); ok {
		return last
	}
	if greeting, ok := research.ExtractSection(card.Description, research.SectionGreeting); ok && strings.TrimSpace(greeting) != "" {
		return greeting
	}
	return fmt.Sprintf(responses.ListCharacters.SetSuccess, card.DisplayName)
}

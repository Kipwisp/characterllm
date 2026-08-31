package commands

import (
	"context"

	"characterllm/internal/logger"
	"characterllm/internal/responses"
	"characterllm/internal/session"

	"github.com/bwmarrin/discordgo"
)

type clearThreadCmd struct {
	session *session.Manager
	lock    func(guildID, threadID string) func()
}

// Definition returns the Discord application command definition for clearing a thread.
func (c *clearThreadCmd) Definition() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        "clearthread",
		Description: "Clear the active thread's conversation history.",
	}
}

// Execute clears the conversation history of the active thread.
func (c *clearThreadCmd) Execute(ctx context.Context, s DiscordSession, i *discordgo.InteractionCreate) error {
	details, err := c.session.GetCharacterDetails(ctx, i.GuildID)
	if err != nil || details == nil || details.CharacterID == "" {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: responses.General.NoCharacterSet,
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return nil
	}

	if err := c.session.EnsureDefaultThread(ctx, i.GuildID, details.CharacterID); err != nil {
		logger.FromContext(ctx).Warn("failed to ensure default thread", "error", err)
	}
	threadID := details.ActiveThreadID

	defer c.lock(i.GuildID, threadID)()
	if err := c.session.ClearHistory(ctx, i.GuildID, threadID); err != nil {
		logger.FromContext(ctx).Error("failed to clear thread history", "error", err)
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: responses.ClearThread.Error,
			},
		})
		return err
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: responses.ClearThread.Success,
		},
	})
	return nil
}

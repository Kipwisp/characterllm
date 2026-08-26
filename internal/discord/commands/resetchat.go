package commands

import (
	"context"

	"characterllm/internal/logger"
	"characterllm/internal/responses"
	"characterllm/internal/session"

	"github.com/bwmarrin/discordgo"
)

type resetChatCmd struct {
	session *session.Manager
	lock    func(guildID, threadID string) func()
}

// Definition returns the Discord application command definition for resetting chat.
func (c *resetChatCmd) Definition() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        "resetchat",
		Description: "Reset the chat history.",
	}
}

// Execute clears the conversation history for the guild.
func (c *resetChatCmd) Execute(ctx context.Context, s DiscordSession, i *discordgo.InteractionCreate) error {
	defer c.lock(i.GuildID, "")()
	if err := c.session.ClearHistory(ctx, i.GuildID, ""); err != nil {
		logger.FromContext(ctx).Error("failed to clear chat history", "error", err, "guild_id", i.GuildID)
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: responses.ResetChat.Error,
			},
		})
		return err
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: responses.ResetChat.Success,
		},
	})
	return nil
}

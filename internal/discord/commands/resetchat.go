package commands

import (
	"characterllm/internal/responses"
	"context"
	"github.com/bwmarrin/discordgo"
)

type resetChatCmd struct{}

// Definition returns the Discord application command definition for resetting chat.
func (c *resetChatCmd) Definition() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        "resetchat",
		Description: "Reset the chat history.",
	}
}

// Execute clears the conversation history for the guild.
func (c *resetChatCmd) Execute(ctx context.Context, cmdCtx CommandContext, s DiscordSession, i *discordgo.InteractionCreate) error {
	defer cmdCtx.LockConversation(i.GuildID, "")()
	cmdCtx.GetSession().ClearHistory(ctx, i.GuildID, "")
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: responses.ResetChat.Success,
		},
	})
	return nil
}

func init() {
	Register(&resetChatCmd{})
}

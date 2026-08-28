package commands

import (
	"context"
	"fmt"

	"characterllm/internal/responses"

	"github.com/bwmarrin/discordgo"
)

// invitePermissions is the permission set offered by the invite link:
// View Channels, Send Messages, Embed Links, Attach Files, Read Message
// History, Change Nickname, and Send Messages in Threads.
const invitePermissions = 274945133568

type inviteCmd struct {
	clientID string
}

// Definition returns the Discord application command definition for sharing the invite link.
func (c *inviteCmd) Definition() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        "invite",
		Description: "Get a link to add this bot to your server.",
	}
}

// Execute replies with a Discord invite link built from the configured client ID.
func (c *inviteCmd) Execute(ctx context.Context, s DiscordSession, i *discordgo.InteractionCreate) error {
	url := fmt.Sprintf(
		"https://discord.com/oauth2/authorize?client_id=%s&permissions=%d&integration_type=0&scope=bot",
		c.clientID, invitePermissions,
	)
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: fmt.Sprintf(responses.Invite.Link, url),
		},
	})
	return nil
}

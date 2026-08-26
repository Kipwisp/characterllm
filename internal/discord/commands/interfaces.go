// Package commands implements the logic for Discord slash commands.
package commands

import (
	"github.com/bwmarrin/discordgo"
)

// DiscordSession defines the subset of discordgo.Session methods used by commands and handlers.
type DiscordSession interface {
	ChannelTyping(channelID string) error
	ChannelMessageSend(channelID string, content string) (*discordgo.Message, error)
	ChannelMessageSendReply(channelID string, content string, response *discordgo.MessageReference) (*discordgo.Message, error)
	InteractionRespond(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error
	InteractionResponseEdit(interaction *discordgo.Interaction, edit *discordgo.WebhookEdit) (*discordgo.Message, error)
	GuildMemberNickname(guildID string, member string, nickname string) error
	UpdateGuildAvatar(guildID, avatarDataURI string) error
	GetToken() string
	GetUserMention() string
	GetUserID() string
}

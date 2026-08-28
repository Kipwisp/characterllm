package mocks

import (
	"github.com/bwmarrin/discordgo"
)

// MockDiscordSession is a configurable test double for the DiscordSession
// interface used by the discord handlers and commands.
type MockDiscordSession struct {
	State                       *discordgo.State
	ChannelTypingFn             func(channelID string) error
	ChannelMessageSendFn        func(channelID string, content string) (*discordgo.Message, error)
	ChannelMessageSendReplyFn   func(channelID string, content string, response *discordgo.MessageReference) (*discordgo.Message, error)
	ChannelMessageSendComplexFn func(channelID string, msg *discordgo.MessageSend) (*discordgo.Message, error)
	ChannelMessageEditComplexFn func(channelID, messageID string, edit *discordgo.MessageEdit) (*discordgo.Message, error)
	ChannelMessageDeleteFn      func(channelID, messageID string) error
	ChannelMessagesFn           func(channelID string, limit int, beforeID, afterID, aroundID string) ([]*discordgo.Message, error)
	GuildChannelsFn             func(guildID string) ([]*discordgo.Channel, error)
	InteractionRespondFn        func(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error
	InteractionResponseEditFn   func(interaction *discordgo.Interaction, edit *discordgo.WebhookEdit) (*discordgo.Message, error)
	GuildMemberNicknameFn       func(guildID string, member string, nickname string) error
	UpdateGuildAvatarFn         func(guildID, avatarDataURI string) error
	GetTokenFn                  func() string
	GetUserMentionFn            func() string
	GetUserIDFn                 func() string
}

func (m *MockDiscordSession) ChannelTyping(channelID string) error {
	if m.ChannelTypingFn == nil {
		return nil
	}
	return m.ChannelTypingFn(channelID)
}

func (m *MockDiscordSession) ChannelMessageSend(channelID string, content string) (*discordgo.Message, error) {
	if m.ChannelMessageSendFn == nil {
		return nil, nil
	}
	return m.ChannelMessageSendFn(channelID, content)
}

func (m *MockDiscordSession) ChannelMessageSendReply(channelID string, content string, response *discordgo.MessageReference) (*discordgo.Message, error) {
	if m.ChannelMessageSendReplyFn == nil {
		return nil, nil
	}
	return m.ChannelMessageSendReplyFn(channelID, content, response)
}

func (m *MockDiscordSession) ChannelMessageSendComplex(channelID string, msg *discordgo.MessageSend) (*discordgo.Message, error) {
	if m.ChannelMessageSendComplexFn == nil {
		return nil, nil
	}
	return m.ChannelMessageSendComplexFn(channelID, msg)
}

func (m *MockDiscordSession) ChannelMessageEditComplex(channelID, messageID string, edit *discordgo.MessageEdit) (*discordgo.Message, error) {
	if m.ChannelMessageEditComplexFn == nil {
		return nil, nil
	}
	return m.ChannelMessageEditComplexFn(channelID, messageID, edit)
}

func (m *MockDiscordSession) ChannelMessageDelete(channelID, messageID string) error {
	if m.ChannelMessageDeleteFn == nil {
		return nil
	}
	return m.ChannelMessageDeleteFn(channelID, messageID)
}

func (m *MockDiscordSession) ChannelMessages(channelID string, limit int, beforeID, afterID, aroundID string) ([]*discordgo.Message, error) {
	if m.ChannelMessagesFn == nil {
		return nil, nil
	}
	return m.ChannelMessagesFn(channelID, limit, beforeID, afterID, aroundID)
}

func (m *MockDiscordSession) GuildChannels(guildID string) ([]*discordgo.Channel, error) {
	if m.GuildChannelsFn == nil {
		return nil, nil
	}
	return m.GuildChannelsFn(guildID)
}

func (m *MockDiscordSession) InteractionRespond(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
	if m.InteractionRespondFn == nil {
		return nil
	}
	return m.InteractionRespondFn(interaction, response)
}

func (m *MockDiscordSession) InteractionResponseEdit(interaction *discordgo.Interaction, edit *discordgo.WebhookEdit) (*discordgo.Message, error) {
	if m.InteractionResponseEditFn == nil {
		return nil, nil
	}
	return m.InteractionResponseEditFn(interaction, edit)
}

func (m *MockDiscordSession) GuildMemberNickname(guildID string, member string, nickname string) error {
	if m.GuildMemberNicknameFn == nil {
		return nil
	}
	return m.GuildMemberNicknameFn(guildID, member, nickname)
}

func (m *MockDiscordSession) UpdateGuildAvatar(guildID, avatarDataURI string) error {
	if m.UpdateGuildAvatarFn == nil {
		return nil
	}
	return m.UpdateGuildAvatarFn(guildID, avatarDataURI)
}

func (m *MockDiscordSession) GetToken() string {
	if m.GetTokenFn == nil {
		return ""
	}
	return m.GetTokenFn()
}

func (m *MockDiscordSession) GetUserMention() string {
	if m.GetUserMentionFn == nil {
		return ""
	}
	return m.GetUserMentionFn()
}

func (m *MockDiscordSession) GetUserID() string {
	if m.GetUserIDFn == nil {
		return ""
	}
	return m.GetUserIDFn()
}

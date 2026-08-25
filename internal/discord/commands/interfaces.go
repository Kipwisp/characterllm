// Package commands implements the logic for Discord slash commands.
package commands

import (
	"context"

	"characterllm/internal/audit"
	"characterllm/internal/config"
	"characterllm/internal/images"
	"characterllm/internal/llm"
	"characterllm/internal/research"
	"characterllm/internal/search"
	"characterllm/internal/session"

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

// CommandContext defines the dependencies available to commands.
type CommandContext interface {
	GetSession() *session.Manager
	GetLLM() llm.LLMClient
	GetConfig() *config.Config
	GetAudit() *audit.AuditLogger
	GetSearchProvider() search.SearchProvider
	GetImageSearchProvider() search.ImageSearchProvider
	GetSynthesizer() research.Synthesizer
	GetImageClient() images.ImageClient
}

// Command defines the interface for a discord slash command.
type Command interface {
	Definition() *discordgo.ApplicationCommand
	Execute(ctx context.Context, cmdCtx CommandContext, s DiscordSession, i *discordgo.InteractionCreate) error
}

var registry = make(map[string]Command)

// Register adds a command to the registry.
func Register(cmd Command) {
	registry[cmd.Definition().Name] = cmd
}

// Get retrieves a command from the registry by name.
func Get(name string) Command {
	return registry[name]
}

// All returns all registered commands.
func All() []Command {
	var cmds []Command
	for _, cmd := range registry {
		cmds = append(cmds, cmd)
	}
	return cmds
}

// GetAllDefinitions returns the discordgo definitions for all registered commands.
func GetAllDefinitions() []*discordgo.ApplicationCommand {
	var defs []*discordgo.ApplicationCommand
	for _, cmd := range registry {
		defs = append(defs, cmd.Definition())
	}
	return defs
}

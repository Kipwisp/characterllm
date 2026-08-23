// Package commands implements the logic for Discord slash commands.
package commands

import (
	"context"

	"characterllm/internal/audit"
	"characterllm/internal/config"
	"characterllm/internal/llm"
	"characterllm/internal/session"

	"github.com/bwmarrin/discordgo"
)

// CommandContext defines the dependencies available to commands.
type CommandContext interface {
	GetSession() *session.Manager
	GetLLM() *llm.Client
	GetConfig() *config.Config
	GetAudit() *audit.AuditLogger
}

// Command defines the interface for a discord slash command.
type Command interface {
	Definition() *discordgo.ApplicationCommand
	Execute(ctx context.Context, cmdCtx CommandContext, s *discordgo.Session, i *discordgo.InteractionCreate) error
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

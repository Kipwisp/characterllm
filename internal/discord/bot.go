// Package discord implements the Discord bot logic, including connection management,
// event handling, and command dispatching.
package discord

import (
	"characterllm/internal/discord/commands"
	"log/slog"

	"github.com/bwmarrin/discordgo"
)

// Bot represents the Discord bot instance.
type Bot struct {
	Session  *discordgo.Session
	Handlers *Handlers
}

// NewBot creates a new Bot instance with the provided token and handlers.
func NewBot(token string, handlers *Handlers) (*Bot, error) {
	dg, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, err
	}

	return &Bot{
		Session:  dg,
		Handlers: handlers,
	}, nil
}

// Start opens the Discord connection and registers event handlers.
func (b *Bot) Start() error {
	b.Session.AddHandler(b.Handlers.MessageCreate)
	b.Session.AddHandler(b.Handlers.InteractionCreate)
	b.Session.AddHandler(b.Handlers.ComponentCreate)

	err := b.Session.Open()
	if err != nil {
		return err
	}

	slog.Info("bot logged in",
		"username", b.Session.State.User.Username,
		"discriminator", b.Session.State.User.Discriminator,
		"user_id", b.Session.State.User.ID,
	)

	b.RegisterCommands()

	return nil
}

// RegisterCommands registers the bot's slash commands globally.
func (b *Bot) RegisterCommands() {
	appCommands := commands.GetAllDefinitions()

	// Register commands globally
	_, err := b.Session.ApplicationCommandBulkOverwrite(b.Session.State.User.ID, "", appCommands)
	if err != nil {
		slog.Error("error bulk overwriting global commands", "error", err)
	} else {
		slog.Info("successfully registered commands globally", "count", len(appCommands))
	}
}

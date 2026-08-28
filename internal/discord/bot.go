// Package discord implements the Discord bot logic, including connection management,
// event handling, and command dispatching.
package discord

import (
	"log/slog"

	"github.com/bwmarrin/discordgo"
)

// Bot represents the Discord bot instance.
type Bot struct {
	Session *discordgo.Session
	Router  *Router
}

// NewBot creates a new Bot instance with the provided token and event router.
func NewBot(token string, router *Router) (*Bot, error) {
	dg, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, err
	}

	// Request the privileged message content intent so chat handlers receive
	// message text and attachments.
	dg.Identify.Intents = discordgo.IntentsAllWithoutPrivileged | discordgo.IntentMessageContent

	return &Bot{
		Session: dg,
		Router:  router,
	}, nil
}

// Start opens the Discord connection and registers event handlers.
func (b *Bot) Start() error {
	b.Session.AddHandler(b.Router.MessageCreate)
	b.Session.AddHandler(b.Router.InteractionCreate)
	b.Session.AddHandler(b.Router.ComponentCreate)

	err := b.Session.Open()
	if err != nil {
		return err
	}

	slog.Info("bot logged in",
		"username", b.Session.State.User.Username,
		"discriminator", b.Session.State.User.Discriminator,
		"user_id", b.Session.State.User.ID,
	)

	return nil
}

// RegisterCommands registers the bot's slash commands globally.
func (b *Bot) RegisterCommands(appCommands []*discordgo.ApplicationCommand) {
	_, err := b.Session.ApplicationCommandBulkOverwrite(b.Session.State.User.ID, "", appCommands)
	if err != nil {
		slog.Error("error bulk overwriting global commands", "error", err)
	} else {
		slog.Info("successfully registered commands globally", "count", len(appCommands))
	}
}

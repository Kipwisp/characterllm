// Package discord implements the Discord bot logic, including connection management,
// event handling, and command dispatching.
package discord

import (
	"characterllm/internal/discord/commands"
	"context"
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
	b.SyncNicknames()

	return nil
}

// SyncNicknames updates the bot's nickname in all guilds to match the set character persona.
func (b *Bot) SyncNicknames() {
	slog.Info("synchronizing nicknames across guilds")
	for _, guild := range b.Session.State.Guilds {
		details, err := b.Handlers.Session.GetCharacterDetails(context.Background(), guild.ID)
		if err != nil || details == nil || details.DisplayName == "" {
			continue
		}
		nickname := details.DisplayName
		err = b.Session.GuildMemberNickname(guild.ID, "@me", nickname)
		if err != nil {
			slog.Error("could not sync nickname", "guild_id", guild.ID, "error", err)
		} else {
			slog.Info("synced nickname", "nickname", nickname, "guild_id", guild.ID)
		}
	}
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

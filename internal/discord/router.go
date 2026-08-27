package discord

import (
	"context"
	"errors"

	"characterllm/internal/discord/commands"
	"characterllm/internal/logger"

	"github.com/bwmarrin/discordgo"
	"github.com/google/uuid"
)

// Router is the thin Discord gateway event router.
type Router struct {
	Chat            *Chat
	CommandRegistry *commands.Registry
}

// MessageCreate handles incoming Discord messages. It triggers LLM responses when the bot is mentioned.
func (h *Router) MessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	h.Chat.Handle(NewSessionWrapper(s), m)
}

// InteractionCreate handles Discord slash command interactions.
func (h *Router) InteractionCreate(s *discordgo.Session, i *discordgo.InteractionCreate) {
	h.handleInteraction(NewSessionWrapper(s), i)
}

func (h *Router) handleInteraction(s commands.DiscordSession, i *discordgo.InteractionCreate) {
	// Initialize request tracking
	reqID := uuid.New().String()
	ctx := logger.ToContext(context.Background(), logger.WithRequestID(reqID, "guild_id", i.GuildID))

	switch i.Type {
	case discordgo.InteractionApplicationCommand:
		name := i.ApplicationCommandData().Name
		if err := h.CommandRegistry.Execute(ctx, name, s, i); err != nil {
			if errors.Is(err, commands.ErrUnknownCommand) {
				logger.FromContext(ctx).Warn("unknown command", "command", name)
			} else {
				logger.FromContext(ctx).Error("error executing command", "command", name, "error", err)
			}
		}
	case discordgo.InteractionApplicationCommandAutocomplete:
		h.CommandRegistry.HandleAutocomplete(ctx, s, i)
	default:
		return
	}
}

// ComponentCreate handles Discord message component interactions (e.g., select menus).
func (h *Router) ComponentCreate(s *discordgo.Session, i *discordgo.InteractionCreate) {
	h.handleComponent(NewSessionWrapper(s), i)
}

func (h *Router) handleComponent(s commands.DiscordSession, i *discordgo.InteractionCreate) {
	if i.Type != discordgo.InteractionMessageComponent {
		return
	}

	// Initialize request tracking
	reqID := uuid.New().String()
	ctx := logger.ToContext(context.Background(), logger.WithRequestID(reqID, "guild_id", i.GuildID))

	h.CommandRegistry.HandleComponent(ctx, s, i)
}

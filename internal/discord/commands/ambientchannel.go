package commands

import (
	"context"
	"fmt"
	"strings"

	"characterllm/internal/logger"
	"characterllm/internal/responses"
	"characterllm/internal/session"

	"github.com/bwmarrin/discordgo"
)

const (
	ambientClearKey   = "clear"
	ambientClearLabel = "Clear ambient channel"
)

type ambientChannelCmd struct {
	session *session.Manager
}

// Definition returns the Discord application command definition for setting the ambient channel.
func (c *ambientChannelCmd) Definition() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        "setambientchannel",
		Description: "Set the channel where the bot speaks on its own from time to time.",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:         discordgo.ApplicationCommandOptionString,
				Name:         "channel",
				Description:  "Channel to use. Omit to use this channel.",
				Required:     true,
				Autocomplete: true,
			},
		},
	}
}

// Execute sets (or clears) the guild's ambient channel.
func (c *ambientChannelCmd) Execute(ctx context.Context, s DiscordSession, i *discordgo.InteractionCreate) error {
	channelID := i.ChannelID
	if opt := i.ApplicationCommandData().GetOption("channel"); opt != nil {
		value := opt.StringValue()
		if value != ambientClearKey {
			channelID = value
		} else {
			channelID = ""
		}
	}

	if err := c.session.SetAmbientChannel(ctx, i.GuildID, channelID); err != nil {
		logger.FromContext(ctx).Error("failed to set ambient channel", "error", err, "guild_id", i.GuildID)
		c.respond(i, s, responses.AmbientChannel.Error, false)
		return err
	}

	if channelID == "" {
		c.respond(i, s, responses.AmbientChannel.Cleared, false)
		return nil
	}

	c.respond(i, s, fmt.Sprintf(responses.AmbientChannel.Success, c.channelName(ctx, s, i.GuildID, channelID)), false)
	return nil
}

// channelName resolves a channel ID to a "#name" for the confirmation reply,
// falling back to the ID when the lookup fails.
func (c *ambientChannelCmd) channelName(ctx context.Context, s DiscordSession, guildID, channelID string) string {
	channels, err := s.GuildChannels(guildID)
	if err != nil {
		logger.FromContext(ctx).Warn("failed to list guild channels", "error", err)
		return channelID
	}
	for _, ch := range channels {
		if ch.ID == channelID {
			return "#" + ch.Name
		}
	}
	return channelID
}

// autocompleteAmbientChannels suggests the guild's text channels, filtered
// by the query. The channel currently in use is suffixed with " (active)";
// the Clear choice is offered only while a channel is set.
func autocompleteAmbientChannels(ctx context.Context, sm *session.Manager, s DiscordSession, guildID, query string) []*discordgo.ApplicationCommandOptionChoice {
	query = strings.ToLower(strings.TrimPrefix(query, "#"))

	current := ""
	if id, err := sm.GetAmbientChannel(ctx, guildID); err == nil {
		current = id
	}

	var choices []*discordgo.ApplicationCommandOptionChoice
	if current != "" && (query == "" || strings.Contains(strings.ToLower(ambientClearLabel), query)) {
		choices = append(choices, &discordgo.ApplicationCommandOptionChoice{Name: ambientClearLabel, Value: ambientClearKey})
	}

	channels, err := s.GuildChannels(guildID)
	if err != nil {
		logger.FromContext(ctx).Warn("failed to list guild channels for autocomplete", "error", err)
		return choices
	}
	for _, ch := range channels {
		if ch.Type != discordgo.ChannelTypeGuildText {
			continue
		}
		name := "#" + ch.Name
		if query != "" && !strings.Contains(strings.ToLower(name), query) {
			continue
		}
		if ch.ID == current {
			name += activeChoiceSuffix
		}
		choices = append(choices, &discordgo.ApplicationCommandOptionChoice{Name: name, Value: ch.ID})
	}
	return choices
}

func (c *ambientChannelCmd) respond(i *discordgo.InteractionCreate, s DiscordSession, content string, ephemeral bool) {
	flags := discordgo.MessageFlags(0)
	if ephemeral {
		flags = discordgo.MessageFlagsEphemeral
	}
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Content: content, Flags: flags},
	})
}

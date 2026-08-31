package commands

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"characterllm/internal/logger"
	"characterllm/internal/responses"
	"characterllm/internal/session"

	"github.com/bwmarrin/discordgo"
)

type addAmbientChannelCmd struct {
	session *session.Manager
}

// Definition returns the Discord application command definition for adding an ambient channel.
func (c *addAmbientChannelCmd) Definition() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        "addambientchannel",
		Description: "Add a channel where the bot speaks on its own from time to time.",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:         discordgo.ApplicationCommandOptionString,
				Name:         "channel",
				Description:  "Channel to add. Omit to use this channel.",
				Autocomplete: true,
			},
		},
	}
}

// Execute adds a channel to the guild's ambient set.
func (c *addAmbientChannelCmd) Execute(ctx context.Context, s DiscordSession, i *discordgo.InteractionCreate) error {
	channelID := i.ChannelID
	if opt := i.ApplicationCommandData().GetOption("channel"); opt != nil {
		channelID = opt.StringValue()
	}

	current, err := c.session.GetAmbientChannels(ctx, i.GuildID)
	if err != nil {
		logger.FromContext(ctx).Error("failed to read ambient channels", "error", err)
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{Content: responses.AmbientChannel.Error},
		})
		return err
	}
	if slices.Contains(current, channelID) {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{Content: fmt.Sprintf(responses.AmbientChannel.AlreadySet, ambientChannelName(ctx, s, i.GuildID, channelID))},
		})
		return nil
	}

	if err := c.session.AddAmbientChannel(ctx, i.GuildID, channelID); err != nil {
		logger.FromContext(ctx).Error("failed to add ambient channel", "error", err)
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{Content: responses.AmbientChannel.Error},
		})
		return err
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Content: fmt.Sprintf(responses.AmbientChannel.Added, ambientChannelName(ctx, s, i.GuildID, channelID))},
	})
	return nil
}

// autocompleteAmbientChannels suggests the guild's text channels, filtered
// by the query; the channels currently in the set are suffixed with
// " (active)".
func autocompleteAmbientChannels(ctx context.Context, sm *session.Manager, s DiscordSession, guildID, query string) []*discordgo.ApplicationCommandOptionChoice {
	query = strings.ToLower(strings.TrimPrefix(query, "#"))

	current := map[string]bool{}
	if ids, err := sm.GetAmbientChannels(ctx, guildID); err == nil {
		for _, id := range ids {
			current[id] = true
		}
	}

	channels, err := s.GuildChannels(guildID)
	if err != nil {
		logger.FromContext(ctx).Warn("failed to list guild channels for autocomplete", "error", err)
		return nil
	}
	var choices []*discordgo.ApplicationCommandOptionChoice
	for _, ch := range channels {
		if ch.Type != discordgo.ChannelTypeGuildText {
			continue
		}
		name := "#" + ch.Name
		if query != "" && !strings.Contains(strings.ToLower(name), query) {
			continue
		}
		if current[ch.ID] {
			name += activeChoiceSuffix
		}
		choices = append(choices, &discordgo.ApplicationCommandOptionChoice{Name: name, Value: ch.ID})
	}
	return choices
}

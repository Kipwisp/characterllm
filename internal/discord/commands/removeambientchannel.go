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

const (
	ambientRemoveAllKey   = "all"
	ambientRemoveAllLabel = "Remove all"
)

// removeAmbientChannelCmd removes one channel (or all channels) from the
// guild's ambient set.
type removeAmbientChannelCmd struct {
	session *session.Manager
}

// Definition returns the Discord application command definition for removing an ambient channel.
func (c *removeAmbientChannelCmd) Definition() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        "removeambientchannel",
		Description: "Remove a channel from the set where the bot speaks on its own from time to time.",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:         discordgo.ApplicationCommandOptionString,
				Name:         "channel",
				Description:  "Channel to remove. Omit to remove this channel.",
				Autocomplete: true,
			},
		},
	}
}

// Execute removes a channel (or all channels) from the guild's ambient set.
func (c *removeAmbientChannelCmd) Execute(ctx context.Context, s DiscordSession, i *discordgo.InteractionCreate) error {
	channelID := ""
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

	if channelID == "" {
		channelID = i.ChannelID
	}
	if channelID == ambientRemoveAllKey {
		if len(current) == 0 {
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{Content: responses.AmbientChannel.NoneSet},
			})
			return nil
		}
		if err := c.session.ClearAmbientChannels(ctx, i.GuildID); err != nil {
			logger.FromContext(ctx).Error("failed to clear ambient channels", "error", err)
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{Content: responses.AmbientChannel.Error},
			})
			return err
		}
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{Content: responses.AmbientChannel.Cleared},
		})
		return nil
	}
	if !slices.Contains(current, channelID) {
		name := ambientChannelName(ctx, s, i.GuildID, channelID)
		if len(current) == 0 {
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{Content: responses.AmbientChannel.NoneSet},
			})
			return nil
		}
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{Content: fmt.Sprintf(responses.AmbientChannel.NotMember, name)},
		})
		return nil
	}
	if err := c.session.RemoveAmbientChannel(ctx, i.GuildID, channelID); err != nil {
		logger.FromContext(ctx).Error("failed to remove ambient channel", "error", err)
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{Content: responses.AmbientChannel.Error},
		})
		return err
	}
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Content: fmt.Sprintf(responses.AmbientChannel.Removed, ambientChannelName(ctx, s, i.GuildID, channelID))},
	})
	return nil
}

// autocompleteRemoveAmbientChannels suggests the channels currently in the
// set, filtered by the query, plus a leading Remove all choice (offered only
// while the set is non-empty).
func autocompleteRemoveAmbientChannels(ctx context.Context, sm *session.Manager, s DiscordSession, guildID, query string) []*discordgo.ApplicationCommandOptionChoice {
	query = strings.ToLower(strings.TrimPrefix(query, "#"))

	current, err := sm.GetAmbientChannels(ctx, guildID)
	if err != nil {
		logger.FromContext(ctx).Warn("failed to read ambient channels for autocomplete", "error", err)
		return nil
	}
	if len(current) == 0 {
		return nil
	}

	var choices []*discordgo.ApplicationCommandOptionChoice
	if query == "" || strings.Contains(strings.ToLower(ambientRemoveAllLabel), query) {
		choices = append(choices, &discordgo.ApplicationCommandOptionChoice{Name: ambientRemoveAllLabel, Value: ambientRemoveAllKey})
	}

	nameByID := map[string]string{}
	if channels, err := s.GuildChannels(guildID); err == nil {
		for _, ch := range channels {
			nameByID[ch.ID] = ch.Name
		}
	} else {
		logger.FromContext(ctx).Warn("failed to list guild channels for autocomplete", "error", err)
	}
	for _, id := range current {
		name := id
		if n, ok := nameByID[id]; ok {
			name = "#" + n
		}
		if query != "" && !strings.Contains(strings.ToLower(name), query) {
			continue
		}
		choices = append(choices, &discordgo.ApplicationCommandOptionChoice{Name: name, Value: id})
	}
	return choices
}

package commands

import (
	"context"
	"fmt"
	"strings"

	"strconv"

	"characterllm/internal/logger"
	"characterllm/internal/responses"
	"characterllm/internal/session"

	"github.com/bwmarrin/discordgo"
)

type deleteThreadCmd struct {
	session *session.Manager
	lock    func(guildID, threadID string) func()
}

// Definition returns the Discord application command definition for deleting a thread.
func (c *deleteThreadCmd) Definition() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        "deletethread",
		Description: "Delete a conversation thread of the active character and its history.",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:         discordgo.ApplicationCommandOptionString,
				Name:         "thread",
				Description:  "Thread to delete, or 'current' for the active thread.",
				Required:     true,
				Autocomplete: true,
			},
		},
	}
}

// Execute resolves the thread and asks for confirmation before deleting it.
func (c *deleteThreadCmd) Execute(ctx context.Context, s DiscordSession, i *discordgo.InteractionCreate) error {
	details, err := c.session.GetCharacterDetails(ctx, i.GuildID)
	if err != nil || details == nil || details.CharacterID == "" {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: responses.General.NoCharacterSet,
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return nil
	}

	if err := c.session.EnsureDefaultThread(ctx, i.GuildID, details.CharacterID); err != nil {
		logger.FromContext(ctx).Warn("failed to ensure default thread", "error", err, "guild_id", i.GuildID)
	}

	value := i.ApplicationCommandData().GetOption("thread").StringValue()
	thread := resolveThreadOption(ctx, c.session, i.GuildID, details.CharacterID, value, true)
	if thread == nil {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: responses.SetThread.NotFound,
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return nil
	}

	count, err := c.session.CountCharacterThreads(ctx, i.GuildID, details.CharacterID)
	if err != nil {
		logger.FromContext(ctx).Warn("failed to count threads before deletion confirmation", "error", err, "characterID", details.CharacterID)
	}

	var messages string
	if msgCount, err := c.session.GetHistoryCount(ctx, i.GuildID, thread.ThreadID); err != nil {
		logger.FromContext(ctx).Warn("failed to count thread messages before deletion confirmation", "error", err, "characterID", details.CharacterID)
		messages = "?"
	} else {
		messages = strconv.Itoa(msgCount)
	}

	var description string
	if count == 1 {
		description = fmt.Sprintf(responses.DeleteThread.ConfirmClear, thread.Name, details.DisplayName)
	} else {
		description = fmt.Sprintf(responses.DeleteThread.ConfirmDelete, thread.Name)
	}

	components := []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{
					Label:    "Delete",
					Style:    discordgo.DangerButton,
					CustomID: deleteThreadConfirmID(thread.ThreadID),
				},
				discordgo.Button{
					Label:    "Cancel",
					Style:    discordgo.SecondaryButton,
					CustomID: deleteThreadCancelID(thread.ThreadID),
				},
			},
		},
	}

	embed := &discordgo.MessageEmbed{
		Title:       thread.Name,
		Description: description,
		Color:       0x5865F2,
	}
	embed.Fields = append(embed.Fields,
		&discordgo.MessageEmbedField{Name: "Character", Value: details.DisplayName, Inline: true},
		&discordgo.MessageEmbedField{Name: "Messages", Value: messages, Inline: true},
	)

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: components,
		},
	})
	return nil
}

// handleDeleteConfirm deletes the thread (or clears it when it is the
// character's last one), moving the active pointer off it when needed.
func (c *deleteThreadCmd) handleDeleteConfirm(ctx context.Context, s DiscordSession, i *discordgo.InteractionCreate) {
	threadID := strings.TrimPrefix(i.MessageComponentData().CustomID, deleteThreadConfirmPrefix)

	details, err := c.session.GetCharacterDetails(ctx, i.GuildID)
	if err != nil || details == nil || details.CharacterID == "" {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: responses.General.NoCharacterSet,
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	if err := c.session.EnsureDefaultThread(ctx, i.GuildID, details.CharacterID); err != nil {
		logger.FromContext(ctx).Warn("failed to ensure default thread", "error", err, "guild_id", i.GuildID)
	}
	thread, err := c.session.GetThread(ctx, i.GuildID, details.CharacterID, threadID)
	if err != nil || thread == nil {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Content:     responses.DeleteThread.NotFound,
				Embeds:      []*discordgo.MessageEmbed{},
				Attachments: &[]*discordgo.MessageAttachment{},
				Components:  nil,
			},
		})
		return
	}

	defer c.lock(i.GuildID, threadID)()
	cleared, err := c.session.DeleteThread(ctx, i.GuildID, details.CharacterID, threadID)
	if err != nil {
		logger.FromContext(ctx).Error("failed to delete thread", "error", err, "guild_id", i.GuildID)
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Content:     responses.DeleteThread.Error,
				Embeds:      []*discordgo.MessageEmbed{},
				Attachments: &[]*discordgo.MessageAttachment{},
				Components:  nil,
			},
		})
		return
	}

	var content string
	if cleared {
		content = fmt.Sprintf(responses.DeleteThread.Cleared, thread.Name)
	} else {
		content = fmt.Sprintf(responses.DeleteThread.Deleted, thread.Name)
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Content:     content,
			Embeds:      []*discordgo.MessageEmbed{},
			Attachments: &[]*discordgo.MessageAttachment{},
			Components:  nil,
		},
	})
}

// handleDeleteCancel backs out of a pending thread deletion.
func (c *deleteThreadCmd) handleDeleteCancel(ctx context.Context, s DiscordSession, i *discordgo.InteractionCreate) {
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Content:     responses.DeleteThread.Cancelled,
			Embeds:      []*discordgo.MessageEmbed{},
			Attachments: &[]*discordgo.MessageAttachment{},
			Components:  nil,
		},
	})
}

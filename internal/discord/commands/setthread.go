package commands

import (
	"context"
	"fmt"

	"characterllm/internal/logger"
	"characterllm/internal/responses"
	"characterllm/internal/session"

	"github.com/bwmarrin/discordgo"
)

type setThreadCmd struct {
	session *session.Manager
	lock    func(guildID, threadID string) func()
}

// Definition returns the Discord application command definition for switching threads.
func (c *setThreadCmd) Definition() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        "setthread",
		Description: "Make an existing conversation thread of the active character the active thread.",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:         discordgo.ApplicationCommandOptionString,
				Name:         "thread",
				Description:  "Thread to switch to.",
				Required:     true,
				Autocomplete: true,
			},
		},
	}
}

// Execute makes the chosen thread of the active character active.
func (c *setThreadCmd) Execute(ctx context.Context, s DiscordSession, i *discordgo.InteractionCreate) error {
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
	thread := resolveThreadOption(ctx, c.session, i.GuildID, details.CharacterID, value, false)
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

	defer c.lock(i.GuildID, details.ActiveThreadID)()
	if err := c.session.SetActiveThread(ctx, i.GuildID, details.CharacterID, thread.ThreadID); err != nil {
		logger.FromContext(ctx).Error("failed to switch thread", "error", err, "guild_id", i.GuildID)
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: responses.SetThread.Error,
			},
		})
		return err
	}

	content := fmt.Sprintf(responses.SetThread.Success, thread.Name)
	if last, ok := c.session.GetLastCharacterMessage(ctx, i.GuildID, details.CharacterID, thread.ThreadID); ok {
		content += "\n\n" + last
	} else if greeting := characterGreeting(details.Description); greeting != "" {
		content += "\n\n" + greeting
	}
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: content,
		},
	})
	return nil
}

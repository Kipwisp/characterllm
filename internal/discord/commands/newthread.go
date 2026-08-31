package commands

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"characterllm/internal/logger"
	"characterllm/internal/responses"
	"characterllm/internal/session"

	"github.com/bwmarrin/discordgo"
)

// threadNameLimit is the maximum length of a user-supplied thread name.
const threadNameLimit = 32

type newThreadCmd struct {
	session *session.Manager
	lock    func(guildID, threadID string) func()
}

// Definition returns the Discord application command definition for creating a thread.
func (c *newThreadCmd) Definition() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        "newthread",
		Description: "Start a new conversation thread with the active character.",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "name",
				Description: "Name for the new thread (defaults to a numbered name).",
			},
		},
	}
}

// Execute creates a thread on the active character and makes it active.
func (c *newThreadCmd) Execute(ctx context.Context, s DiscordSession, i *discordgo.InteractionCreate) error {
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
		logger.FromContext(ctx).Warn("failed to ensure default thread", "error", err)
	}

	defer c.lock(i.GuildID, details.ActiveThreadID)()

	var name string
	if opt := i.ApplicationCommandData().GetOption("name"); opt != nil {
		name = opt.StringValue()
	}
	name = strings.TrimSpace(name)
	name = truncateToRuneLimit(name, threadNameLimit)
	if name == "" {
		name, err = c.defaultThreadName(ctx, i.GuildID, details.CharacterID)
		if err != nil {
			logger.FromContext(ctx).Error("failed to derive default thread name", "error", err)
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: responses.NewThread.Error,
				},
			})
			return err
		}
	}

	thread, err := c.session.CreateThread(ctx, i.GuildID, details.CharacterID, name)
	if err != nil {
		if errors.Is(err, session.ErrThreadNameTaken) {
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: fmt.Sprintf(responses.NewThread.Duplicate, name),
					Flags:   discordgo.MessageFlagsEphemeral,
				},
			})
			return nil
		}
		logger.FromContext(ctx).Error("failed to create thread", "error", err)
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: responses.NewThread.Error,
			},
		})
		return err
	}

	content := fmt.Sprintf(responses.NewThread.Success, thread.Name)
	if greeting := characterGreeting(details.Description); greeting != "" {
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

// defaultThreadName returns the first free "Thread N" name, filling gaps
// left by deleted threads.
func (c *newThreadCmd) defaultThreadName(ctx context.Context, guildID, characterID string) (string, error) {
	threads, err := c.session.ListThreads(ctx, guildID, characterID)
	if err != nil {
		return "", err
	}
	existing := make(map[string]bool, len(threads))
	for _, th := range threads {
		existing[th.Name] = true
	}
	for n := 1; ; n++ {
		name := fmt.Sprintf("Thread %d", n)
		if !existing[name] {
			return name, nil
		}
	}
}

package commands

import (
	"characterllm/internal/responses"
	"context"
	"fmt"
	"github.com/bwmarrin/discordgo"
)

type statusCmd struct{}

// Definition returns the Discord application command definition for checking status.
func (c *statusCmd) Definition() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        "status",
		Description: "Check the status of the LLM server.",
	}
}

// Execute pings the LLM server and returns the latency to the user.
func (c *statusCmd) Execute(ctx context.Context, cmdCtx CommandContext, s DiscordSession, i *discordgo.InteractionCreate) error {
	latency, err := cmdCtx.GetLLM().Ping(ctx)
	if err != nil {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: fmt.Sprintf(responses.Status.Offline, err),
			},
		})
		return err
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: fmt.Sprintf(responses.Status.Online, latency),
		},
	})
	return nil
}

func init() {
	Register(&statusCmd{})
}

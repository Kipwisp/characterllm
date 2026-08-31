package commands

import (
	"context"

	"characterllm/internal/llm"
	"characterllm/internal/responses"
	"characterllm/internal/version"

	"github.com/bwmarrin/discordgo"
)

type statusCmd struct {
	llm llm.LLMClient
}

// Definition returns the Discord application command definition for checking status.
func (c *statusCmd) Definition() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        "status",
		Description: "Check the status of the LLM server.",
	}
}

// Execute pings the LLM server and reports the status.
func (c *statusCmd) Execute(ctx context.Context, s DiscordSession, i *discordgo.InteractionCreate) error {
	embed := &discordgo.MessageEmbed{
		Title: responses.Status.Title,
		Color: 0x57F287,
	}
	latency, err := c.llm.Ping(ctx)
	if err != nil {
		embed.Color = 0xED4245
		embed.Fields = []*discordgo.MessageEmbedField{
			{Name: responses.Status.State, Value: responses.Status.Offline, Inline: true},
			{Name: responses.Status.Error, Value: err.Error()},
		}
	} else {
		embed.Fields = []*discordgo.MessageEmbedField{
			{Name: responses.Status.State, Value: responses.Status.Online, Inline: true},
			{Name: responses.Status.Latency, Value: latency.String(), Inline: true},
		}
	}

	embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
		Name:  responses.Status.Version,
		Value: version.String(),
	})

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds: []*discordgo.MessageEmbed{embed},
		},
	})
	return err
}

package commands

import (
	"context"

	"characterllm/internal/images"
	"characterllm/internal/session"

	"github.com/bwmarrin/discordgo"
)

type viewCharacterCmd struct {
	session     *session.Manager
	imageClient images.ImageClient
}

// Definition returns the Discord application command definition for viewing a saved character card.
func (c *viewCharacterCmd) Definition() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        "viewcharacterdetails",
		Description: "Show a saved character card without switching to it.",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:         discordgo.ApplicationCommandOptionString,
				Name:         "name",
				Description:  "Character name, or 'current' for the active character.",
				Required:     true,
				Autocomplete: true,
			},
		},
	}
}

// Execute renders the card of the named character.
func (c *viewCharacterCmd) Execute(ctx context.Context, s DiscordSession, i *discordgo.InteractionCreate) error {
	name := i.ApplicationCommandData().GetOption("name").StringValue()
	card, err := resolveNameOrCurrent(ctx, c.session, i.GuildID, name)
	if err != nil {
		return respondResolveError(ctx, s, i, err)
	}
	return c.viewCard(ctx, s, i, card)
}

// viewCard renders the full character card as an embed, with the cached
// avatar attached as the embed image when available.
func (c *viewCharacterCmd) viewCard(ctx context.Context, s DiscordSession, i *discordgo.InteractionCreate, card *session.CharacterCard) error {
	embeds, files, closeFiles := buildCharacterCardEmbed(ctx, c.imageClient, i.GuildID, card)
	defer closeFiles()
	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds: embeds,
			Files:  files,
		},
	}); err != nil {
		return err
	}
	return nil
}

package commands

import (
	"context"
	"fmt"
	"os"
	"strings"

	"characterllm/internal/logger"
	"characterllm/internal/responses"

	"github.com/bwmarrin/discordgo"
)

// maxAvatarBytes mirrors Discord's 10 MB avatar upload limit.
const maxAvatarBytes = 10 << 20 // 10 MiB

type setAvatarCmd struct{}

// Definition returns the Discord application command definition for setting the bot avatar.
func (c *setAvatarCmd) Definition() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        "setavatar",
		Description: "Set the bot's profile picture for the active character.",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionAttachment,
				Name:        "image",
				Description: "Image to use as the avatar.",
				Required:    false,
			},
		},
	}
}

// Execute downloads the attached image into the local cache (the source of
// truth for the character's avatar) and sets it as the bot's guild avatar.
func (c *setAvatarCmd) Execute(ctx context.Context, cmdCtx CommandContext, s DiscordSession, i *discordgo.InteractionCreate) error {
	details, err := cmdCtx.GetSession().GetCharacterDetails(ctx, i.GuildID)
	if err != nil || details == nil || details.CharacterID == "" {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: responses.SetAvatar.NoCharacter,
			},
		})
		return fmt.Errorf("no active character in guild %s", i.GuildID)
	}

	sourceURL := attachmentOptionURL(i)
	if sourceURL == "" {
		sourceURL = firstImageAttachment(i)
	}
	if sourceURL == "" {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: responses.SetAvatar.MissingSource,
			},
		})
		return fmt.Errorf("no image source provided")
	}

	imgClient := cmdCtx.GetImageClient()
	if imgClient == nil {
		logger.FromContext(ctx).Error("no image client available in context")
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: responses.SetAvatar.DownloadError,
			},
		})
		return fmt.Errorf("no image client available")
	}

	path, err := imgClient.SaveImage(ctx, i.GuildID, details.CharacterID, sourceURL)
	if err != nil {
		logger.FromContext(ctx).Error("failed to download avatar image", "error", err, "guild_id", i.GuildID)
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: responses.SetAvatar.DownloadError,
			},
		})
		return err
	}

	if fi, err := os.Stat(path); err == nil && fi.Size() > maxAvatarBytes {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: responses.SetAvatar.TooLarge,
			},
		})
		return fmt.Errorf("avatar image exceeds %d bytes", maxAvatarBytes)
	}

	// clear image_url since we are using an user uploaded image now
	if err := cmdCtx.GetSession().SetCharacterImage(ctx, i.GuildID, details.CharacterID, ""); err != nil {
		logger.FromContext(ctx).Error("failed to clear character image url", "error", err, "guild_id", i.GuildID, "character_id", details.CharacterID)
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: responses.SetAvatar.DownloadError,
			},
		})
		return err
	}

	dataURI, err := imgClient.ImageToBase64(ctx, path)
	if err != nil {
		logger.FromContext(ctx).Error("failed to encode avatar image", "error", err)
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: responses.SetAvatar.AvatarError,
			},
		})
		return err
	}

	if err := s.UpdateGuildAvatar(i.GuildID, dataURI); err != nil {
		logger.FromContext(ctx).Error("failed to update guild avatar", "error", err, "guild_id", i.GuildID)
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: responses.SetAvatar.AvatarError,
			},
		})
		return err
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: responses.SetAvatar.Success,
		},
	})
	return nil
}

// attachmentOptionURL resolves the command's attachment option — whose value
// is the attachment ID — to the attachment's CDN URL via the interaction's
// resolved data.
func attachmentOptionURL(i *discordgo.InteractionCreate) string {
	data := i.ApplicationCommandData()
	if data.Resolved == nil {
		return ""
	}
	for _, opt := range data.Options {
		if opt.Name != "image" {
			continue
		}
		id, ok := opt.Value.(string)
		if !ok {
			return ""
		}
		if att := data.Resolved.Attachments[id]; att != nil {
			return att.URL
		}
	}
	return ""
}

// firstImageAttachment returns the URL of the first image attached to the
// interaction message in the composer.
func firstImageAttachment(i *discordgo.InteractionCreate) string {
	if i.Message == nil {
		return ""
	}
	for _, a := range i.Message.Attachments {
		if strings.HasPrefix(a.ContentType, "image/") {
			return a.URL
		}
	}
	return ""
}

func init() {
	Register(&setAvatarCmd{})
}

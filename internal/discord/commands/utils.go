package commands

import (
	"context"
	"fmt"

	"characterllm/internal/images"
	"characterllm/internal/logger"
	"characterllm/internal/session"
)

const MaxSelectMenuDescriptionLength = 100

// discordNickLimit is the maximum length of a guild nickname.
const discordNickLimit = 32

// Message component custom IDs.
const (
	selectCharacterCardID = "select_character_card"
	setCharacterImageID   = "select_char_image"
	listPaginationPrefix  = "list_char_"
)

func listPaginationID(direction string, page int) string {
	return fmt.Sprintf("%s%s_%d", listPaginationPrefix, direction, page)
}

// truncateToRuneLimit shortens s to at most limit runes so it never trips
// Discord's length validation on display names.
func truncateToRuneLimit(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit])
}

// SyncGuildIdentity aligns the bot's visible identity in a guild (nickname and
// avatar) with the active character.
func SyncGuildIdentity(ctx context.Context, sm *session.Manager, imgClient images.ImageClient, s DiscordSession, guildID string) error {
	details, err := sm.GetCharacterDetails(ctx, guildID)
	if err != nil || details == nil || details.DisplayName == "" {
		return nil
	}

	if err := s.GuildMemberNickname(guildID, "@me", truncateToRuneLimit(details.DisplayName, discordNickLimit)); err != nil {
		return fmt.Errorf("failed to sync nickname: %w", err)
	}

	return ApplyCharacterAvatar(ctx, imgClient, s, guildID, details.CharacterID, details.ImageURL)
}

// ApplyCharacterAvatar sets the guild avatar to the character's image. Uses
// the local cache first, then tries to refetch via the image URL.
func ApplyCharacterAvatar(ctx context.Context, imgClient images.ImageClient, s DiscordSession, guildID, characterID, imageURL string) error {
	if imgClient == nil {
		logger.FromContext(ctx).Error("no image client available")
		return fmt.Errorf("no image client available")
	}

	path, err := imgClient.GetImage(guildID, characterID)
	if err != nil {
		if imageURL == "" {
			return nil
		}
		path, err = imgClient.SaveImage(ctx, guildID, characterID, imageURL)
		if err != nil {
			return fmt.Errorf("failed to download character image: %w", err)
		}
	}

	dataURI, err := imgClient.ImageToBase64(ctx, path)
	if err != nil {
		return fmt.Errorf("failed to encode character image: %w", err)
	}

	if err := s.UpdateGuildAvatar(guildID, dataURI); err != nil {
		return fmt.Errorf("failed to update guild avatar: %w", err)
	}
	return nil
}

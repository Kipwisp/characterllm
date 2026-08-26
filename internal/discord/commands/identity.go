package commands

import (
	"context"
	"fmt"

	"characterllm/internal/logger"
)

// discordNickLimit is the maximum length of a guild nickname.
const discordNickLimit = 32

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
func SyncGuildIdentity(ctx context.Context, cmdCtx CommandContext, s DiscordSession, guildID string) error {
	details, err := cmdCtx.GetSession().GetCharacterDetails(ctx, guildID)
	if err != nil || details == nil || details.DisplayName == "" {
		return nil
	}

	if err := s.GuildMemberNickname(guildID, "@me", truncateToRuneLimit(details.DisplayName, discordNickLimit)); err != nil {
		return fmt.Errorf("failed to sync nickname: %w", err)
	}

	return ApplyCharacterAvatar(ctx, cmdCtx, s, guildID, details.CharacterID, details.ImageURL)
}

// ApplyCharacterAvatar sets the guild avatar to the character's image. The
// local cache file is the source of truth; imageURL is a best-effort re-fetch
// hint consulted only on a cache miss. It is a no-op when the character has
// neither a cached file nor a hint.
func ApplyCharacterAvatar(ctx context.Context, cmdCtx CommandContext, s DiscordSession, guildID, characterID, imageURL string) error {
	imgClient := cmdCtx.GetImageClient()
	if imgClient == nil {
		logger.FromContext(ctx).Error("no image client available in context")
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

package session

import (
	"context"
	"fmt"
)

// guildStore persists per-guild configuration that is independent of the
// active character.
type guildStore struct{ *core }

// AddAmbientChannel inserts a channel into the guild's ambient set; adding
// a member that is already present is a no-op.
func (s *guildStore) AddAmbientChannel(ctx context.Context, guildID, channelID string) error {
	if channelID == "" {
		return fmt.Errorf("failed to add ambient channel for guild %s: channel ID is required", guildID)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := s.db.ExecContext(ctx, "INSERT OR IGNORE INTO guild_ambient_channels (guild_id, channel_id) VALUES (?, ?)", guildID, channelID); err != nil {
		return fmt.Errorf("failed to add ambient channel for guild %s: %w", guildID, err)
	}
	return nil
}

// RemoveAmbientChannel removes one channel from the guild's ambient set;
// removing a channel that is not present is a no-op.
func (s *guildStore) RemoveAmbientChannel(ctx context.Context, guildID, channelID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := s.db.ExecContext(ctx, "DELETE FROM guild_ambient_channels WHERE guild_id = ? AND channel_id = ?", guildID, channelID); err != nil {
		return fmt.Errorf("failed to remove ambient channel for guild %s: %w", guildID, err)
	}
	return nil
}

// ClearAmbientChannels removes every channel from the guild's ambient set.
func (s *guildStore) ClearAmbientChannels(ctx context.Context, guildID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := s.db.ExecContext(ctx, "DELETE FROM guild_ambient_channels WHERE guild_id = ?", guildID); err != nil {
		return fmt.Errorf("failed to clear ambient channels for guild %s: %w", guildID, err)
	}
	return nil
}

// GetAmbientChannels returns the guild's ambient channel IDs sorted, or an
// empty slice when none are set.
func (s *guildStore) GetAmbientChannels(ctx context.Context, guildID string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.QueryContext(ctx, "SELECT channel_id FROM guild_ambient_channels WHERE guild_id = ? ORDER BY channel_id", guildID)
	if err != nil {
		return nil, fmt.Errorf("failed to get ambient channels for guild %s: %w", guildID, err)
	}
	defer rows.Close()

	channels := []string{}
	for rows.Next() {
		var channelID string
		if err := rows.Scan(&channelID); err != nil {
			return nil, fmt.Errorf("failed to scan ambient channel row: %w", err)
		}
		channels = append(channels, channelID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate ambient channel rows: %w", err)
	}
	return channels, nil
}

// ListAmbientChannels returns every guild that has at least one ambient
// channel, mapped to its sorted channel IDs.
func (s *guildStore) ListAmbientChannels(ctx context.Context) (map[string][]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.QueryContext(ctx, "SELECT guild_id, channel_id FROM guild_ambient_channels ORDER BY guild_id, channel_id")
	if err != nil {
		return nil, fmt.Errorf("failed to list ambient channels: %w", err)
	}
	defer rows.Close()

	channels := make(map[string][]string)
	for rows.Next() {
		var guildID, channelID string
		if err := rows.Scan(&guildID, &channelID); err != nil {
			return nil, fmt.Errorf("failed to scan ambient channel row: %w", err)
		}
		channels[guildID] = append(channels[guildID], channelID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate ambient channel rows: %w", err)
	}
	return channels, nil
}

package session

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// guildStore persists per-guild configuration that is independent of the
// active character.
type guildStore struct{ *core }

// SetAmbientChannel sets the guild's ambient channel, or clears it when
// channelID is empty.
func (s *guildStore) SetAmbientChannel(ctx context.Context, guildID, channelID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.ExecContext(ctx, "INSERT INTO guild_config (guild_id, ambient_channel_id) VALUES (?, ?) ON CONFLICT(guild_id) DO UPDATE SET ambient_channel_id = ?", guildID, channelID, channelID)
	if err != nil {
		return fmt.Errorf("failed to set ambient channel for guild %s: %w", guildID, err)
	}
	return nil
}

// GetAmbientChannel returns the guild's ambient channel ID, or "" when unset.
func (s *guildStore) GetAmbientChannel(ctx context.Context, guildID string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var channelID string
	err := s.db.QueryRowContext(ctx, "SELECT COALESCE(ambient_channel_id, '') FROM guild_config WHERE guild_id = ?", guildID).Scan(&channelID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("failed to get ambient channel for guild %s: %w", guildID, err)
	}
	return channelID, nil
}

// ListAmbientChannels returns every guild that has an ambient channel set,
// mapped to its channel ID.
func (s *guildStore) ListAmbientChannels(ctx context.Context) (map[string]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.QueryContext(ctx, "SELECT guild_id, ambient_channel_id FROM guild_config WHERE ambient_channel_id IS NOT NULL AND ambient_channel_id != ''")
	if err != nil {
		return nil, fmt.Errorf("failed to list ambient channels: %w", err)
	}
	defer rows.Close()

	channels := make(map[string]string)
	for rows.Next() {
		var guildID, channelID string
		if err := rows.Scan(&guildID, &channelID); err != nil {
			return nil, fmt.Errorf("failed to scan ambient channel row: %w", err)
		}
		channels[guildID] = channelID
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate ambient channel rows: %w", err)
	}
	return channels, nil
}

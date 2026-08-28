package session

import (
	"context"
	"database/sql"
	"fmt"
)

// characterStore persists character cards and the per-guild active
// character pointer.
type characterStore struct{ *core }

func (s *characterStore) GetCharacterCard(ctx context.Context, guildID, characterID string) (*CharacterCard, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var card CharacterCard
	err := s.db.QueryRow("SELECT guild_id, character_id, COALESCE(official_name, ''), COALESCE(series, ''), COALESCE(display_name, ''), COALESCE(description, ''), COALESCE(image_url, '') FROM character_cards WHERE guild_id = ? AND character_id = ?", guildID, characterID).Scan(&card.GuildID, &card.CharacterID, &card.OfficialName, &card.Series, &card.DisplayName, &card.Description, &card.ImageURL)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get character card %s for guild %s: %w", characterID, guildID, err)
	}

	return &card, nil
}

// SaveCharacterCard persists a guild-specific character card.
func (s *characterStore) SaveCharacterCard(ctx context.Context, guildID string, card *CharacterCard) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.Exec("INSERT INTO character_cards (guild_id, character_id, official_name, series, display_name, description) VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT(guild_id, character_id) DO UPDATE SET official_name = ?, series = ?, display_name = ?, description = ?",
		guildID, card.CharacterID, card.OfficialName, card.Series, card.DisplayName, card.Description, card.OfficialName, card.Series, card.DisplayName, card.Description)
	if err != nil {
		return fmt.Errorf("failed to save character card %s for guild %s: %w", card.CharacterID, guildID, err)
	}

	return tx.Commit()
}

// DeleteCharacterCard removes a character card, its chat history, and its
// rolling summaries in one transaction. Deleting an unknown character is a
// no-op.
func (s *characterStore) DeleteCharacterCard(ctx context.Context, guildID, characterID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	for _, q := range []string{
		"DELETE FROM chat_history WHERE guild_id = ? AND character_id = ?",
		"DELETE FROM conversation_summaries WHERE guild_id = ? AND character_id = ?",
		"DELETE FROM threads WHERE guild_id = ? AND character_id = ?",
		"DELETE FROM character_cards WHERE guild_id = ? AND character_id = ?",
	} {
		if _, err := tx.Exec(q, guildID, characterID); err != nil {
			return fmt.Errorf("failed to delete character card %s for guild %s: %w", characterID, guildID, err)
		}
	}

	return tx.Commit()
}

// GetGuildCharacters retrieves all character cards created by a specific guild.
func (s *characterStore) GetGuildCharacters(ctx context.Context, guildID string) ([]*CharacterCard, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query("SELECT guild_id, character_id, COALESCE(official_name, ''), COALESCE(series, ''), COALESCE(display_name, ''), COALESCE(description, ''), COALESCE(image_url, '') FROM character_cards WHERE guild_id = ?", guildID)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve characters for guild %s: %w", guildID, err)
	}
	defer rows.Close()

	var cards []*CharacterCard
	for rows.Next() {
		var card CharacterCard
		if err := rows.Scan(&card.GuildID, &card.CharacterID, &card.OfficialName, &card.Series, &card.DisplayName, &card.Description, &card.ImageURL); err != nil {
			return nil, fmt.Errorf("failed to scan character card for guild %s: %w", guildID, err)
		}
		cards = append(cards, &card)
	}

	return cards, nil
}

// SetActiveCharacter updates or creates the active character for a guild.
func (s *characterStore) SetActiveCharacter(ctx context.Context, guildID string, characterID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec("INSERT INTO guild_config (guild_id, active_character_id) VALUES (?, ?) ON CONFLICT(guild_id) DO UPDATE SET active_character_id = ?", guildID, characterID, characterID)
	if err != nil {
		return fmt.Errorf("failed to set active character for guild %s: %w", guildID, err)
	}
	return nil
}

// SetCharacterImage stores the profile image URL on the character card.
func (s *characterStore) SetCharacterImage(ctx context.Context, guildID, characterID, imageURL string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	res, err := s.db.Exec("UPDATE character_cards SET image_url = ? WHERE guild_id = ? AND character_id = ?", imageURL, guildID, characterID)
	if err != nil {
		return fmt.Errorf("failed to set character image for character %s in guild %s: %w", characterID, guildID, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("character %s not found in guild %s", characterID, guildID)
	}
	return nil
}

// GetCharacterDetails retrieves the active character identity and persona for a guild.
func (s *characterStore) GetCharacterDetails(ctx context.Context, guildID string) (*CharacterDetails, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var details CharacterDetails
	err := s.db.QueryRow(`
		SELECT s.active_character_id, COALESCE(c.official_name, ''), c.display_name, COALESCE(c.series, ''), c.description, COALESCE(c.image_url, ''), COALESCE(c.active_thread_id, '')
		FROM guild_config s
		JOIN character_cards c ON s.active_character_id = c.character_id
		WHERE s.guild_id = ?`, guildID).Scan(&details.CharacterID, &details.OfficialName, &details.DisplayName, &details.Series, &details.Description, &details.ImageURL, &details.ActiveThreadID)
	if err != nil {
		return nil, fmt.Errorf("failed to get character details for guild %s: %w", guildID, err)
	}
	return &details, nil
}

package session

import (
	"context"
	"database/sql"
	"fmt"
)

// summaryStore maintains the rolling conversation summaries and the
// history pruning that feeds them.
type summaryStore struct{ *core }

// PruneAndSummarize removes the oldest messages and replaces them with a rolling summary stored in conversation_summaries.
func (s *summaryStore) PruneAndSummarize(ctx context.Context, guildID, threadID string, summary string, deletedCount int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	charID := s.getCurrentCharacterID(ctx, guildID)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	if deletedCount > 0 {
		var maxID int
		err = tx.QueryRow("SELECT id FROM chat_history WHERE guild_id = ? AND character_id = ? AND thread_id = ? ORDER BY created_at ASC, id ASC LIMIT 1 OFFSET ?", guildID, charID, threadID, deletedCount-1).Scan(&maxID)
		if err != nil {
			return fmt.Errorf("failed to find boundary ID for pruning for guild %s, thread %s: %w", guildID, threadID, err)
		}

		_, err = tx.Exec("DELETE FROM chat_history WHERE guild_id = ? AND character_id = ? AND thread_id = ? AND id <= ?", guildID, charID, threadID, maxID)
		if err != nil {
			return fmt.Errorf("failed to prune history for guild %s, thread %s: %w", guildID, threadID, err)
		}
	}

	_, err = tx.Exec(`INSERT INTO conversation_summaries (guild_id, character_id, thread_id, content)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(guild_id, character_id, thread_id) DO UPDATE SET
			content = excluded.content,
			created_at = CURRENT_TIMESTAMP`,
		guildID, charID, threadID, summary)
	if err != nil {
		return fmt.Errorf("failed to upsert summary for guild %s, thread %s: %w", guildID, threadID, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit prune transaction for guild %s, thread %s: %w", guildID, threadID, err)
	}
	return nil
}

// GetSummary returns the rolling conversation summary for a guild and thread, or an empty string if none exists.
func (s *summaryStore) GetSummary(ctx context.Context, guildID, threadID string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	charID := s.getCurrentCharacterID(ctx, guildID)
	var summary string
	err := s.db.QueryRow("SELECT content FROM conversation_summaries WHERE guild_id = ? AND character_id = ? AND thread_id = ?", guildID, charID, threadID).Scan(&summary)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", fmt.Errorf("failed to get summary for guild %s, thread %s: %w", guildID, threadID, err)
	}
	return summary, nil
}

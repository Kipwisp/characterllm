package session

import (
	"context"
	"fmt"

	"characterllm/internal/llm"
)

// historyStore persists per-thread chat history messages.
type historyStore struct{ *core }

// GetHistory retrieves conversation history for a given guild and thread.
func (s *historyStore) GetHistory(ctx context.Context, guildID, threadID string, limit, offset int) ([]llm.Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var messages []llm.Message

	charID := s.getCurrentCharacterID(ctx, guildID)
	rows, err := s.db.Query(`
		SELECT role, content FROM chat_history
		WHERE guild_id = ? AND character_id = ? AND thread_id = ?
		ORDER BY created_at ASC, id ASC
		LIMIT ? OFFSET ?`,
		guildID, charID, threadID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to query history for guild %s, thread %s: %w", guildID, threadID, err)
	}
	defer rows.Close()

	for rows.Next() {
		var msg llm.Message
		if err := rows.Scan(&msg.Role, &msg.Content); err != nil {
			return nil, fmt.Errorf("failed to scan history message for guild %s: %w", guildID, err)
		}
		messages = append(messages, msg)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error during history row iteration for guild %s: %w", guildID, err)
	}

	return messages, nil
}

// GetLastCharacterMessage returns the most recent message spoken by the
// character (assistant) in its thread, and whether one exists. A query
// error is treated as no history so callers degrade gracefully.
func (s *historyStore) GetLastCharacterMessage(ctx context.Context, guildID, characterID, threadID string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var content string
	err := s.db.QueryRowContext(ctx, `
		SELECT content FROM chat_history
		WHERE guild_id = ? AND character_id = ? AND thread_id = ? AND role = 'assistant'
		ORDER BY created_at DESC, id DESC
		LIMIT 1`, guildID, characterID, threadID).Scan(&content)
	if err != nil {
		return "", false
	}
	return content, true
}

// SaveMessage persists a new message to the chat history for a guild and thread.
func (s *historyStore) SaveMessage(ctx context.Context, guildID, threadID, role, content string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	charID := s.getCurrentCharacterID(ctx, guildID)
	_, err := s.db.ExecContext(ctx, "INSERT INTO chat_history (guild_id, character_id, thread_id, role, content) VALUES (?, ?, ?, ?, ?)", guildID, charID, threadID, role, content)
	if err != nil {
		return fmt.Errorf("failed to save message for guild %s, thread %s: %w", guildID, threadID, err)
	}
	return nil
}

// AppendToLastUserMessage appends suffix to the most recent user message for a
// guild and thread, e.g. to attach a harvested image description to the turn.
func (s *historyStore) AppendToLastUserMessage(ctx context.Context, guildID, threadID, suffix string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	charID := s.getCurrentCharacterID(ctx, guildID)
	res, err := s.db.Exec(`UPDATE chat_history SET content = content || ?
		WHERE id = (SELECT MAX(id) FROM chat_history
			WHERE guild_id = ? AND character_id = ? AND thread_id = ? AND role = 'user')`,
		suffix, guildID, charID, threadID)
	if err != nil {
		return fmt.Errorf("failed to append to last user message for guild %s, thread %s: %w", guildID, threadID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check append result: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("no user message found to append to for guild %s, thread %s", guildID, threadID)
	}
	return nil
}

// ClearHistory deletes all chat history for the current character in a guild and thread.
func (s *historyStore) ClearHistory(ctx context.Context, guildID, threadID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	charID := s.getCurrentCharacterID(ctx, guildID)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.Exec("DELETE FROM chat_history WHERE guild_id = ? AND character_id = ? AND thread_id = ?", guildID, charID, threadID)
	if err != nil {
		return fmt.Errorf("failed to clear history for guild %s, thread %s: %w", guildID, threadID, err)
	}

	_, err = tx.Exec("DELETE FROM conversation_summaries WHERE guild_id = ? AND character_id = ? AND thread_id = ?", guildID, charID, threadID)
	if err != nil {
		return fmt.Errorf("failed to clear summary for guild %s, thread %s: %w", guildID, threadID, err)
	}

	return tx.Commit()
}

// GetHistoryCount returns the total number of messages in the history for a guild and thread.
func (s *historyStore) GetHistoryCount(ctx context.Context, guildID, threadID string) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	charID := s.getCurrentCharacterID(ctx, guildID)
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM chat_history WHERE guild_id = ? AND character_id = ? AND thread_id = ?", guildID, charID, threadID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get history count for guild %s, thread %s: %w", guildID, threadID, err)
	}
	return count, nil
}

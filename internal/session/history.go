package session

import (
	"context"
	"fmt"
	"strings"

	"characterllm/internal/llm"
)

// ImageMarkerPrefix introduces a numbered image placeholder in a stored
// transcript; see ImageMarker.
const ImageMarkerPrefix = "[IMG-"

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
		var role, content string
		if err := rows.Scan(&role, &content); err != nil {
			return nil, fmt.Errorf("failed to scan history message for guild %s: %w", guildID, err)
		}
		parsed, err := llm.ParseRole(role)
		if err != nil {
			return nil, fmt.Errorf("history row for guild %s: %w", guildID, err)
		}
		messages = append(messages, llm.TextMessage(parsed, content))
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
func (s *historyStore) SaveMessage(ctx context.Context, guildID, threadID string, role llm.Role, content string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	charID := s.getCurrentCharacterID(ctx, guildID)
	_, err := s.db.ExecContext(ctx, "INSERT INTO chat_history (guild_id, character_id, thread_id, role, content) VALUES (?, ?, ?, ?, ?)", guildID, charID, threadID, role.String(), content)
	if err != nil {
		return fmt.Errorf("failed to save message for guild %s, thread %s: %w", guildID, threadID, err)
	}
	return nil
}

// ImageMarker is the persisted placeholder for the 1-based image position in
// a stored transcript. It is resolved to a harvested image note after the
// reply, so each description lands under the line whose image it describes.
func ImageMarker(i int) string { return fmt.Sprintf("%s%d]", ImageMarkerPrefix, i) }

// ResolveImageNotes replaces the image markers in the most recent user
// message with the harvested notes: marker i takes note i (both in the order
// the model saw the images), markers without a note become
// [Image: no description] so the image's existence stays in the record, and
// surplus notes are appended at the end as a single [Image: ...] line. It
// returns the persisted content so the caller can log the resulting row.
func (s *historyStore) ResolveImageNotes(ctx context.Context, guildID, threadID string, notes []string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	charID := s.getCurrentCharacterID(ctx, guildID)
	var content string
	err := s.db.QueryRow(`SELECT content FROM chat_history
		WHERE id = (SELECT MAX(id) FROM chat_history
			WHERE guild_id = ? AND character_id = ? AND thread_id = ? AND role = 'user')`,
		guildID, charID, threadID).Scan(&content)
	if err != nil {
		return "", fmt.Errorf("failed to read last user message for guild %s, thread %s: %w", guildID, threadID, err)
	}

	resolved := 0
	for i := 1; ; i++ {
		marker := "\n" + ImageMarker(i)
		if !strings.Contains(content, marker) {
			break
		}
		resolved++
		if i-1 < len(notes) {
			content = strings.Replace(content, marker, "\n[Image: "+notes[i-1]+"]", 1)
		} else {
			content = strings.Replace(content, marker, "\n[Image: no description]", 1)
		}
	}
	if resolved == 0 {
		return content, nil
	}
	if len(notes) > resolved {
		content += "\n[Image: " + strings.Join(notes[resolved:], "; ") + "]"
	}

	_, err = s.db.Exec(`UPDATE chat_history SET content = ?
		WHERE id = (SELECT MAX(id) FROM chat_history
			WHERE guild_id = ? AND character_id = ? AND thread_id = ? AND role = 'user')`,
		content, guildID, charID, threadID)
	if err != nil {
		return "", fmt.Errorf("failed to resolve image notes for guild %s, thread %s: %w", guildID, threadID, err)
	}
	return content, nil
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

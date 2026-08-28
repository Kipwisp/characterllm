package session

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
)

// ErrThreadNameTaken is returned by CreateThread when another thread of the
// same character already has the requested name.
var ErrThreadNameTaken = errors.New("thread name already taken")

// Thread is one named conversation of a character.
type Thread struct {
	ThreadID string
	Name     string
	Active   bool
}

// EnsureDefaultThread gives a character its default thread (ID 1, named
// "Thread 1") when it has no threads yet, and points the active thread at
// the most recently used one whenever the pointer is unset.
func (m *Manager) EnsureDefaultThread(ctx context.Context, guildID, characterID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var cardCount int
	err := m.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM character_cards WHERE guild_id = ? AND character_id = ?", guildID, characterID).Scan(&cardCount)
	if err != nil {
		return fmt.Errorf("failed to look up character %s in guild %s: %w", characterID, guildID, err)
	}
	if cardCount == 0 {
		return nil
	}

	var count int
	err = m.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM threads WHERE guild_id = ? AND character_id = ?", guildID, characterID).Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to count threads for character %s in guild %s: %w", characterID, guildID, err)
	}
	if count == 0 {
		if _, err := m.db.ExecContext(ctx, `INSERT INTO threads (guild_id, character_id, thread_id, name, last_used_seq)
			VALUES (?, ?, '1', 'Thread 1', COALESCE((SELECT MAX(last_used_seq) FROM threads WHERE guild_id = ? AND character_id = ?), 0) + 1)`,
			guildID, characterID, guildID, characterID); err != nil {
			return fmt.Errorf("failed to create default thread for character %s in guild %s: %w", characterID, guildID, err)
		}
	}

	_, err = m.db.ExecContext(ctx, `
		UPDATE character_cards
		SET active_thread_id = (
			SELECT thread_id FROM threads
			WHERE guild_id = ? AND character_id = ?
			ORDER BY last_used_seq DESC, created_at DESC, thread_id ASC
			LIMIT 1
		)
		WHERE guild_id = ? AND character_id = ? AND (active_thread_id IS NULL OR active_thread_id = '')`,
		guildID, characterID, guildID, characterID)
	if err != nil {
		return fmt.Errorf("failed to ensure active thread for character %s in guild %s: %w", characterID, guildID, err)
	}
	return nil
}

// GetActiveThreadID returns the character's active thread ID, or an empty
// string when the character is unknown or has no active thread set.
func (m *Manager) GetActiveThreadID(ctx context.Context, guildID, characterID string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var id string
	err := m.db.QueryRowContext(ctx, "SELECT COALESCE(active_thread_id, '') FROM character_cards WHERE guild_id = ? AND character_id = ?", guildID, characterID).Scan(&id)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", fmt.Errorf("failed to get active thread for character %s in guild %s: %w", characterID, guildID, err)
	}
	return id, nil
}

// CreateThread mints a new thread for a character and makes it active. The
// thread ID is the smallest unused positive integer, so IDs start at 1 and a
// deleted thread's ID is reused by the next created thread.
func (m *Manager) CreateThread(ctx context.Context, guildID, characterID, name string) (*Thread, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	var existing string
	err = tx.QueryRow("SELECT name FROM threads WHERE guild_id = ? AND character_id = ? AND name = ?", guildID, characterID, name).Scan(&existing)
	if err == nil {
		return nil, fmt.Errorf("%w: %q", ErrThreadNameTaken, name)
	}
	if err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to check thread name for character %s in guild %s: %w", characterID, guildID, err)
	}

	id, err := mintThreadID(tx, guildID, characterID)
	if err != nil {
		return nil, err
	}

	if _, err := tx.Exec(`INSERT INTO threads (guild_id, character_id, thread_id, name, last_used_seq)
		VALUES (?, ?, ?, ?, COALESCE((SELECT MAX(last_used_seq) FROM threads WHERE guild_id = ? AND character_id = ?), 0) + 1)`,
		guildID, characterID, id, name, guildID, characterID); err != nil {
		return nil, fmt.Errorf("failed to create thread %q for character %s in guild %s: %w", name, characterID, guildID, err)
	}

	if _, err := tx.Exec("UPDATE character_cards SET active_thread_id = ? WHERE guild_id = ? AND character_id = ?", id, guildID, characterID); err != nil {
		return nil, fmt.Errorf("failed to set active thread for character %s in guild %s: %w", characterID, guildID, err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit thread creation: %w", err)
	}
	return &Thread{ThreadID: id, Name: name, Active: true}, nil
}

// mintThreadID returns the smallest positive integer not already in use as a
// thread ID for the character.
func mintThreadID(tx *sql.Tx, guildID, characterID string) (string, error) {
	rows, err := tx.Query("SELECT thread_id FROM threads WHERE guild_id = ? AND character_id = ?", guildID, characterID)
	if err != nil {
		return "", fmt.Errorf("failed to list thread IDs for character %s in guild %s: %w", characterID, guildID, err)
	}
	defer rows.Close()

	taken := make(map[int]bool)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return "", fmt.Errorf("failed to scan thread ID: %w", err)
		}
		if n, err := strconv.Atoi(id); err == nil && n > 0 {
			taken[n] = true
		}
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("failed to list thread IDs: %w", err)
	}

	for n := 1; ; n++ {
		if !taken[n] {
			return strconv.Itoa(n), nil
		}
	}
}

// ListThreads returns the character's threads, most recently used first.
func (m *Manager) ListThreads(ctx context.Context, guildID, characterID string) ([]*Thread, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	rows, err := m.db.Query(`
		SELECT thread_id, name FROM threads
		WHERE guild_id = ? AND character_id = ?
		ORDER BY last_used_seq DESC, created_at DESC, thread_id ASC`, guildID, characterID)
	if err != nil {
		return nil, fmt.Errorf("failed to list threads for character %s in guild %s: %w", characterID, guildID, err)
	}
	defer rows.Close()

	var threads []*Thread
	for rows.Next() {
		var th Thread
		if err := rows.Scan(&th.ThreadID, &th.Name); err != nil {
			return nil, fmt.Errorf("failed to scan thread for character %s in guild %s: %w", characterID, guildID, err)
		}
		threads = append(threads, &th)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to list threads for character %s in guild %s: %w", characterID, guildID, err)
	}

	var active string
	err = m.db.QueryRow("SELECT COALESCE(active_thread_id, '') FROM character_cards WHERE guild_id = ? AND character_id = ?", guildID, characterID).Scan(&active)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to get active thread for character %s in guild %s: %w", characterID, guildID, err)
	}
	for _, th := range threads {
		if th.ThreadID == active {
			th.Active = true
		}
	}
	return threads, nil
}

// GetThread returns one of the character's threads by ID, or nil when no
// such thread exists.
func (m *Manager) GetThread(ctx context.Context, guildID, characterID, threadID string) (*Thread, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var th Thread
	err := m.db.QueryRow(`
		SELECT thread_id, name FROM threads
		WHERE guild_id = ? AND character_id = ? AND thread_id = ?`, guildID, characterID, threadID).Scan(&th.ThreadID, &th.Name)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get thread %s for character %s in guild %s: %w", threadID, characterID, guildID, err)
	}

	var active string
	err = m.db.QueryRow("SELECT COALESCE(active_thread_id, '') FROM character_cards WHERE guild_id = ? AND character_id = ?", guildID, characterID).Scan(&active)
	if err == nil && active == threadID {
		th.Active = true
	}
	return &th, nil
}

// SetActiveThread makes the named thread the character's active thread.
func (m *Manager) SetActiveThread(ctx context.Context, guildID, characterID, threadID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	var exists int
	if err := tx.QueryRow("SELECT COUNT(*) FROM threads WHERE guild_id = ? AND character_id = ? AND thread_id = ?", guildID, characterID, threadID).Scan(&exists); err != nil {
		return fmt.Errorf("failed to check thread %s for character %s in guild %s: %w", threadID, characterID, guildID, err)
	}
	if exists == 0 {
		return fmt.Errorf("thread %s not found for character %s in guild %s", threadID, characterID, guildID)
	}

	if _, err := tx.Exec("UPDATE character_cards SET active_thread_id = ? WHERE guild_id = ? AND character_id = ?", threadID, guildID, characterID); err != nil {
		return fmt.Errorf("failed to set active thread for character %s in guild %s: %w", characterID, guildID, err)
	}

	if _, err := tx.Exec(`UPDATE threads SET last_used_seq = COALESCE((SELECT MAX(last_used_seq) FROM threads WHERE guild_id = ? AND character_id = ?), 0) + 1
		WHERE guild_id = ? AND character_id = ? AND thread_id = ?`, guildID, characterID, guildID, characterID, threadID); err != nil {
		return fmt.Errorf("failed to touch thread %s for character %s in guild %s: %w", threadID, characterID, guildID, err)
	}

	return tx.Commit()
}

// TouchThread records a thread as used now, keeping "most recently used"
// ordering current even for threads that have no history yet.
func (m *Manager) TouchThread(ctx context.Context, guildID, characterID, threadID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, err := m.db.ExecContext(ctx, `UPDATE threads SET last_used_seq = COALESCE((SELECT MAX(last_used_seq) FROM threads WHERE guild_id = ? AND character_id = ?), 0) + 1
		WHERE guild_id = ? AND character_id = ? AND thread_id = ?`, guildID, characterID, guildID, characterID, threadID); err != nil {
		return fmt.Errorf("failed to touch thread %s for character %s in guild %s: %w", threadID, characterID, guildID, err)
	}
	return nil
}

// DeleteThread removes a thread and its conversation. When it is the
// character's last remaining thread, only the conversation is cleared and
// the thread itself survives; it reports whether that is what happened.
// Deleting the active thread hands the active pointer to the most recently
// used surviving thread.
func (m *Manager) DeleteThread(ctx context.Context, guildID, characterID, threadID string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	var count int
	if err := tx.QueryRow("SELECT COUNT(*) FROM threads WHERE guild_id = ? AND character_id = ?", guildID, characterID).Scan(&count); err != nil {
		return false, fmt.Errorf("failed to count threads for character %s in guild %s: %w", characterID, guildID, err)
	}

	for _, q := range []string{
		"DELETE FROM chat_history WHERE guild_id = ? AND character_id = ? AND thread_id = ?",
		"DELETE FROM conversation_summaries WHERE guild_id = ? AND character_id = ? AND thread_id = ?",
	} {
		if _, err := tx.Exec(q, guildID, characterID, threadID); err != nil {
			return false, fmt.Errorf("failed to clear conversation of thread %s for character %s in guild %s: %w", threadID, characterID, guildID, err)
		}
	}

	if count > 1 {
		if _, err := tx.Exec("DELETE FROM threads WHERE guild_id = ? AND character_id = ? AND thread_id = ?", guildID, characterID, threadID); err != nil {
			return false, fmt.Errorf("failed to delete thread %s for character %s in guild %s: %w", threadID, characterID, guildID, err)
		}

		var active string
		err := tx.QueryRow("SELECT COALESCE(active_thread_id, '') FROM character_cards WHERE guild_id = ? AND character_id = ?", guildID, characterID).Scan(&active)
		if err != nil {
			return false, fmt.Errorf("failed to get active thread for character %s in guild %s: %w", characterID, guildID, err)
		}
		if active == threadID {
			var next string
			err = tx.QueryRow(`
				SELECT thread_id FROM threads
				WHERE guild_id = ? AND character_id = ? AND thread_id != ?
				ORDER BY last_used_seq DESC, created_at DESC, thread_id ASC
				LIMIT 1`, guildID, characterID, threadID).Scan(&next)
			if err != nil {
				return false, fmt.Errorf("failed to pick replacement active thread for character %s in guild %s: %w", characterID, guildID, err)
			}
			if _, err := tx.Exec("UPDATE character_cards SET active_thread_id = ? WHERE guild_id = ? AND character_id = ?", next, guildID, characterID); err != nil {
				return false, fmt.Errorf("failed to set replacement active thread for character %s in guild %s: %w", characterID, guildID, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("failed to commit thread deletion: %w", err)
	}
	return count == 1, nil
}

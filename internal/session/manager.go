// Package session manages user-specific character personas and conversation history using SQLite.
package session

import (
	"context"
	"database/sql"
	"fmt"
	"sync"

	"characterllm/internal/llm"

	_ "modernc.org/sqlite"
)

// CharacterCard represents a reusable character profile stored globally.
// Description should contain the full Persona Specification (Identity, Appearance, Voice, Examples, Scenario, and Style Anchor).
type CharacterCard struct {
	GuildID      string
	CharacterID  string
	OfficialName string
	Series       string
	DisplayName  string
	Description  string
	ImageURL     string
}

// CharacterDetails represents the active character persona for a guild.
type CharacterDetails struct {
	CharacterID  string
	OfficialName string
	DisplayName  string
	Series       string
	Description  string
	ImageURL     string
}

// Manager handles the persistence and retrieval of character data and conversation history for Discord guilds.
type Manager struct {
	db                  *sql.DB
	mu                  sync.RWMutex
	defaultSystemPrompt string
	maxSize             int
	imageCandidates     map[string][]string
}

// NewManager initializes a new session manager and creates the necessary database tables.
func NewManager(dbPath string, defaultPrompt string) (*Manager, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite db: %v", err)
	}

	m := &Manager{
		db:                  db,
		defaultSystemPrompt: defaultPrompt,
		maxSize:             30,
		imageCandidates:     make(map[string][]string),
	}

	if err := m.initDB(); err != nil {
		return nil, err
	}

	return m, nil
}

func (m *Manager) GetCharacterCard(ctx context.Context, guildID, characterID string) (*CharacterCard, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var card CharacterCard
	err := m.db.QueryRow("SELECT guild_id, character_id, COALESCE(official_name, ''), COALESCE(series, ''), COALESCE(display_name, ''), COALESCE(description, ''), COALESCE(image_url, '') FROM character_cards WHERE guild_id = ? AND character_id = ?", guildID, characterID).Scan(&card.GuildID, &card.CharacterID, &card.OfficialName, &card.Series, &card.DisplayName, &card.Description, &card.ImageURL)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get character card %s for guild %s: %w", characterID, guildID, err)
	}

	return &card, nil
}

// SaveCharacterCard persists a guild-specific character card.
func (m *Manager) SaveCharacterCard(ctx context.Context, guildID string, card *CharacterCard) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	tx, err := m.db.Begin()
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
func (m *Manager) DeleteCharacterCard(ctx context.Context, guildID, characterID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	for _, q := range []string{
		"DELETE FROM chat_history WHERE guild_id = ? AND character_id = ?",
		"DELETE FROM conversation_summaries WHERE guild_id = ? AND character_id = ?",
		"DELETE FROM character_cards WHERE guild_id = ? AND character_id = ?",
	} {
		if _, err := tx.Exec(q, guildID, characterID); err != nil {
			return fmt.Errorf("failed to delete character card %s for guild %s: %w", characterID, guildID, err)
		}
	}

	return tx.Commit()
}

// GetGuildCharacters retrieves all character cards created by a specific guild.
func (m *Manager) GetGuildCharacters(ctx context.Context, guildID string) ([]*CharacterCard, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	rows, err := m.db.Query("SELECT guild_id, character_id, COALESCE(official_name, ''), COALESCE(series, ''), COALESCE(display_name, ''), COALESCE(description, ''), COALESCE(image_url, '') FROM character_cards WHERE guild_id = ?", guildID)
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

// Close closes the underlying database connection.
func (m *Manager) Close() error {
	return m.db.Close()
}

func (m *Manager) initDB() error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS guild_config (
			guild_id TEXT PRIMARY KEY,
			active_character_id TEXT
		);`,
		`CREATE TABLE IF NOT EXISTS character_cards (
			guild_id TEXT,
			character_id TEXT,
			official_name TEXT,
			series TEXT,
			display_name TEXT,
			description TEXT,
			image_url TEXT DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (guild_id, character_id)
		);`,
		`CREATE TABLE IF NOT EXISTS chat_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			guild_id TEXT,
			character_id TEXT,
			thread_id TEXT,
			role TEXT,
			content TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS conversation_summaries (
			guild_id TEXT,
			character_id TEXT,
			thread_id TEXT,
			content TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (guild_id, character_id, thread_id)
		);`,
	}

	for _, q := range queries {
		if _, err := m.db.Exec(q); err != nil {
			return fmt.Errorf("failed to initialize db table: %v", err)
		}
	}

	_, err := m.db.Exec("CREATE INDEX IF NOT EXISTS idx_history_char ON chat_history(guild_id, character_id, thread_id);")
	if err != nil {
		return fmt.Errorf("failed to create character history index: %v", err)
	}

	return nil
}

func (m *Manager) getCurrentCharacterID(ctx context.Context, guildID string) string {
	var id string
	err := m.db.QueryRowContext(ctx, "SELECT active_character_id FROM guild_config WHERE guild_id = ?", guildID).Scan(&id)
	if err != nil {
		return ""
	}
	return id
}

// GetHistory retrieves conversation history for a given guild and thread.
func (m *Manager) GetHistory(ctx context.Context, guildID, threadID string, limit, offset int) ([]llm.Message, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var messages []llm.Message

	charID := m.getCurrentCharacterID(ctx, guildID)
	rows, err := m.db.Query(`
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

// SaveMessage persists a new message to the chat history for a guild and thread.
func (m *Manager) SaveMessage(ctx context.Context, guildID, threadID, role, content string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	charID := m.getCurrentCharacterID(ctx, guildID)
	_, err := m.db.ExecContext(ctx, "INSERT INTO chat_history (guild_id, character_id, thread_id, role, content) VALUES (?, ?, ?, ?, ?)", guildID, charID, threadID, role, content)
	if err != nil {
		return fmt.Errorf("failed to save message for guild %s, thread %s: %w", guildID, threadID, err)
	}
	return nil
}

// AppendToLastUserMessage appends suffix to the most recent user message for a
// guild and thread, e.g. to attach a harvested image description to the turn.
func (m *Manager) AppendToLastUserMessage(ctx context.Context, guildID, threadID, suffix string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	charID := m.getCurrentCharacterID(ctx, guildID)
	res, err := m.db.Exec(`UPDATE chat_history SET content = content || ?
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

// SetActiveCharacter updates or creates the active character for a guild.
func (m *Manager) SetActiveCharacter(ctx context.Context, guildID string, characterID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, err := m.db.Exec("INSERT INTO guild_config (guild_id, active_character_id) VALUES (?, ?) ON CONFLICT(guild_id) DO UPDATE SET active_character_id = ?", guildID, characterID, characterID)
	if err != nil {
		return fmt.Errorf("failed to set active character for guild %s: %w", guildID, err)
	}
	return nil
}

// SetCharacterImage stores the profile image URL on the character card.
func (m *Manager) SetCharacterImage(ctx context.Context, guildID, characterID, imageURL string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	res, err := m.db.Exec("UPDATE character_cards SET image_url = ? WHERE guild_id = ? AND character_id = ?", imageURL, guildID, characterID)
	if err != nil {
		return fmt.Errorf("failed to set character image for character %s in guild %s: %w", characterID, guildID, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("character %s not found in guild %s", characterID, guildID)
	}
	return nil
}

// SaveImageCandidates stores a list of candidate image URLs for a guild during the character setup process.
func (m *Manager) SaveImageCandidates(ctx context.Context, guildID string, urls []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.imageCandidates[guildID] = urls
	return nil
}

// GetImageCandidates retrieves the candidate image URLs for a guild.
func (m *Manager) GetImageCandidates(ctx context.Context, guildID string) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	urls, ok := m.imageCandidates[guildID]
	if !ok {
		return nil, nil
	}
	return urls, nil
}

// ClearImageCandidates removes the candidate image URLs for a guild.
func (m *Manager) ClearImageCandidates(ctx context.Context, guildID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.imageCandidates, guildID)
	return nil
}

// GetCharacterDetails retrieves the active character identity and persona for a guild.
func (m *Manager) GetCharacterDetails(ctx context.Context, guildID string) (*CharacterDetails, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var details CharacterDetails
	err := m.db.QueryRow(`
		SELECT s.active_character_id, COALESCE(c.official_name, ''), c.display_name, COALESCE(c.series, ''), c.description, COALESCE(c.image_url, '')
		FROM guild_config s
		JOIN character_cards c ON s.active_character_id = c.character_id
		WHERE s.guild_id = ?`, guildID).Scan(&details.CharacterID, &details.OfficialName, &details.DisplayName, &details.Series, &details.Description, &details.ImageURL)
	if err != nil {
		return nil, fmt.Errorf("failed to get character details for guild %s: %w", guildID, err)
	}
	return &details, nil
}

// ClearHistory deletes all chat history for the current character in a guild and thread.
func (m *Manager) ClearHistory(ctx context.Context, guildID, threadID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	charID := m.getCurrentCharacterID(ctx, guildID)
	tx, err := m.db.BeginTx(ctx, nil)
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
func (m *Manager) GetHistoryCount(ctx context.Context, guildID, threadID string) (int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	charID := m.getCurrentCharacterID(ctx, guildID)
	var count int
	err := m.db.QueryRow("SELECT COUNT(*) FROM chat_history WHERE guild_id = ? AND character_id = ? AND thread_id = ?", guildID, charID, threadID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get history count for guild %s, thread %s: %w", guildID, threadID, err)
	}
	return count, nil
}

// CountCharacterThreads returns the number of distinct threads with chat
// history stored for a single character.
func (m *Manager) CountCharacterThreads(ctx context.Context, guildID, characterID string) (int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var count int
	err := m.db.QueryRow("SELECT COUNT(DISTINCT thread_id) FROM chat_history WHERE guild_id = ? AND character_id = ?", guildID, characterID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count history for character %s in guild %s: %w", characterID, guildID, err)
	}
	return count, nil
}

// PruneAndSummarize removes the oldest messages and replaces them with a rolling summary stored in conversation_summaries.
func (m *Manager) PruneAndSummarize(ctx context.Context, guildID, threadID string, summary string, deletedCount int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	charID := m.getCurrentCharacterID(ctx, guildID)
	tx, err := m.db.BeginTx(ctx, nil)
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
func (m *Manager) GetSummary(ctx context.Context, guildID, threadID string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	charID := m.getCurrentCharacterID(ctx, guildID)
	var summary string
	err := m.db.QueryRow("SELECT content FROM conversation_summaries WHERE guild_id = ? AND character_id = ? AND thread_id = ?", guildID, charID, threadID).Scan(&summary)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", fmt.Errorf("failed to get summary for guild %s, thread %s: %w", guildID, threadID, err)
	}
	return summary, nil
}

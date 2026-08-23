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
	SourceURL    string
	Modifiers    string
	Scenario     string
}

// CharacterDetails represents the active character persona for a guild.
type CharacterDetails struct {
	CharacterID string
	DisplayName string
	Description string
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
	err := m.db.QueryRow("SELECT guild_id, character_id, COALESCE(official_name, ''), COALESCE(series, ''), COALESCE(display_name, ''), COALESCE(description, ''), COALESCE(source_url, ''), COALESCE(modifiers, ''), COALESCE(scenario, '') FROM character_cards WHERE guild_id = ? AND character_id = ?", guildID, characterID).Scan(&card.GuildID, &card.CharacterID, &card.OfficialName, &card.Series, &card.DisplayName, &card.Description, &card.SourceURL, &card.Modifiers, &card.Scenario)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get character card %s for guild %s: %w", characterID, guildID, err)
	}

	return &card, nil
}

// GetCardByAlias retrieves a character card using a known alias for a specific guild.
func (m *Manager) GetCardByAlias(ctx context.Context, guildID, alias string) (*CharacterCard, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var characterID string
	err := m.db.QueryRow("SELECT character_id FROM character_aliases WHERE guild_id = ? AND alias = ?", guildID, alias).Scan(&characterID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to resolve alias %s for guild %s: %w", alias, guildID, err)
	}

	var card CharacterCard
	err = m.db.QueryRow("SELECT guild_id, character_id, COALESCE(official_name, ''), COALESCE(series, ''), COALESCE(display_name, ''), COALESCE(description, ''), COALESCE(source_url, ''), COALESCE(modifiers, ''), COALESCE(scenario, '') FROM character_cards WHERE guild_id = ? AND character_id = ?", guildID, characterID).Scan(&card.GuildID, &card.CharacterID, &card.OfficialName, &card.Series, &card.DisplayName, &card.Description, &card.SourceURL, &card.Modifiers, &card.Scenario)
	if err != nil {
		return nil, fmt.Errorf("failed to load card for alias %s in guild %s: %w", alias, guildID, err)
	}

	return &card, nil
}

// SaveCharacterCard persists a guild-specific character card and its aliases.
func (m *Manager) SaveCharacterCard(ctx context.Context, guildID string, card *CharacterCard, aliases []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	tx, err := m.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.Exec("INSERT INTO character_cards (guild_id, character_id, official_name, series, display_name, description, source_url, modifiers, scenario) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(guild_id, character_id) DO UPDATE SET official_name = ?, display_name = ?, description = ?, source_url = ?, modifiers = ?, scenario = ?",
		guildID, card.CharacterID, card.OfficialName, card.Series, card.DisplayName, card.Description, card.SourceURL, card.Modifiers, card.Scenario, card.OfficialName, card.DisplayName, card.Description, card.SourceURL, card.Modifiers, card.Scenario)
	if err != nil {
		return fmt.Errorf("failed to save character card %s for guild %s: %w", card.CharacterID, guildID, err)
	}

	for _, alias := range aliases {
		_, err = tx.Exec("INSERT INTO character_aliases (guild_id, alias, character_id) VALUES (?, ?, ?) ON CONFLICT(guild_id, alias) DO UPDATE SET character_id = ?",
			guildID, alias, card.CharacterID, card.CharacterID)
		if err != nil {
			return fmt.Errorf("failed to save alias %s for guild %s: %w", alias, guildID, err)
		}
	}

	return tx.Commit()
}

// GetGuildCharacters retrieves all character cards created by a specific guild.
func (m *Manager) GetGuildCharacters(ctx context.Context, guildID string) ([]*CharacterCard, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	rows, err := m.db.Query("SELECT guild_id, character_id, COALESCE(official_name, ''), COALESCE(series, ''), COALESCE(display_name, ''), COALESCE(description, ''), COALESCE(source_url, ''), COALESCE(modifiers, ''), COALESCE(scenario, '') FROM character_cards WHERE guild_id = ?", guildID)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve characters for guild %s: %w", guildID, err)
	}
	defer rows.Close()

	var cards []*CharacterCard
	for rows.Next() {
		var card CharacterCard
		if err := rows.Scan(&card.GuildID, &card.CharacterID, &card.OfficialName, &card.Series, &card.DisplayName, &card.Description, &card.SourceURL, &card.Modifiers, &card.Scenario); err != nil {
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
			active_character_id TEXT,
			image_url TEXT
		);`,
		`CREATE TABLE IF NOT EXISTS character_cards (
			guild_id TEXT,
			character_id TEXT,
			official_name TEXT,
			series TEXT,
			display_name TEXT,
			description TEXT,
			source_url TEXT,
			modifiers TEXT,
			scenario TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (guild_id, character_id)
		);`,
		`CREATE TABLE IF NOT EXISTS character_aliases (
			guild_id TEXT,
			alias TEXT,
			character_id TEXT,
			PRIMARY KEY (guild_id, alias),
			FOREIGN KEY (guild_id, character_id) REFERENCES character_cards(guild_id, character_id)
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
		`CREATE INDEX IF NOT EXISTS idx_history_guild ON chat_history(guild_id);`,
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
	err := m.db.QueryRow("SELECT active_character_id FROM guild_config WHERE guild_id = ?", guildID).Scan(&id)
	if err != nil {
		return ""
	}
	return id
}

// GetHistory retrieves the recent conversation history for a given guild and thread.
func (m *Manager) GetHistory(ctx context.Context, guildID, threadID string) ([]llm.Message, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var messages []llm.Message

	charID := m.getCurrentCharacterID(ctx, guildID)
	rows, err := m.db.Query(`
		SELECT role, content FROM chat_history
		WHERE guild_id = ? AND character_id = ? AND thread_id = ?
		ORDER BY created_at ASC, id ASC
		LIMIT ? OFFSET (SELECT MAX(0, COUNT(*) - ?) FROM (SELECT COUNT(*) as cnt FROM chat_history WHERE guild_id = ? AND character_id = ? AND thread_id = ?))`,
		guildID, charID, threadID, m.maxSize, m.maxSize, guildID, charID, threadID)
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
func (m *Manager) SaveMessage(ctx context.Context, guildID, threadID, role string, content string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	charID := m.getCurrentCharacterID(ctx, guildID)
	_, err := m.db.Exec("INSERT INTO chat_history (guild_id, character_id, thread_id, role, content) VALUES (?, ?, ?, ?, ?)", guildID, charID, threadID, role, content)
	if err != nil {
		return fmt.Errorf("failed to save message for guild %s, thread %s: %w", guildID, threadID, err)
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

// SetCharacterImage updates the profile image URL for the character in a guild.
func (m *Manager) SetCharacterImage(ctx context.Context, guildID string, imageURL string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, err := m.db.Exec("UPDATE guild_config SET image_url = ? WHERE guild_id = ?", imageURL, guildID)
	if err != nil {
		return fmt.Errorf("failed to set character image for guild %s: %w", guildID, err)
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
		SELECT s.active_character_id, c.display_name, c.description
		FROM guild_config s
		JOIN character_cards c ON s.active_character_id = c.character_id
		WHERE s.guild_id = ?`, guildID).Scan(&details.CharacterID, &details.DisplayName, &details.Description)
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
	_, err := m.db.Exec("DELETE FROM chat_history WHERE guild_id = ? AND character_id = ? AND thread_id = ?", guildID, charID, threadID)
	if err != nil {
		return fmt.Errorf("failed to clear history for guild %s, thread %s: %w", guildID, threadID, err)
	}
	return nil
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

// GetOldestMessages retrieves the specified number of oldest messages for a guild and thread.
func (m *Manager) GetOldestMessages(ctx context.Context, guildID, threadID string, count int) ([]llm.Message, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	charID := m.getCurrentCharacterID(ctx, guildID)
	var messages []llm.Message
	rows, err := m.db.Query("SELECT role, content FROM chat_history WHERE guild_id = ? AND character_id = ? AND thread_id = ? ORDER BY created_at ASC LIMIT ?", guildID, charID, threadID, count)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch oldest messages for guild %s, thread %s: %w", guildID, threadID, err)
	}
	defer rows.Close()

	for rows.Next() {
		var msg llm.Message
		if err := rows.Scan(&msg.Role, &msg.Content); err != nil {
			return nil, fmt.Errorf("failed to scan oldest message for guild %s: %w", guildID, err)
		}
		messages = append(messages, msg)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error during oldest messages row iteration for guild %s: %w", guildID, err)
	}

	return messages, nil
}

// PruneAndSummarize removes the oldest messages and replaces them with a summary message.
func (m *Manager) PruneAndSummarize(ctx context.Context, guildID, threadID string, summary string, deletedCount int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	charID := m.getCurrentCharacterID(ctx, guildID)
	var maxID int
	err := m.db.QueryRow("SELECT id FROM chat_history WHERE guild_id = ? AND character_id = ? AND thread_id = ? ORDER BY created_at ASC LIMIT 1 OFFSET ?", guildID, charID, threadID, deletedCount-1).Scan(&maxID)
	if err != nil {
		return fmt.Errorf("failed to find boundary ID for pruning for guild %s, thread %s: %w", guildID, threadID, err)
	}

	_, err = m.db.Exec("DELETE FROM chat_history WHERE guild_id = ? AND character_id = ? AND thread_id = ? AND id <= ?", guildID, charID, threadID, maxID)
	if err != nil {
		return fmt.Errorf("failed to prune history for guild %s, thread %s: %w", guildID, threadID, err)
	}

	_, err = m.db.Exec("INSERT INTO chat_history (guild_id, character_id, thread_id, role, content) VALUES (?, ?, ?, ?, ?)", guildID, charID, threadID, "system", "Summary of previous conversation: "+summary)
	if err != nil {
		return fmt.Errorf("failed to insert summary for guild %s, thread %s: %w", guildID, threadID, err)
	}
	return nil
}

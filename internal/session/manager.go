// Package session manages user-specific character personas and conversation history using SQLite.
package session

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"

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
	CharacterID    string
	OfficialName   string
	DisplayName    string
	Series         string
	Description    string
	ImageURL       string
	ActiveThreadID string
}

// core holds the shared state behind Manager: the database handle, the
// lock guarding it, and the in-memory configuration.
type core struct {
	db                  *sql.DB
	mu                  sync.RWMutex
	defaultSystemPrompt string
	maxSize             int
	imageCandidates     map[string][]string
}

// Manager handles the persistence and retrieval of character data and
// conversation history for Discord guilds. It is a facade over the
// per-domain stores, which share one core; callers use Manager and never
// the stores directly.
type Manager struct {
	*core
	*characterStore
	*guildStore
	*historyStore
	*imageCandidateStore
	*summaryStore
	*threadStore
}

// NewManager initializes a new session manager and creates the necessary database tables.
func NewManager(dbPath string, defaultPrompt string) (*Manager, error) {
	if dir := filepath.Dir(dbPath); dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create db directory: %v", err)
		}
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite db: %v", err)
	}

	c := &core{
		db:                  db,
		defaultSystemPrompt: defaultPrompt,
		maxSize:             30,
		imageCandidates:     make(map[string][]string),
	}
	m := &Manager{
		core:                c,
		characterStore:      &characterStore{c},
		guildStore:          &guildStore{c},
		historyStore:        &historyStore{c},
		imageCandidateStore: &imageCandidateStore{c},
		summaryStore:        &summaryStore{c},
		threadStore:         &threadStore{c},
	}

	if err := m.initDB(); err != nil {
		return nil, err
	}

	return m, nil
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
		`CREATE TABLE IF NOT EXISTS guild_ambient_channels (
			guild_id TEXT,
			channel_id TEXT,
			PRIMARY KEY (guild_id, channel_id)
		);`,
		`CREATE TABLE IF NOT EXISTS character_cards (
			guild_id TEXT,
			character_id TEXT,
			official_name TEXT,
			series TEXT,
			display_name TEXT,
			description TEXT,
			image_url TEXT DEFAULT '',
			active_thread_id TEXT,
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
		`CREATE TABLE IF NOT EXISTS threads (
			guild_id TEXT,
			character_id TEXT,
			thread_id TEXT,
			name TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			last_used_seq INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (guild_id, character_id, thread_id),
			UNIQUE (guild_id, character_id, name)
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

func (c *core) getCurrentCharacterID(ctx context.Context, guildID string) string {
	var id string
	err := c.db.QueryRowContext(ctx, "SELECT active_character_id FROM guild_config WHERE guild_id = ?", guildID).Scan(&id)
	if err != nil {
		return ""
	}
	return id
}

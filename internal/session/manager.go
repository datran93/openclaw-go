package session

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// Message represents a single chat message
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Session represents a conversation thread
type Session struct {
	ID        string            `json:"id"`
	Metadata  map[string]string `json:"metadata"`
	History   []Message         `json:"history"`
	UpdatedAt time.Time         `json:"updated_at"`
	mu        sync.RWMutex      // explicitly protect fields when updating
}

// Clone deeply copies the session to prevent external mutations
func (s *Session) Clone() *Session {
	s.mu.RLock()
	defer s.mu.RUnlock()

	clone := &Session{
		ID:        s.ID,
		Metadata:  make(map[string]string),
		History:   make([]Message, len(s.History)),
		UpdatedAt: s.UpdatedAt,
	}

	for k, v := range s.Metadata {
		clone.Metadata[k] = v
	}
	copy(clone.History, s.History)

	return clone
}

// Manager handles routing and storage for sessions
type Manager struct {
	db    *sql.DB
	cache sync.Map // map[string]*Session
}

// NewManager initializes the SQLite DB and in-memory cache
func NewManager(dbPath string) (*Manager, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite: %w", err)
	}

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			metadata TEXT,
			history TEXT,
			updated_at DATETIME
		);
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to create tables: %w", err)
	}

	m := &Manager{
		db: db,
	}

	if err := m.loadAll(); err != nil {
		return nil, fmt.Errorf("failed to preload sessions: %w", err)
	}

	return m, nil
}

func (m *Manager) loadAll() error {
	rows, err := m.db.Query("SELECT id, metadata, history, updated_at FROM sessions")
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var id, metadataJSON, historyJSON string
		var updatedAt time.Time
		if err := rows.Scan(&id, &metadataJSON, &historyJSON, &updatedAt); err != nil {
			return err
		}

		s := &Session{
			ID:        id,
			Metadata:  make(map[string]string),
			History:   []Message{},
			UpdatedAt: updatedAt,
		}

		_ = json.Unmarshal([]byte(metadataJSON), &s.Metadata)
		_ = json.Unmarshal([]byte(historyJSON), &s.History)

		m.cache.Store(id, s)
	}

	return rows.Err()
}

// Get retrieves a session from the map cache
func (m *Manager) Get(id string) (*Session, bool) {
	val, ok := m.cache.Load(id)
	if !ok {
		return nil, false
	}
	return val.(*Session).Clone(), true
}

// Save upserts a session in both memory and SQLite
func (m *Manager) Save(s *Session) error {
	s.UpdatedAt = time.Now()

	// Update memory cache
	m.cache.Store(s.ID, s)

	// Update SQLite DB
	metadataBytes, _ := json.Marshal(s.Metadata)
	historyBytes, _ := json.Marshal(s.History)

	_, err := m.db.Exec(`
		INSERT INTO sessions (id, metadata, history, updated_at) 
		VALUES (?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			metadata = excluded.metadata,
			history = excluded.history,
			updated_at = excluded.updated_at
	`, s.ID, string(metadataBytes), string(historyBytes), s.UpdatedAt)

	return err
}

// Delete removes a session
func (m *Manager) Delete(id string) error {
	m.cache.Delete(id)

	_, err := m.db.Exec("DELETE FROM sessions WHERE id = ?", id)
	return err
}

// Close closes the underlying DB connection
func (m *Manager) Close() error {
	return m.db.Close()
}

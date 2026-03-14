package session_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/openclaw/openclaw-go/internal/session"
)

func TestSessionManager(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "sessions.db")

	sm, err := session.NewManager(dbPath)
	if err != nil {
		t.Fatalf("failed to init session manager: %v", err)
	}
	defer sm.Close()

	// Test 1: Save
	sess := &session.Session{
		ID:        "chat_123",
		Metadata:  map[string]string{"channel": "telegram"},
		History:   []session.Message{{Role: "user", Content: "Hello"}},
		UpdatedAt: time.Now(),
	}

	if err := sm.Save(sess); err != nil {
		t.Fatalf("failed to save session: %v", err)
	}

	// Test 2: Get
	fetched, found := sm.Get("chat_123")
	if !found {
		t.Fatalf("expected to find session, but it was lost")
	}

	if fetched.Metadata["channel"] != "telegram" {
		t.Errorf("expected metadata channel=telegram, got %v", fetched.Metadata)
	}

	if len(fetched.History) != 1 || fetched.History[0].Content != "Hello" {
		t.Errorf("expected history to contain 1 message 'Hello', got %v", fetched.History)
	}

	// Test 3: Edit and re-save
	fetched.History = append(fetched.History, session.Message{Role: "assistant", Content: "Hi there"})
	if err := sm.Save(fetched); err != nil {
		t.Fatalf("failed to append to session: %v", err)
	}

	// Test 4: Reload from DB (simulate restart)
	if err := sm.Close(); err != nil {
		t.Fatal(err)
	}

	sm2, err := session.NewManager(dbPath)
	if err != nil {
		t.Fatalf("failed to reload manager: %v", err)
	}
	defer sm2.Close()

	reloaded, found := sm2.Get("chat_123")
	if !found {
		t.Fatalf("expected to find session after reload, but it was lost")
	}

	if len(reloaded.History) != 2 {
		t.Errorf("expected history length 2, got %d", len(reloaded.History))
	}

	// Test 5: Delete
	if err := sm2.Delete("chat_123"); err != nil {
		t.Fatalf("failed to delete session: %v", err)
	}

	_, found = sm2.Get("chat_123")
	if found {
		t.Fatal("expected session to be logically deleted")
	}
}

package router_test

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/openclaw/openclaw-go/internal/agent"
	"github.com/openclaw/openclaw-go/internal/channels"
	"github.com/openclaw/openclaw-go/internal/router"
	"github.com/openclaw/openclaw-go/internal/session"
)

// --- Stubs ---

// stubAgent always returns a single-chunk response.
type stubAgent struct{ text string }

func (s *stubAgent) StreamChat(_ context.Context, _ []agent.Message) (<-chan agent.StreamResponse, error) {
	ch := make(chan agent.StreamResponse, 1)
	ch <- agent.StreamResponse{Text: s.text}
	close(ch)
	return ch, nil
}

// mockAdapter records sent messages concurrency-safely using a mutex.
type mockAdapter struct {
	name    string
	trigger channels.IncomingMessage
	mu      sync.Mutex
	sent    []channels.OutgoingMessage
	done    chan struct{}
}

func (m *mockAdapter) Name() string { return m.name }

func (m *mockAdapter) Start(ctx context.Context, in chan<- channels.IncomingMessage) error {
	// Push trigger once.
	select {
	case in <- m.trigger:
	case <-ctx.Done():
		return nil
	}
	// Wait for done signal or cancellation.
	select {
	case <-m.done:
	case <-ctx.Done():
	}
	return nil
}

// Send is called from the Router's goroutine — must be goroutine-safe.
func (m *mockAdapter) Send(_ context.Context, msg channels.OutgoingMessage) error {
	m.mu.Lock()
	m.sent = append(m.sent, msg)
	m.mu.Unlock()
	// Signal done after we receive the final (non-streaming, empty-text) sentinel.
	if !msg.Streaming && msg.Text == "" {
		select {
		case m.done <- struct{}{}:
		default:
		}
	}
	return nil
}

func (m *mockAdapter) Stop() error { return nil }

// messages returns a safe snapshot of sent messages.
func (m *mockAdapter) messages() []channels.OutgoingMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]channels.OutgoingMessage, len(m.sent))
	copy(cp, m.sent)
	return cp
}

// --- Test ---

func TestRouter_RoutesThroughAgent(t *testing.T) {
	tmpDB := filepath.Join(t.TempDir(), "sessions.db")
	mgr, err := session.NewManager(tmpDB)
	if err != nil {
		t.Fatalf("session manager: %v", err)
	}
	defer mgr.Close()

	agnt := &stubAgent{text: "hello world"}

	mock := &mockAdapter{
		name: "test",
		trigger: channels.IncomingMessage{
			ChannelID: "test",
			SessionID: "sess-1",
			UserID:    "user-1",
			Text:      "ping",
		},
		done: make(chan struct{}, 1),
	}

	r := router.New(mgr, agnt, []channels.Adapter{mock}, 16)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() { _ = r.Run(ctx) }()

	// Poll until both the streaming chunk and the final sentinel arrive.
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
		sent := mock.messages()
		hasChunk, hasFinal := false, false
		for _, s := range sent {
			if s.Streaming && s.Text == "hello world" {
				hasChunk = true
			}
			if !s.Streaming && s.Text == "" {
				hasFinal = true
			}
		}
		if hasChunk && hasFinal {
			cancel()
			goto verify
		}
	}
	cancel()
	t.Fatal("timed out waiting for router to complete the round-trip")

verify:
	sent := mock.messages()
	hasChunk := false
	for _, s := range sent {
		if s.Streaming && s.Text == "hello world" {
			hasChunk = true
		}
	}
	if !hasChunk {
		t.Errorf("expected streaming chunk 'hello world', got: %+v", sent)
	}

	// Verify session was persisted.
	sess, ok := mgr.Get("sess-1")
	if !ok {
		t.Fatal("session not persisted after routing")
	}
	if len(sess.History) < 2 {
		t.Fatalf("expected at least 2 history entries, got %d", len(sess.History))
	}
	if sess.History[0].Role != "user" || sess.History[0].Content != "ping" {
		t.Errorf("unexpected user history: %+v", sess.History[0])
	}
	if sess.History[1].Role != "assistant" || sess.History[1].Content != "hello world" {
		t.Errorf("unexpected assistant history: %+v", sess.History[1])
	}
}

// Package router contains the central message dispatcher for OpenClaw.
// It receives IncomingMessages from all channel adapters, manages sessions,
// calls the Agent, and streams responses back via the originating adapter.
package router

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/openclaw/openclaw-go/internal/agent"
	"github.com/openclaw/openclaw-go/internal/channels"
	"github.com/openclaw/openclaw-go/internal/session"
)

// Router dispatches incoming messages through sessions and the agent engine,
// then streams responses back via the appropriate channel adapter.
type Router struct {
	sessions   *session.Manager
	agent      agent.AgentService
	adapters   map[string]channels.Adapter // keyed by Adapter.Name()
	inbound    chan channels.IncomingMessage
	wg         sync.WaitGroup
	sessionMus sync.Map // map[sessionID string]*sync.Mutex — per-session serialisation
}

// New creates a Router. Pass all registered adapters; the Router maps them by name.
func New(
	sessions *session.Manager,
	agentSvc agent.AgentService,
	adapters []channels.Adapter,
	bufferSize int,
) *Router {
	if bufferSize <= 0 {
		bufferSize = 256
	}
	adapterMap := make(map[string]channels.Adapter, len(adapters))
	for _, a := range adapters {
		adapterMap[a.Name()] = a
	}
	return &Router{
		sessions: sessions,
		agent:    agentSvc,
		adapters: adapterMap,
		inbound:  make(chan channels.IncomingMessage, bufferSize),
	}
}

// Inbound returns the shared write-only channel adapters push messages into.
func (r *Router) Inbound() chan<- channels.IncomingMessage {
	return r.inbound
}

// Run starts the Router's dispatch loop. It blocks until ctx is cancelled.
// All registered adapters are started concurrently inside this call.
func (r *Router) Run(ctx context.Context) error {
	// Start all adapters in parallel.
	for _, a := range r.adapters {
		a := a
		r.wg.Add(1)
		go func() {
			defer r.wg.Done()
			if err := a.Start(ctx, r.inbound); err != nil && ctx.Err() == nil {
				slog.Error("adapter stopped with error", "adapter", a.Name(), "error", err)
			}
		}()
	}

	// Dispatch loop.
	for {
		select {
		case <-ctx.Done():
			r.stopAdapters()
			r.wg.Wait()
			return ctx.Err()
		case msg, ok := <-r.inbound:
			if !ok {
				// inbound channel was explicitly closed — stop gracefully.
				r.stopAdapters()
				r.wg.Wait()
				return nil
			}
			r.wg.Add(1)
			go func(m channels.IncomingMessage) {
				defer r.wg.Done()
				r.handle(ctx, m)
			}(msg)
		}
	}
}

// sessionMu returns the per-session mutex, creating it on first access.
// This ensures only one goroutine processes messages for a given session at a time,
// preventing history corruption when two messages arrive concurrently for the same session.
func (r *Router) sessionMu(sessionID string) *sync.Mutex {
	mu, _ := r.sessionMus.LoadOrStore(sessionID, &sync.Mutex{})
	return mu.(*sync.Mutex)
}

// handle processes a single IncomingMessage end-to-end.
func (r *Router) handle(ctx context.Context, msg channels.IncomingMessage) {
	adapter, ok := r.adapters[msg.ChannelID]
	if !ok {
		slog.Warn("router: unknown channel, dropping message", "channel", msg.ChannelID)
		return
	}

	// Serialize access per-session to prevent concurrent history corruption.
	mu := r.sessionMu(msg.SessionID)
	mu.Lock()
	defer mu.Unlock()

	// Get or create session.
	sess, exists := r.sessions.Get(msg.SessionID)
	if !exists {
		sess = &session.Session{
			ID:       msg.SessionID,
			Metadata: map[string]string{"channel": msg.ChannelID, "user": msg.UserID},
			History:  []session.Message{},
		}
	}

	// Append user message.
	sess.History = append(sess.History, session.Message{
		Role:    "user",
		Content: msg.Text,
	})

	// Build agent messages from history.
	agentMsgs := make([]agent.Message, len(sess.History))
	for i, h := range sess.History {
		agentMsgs[i] = agent.Message{Role: h.Role, Content: h.Content}
	}

	// Stream response from agent.
	stream, err := r.agent.StreamChat(ctx, agentMsgs)
	if err != nil {
		slog.Error("router: agent stream error", "session", msg.SessionID, "error", err)
		_ = adapter.Send(ctx, channels.OutgoingMessage{
			SessionID: msg.SessionID,
			Text:      fmt.Sprintf("[error: %v]", err),
		})
		return
	}

	var fullResponse strings.Builder
	for chunk := range stream {
		if chunk.Error != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Error("router: stream chunk error", "error", chunk.Error)
			break
		}
		fullResponse.WriteString(chunk.Text)
		_ = adapter.Send(ctx, channels.OutgoingMessage{
			SessionID: msg.SessionID,
			Text:      chunk.Text,
			Streaming: true,
		})
	}

	// Append assistant message to history and persist.
	sess.History = append(sess.History, session.Message{
		Role:    "assistant",
		Content: fullResponse.String(),
	})
	if err := r.sessions.Save(sess); err != nil {
		slog.Error("router: failed to persist session", "session", msg.SessionID, "error", err)
	}

	// Signal stream end.
	_ = adapter.Send(ctx, channels.OutgoingMessage{
		SessionID: msg.SessionID,
		Streaming: false,
	})
}

// stopAdapters stops all registered adapters.
func (r *Router) stopAdapters() {
	for _, a := range r.adapters {
		if err := a.Stop(); err != nil {
			slog.Warn("adapter stop error", "adapter", a.Name(), "error", err)
		}
	}
}

// Package webchat provides a WebSocket channel adapter that bridges the
// Gateway's WebSocket server with the central Router.
package webchat

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

	"github.com/openclaw/openclaw-go/internal/channels"
	"github.com/openclaw/openclaw-go/internal/gateway"
)

const adapterName = "webchat"

// sendMsg is the JSON shape written to the WebSocket for each outgoing chunk.
type sendMsg struct {
	SessionID string `json:"session_id"`
	Text      string `json:"text"`
	Streaming bool   `json:"streaming"`
}

// Adapter implements channels.Adapter by bridging gateway.Server WebSocket
// connections into the Router's inbound message channel.
//
// Flow:
//
//	Browser  →  WS frame  →  gateway.Server.OnMessage  →  inbound chan  →  Router
//	Router   →  Adapter.Send  →  gateway.Server.Broadcast  →  Browser
type Adapter struct {
	gw      *gateway.Server
	inbound chan<- channels.IncomingMessage

	// sessions tracks which session IDs are active so we can associate
	// outgoing messages to the correct WebSocket connection.
	// For a single-user assistant, Broadcast to all clients is acceptable.
	mu       sync.RWMutex
	sessions map[string]struct{} // set of known session IDs
}

// New creates a WebChat Adapter backed by the provided gateway.Server.
func New(gw *gateway.Server) *Adapter {
	return &Adapter{
		gw:       gw,
		sessions: make(map[string]struct{}),
	}
}

// Name returns the adapter identifier.
func (a *Adapter) Name() string { return adapterName }

// Start wires the gateway's OnMessage callback to forward inbound WebSocket
// messages as IncomingMessages into the Router. It blocks until ctx is cancelled.
func (a *Adapter) Start(ctx context.Context, in chan<- channels.IncomingMessage) error {
	a.inbound = in

	// Register the message handler on the gateway.
	a.gw.OnMessage = func(sessionID, text string) {
		a.mu.Lock()
		a.sessions[sessionID] = struct{}{}
		a.mu.Unlock()

		msg := channels.IncomingMessage{
			ChannelID: adapterName,
			SessionID: sessionID,
			UserID:    sessionID, // for single-user setup, connID doubles as userID
			Text:      text,
		}
		select {
		case in <- msg:
		case <-ctx.Done():
		}
	}

	slog.Info("webchat adapter ready — listening for WebSocket messages")

	// Block until context is cancelled.
	<-ctx.Done()

	// Deregister the handler on shutdown.
	a.gw.OnMessage = nil
	return nil
}

// Send delivers an outgoing message back to all connected WebSocket clients.
// Because gorilla/websocket Broadcast reaches every connection, this works
// correctly for single-user setups. Multi-user setups would require per-conn routing.
func (a *Adapter) Send(_ context.Context, msg channels.OutgoingMessage) error {
	payload := sendMsg{
		SessionID: msg.SessionID,
		Text:      msg.Text,
		Streaming: msg.Streaming,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("webchat: marshal error: %w", err)
	}
	a.gw.Broadcast("message", json.RawMessage(raw))
	return nil
}

// Stop is a no-op; the gateway lifecycle is managed externally.
func (a *Adapter) Stop() error { return nil }

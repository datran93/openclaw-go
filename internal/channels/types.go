// Package channels defines the contracts shared by all channel adapters.
// An Adapter bridges an external messaging platform (Telegram, WebSocket,
// stdin/stdout, etc.) with the central Router.
package channels

import "context"

// IncomingMessage is the normalised representation of a user message
// arriving from any channel adapter.
type IncomingMessage struct {
	// ChannelID identifies the source adapter (e.g. "telegram", "webchat", "cli").
	ChannelID string
	// SessionID is a stable conversation key (e.g. Telegram chat ID, WS conn ID).
	SessionID string
	// UserID is an optional identifier for the originating user.
	UserID string
	// Text is the raw user input.
	Text string
	// Metadata carries adapter-specific extra fields (e.g. username, reply-to).
	Metadata map[string]string
}

// OutgoingMessage is the normalised response the Router sends back to an adapter.
type OutgoingMessage struct {
	// SessionID must match the IncomingMessage that triggered this response.
	SessionID string
	// Text is the content to deliver to the user.
	Text string
	// Streaming indicates this is a partial chunk (true) or the final message (false).
	Streaming bool
}

// Adapter is the interface that every channel must implement.
// Adapters run as background goroutines and communicate with the Router
// exclusively through the shared inbound channel.
type Adapter interface {
	// Name returns the unique identifier for this adapter (e.g. "telegram").
	Name() string

	// Start launches the adapter's receive loop.
	// Every message received from the external platform must be sent to `in`.
	// Start must return when ctx is cancelled.
	Start(ctx context.Context, in chan<- IncomingMessage) error

	// Send delivers an outgoing message to the external platform.
	// Implementations should be non-blocking where possible.
	Send(ctx context.Context, msg OutgoingMessage) error

	// Stop gracefully shuts down the adapter and releases resources.
	Stop() error
}

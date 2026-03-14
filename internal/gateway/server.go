package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// writeBufSize is the number of outgoing frames to buffer per connection.
	// If the client is slow and the buffer fills, new frames are dropped.
	writeBufSize = 64
	writeTimeout = 10 * time.Second
	pingInterval = 30 * time.Second
	pongTimeout  = 60 * time.Second
)

// client wraps a WebSocket connection with its own serialised write channel.
// gorilla/websocket requires that at most ONE goroutine writes at a time.
// The write pump goroutine is the only writer; Broadcast enqueues to send.
type client struct {
	conn *websocket.Conn
	send chan []byte // buffered outgoing frames
}

// Server represents the API and WebSocket gateway
type Server struct {
	addr     string
	upgrader websocket.Upgrader
	srv      *http.Server

	clientsMu sync.RWMutex
	clients   map[*websocket.Conn]*client // conn → client (for Broadcast lookup)

	// OnMessage is an optional callback invoked for every inbound WS text frame.
	// connID is a stable string key for the connection (used as session ID).
	OnMessage func(connID, text string)
}

// WSMessage represents a standard envelope for WebSocket messages
type WSMessage struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// WSClientMessage is the JSON payload a browser client sends to the gateway.
type WSClientMessage struct {
	SessionID string `json:"session_id"` // empty → server assigns from conn identity
	Text      string `json:"text"`
}

// NewServer initializes a new Gateway server
func NewServer(port int, bind string) (*Server, error) {
	if port <= 0 || port > 65535 {
		return nil, fmt.Errorf("invalid port: %d", port)
	}

	addr := fmt.Sprintf("%s:%d", bind, port)

	return &Server{
		addr: addr,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				// Allow all origins for the personal assistant use case
				return true
			},
		},
		clients: make(map[*websocket.Conn]*client),
	}, nil
}

// Start launches the HTTP server
func (s *Server) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/ws", s.handleWebSocket)

	s.srv = &http.Server{
		Addr:    s.addr,
		Handler: mux,
	}

	slog.Info("Starting Gateway Server", "addr", s.addr)
	return s.srv.ListenAndServe()
}

// Stop gracefully shuts down the server and active connections
func (s *Server) Stop(ctx context.Context) error {
	slog.Info("Stopping Gateway Server")

	// Close all active websocket clients — closing send channel signals writePump to exit.
	s.clientsMu.Lock()
	for _, c := range s.clients {
		close(c.send)
	}
	s.clients = make(map[*websocket.Conn]*client)
	s.clientsMu.Unlock()

	if s.srv != nil {
		return s.srv.Shutdown(ctx)
	}
	return nil
}

// Broadcast enqueues a message to every connected client's write channel.
// It is safe to call from multiple goroutines concurrently.
func (s *Server) Broadcast(msgType string, payload interface{}) {
	data, err := json.Marshal(payload)
	if err != nil {
		slog.Error("failed to marshal broadcast payload", "error", err)
		return
	}

	wsMsg := WSMessage{Type: msgType, Payload: data}
	encoded, err := json.Marshal(wsMsg)
	if err != nil {
		slog.Error("failed to marshal broadcast envelope", "error", err)
		return
	}

	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()

	for _, c := range s.clients {
		select {
		case c.send <- encoded: // enqueue — never blocks the caller
		default:
			// Buffer full: drop this frame for this slow client.
			slog.Debug("websocket send buffer full, dropping frame")
		}
	}
}

// REST: Health check endpoint
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

// handleWebSocket upgrades the connection and starts the read + write pumps.
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("websocket upgrade failed", "error", err)
		return
	}

	c := &client{
		conn: conn,
		send: make(chan []byte, writeBufSize),
	}

	s.addClient(conn, c)
	defer s.removeClient(conn)

	// Start the write pump — the ONLY goroutine that calls conn.WriteMessage.
	go s.writePump(c)

	// Read pump runs in the current goroutine (handleWebSocket's goroutine).
	s.readPump(c)
}

// writePump serialises all outgoing writes for a single connection.
// It exits when the send channel is closed or a write error occurs.
func (s *Server) writePump(c *client) {
	ticker := time.NewTicker(pingInterval)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case msg, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeTimeout))
			if !ok {
				// Channel closed — send close frame and exit.
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				slog.Debug("websocket write error", "error", err)
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeTimeout))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// readPump reads inbound frames and invokes OnMessage. Exits on error / close.
func (s *Server) readPump(c *client) {
	defer c.conn.Close()

	c.conn.SetReadDeadline(time.Now().Add(pongTimeout))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongTimeout))
		return nil
	})

	connID := c.conn.RemoteAddr().String()

	for {
		msgType, raw, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				slog.Debug("websocket read error", "error", err)
			}
			return
		}
		if msgType != websocket.TextMessage {
			continue
		}
		if s.OnMessage == nil {
			continue
		}
		// Parse as WSClientMessage; fall back to treating raw as plain text.
		var cm WSClientMessage
		if err := json.Unmarshal(raw, &cm); err == nil && cm.Text != "" {
			sessID := cm.SessionID
			if sessID == "" {
				sessID = connID
			}
			s.OnMessage(sessID, cm.Text)
		} else {
			s.OnMessage(connID, string(raw))
		}
	}
}

func (s *Server) addClient(conn *websocket.Conn, c *client) {
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()
	s.clients[conn] = c
	slog.Debug("websocket client connected", "clients", len(s.clients))
}

func (s *Server) removeClient(conn *websocket.Conn) {
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()
	if c, ok := s.clients[conn]; ok {
		// Close send channel to signal writePump to exit cleanly.
		// Guard against double-close (Stop() may have already closed it).
		select {
		case _, stillOpen := <-c.send:
			if stillOpen {
				close(c.send)
			}
		default:
			// Channel is empty but still open — safe to close.
			close(c.send)
		}
		delete(s.clients, conn)
	}
	slog.Debug("websocket client disconnected", "clients", len(s.clients))
}

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

// Server represents the API and WebSocket gateway
type Server struct {
	addr     string
	upgrader websocket.Upgrader
	srv      *http.Server

	clientsMu sync.RWMutex
	clients   map[*websocket.Conn]struct{}
}

// WSMessage represents a standard envelope for WebSocket messages
type WSMessage struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
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
		clients: make(map[*websocket.Conn]struct{}),
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

	// Close all active websocket clients
	s.clientsMu.Lock()
	for conn := range s.clients {
		_ = conn.Close()
	}
	s.clients = make(map[*websocket.Conn]struct{})
	s.clientsMu.Unlock()

	if s.srv != nil {
		return s.srv.Shutdown(ctx)
	}
	return nil
}

// Broadcast sends a message to all connected websocket clients
func (s *Server) Broadcast(msgType string, payload interface{}) {
	data, err := json.Marshal(payload)
	if err != nil {
		slog.Error("failed to marshal broadcast payload", "error", err)
		return
	}

	wsMsg := WSMessage{
		Type:    msgType,
		Payload: data,
	}

	encoded, err := json.Marshal(wsMsg)
	if err != nil {
		slog.Error("failed to marshal broadcast envelope", "error", err)
		return
	}

	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()

	for conn := range s.clients {
		go func(c *websocket.Conn) {
			if err := c.WriteMessage(websocket.TextMessage, encoded); err != nil {
				slog.Debug("failed to write to websocket client", "error", err)
			}
		}(conn)
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

// WebSocket: Connection handler
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("websocket upgrade failed", "error", err)
		return
	}

	s.addClient(conn)
	defer s.removeClient(conn)

	// Keep-alive setup
	conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				slog.Debug("websocket error", "error", err)
			}
			break
		}
		// TODO: Parse inbound messages and Route to Channels / Agents
	}
}

func (s *Server) addClient(conn *websocket.Conn) {
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()
	s.clients[conn] = struct{}{}
	slog.Debug("websocket client connected", "clients", len(s.clients))
}

func (s *Server) removeClient(conn *websocket.Conn) {
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()
	delete(s.clients, conn)
	_ = conn.Close()
	slog.Debug("websocket client disconnected", "clients", len(s.clients))
}

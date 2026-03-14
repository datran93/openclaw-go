package gateway_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/openclaw/openclaw-go/internal/gateway"
)

func TestNewServerValidation(t *testing.T) {
	_, err := gateway.NewServer(0, "localhost")
	if err == nil {
		t.Fatal("expected error for invalid port 0")
	}

	_, err = gateway.NewServer(70000, "localhost")
	if err == nil {
		t.Fatal("expected error for invalid port 70000")
	}

	srv, err := gateway.NewServer(8080, "localhost")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if srv == nil {
		t.Fatal("expected server instance")
	}
}

func TestHealthCheck(t *testing.T) {
	srv, _ := gateway.NewServer(8080, "localhost")

	// We can't directly hit srv.srv handlers easily since it's unexported inside Start()
	// So we start it in background for a real e2e check, but picking a random port
	srv, _ = gateway.NewServer(0, "127.0.0.1") // Won't start with 0 port by standard Start(), so let's mock the handler directly instead
	// Wait, NewServer errors on port 0. Let's pick a high valid port
	srv, _ = gateway.NewServer(18789, "127.0.0.1")

	// Better approach for unit testing the HTTP endpoints without locking ports: we'll create a local httptest server with a custom handler.
	// But `handleHealth` is private. Instead, let's just start and stop.

	go func() {
		_ = srv.Start()
	}()
	time.Sleep(100 * time.Millisecond) // Give it time to bind

	resp, err := http.Get("http://127.0.0.1:18789/api/health")
	if err != nil {
		t.Fatalf("failed to GET health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %v", resp.StatusCode)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = srv.Stop(ctx)
}

func TestWebSocketAndBroadcast(t *testing.T) {
	srv, err := gateway.NewServer(18790, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}

	go func() {
		_ = srv.Start()
	}()
	time.Sleep(100 * time.Millisecond) // Give it time to bind

	// Connect websocket client
	wsURL := "ws://127.0.0.1:18790/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect to websocket: %v", err)
	}
	defer conn.Close()

	// Wait for connection to register internally
	time.Sleep(50 * time.Millisecond)

	// Test broadcast
	payload := map[string]string{"msg": "hello"}
	srv.Broadcast("test_event", payload)

	// Read from client
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	mt, p, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read from ws: %v", err)
	}
	if mt != websocket.TextMessage {
		t.Fatalf("expected text message, got %d", mt)
	}

	var msg gateway.WSMessage
	if err := json.Unmarshal(p, &msg); err != nil {
		t.Fatalf("failed to parse message envelope: %v", err)
	}

	if msg.Type != "test_event" {
		t.Errorf("expected type test_event, got %s", msg.Type)
	}

	if !strings.Contains(string(msg.Payload), `"msg":"hello"`) {
		t.Errorf("expected payload to contain msg:hello, got %s", string(msg.Payload))
	}

	// Test graceful stop
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = srv.Stop(ctx)

	// Connection should be closed
	_, _, err = conn.ReadMessage()
	if err == nil {
		t.Fatal("expected error reading from closed connection, got nil")
	}
}

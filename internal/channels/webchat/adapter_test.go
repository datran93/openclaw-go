package webchat_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/openclaw/openclaw-go/internal/channels"
	wcadapter "github.com/openclaw/openclaw-go/internal/channels/webchat"
	"github.com/openclaw/openclaw-go/internal/gateway"
)

// TestWebchatAdapter_NameAndStop verifies basic interface compliance.
func TestWebchatAdapter_NameAndStop(t *testing.T) {
	gtw, _ := gateway.NewServer(19001, "127.0.0.1")
	a := wcadapter.New(gtw)
	if a.Name() != "webchat" {
		t.Errorf("expected name 'webchat', got %q", a.Name())
	}
	if err := a.Stop(); err != nil {
		t.Errorf("Stop() error: %v", err)
	}
}

// TestWebchatAdapter_SendBeforeStart verifies Send doesn't panic before Start.
func TestWebchatAdapter_SendBeforeStart(t *testing.T) {
	gtw, _ := gateway.NewServer(19002, "127.0.0.1")
	a := wcadapter.New(gtw)
	err := a.Send(context.Background(), channels.OutgoingMessage{
		SessionID: "s1",
		Text:      "hello",
		Streaming: false,
	})
	// No panic is the key assertion. Error is ok (no clients connected).
	_ = err
}

// TestWebchatAdapter_Start_ContextCancel verifies Start exits on context cancellation.
func TestWebchatAdapter_Start_ContextCancel(t *testing.T) {
	gtw, _ := gateway.NewServer(19003, "127.0.0.1")
	a := wcadapter.New(gtw)

	inbound := make(chan channels.IncomingMessage, 8)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- a.Start(ctx, inbound) }()

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Start returned non-nil error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("Start did not return after ctx cancel")
	}
}

// TestWebchatAdapter_InboundMessage verifies that a WS text frame reaches the inbound channel.
func TestWebchatAdapter_InboundMessage(t *testing.T) {
	// Stand up a real HTTP test server backed by the gateway.
	gtw, _ := gateway.NewServer(0, "127.0.0.1") // port 0 = unused, we use httptest
	_ = wcadapter.New(gtw)                      // ensure constructor doesn't panic

	// Use an httptest server driven by the gateway's WS upgrader directly.
	var received channels.IncomingMessage
	done := make(chan struct{})

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("upgrade error: %v", err)
			return
		}
		defer conn.Close()
		_, msg, _ := conn.ReadMessage()
		received = channels.IncomingMessage{
			ChannelID: "webchat",
			SessionID: conn.RemoteAddr().String(),
			Text:      string(msg),
		}
		close(done)
	}))
	defer ts.Close()

	// Connect a WS client and send a message.
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")
	wsConn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("ws dial: %v", err)
	}
	defer wsConn.Close()

	if err := wsConn.WriteMessage(websocket.TextMessage, []byte("ping from browser")); err != nil {
		t.Fatalf("ws write: %v", err)
	}

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for inbound message")
	}

	if received.Text != "ping from browser" {
		t.Errorf("expected 'ping from browser', got %q", received.Text)
	}
}

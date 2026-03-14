package agent_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/openclaw/openclaw-go/internal/agent"
)

func TestNewAgentValidation(t *testing.T) {
	_, err := agent.NewAgent(agent.ProviderOpenAI, "gpt-4", "", "")
	if err == nil {
		t.Fatal("expected error for empty api key")
	}

	_, err = agent.NewAgent("unknown", "model", "key", "")
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}

	oai, err := agent.NewAgent(agent.ProviderOpenAI, "gpt-4", "test", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if oai == nil {
		t.Fatal("expected openai agent, got nil")
	}

	ant, err := agent.NewAgent(agent.ProviderAnthropic, "claude-3-opus", "test", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ant == nil {
		t.Fatal("expected anthropic agent, got nil")
	}
}

// TestOpenAIStream tests the parsing of text streaming from an OpenAI compatible endpoint
func TestOpenAIStream(t *testing.T) {
	// Mock OpenAI SSE server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")

		// Send mock delta
		msg := `{"choices": [{"delta": {"content": "Hello "}}]}`
		w.Write([]byte("data: " + msg + "\n\n"))

		msg2 := `{"choices": [{"delta": {"content": "world"}}], "finish_reason": "stop"}`
		w.Write([]byte("data: " + msg2 + "\n\n"))

		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer ts.Close()

	oai, _ := agent.NewAgent(agent.ProviderOpenAI, "gpt-4", "test-key", ts.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	msgs := []agent.Message{
		{Role: "user", Content: "Hi"},
	}

	ch, err := oai.StreamChat(ctx, msgs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var output []string
	for resp := range ch {
		if resp.Error != nil {
			t.Fatalf("stream error: %v", resp.Error)
		}
		output = append(output, resp.Text)
	}

	full := strings.Join(output, "")
	if full != "Hello world" {
		t.Errorf("expected 'Hello world', got '%s'", full)
	}
}

// TestAnthropicStream tests the parsing of text streaming from an Anthropic endpoint
func TestAnthropicStream(t *testing.T) {
	// Mock Anthropic SSE server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")

		// Send mock delta
		msg := `{"type": "content_block_delta", "delta": {"type": "text_delta", "text": "Hi "}}`
		w.Write([]byte("event: content_block_delta\ndata: " + msg + "\n\n"))

		msg2 := `{"type": "content_block_delta", "delta": {"type": "text_delta", "text": "there"}}`
		w.Write([]byte("event: content_block_delta\ndata: " + msg2 + "\n\n"))

		// End stream
		w.Write([]byte("event: message_stop\ndata: {\"type\": \"message_stop\"}\n\n"))
	}))
	defer ts.Close()

	ant, _ := agent.NewAgent(agent.ProviderAnthropic, "claude-3-opus", "test-key", ts.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	msgs := []agent.Message{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "Hi"},
	}

	ch, err := ant.StreamChat(ctx, msgs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var output []string
	for resp := range ch {
		if resp.Error != nil {
			t.Fatalf("stream error: %v", resp.Error)
		}
		output = append(output, resp.Text)
	}

	full := strings.Join(output, "")
	if full != "Hi there" {
		t.Errorf("expected 'Hi there', got '%s'", full)
	}
}

func TestContextCancellationPropagates(t *testing.T) {
	oai, _ := agent.NewAgent(agent.ProviderOpenAI, "gpt-4", "test", "")

	// Create an already canceled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ch, err := oai.StreamChat(ctx, []agent.Message{{Role: "user", Content: "Fail"}})
	if err != nil {
		return // It's okay if it errors immediately on setup
	}

	for resp := range ch {
		if resp.Error != nil {
			break
		}
	}
}

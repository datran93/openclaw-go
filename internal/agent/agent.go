package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/liushuangls/go-anthropic/v2"
	"github.com/sashabaranov/go-openai"
)

type Message struct {
	Role    string
	Content string
}

// StreamResponse represents a chunk of text or an error stream
type StreamResponse struct {
	Text  string
	Error error
}

type AgentService interface {
	StreamChat(ctx context.Context, msgs []Message) (<-chan StreamResponse, error)
}

type Provider string

const (
	ProviderOpenAI    Provider = "openai"
	ProviderAnthropic Provider = "anthropic"

	// OpenAI-compatible providers — reuse OpenAIAgent with a custom base_url.
	ProviderOpenRouter Provider = "openrouter"
	ProviderOllama     Provider = "ollama"
	ProviderGroq       Provider = "groq"
)

// isOpenAICompatible returns true for providers that speak the OpenAI Chat Completions API.
func isOpenAICompatible(p Provider) bool {
	switch p {
	case ProviderOpenAI, ProviderOpenRouter, ProviderOllama, ProviderGroq:
		return true
	}
	return false
}

// NewAgent factory
func NewAgent(provider Provider, model string, apiKey string, baseURL string) (AgentService, error) {
	if apiKey == "" && baseURL == "" {
		return nil, errors.New("api key is required")
	}

	switch {
	case isOpenAICompatible(provider):
		config := openai.DefaultConfig(apiKey)
		if baseURL != "" {
			config.BaseURL = baseURL
		}
		return &OpenAIAgent{
			client: openai.NewClientWithConfig(config),
			model:  model,
		}, nil
	case provider == ProviderAnthropic:
		opts := []anthropic.ClientOption{}
		if baseURL != "" {
			opts = append(opts, anthropic.WithBaseURL(baseURL))
		}
		return &AnthropicAgent{
			client: anthropic.NewClient(apiKey, opts...),
			model:  model,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported provider: %q (supported: openai, anthropic, openrouter, ollama, groq)", provider)
	}
}

// OpenAIAgent implementation
type OpenAIAgent struct {
	client *openai.Client
	model  string
}

func (a *OpenAIAgent) StreamChat(ctx context.Context, msgs []Message) (<-chan StreamResponse, error) {
	oaiMsgs := make([]openai.ChatCompletionMessage, len(msgs))
	for i, m := range msgs {
		role := m.Role
		if role == "" {
			role = openai.ChatMessageRoleUser
		}
		oaiMsgs[i] = openai.ChatCompletionMessage{
			Role:    role,
			Content: m.Content,
		}
	}

	req := openai.ChatCompletionRequest{
		Model:    a.model,
		Messages: oaiMsgs,
		Stream:   true,
	}

	stream, err := a.client.CreateChatCompletionStream(ctx, req)
	if err != nil {
		return nil, err
	}

	ch := make(chan StreamResponse)

	go func() {
		defer close(ch)
		defer stream.Close()

		for {
			select {
			case <-ctx.Done():
				ch <- StreamResponse{Error: ctx.Err()}
				return
			default:
				resp, err := stream.Recv()
				if errors.Is(err, io.EOF) {
					return
				}
				if err != nil {
					ch <- StreamResponse{Error: err}
					return
				}

				if len(resp.Choices) > 0 {
					ch <- StreamResponse{Text: resp.Choices[0].Delta.Content}
				}
			}
		}
	}()

	return ch, nil
}

// AnthropicAgent implementation
type AnthropicAgent struct {
	client *anthropic.Client
	model  string
}

func (a *AnthropicAgent) StreamChat(ctx context.Context, msgs []Message) (<-chan StreamResponse, error) {
	var antMsgs []anthropic.Message
	var systemPrompt string

	for _, m := range msgs {
		if m.Role == "system" {
			systemPrompt += m.Content + "\n"
			continue
		}

		antRole := anthropic.RoleUser
		if m.Role == "assistant" {
			antRole = anthropic.RoleAssistant
		}

		antMsgs = append(antMsgs, anthropic.Message{
			Role: antRole,
			Content: []anthropic.MessageContent{
				anthropic.NewTextMessageContent(m.Content),
			},
		})
	}

	req := anthropic.MessagesStreamRequest{
		MessagesRequest: anthropic.MessagesRequest{
			Model:     anthropic.Model(a.model),
			Messages:  antMsgs,
			MaxTokens: 4096,
		},
	}

	if systemPrompt != "" {
		req.System = strings.TrimSpace(systemPrompt)
	}

	ch := make(chan StreamResponse)

	go func() {
		defer close(ch)

		req.OnContentBlockDelta = func(data anthropic.MessagesEventContentBlockDeltaData) {
			if data.Delta.Text != nil {
				ch <- StreamResponse{Text: *data.Delta.Text}
			}
		}

		_, err := a.client.CreateMessagesStream(ctx, req)
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				ch <- StreamResponse{Error: err}
			}
		}
	}()

	return ch, nil
}

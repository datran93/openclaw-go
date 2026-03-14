// Package telegram provides a Telegram Bot API channel adapter for OpenClaw.
// It uses long-polling to receive updates and sends responses via the Bot API.
package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/openclaw/openclaw-go/internal/channels"
)

const adapterName = "telegram"

// Adapter implements channels.Adapter for Telegram using long-polling.
type Adapter struct {
	bot     *tgbotapi.BotAPI
	partial map[string]string // sessionID → accumulated streaming text
}

// New creates a Telegram Adapter using the provided bot token.
// Returns an error if the token is invalid or the Bot API is unreachable.
func New(token string) (*Adapter, error) {
	if token == "" {
		return nil, fmt.Errorf("telegram: bot token is required")
	}
	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, fmt.Errorf("telegram: failed to connect to Bot API: %w", err)
	}
	slog.Info("telegram bot authenticated", "username", bot.Self.UserName)
	return &Adapter{
		bot:     bot,
		partial: make(map[string]string),
	}, nil
}

// Name returns the adapter identifier.
func (a *Adapter) Name() string { return adapterName }

// Start begins long-polling for updates and forwards each text message to the
// Router via the inbound channel. It returns when ctx is cancelled.
func (a *Adapter) Start(ctx context.Context, in chan<- channels.IncomingMessage) error {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 30 // seconds per long-poll cycle

	updates := a.bot.GetUpdatesChan(u)
	slog.Info("telegram adapter started, listening for updates")

	for {
		select {
		case <-ctx.Done():
			a.bot.StopReceivingUpdates()
			return nil
		case update, ok := <-updates:
			if !ok {
				return nil
			}
			if update.Message == nil || update.Message.Text == "" {
				continue
			}
			msg := update.Message
			sessionID := strconv.FormatInt(msg.Chat.ID, 10)
			userID := strconv.FormatInt(msg.From.ID, 10)

			select {
			case in <- channels.IncomingMessage{
				ChannelID: adapterName,
				SessionID: sessionID,
				UserID:    userID,
				Text:      msg.Text,
				Metadata: map[string]string{
					"chat_id":    sessionID,
					"username":   msg.From.UserName,
					"first_name": msg.From.FirstName,
				},
			}:
			case <-ctx.Done():
				a.bot.StopReceivingUpdates()
				return nil
			}
		}
	}
}

// Send delivers a response back to the Telegram chat.
// Streaming chunks are accumulated locally; the final (Streaming=false) message
// is sent as a single Telegram message to avoid flooding the chat with partials.
func (a *Adapter) Send(ctx context.Context, msg channels.OutgoingMessage) error {
	if msg.Streaming {
		// Accumulate chunks — Telegram doesn't support true streaming.
		a.partial[msg.SessionID] += msg.Text
		return nil
	}

	// Final message: send accumulated text (or fallback to msg.Text).
	text := a.partial[msg.SessionID]
	delete(a.partial, msg.SessionID)
	if text == "" {
		return nil // nothing to send
	}

	chatID, err := strconv.ParseInt(msg.SessionID, 10, 64)
	if err != nil {
		return fmt.Errorf("telegram: invalid session ID %q: %w", msg.SessionID, err)
	}

	reply := tgbotapi.NewMessage(chatID, text)
	reply.ParseMode = tgbotapi.ModeMarkdown

	if _, err := a.bot.Send(reply); err != nil {
		return fmt.Errorf("telegram: send error: %w", err)
	}
	return nil
}

// Stop gracefully halts the long-poll loop.
func (a *Adapter) Stop() error {
	a.bot.StopReceivingUpdates()
	return nil
}

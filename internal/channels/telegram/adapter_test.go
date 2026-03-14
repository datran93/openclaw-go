package telegram_test

import (
	"testing"

	tgadapter "github.com/openclaw/openclaw-go/internal/channels/telegram"
)

// TestNew_EmptyToken verifies that an empty token returns an error, not a panic.
func TestNew_EmptyToken(t *testing.T) {
	_, err := tgadapter.New("")
	if err == nil {
		t.Error("expected error for empty token, got nil")
	}
}

// TestNew_InvalidToken verifies that a syntactically invalid token returns an error.
// This does not make a real network call — tgbotapi validates the token format
// before attempting auth.
func TestNew_InvalidToken(t *testing.T) {
	_, err := tgadapter.New("NOT_A_VALID_TOKEN_FORMAT")
	if err == nil {
		t.Error("expected error for invalid token format, got nil")
	}
}

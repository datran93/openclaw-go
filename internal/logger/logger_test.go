package logger_test

import (
	"log/slog"
	"testing"

	"github.com/openclaw/openclaw-go/internal/logger"
)

func TestLoggerInit(t *testing.T) {
	// Initialize logger with different levels to ensure no panics
	levels := []string{"debug", "info", "warn", "error", "unknown"}
	for _, level := range levels {
		logger.Init(level)
		// Try to log something to ensure the default logger is set and works
		slog.Info("test log message", "level_tested", level)
	}
}

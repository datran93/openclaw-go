package logger

import (
	"log/slog"
	"os"
	"strings"
)

// Init initializes the default slog logger with the specified log level.
func Init(level string) {
	var l slog.Level
	switch strings.ToLower(level) {
	case "debug":
		l = slog.LevelDebug
	case "info":
		l = slog.LevelInfo
	case "warn":
		l = slog.LevelWarn
	case "error":
		l = slog.LevelError
	default:
		l = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{
		Level: l,
	}

	// Use structured JSON logging
	handler := slog.NewJSONHandler(os.Stdout, opts)
	logger := slog.New(handler)

	// Set default logger
	slog.SetDefault(logger)
}

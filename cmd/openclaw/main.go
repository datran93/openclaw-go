package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/openclaw/openclaw-go/internal/config"
	"github.com/openclaw/openclaw-go/internal/gateway"
	"github.com/openclaw/openclaw-go/internal/logger"
	"github.com/openclaw/openclaw-go/internal/session"
)

const defaultConfigPath = "openclaw.yaml"

func main() {
	// ── CLI flags ──────────────────────────────────────────────────────────
	var configPath string
	var port int
	var logLevel string
	var dbPath string

	flag.StringVar(&configPath, "config", defaultConfigPath, "path to config file (default: openclaw.yaml)")
	flag.IntVar(&port, "port", 0, "override gateway port (0 = use config value)")
	flag.StringVar(&logLevel, "log", "", "override log level: debug|info|warn|error")
	flag.StringVar(&dbPath, "db", "openclaw.db", "path to SQLite session database")
	flag.Parse()

	// ── Config resolution (priority: explicit flag > default file > built-in defaults) ──
	cfg := loadConfig(configPath, configPath != defaultConfigPath)

	// CLI flag overrides always win.
	if port != 0 {
		cfg.Gateway.Port = port
	}
	if logLevel != "" {
		cfg.Logger.Level = logLevel
	}

	// ── Logger (must init before further logging) ──────────────────────────
	logger.Init(cfg.Logger.Level)

	// ── Session manager ────────────────────────────────────────────────────
	sm, err := session.NewManager(dbPath)
	if err != nil {
		slog.Error("failed to initialize session manager", "error", err)
		os.Exit(1)
	}
	defer sm.Close()

	slog.Info("🚀 OpenClaw Go started",
		"port", cfg.Gateway.Port,
		"provider", cfg.Agent.Provider,
		"model", cfg.Agent.Model,
		"db", dbPath,
	)

	// ── Gateway ────────────────────────────────────────────────────────────
	gtw, err := gateway.NewServer(cfg.Gateway.Port, cfg.Gateway.Bind)
	if err != nil {
		slog.Error("failed to create gateway", "error", err)
		os.Exit(1)
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)

	go func() {
		if err := gtw.Start(); err != nil && err != http.ErrServerClosed {
			slog.Error("gateway server error", "error", err)
		}
	}()

	<-quit
	slog.Info("shutting down cleanly...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := gtw.Stop(ctx); err != nil {
		slog.Error("gateway shutdown error", "error", err)
	}

	slog.Info("OpenClaw stopped")
}

// loadConfig resolves the active configuration using the priority chain:
//
//  1. If explicitFlag is true: the user explicitly passed -config <path>.
//     Load it — if the file doesn't exist, exit with an error (fail-loud).
//  2. If explicitFlag is false (default path "openclaw.yaml"):
//     - File exists   → load it and log INFO.
//     - File missing  → fall back to DefaultConfig and log WARN.
func loadConfig(path string, explicitFlag bool) *config.Config {
	if explicitFlag {
		// User explicitly chose a file — fail hard if it isn't found.
		cfg, err := config.Load(path)
		if err != nil {
			slog.Error("failed to load specified config file",
				"path", path,
				"error", err,
			)
			os.Exit(1)
		}
		slog.Info("config loaded", "source", path)
		return cfg
	}

	// Default path — try to load, fall back gracefully if absent.
	if _, err := os.Stat(path); os.IsNotExist(err) {
		slog.Warn("config file not found, using built-in defaults",
			"looked_at", path,
			"tip", "copy openclaw.example.yaml to "+path+" to get started",
		)
		return config.DefaultConfig()
	}

	cfg, err := config.Load(path)
	if err != nil {
		slog.Warn("failed to parse config file, using built-in defaults",
			"path", path,
			"error", err,
		)
		return config.DefaultConfig()
	}

	slog.Info("config loaded", "source", path)
	return cfg
}

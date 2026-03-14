package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/openclaw/openclaw-go/internal/agent"
	"github.com/openclaw/openclaw-go/internal/channels"
	"github.com/openclaw/openclaw-go/internal/channels/cli"
	"github.com/openclaw/openclaw-go/internal/channels/telegram"
	"github.com/openclaw/openclaw-go/internal/channels/webchat"
	"github.com/openclaw/openclaw-go/internal/config"
	"github.com/openclaw/openclaw-go/internal/gateway"
	"github.com/openclaw/openclaw-go/internal/logger"
	"github.com/openclaw/openclaw-go/internal/router"
	"github.com/openclaw/openclaw-go/internal/session"
	"github.com/openclaw/openclaw-go/internal/tools"
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

	// ── Config ────────────────────────────────────────────────────────────
	cfg := loadConfig(configPath, configPath != defaultConfigPath)
	if port != 0 {
		cfg.Gateway.Port = port
	}
	if logLevel != "" {
		cfg.Logger.Level = logLevel
	}

	// ── Logger ────────────────────────────────────────────────────────────
	logger.Init(cfg.Logger.Level)

	// ── Session manager ───────────────────────────────────────────────────
	sm, err := session.NewManager(dbPath)
	if err != nil {
		slog.Error("failed to initialize session manager", "error", err)
		os.Exit(1)
	}
	defer sm.Close()

	// ── Agent engine ──────────────────────────────────────────────────────
	agentSvc, err := agent.NewAgent(
		agent.Provider(cfg.Agent.Provider),
		cfg.Agent.Model,
		cfg.Agent.APIKey,
		cfg.Agent.BaseURL,
	)
	if err != nil {
		slog.Error("failed to initialize agent", "error", err)
		os.Exit(1)
	}

	// ── Tool engine + MCP servers ─────────────────────────────────────────
	toolEngine, err := tools.NewEngine(cfg.Agent.Workspace)
	if err != nil {
		slog.Warn("tool engine unavailable", "error", err)
		// Non-fatal: continue without file/bash tools
	}
	if toolEngine != nil {
		for _, mcpCfg := range cfg.Tools.MCP {
			if err := toolEngine.RegisterMCP(mcpCfg.Name, mcpCfg.Command, mcpCfg.Args); err != nil {
				slog.Warn("failed to register MCP server", "name", mcpCfg.Name, "error", err)
			}
		}
	}

	// ── Gateway ───────────────────────────────────────────────────────────
	gtw, err := gateway.NewServer(cfg.Gateway.Port, cfg.Gateway.Bind)
	if err != nil {
		slog.Error("failed to create gateway", "error", err)
		os.Exit(1)
	}

	// ── Channel adapters ──────────────────────────────────────────────────
	var adapters []channels.Adapter

	if cfg.Channels.CLI.Enabled {
		adapters = append(adapters, cli.New("", nil, nil))
		slog.Info("channel enabled", "name", "cli")
	}

	if cfg.Channels.WebChat.Enabled {
		adapters = append(adapters, webchat.New(gtw))
		slog.Info("channel enabled", "name", "webchat")
	}

	if cfg.Channels.Telegram.Enabled {
		tgAdapter, err := telegram.New(cfg.Channels.Telegram.Token)
		if err != nil {
			slog.Error("failed to initialize Telegram adapter", "error", err)
			os.Exit(1)
		}
		adapters = append(adapters, tgAdapter)
		slog.Info("channel enabled", "name", "telegram")
	}

	if len(adapters) == 0 {
		slog.Warn("no channel adapters enabled — enabling CLI by default")
		adapters = append(adapters, cli.New("", nil, nil))
	}

	// ── Router ────────────────────────────────────────────────────────────
	r := router.New(sm, agentSvc, adapters, 256)

	slog.Info("🚀 OpenClaw started",
		"port", cfg.Gateway.Port,
		"provider", cfg.Agent.Provider,
		"model", cfg.Agent.Model,
		"adapters", len(adapters),
		"db", dbPath,
	)

	// ── Signal handling ────────────────────────────────────────────────────
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start gateway — fatal on bind error so user gets a clear message.
	gatewayErr := make(chan error, 1)
	go func() {
		if err := gtw.Start(); err != nil && err != http.ErrServerClosed {
			gatewayErr <- err
		}
	}()

	// Start router (blocks until ctx cancelled).
	go func() {
		if err := r.Run(ctx); err != nil && err != context.Canceled {
			slog.Error("router error", "error", err)
		}
	}()

	// Wait for shutdown signal or gateway failure.
	select {
	case <-quit:
		slog.Info("shutting down cleanly...")
	case err := <-gatewayErr:
		slog.Error("gateway failed to start — is port already in use?",
			"port", cfg.Gateway.Port,
			"error", err,
			"tip", "run: lsof -ti:"+fmt.Sprintf("%d", cfg.Gateway.Port)+" | xargs kill -9")
		slog.Info("shutting down cleanly...")
	}
	cancel()

	shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutCancel()
	if err := gtw.Stop(shutCtx); err != nil {
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
		cfg, err := config.Load(path)
		if err != nil {
			slog.Error("failed to load specified config file", "path", path, "error", err)
			os.Exit(1)
		}
		slog.Info("config loaded", "source", path)
		return cfg
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		slog.Warn("config file not found, using built-in defaults",
			"looked_at", path,
			"tip", "copy openclaw.example.yaml to "+path+" to get started",
		)
		return config.DefaultConfig()
	}

	cfg, err := config.Load(path)
	if err != nil {
		slog.Warn("failed to parse config file, using built-in defaults", "path", path, "error", err)
		return config.DefaultConfig()
	}

	slog.Info("config loaded", "source", path)
	return cfg
}

package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/openclaw/openclaw-go/internal/config"
)

func TestDefaultConfig(t *testing.T) {
	cfg := config.DefaultConfig()
	if cfg.Gateway.Port != 18789 {
		t.Errorf("expected default port 18789, got %d", cfg.Gateway.Port)
	}
	if cfg.Agent.Model != "anthropic/claude-3-opus" {
		t.Errorf("expected default model, got %s", cfg.Agent.Model)
	}
	if cfg.Agent.Provider != "anthropic" {
		t.Errorf("expected default provider anthropic, got %s", cfg.Agent.Provider)
	}
	if !cfg.Channels.CLI.Enabled {
		t.Error("expected CLI channel enabled by default")
	}
}

func TestLoadConfig(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "openclaw.yaml")
	yamlData := `
agent:
  provider: "openai"
  model: "gpt-4o"
  workspace: "/tmp/workspace"
gateway:
  port: 9000
  bind: "0.0.0.0"
logger:
  level: "debug"
channels:
  telegram:
    enabled: true
    token: "bot-token-123"
  webchat:
    enabled: true
  cli:
    enabled: false
tools:
  mcp:
    - name: filesystem
      command: npx
      args: ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"]
`
	if err := os.WriteFile(cfgPath, []byte(yamlData), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if cfg.Gateway.Port != 9000 {
		t.Errorf("expected port 9000, got %d", cfg.Gateway.Port)
	}
	if cfg.Agent.Model != "gpt-4o" {
		t.Errorf("expected gpt-4o, got %s", cfg.Agent.Model)
	}
	if cfg.Agent.Provider != "openai" {
		t.Errorf("expected openai provider, got %s", cfg.Agent.Provider)
	}
	if cfg.Logger.Level != "debug" {
		t.Errorf("expected debug logger, got %s", cfg.Logger.Level)
	}
	if !cfg.Channels.Telegram.Enabled {
		t.Error("expected telegram enabled")
	}
	if cfg.Channels.Telegram.Token != "bot-token-123" {
		t.Errorf("expected telegram token bot-token-123, got %s", cfg.Channels.Telegram.Token)
	}
	if !cfg.Channels.WebChat.Enabled {
		t.Error("expected webchat enabled")
	}
	if cfg.Channels.CLI.Enabled {
		t.Error("expected CLI disabled")
	}
	if len(cfg.Tools.MCP) != 1 {
		t.Fatalf("expected 1 MCP tool, got %d", len(cfg.Tools.MCP))
	}
	if cfg.Tools.MCP[0].Name != "filesystem" {
		t.Errorf("expected MCP tool name filesystem, got %s", cfg.Tools.MCP[0].Name)
	}
	if cfg.Tools.MCP[0].Command != "npx" {
		t.Errorf("expected MCP command npx, got %s", cfg.Tools.MCP[0].Command)
	}
}

func TestLoadConfigNotFound(t *testing.T) {
	_, err := config.Load("/path/does/not/exist.yaml")
	if err == nil {
		t.Error("expected error for non-existent file, got nil")
	}
}

func TestLoadConfig_EnvExpansion(t *testing.T) {
	t.Setenv("TEST_TG_TOKEN", "secret-bot-token")
	t.Setenv("TEST_API_KEY", "sk-test-key")

	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "openclaw.yaml")
	yamlData := `
agent:
  api_key: "${TEST_API_KEY}"
channels:
  telegram:
    enabled: true
    token: "${TEST_TG_TOKEN}"
`
	if err := os.WriteFile(cfgPath, []byte(yamlData), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}
	if cfg.Channels.Telegram.Token != "secret-bot-token" {
		t.Errorf("expected token expanded to 'secret-bot-token', got %q", cfg.Channels.Telegram.Token)
	}
	if cfg.Agent.APIKey != "sk-test-key" {
		t.Errorf("expected api_key expanded to 'sk-test-key', got %q", cfg.Agent.APIKey)
	}
}

package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

// Config is the top-level configuration for OpenClaw.
type Config struct {
	Agent    AgentConfig    `yaml:"agent"`
	Gateway  GatewayConfig  `yaml:"gateway"`
	Logger   LoggerConfig   `yaml:"logger"`
	Channels ChannelsConfig `yaml:"channels"`
	Tools    ToolsConfig    `yaml:"tools"`
}

// AgentConfig holds LLM provider settings.
type AgentConfig struct {
	Provider  string `yaml:"provider"` // openai | anthropic
	Model     string `yaml:"model"`
	APIKey    string `yaml:"api_key"`
	BaseURL   string `yaml:"base_url"`
	Workspace string `yaml:"workspace"`
}

// GatewayConfig holds HTTP/WebSocket server settings.
type GatewayConfig struct {
	Port int    `yaml:"port"`
	Bind string `yaml:"bind"`
}

// LoggerConfig holds structured logger settings.
type LoggerConfig struct {
	Level string `yaml:"level"` // debug, info, warn, error
}

// ChannelsConfig holds optional channel adapter settings.
type ChannelsConfig struct {
	Telegram TelegramConfig `yaml:"telegram"`
	WebChat  WebChatConfig  `yaml:"webchat"`
	CLI      CLIConfig      `yaml:"cli"`
}

// TelegramConfig holds Telegram Bot settings.
// Token supports environment variable expansion (e.g. ${TELEGRAM_TOKEN}).
type TelegramConfig struct {
	Enabled bool   `yaml:"enabled"`
	Token   string `yaml:"token"`
}

// WebChatConfig enables the WebSocket WebChat channel.
type WebChatConfig struct {
	Enabled bool `yaml:"enabled"`
}

// CLIConfig enables the stdin/stdout CLI channel.
type CLIConfig struct {
	Enabled bool `yaml:"enabled"`
}

// ToolsConfig holds tool engine settings.
type ToolsConfig struct {
	MCP []MCPToolConfig `yaml:"mcp"`
}

// MCPToolConfig describes a single MCP server to spawn as a sub-process.
type MCPToolConfig struct {
	Name    string   `yaml:"name"`    // logical name used by the agent
	Command string   `yaml:"command"` // executable to run (e.g. npx)
	Args    []string `yaml:"args"`    // command arguments
}

// Load parses an openclaw.yaml file into Config.
// String values that contain ${VAR} or $VAR placeholders are expanded
// using the current process environment (via os.ExpandEnv).
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	// Expand environment variable references in sensitive fields.
	cfg.Agent.APIKey = os.ExpandEnv(cfg.Agent.APIKey)
	cfg.Channels.Telegram.Token = os.ExpandEnv(cfg.Channels.Telegram.Token)
	return &cfg, nil
}

// DefaultConfig returns a sane default configuration.
func DefaultConfig() *Config {
	return &Config{
		Agent: AgentConfig{
			Provider:  "anthropic",
			Model:     "anthropic/claude-3-opus",
			Workspace: "~/.openclaw/workspace",
		},
		Gateway: GatewayConfig{
			Port: 18789,
			Bind: "127.0.0.1",
		},
		Logger: LoggerConfig{
			Level: "info",
		},
		Channels: ChannelsConfig{
			CLI: CLIConfig{Enabled: true},
		},
	}
}

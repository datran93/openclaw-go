<div align="center">

# 🦅 OpenClaw Go

**A high-performance personal AI assistant written in Go.**
Stream conversations with Claude, GPT-4o, or any OpenAI-compatible model — from your terminal, browser, or Telegram.

[![Go Version](https://img.shields.io/badge/Go-1.25+-00ADD8?style=flat-square&logo=go)](https://golang.org)
[![License](https://img.shields.io/badge/license-MIT-blue?style=flat-square)](LICENSE)
[![Build](https://img.shields.io/badge/build-passing-brightgreen?style=flat-square)](#quick-start)

</div>

---

## ✨ Why OpenClaw Go?

OpenClaw is a port of a personal AI assistant (originally TypeScript/Node.js) to **Go**, designed for:

- ⚡ **Low memory footprint** — single statically-compiled binary, no runtime bloat
- 🔄 **True streaming** — goroutine-per-session, zero buffering delay on LLM output
- 🔌 **Multi-channel** — same agent core powering CLI REPL, WebSocket chat, and Telegram simultaneously
- 🛠️ **Extensible tools** — bash sandbox, file I/O, and dynamic MCP server loading
- 💾 **Persistent sessions** — conversation history survives restarts via SQLite

---

## 🏗️ Architecture

```
┌─────────────────────────────────────────────────────────┐
│                     Channel Adapters                    │
│   CLI (stdin/stdout) │ WebChat (WS) │ Telegram Bot      │
└──────────────────────┬──────────────────────────────────┘
                       │  IncomingMessage
                       ▼
┌─────────────────────────────────────────────────────────┐
│                       Router                            │
│  session lookup/create → agent.StreamChat → Send back   │
└───────────┬─────────────────────────┬───────────────────┘
            │                         │
            ▼                         ▼
┌───────────────────┐     ┌───────────────────────────────┐
│  Session Manager  │     │        Agent Engine            │
│  SQLite + sync.Map│     │  Anthropic │ OpenAI │ Ollama  │
└───────────────────┘     └───────────┬───────────────────┘
                                      │ ToolCall
                                      ▼
                          ┌───────────────────────────────┐
                          │        Tool Engine             │
                          │  Bash │ File I/O │ MCP stdio  │
                          └───────────────────────────────┘
```

### Key Components

| Package | Responsibility |
|---|---|
| `internal/agent` | Streams tokens from Anthropic & OpenAI APIs |
| `internal/router` | Fan-in dispatcher — routes messages through sessions → agent → adapters |
| `internal/session` | Thread-safe conversation history with SQLite persistence |
| `internal/channels/cli` | stdin/stdout REPL adapter |
| `internal/channels/webchat` | WebSocket adapter (bridges `gateway.Server`) |
| `internal/channels/telegram` | Telegram Bot API long-poll adapter |
| `internal/tools` | Bash sandbox, file read/write, MCP sub-process client |
| `internal/gateway` | HTTP health check + WebSocket server |
| `internal/config` | YAML config loading with `${ENV_VAR}` expansion |

---

## 🚀 Quick Start

### Prerequisites

- **Go 1.25+** — [install](https://go.dev/dl/)
- An **Anthropic** or **OpenAI** API key

### 1. Clone & Build

```bash
git clone https://github.com/datran93/openclaw-go.git
cd openclaw-go
make build          # builds → bin/openclaw
```

### 2. Set your API key

```bash
export ANTHROPIC_API_KEY="sk-ant-..."
# or for OpenAI:
# export OPENAI_API_KEY="sk-..."
```

### 3. Run

```bash
# Use the default config (openclaw.yaml is already set up)
./bin/openclaw

# Or with Make
make run
```

The CLI REPL starts immediately:

```
OpenClaw CLI ready. Type a message and press Enter. Ctrl+C to quit.
> Hello! What can you do?
I'm OpenClaw, your personal AI assistant...
```

---

## ⚙️ Configuration

OpenClaw uses `openclaw.yaml` at the project root as its **config center**.

### Config resolution priority

| Priority | Source | Behaviour |
|---|---|---|
| **1 (highest)** | `-config <path>` flag | File must exist or process exits |
| **2** | `./openclaw.yaml` (default) | Loaded if present |
| **3 (lowest)** | Built-in defaults | Used when no file is found (WARN logged) |

### Key config sections

```yaml
agent:
  provider: "anthropic"              # openai | anthropic
  model: "claude-3-5-haiku-20241022"
  api_key: "${ANTHROPIC_API_KEY}"    # never hardcode — use env vars

gateway:
  port: 18789
  bind: "127.0.0.1"

channels:
  cli:
    enabled: true       # terminal REPL
  webchat:
    enabled: false      # WebSocket browser chat
  telegram:
    enabled: false
    token: "${TELEGRAM_TOKEN}"

tools:
  mcp: []              # MCP server sub-processes (see openclaw.example.yaml)
```

See [`openclaw.example.yaml`](openclaw.example.yaml) for the fully annotated reference with all options and examples.

### Personal overrides

Create `openclaw.local.yaml` (gitignored) for personal settings:

```bash
cp openclaw.yaml openclaw.local.yaml
# edit to taste…
./bin/openclaw -config openclaw.local.yaml
```

---

## 🔌 Channel Adapters

### CLI (default: enabled)

Interactive terminal REPL. Reads from stdin, streams responses to stdout.

```bash
./bin/openclaw
> What is the capital of France?
Paris is the capital of France.
>
```

### WebChat (WebSocket)

Enable in config, then connect any WebSocket client to `ws://127.0.0.1:18789/ws`.

```yaml
channels:
  webchat:
    enabled: true
```

### Telegram Bot

1. Create a bot with [@BotFather](https://t.me/BotFather) and copy the token
2. Set `TELEGRAM_TOKEN` and enable in config:

```yaml
channels:
  telegram:
    enabled: true
    token: "${TELEGRAM_TOKEN}"
```

---

## 🛠️ Tools

### Built-in

| Tool | Description |
|---|---|
| `ExecBash` | Run shell commands in a sandboxed workspace directory |
| `ReadFile` | Read files within the workspace (path traversal protected) |
| `WriteFile` | Write files within the workspace |

### MCP (Model Context Protocol)

Load any MCP-compatible tool server as a stdio sub-process:

```yaml
tools:
  mcp:
    - name: filesystem
      command: npx
      args: ["-y", "@modelcontextprotocol/server-filesystem", "~/.openclaw/workspace"]
```

---

## 🏃 CLI Flags

```
Usage: openclaw [flags]

Flags:
  -config string   path to config file (default: openclaw.yaml)
  -port   int      override gateway port (0 = use config value)
  -log    string   override log level: debug|info|warn|error
  -db     string   path to SQLite session database (default: openclaw.db)
```

---

## 🧰 Development

```bash
# Run tests (with race detector)
go test ./... -race

# Build binary
make build

# Run directly
make run

# Clean build artifacts
make clean
```

### Project layout

```
openclaw-go/
├── cmd/openclaw/         # main entrypoint
├── internal/
│   ├── agent/            # LLM streaming (Anthropic, OpenAI)
│   ├── channels/         # adapter contracts + implementations
│   │   ├── cli/
│   │   ├── webchat/
│   │   └── telegram/
│   ├── config/           # YAML config loading & env expansion
│   ├── gateway/          # HTTP + WebSocket server
│   ├── logger/           # structured slog initialisation
│   ├── router/           # central message dispatcher
│   ├── session/          # SQLite + in-memory session store
│   └── tools/            # bash, file I/O, MCP client
├── openclaw.yaml         # default config (committed)
├── openclaw.example.yaml # fully annotated reference config
└── DESIGN.md             # architectural decisions
```

---

## 🗺️ Roadmap

- [x] Core agent engine (Anthropic & OpenAI streaming)
- [x] Session manager (SQLite + in-memory cache)
- [x] Gateway WebSocket server
- [x] CLI adapter
- [x] Config file center (`openclaw.yaml`)
- [x] Channel adapter contracts + Router
- [ ] WebChat adapter (WebSocket bridge)
- [ ] Telegram adapter
- [ ] MCP stdio client
- [ ] Full binary wiring (`main.go`)
- [ ] System prompt support
- [ ] Ollama / local model support

---

## 📄 License

MIT — see [LICENSE](LICENSE).

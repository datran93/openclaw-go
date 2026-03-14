// Package tools — mcp_client.go
// MCPClient spawns an MCP server as a stdio sub-process and communicates
// with it via line-delimited JSON-RPC 2.0.
//
// Protocol reference: https://spec.modelcontextprotocol.io
package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"sync"
	"sync/atomic"
)

// rpcRequest is a JSON-RPC 2.0 request sent to the MCP server.
type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// rpcResponse is a JSON-RPC 2.0 response received from the MCP server.
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// toolsCallParams matches the MCP tools/call request structure.
type toolsCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

// toolsCallResult matches the MCP tools/call response structure.
type toolsCallResult struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	IsError bool `json:"isError"`
}

// MCPClient manages a long-running MCP server sub-process.
// It is goroutine-safe.
type MCPClient struct {
	name   string
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Scanner

	mu      sync.Mutex // guards stdin writes and pending map
	pending map[int64]chan rpcResponse
	nextID  atomic.Int64
}

// newMCPClient starts the MCP server sub-process and performs capability initialization.
func newMCPClient(name, command string, args []string) (*MCPClient, error) {
	cmd := exec.Command(command, args...) // #nosec G204 — controlled by user config
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp(%s): stdin pipe: %w", name, err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp(%s): stdout pipe: %w", name, err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("mcp(%s): start process: %w", name, err)
	}

	c := &MCPClient{
		name:    name,
		cmd:     cmd,
		stdin:   stdin,
		stdout:  bufio.NewScanner(stdoutPipe),
		pending: make(map[int64]chan rpcResponse),
	}

	// Read loop — dispatches responses to waiting callers.
	go c.readLoop()

	// MCP initialisation handshake.
	if err := c.initialize(); err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("mcp(%s): initialize: %w", name, err)
	}

	slog.Info("mcp server started", "name", name, "command", command)
	return c, nil
}

// readLoop continuously reads newline-delimited JSON from the sub-process stdout
// and dispatches responses to pending callers.
func (c *MCPClient) readLoop() {
	for c.stdout.Scan() {
		line := c.stdout.Bytes()
		var resp rpcResponse
		if err := json.Unmarshal(line, &resp); err != nil {
			slog.Debug("mcp: unreadable response line", "name", c.name, "line", string(line))
			continue
		}
		c.mu.Lock()
		ch, ok := c.pending[resp.ID]
		if ok {
			delete(c.pending, resp.ID)
		}
		c.mu.Unlock()
		if ok {
			ch <- resp
		}
	}
}

// call sends a JSON-RPC request and waits for the matching response.
func (c *MCPClient) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := c.nextID.Add(1)
	req := rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params}

	raw, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	raw = append(raw, '\n')

	ch := make(chan rpcResponse, 1)

	c.mu.Lock()
	c.pending[id] = ch
	_, writeErr := c.stdin.Write(raw)
	c.mu.Unlock()

	if writeErr != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, fmt.Errorf("mcp(%s): write: %w", c.name, writeErr)
	}

	select {
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, ctx.Err()
	case resp := <-ch:
		if resp.Error != nil {
			return nil, fmt.Errorf("mcp(%s) rpc error %d: %s", c.name, resp.Error.Code, resp.Error.Message)
		}
		return resp.Result, nil
	}
}

// initialize sends the MCP initialize + notifications/initialized handshake.
func (c *MCPClient) initialize() error {
	ctx := context.Background()
	params := map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "openclaw", "version": "0.1.0"},
	}
	if _, err := c.call(ctx, "initialize", params); err != nil {
		return err
	}
	// Send the initialized notification (no response expected).
	notif := rpcRequest{JSONRPC: "2.0", Method: "notifications/initialized"}
	raw, _ := json.Marshal(notif)
	raw = append(raw, '\n')
	c.mu.Lock()
	_, _ = c.stdin.Write(raw)
	c.mu.Unlock()
	return nil
}

// CallTool invokes a named tool on this MCP server.
func (c *MCPClient) CallTool(ctx context.Context, toolName string, arguments map[string]any) (string, error) {
	params := toolsCallParams{Name: toolName, Arguments: arguments}
	raw, err := c.call(ctx, "tools/call", params)
	if err != nil {
		return "", err
	}
	var result toolsCallResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return string(raw), nil // return raw text if unparseable
	}
	if result.IsError {
		return "", fmt.Errorf("mcp(%s) tool error from server", c.name)
	}
	var out string
	for _, c := range result.Content {
		if c.Type == "text" {
			out += c.Text
		}
	}
	return out, nil
}

// Close terminates the sub-process.
func (c *MCPClient) Close() error {
	_ = c.stdin.Close()
	return c.cmd.Process.Kill()
}

// ── Engine integration ────────────────────────────────────────────────────────

// mcpClients holds all registered MCP clients on the Engine.
// It is initialised lazily on first RegisterMCP call.
var mcpRegistry sync.Map // map[name string]*MCPClient

// RegisterMCP spawns an MCP server sub-process and registers it by name.
// Calling RegisterMCP with the same name a second time replaces the old client.
func (e *Engine) RegisterMCP(name, command string, args []string) error {
	client, err := newMCPClient(name, command, args)
	if err != nil {
		return err
	}
	// Close old client if replacing.
	if old, loaded := mcpRegistry.LoadAndDelete(name); loaded {
		_ = old.(*MCPClient).Close()
	}
	mcpRegistry.Store(name, client)
	return nil
}

// CallMCP invokes a registered MCP tool by server name and tool name.
func (e *Engine) CallMCP(ctx context.Context, serverName, toolName string, arguments map[string]any) (string, error) {
	val, ok := mcpRegistry.Load(serverName)
	if !ok {
		return "", fmt.Errorf("mcp: server %q not registered", serverName)
	}
	return val.(*MCPClient).CallTool(ctx, toolName, arguments)
}

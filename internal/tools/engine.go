package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Engine provides a secure execution environment for Agent tools
type Engine struct {
	workspace string
}

// NewEngine creates a new tools engine anchored to a specific workspace directory
func NewEngine(workspace string) (*Engine, error) {
	absPath, err := filepath.Abs(workspace)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path: %w", err)
	}

	if err := os.MkdirAll(absPath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create workspace: %w", err)
	}

	return &Engine{workspace: absPath}, nil
}

// ExecBash runs a shell command contextually bound to the workspace
func (e *Engine) ExecBash(ctx context.Context, script string) (string, error) {
	cmd := exec.CommandContext(ctx, "bash", "-c", script)
	cmd.Dir = e.workspace

	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("bash error: %w, output: %s", err, string(out))
	}
	return string(out), nil
}

// ReadFile securely reads a file within the workspace boundaries
func (e *Engine) ReadFile(filename string) (string, error) {
	safePath, err := e.securePath(filename)
	if err != nil {
		return "", err
	}

	data, err := os.ReadFile(safePath)
	if err != nil {
		return "", err
	}

	return string(data), nil
}

// WriteFile securely writes content to a file within the workspace boundaries
func (e *Engine) WriteFile(filename, content string) error {
	safePath, err := e.securePath(filename)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(safePath), 0755); err != nil {
		return fmt.Errorf("failed to create target directories: %w", err)
	}

	return os.WriteFile(safePath, []byte(content), 0644)
}

// securePath prevents directory traversal attacks outside the workspace
func (e *Engine) securePath(filename string) (string, error) {
	cleanPath := filepath.Clean(filepath.Join(e.workspace, filename))

	// Ensure the resulting path still resides within the workspace
	if !strings.HasPrefix(cleanPath, e.workspace) {
		return "", fmt.Errorf("security violation: path traversal attempt detected (%s)", filename)
	}

	return cleanPath, nil
}

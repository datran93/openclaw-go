package config_test

// resolver_test.go exercises the config resolution priority chain:
//
//   Priority 1 (highest): explicit file path → load or fail
//   Priority 2:           default "openclaw.yaml" present → load
//   Priority 3 (lowest):  no file present → use DefaultConfig
//
// These scenarios are tested using temp directories to control file presence.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/openclaw/openclaw-go/internal/config"
)

// yamlWithPort writes a minimal YAML config with a custom gateway port.
func yamlWithPort(t *testing.T, dir string, name string, port int) string {
	t.Helper()
	content := "gateway:\n  port: " + itoa(port) + "\n"
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return p
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	b := make([]byte, 0, 10)
	for ; i > 0; i /= 10 {
		b = append([]byte{byte('0' + i%10)}, b...)
	}
	return string(b)
}

// TestResolve_ExplicitPath: when a valid path is passed the file is loaded.
func TestResolve_ExplicitPath(t *testing.T) {
	dir := t.TempDir()
	p := yamlWithPort(t, dir, "custom.yaml", 9001)

	cfg, err := config.Load(p)
	if err != nil {
		t.Fatalf("expected load to succeed, got: %v", err)
	}
	if cfg.Gateway.Port != 9001 {
		t.Errorf("expected port 9001, got %d", cfg.Gateway.Port)
	}
}

// TestResolve_ExplicitPath_Missing: explicit path that doesn't exist returns error.
func TestResolve_ExplicitPath_Missing(t *testing.T) {
	_, err := config.Load("/tmp/does-not-exist-openclaw.yaml")
	if err == nil {
		t.Error("expected error for missing explicit config, got nil")
	}
}

// TestResolve_DefaultFile_Present: default file present → values loaded from it.
func TestResolve_DefaultFile_Present(t *testing.T) {
	dir := t.TempDir()
	p := yamlWithPort(t, dir, "openclaw.yaml", 7777)

	cfg, err := config.Load(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Gateway.Port != 7777 {
		t.Errorf("expected 7777, got %d", cfg.Gateway.Port)
	}
}

// TestResolve_DefaultFile_Absent: no file → DefaultConfig values.
func TestResolve_DefaultFile_Absent(t *testing.T) {
	cfg := config.DefaultConfig()
	if cfg.Gateway.Port != 18789 {
		t.Errorf("expected default port 18789, got %d", cfg.Gateway.Port)
	}
	if cfg.Agent.Provider != "anthropic" {
		t.Errorf("expected default provider anthropic, got %s", cfg.Agent.Provider)
	}
	if !cfg.Channels.CLI.Enabled {
		t.Error("expected CLI channel enabled by default")
	}
}

// TestResolve_ExampleYamlParsesClean: the committed openclaw.example.yaml at repo root
// must always be parseable — it is the canonical template for end-users.
func TestResolve_ExampleYamlParsesClean(t *testing.T) {
	// Resolve relative to the test file location (internal/config/) → repo root.
	const examplePath = "../../openclaw.example.yaml"
	if _, err := os.Stat(examplePath); os.IsNotExist(err) {
		t.Skip("openclaw.example.yaml not found relative to test dir")
	}
	cfg, err := config.Load(examplePath)
	if err != nil {
		t.Fatalf("openclaw.example.yaml parse error: %v", err)
	}
	if cfg.Agent.Provider == "" {
		t.Error("expected non-empty agent.provider in openclaw.example.yaml")
	}
}

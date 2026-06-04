// internal/config/project_test.go
package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/huanghao/app-nanny/internal/config"
)

func writeToml(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "app-nanny.toml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadProject_ModeA(t *testing.T) {
	path := writeToml(t, `
name = "parquet-explorer"
autostart = false
restart = "on-failure"
max_restarts = 5

[ports]
PORT = 5001
VITE_PORT = 5173
`)
	cfg, err := config.LoadProject(path)
	if err != nil {
		t.Fatalf("LoadProject error: %v", err)
	}
	if cfg.Name != "parquet-explorer" {
		t.Errorf("Name = %q, want %q", cfg.Name, "parquet-explorer")
	}
	if cfg.Ports["PORT"] != 5001 {
		t.Errorf("Ports[PORT] = %d, want 5001", cfg.Ports["PORT"])
	}
	if cfg.Ports["VITE_PORT"] != 5173 {
		t.Errorf("Ports[VITE_PORT] = %d, want 5173", cfg.Ports["VITE_PORT"])
	}
	if len(cfg.Processes) != 0 {
		t.Errorf("Processes should be empty for Mode A")
	}
}

func TestLoadProject_ModeB(t *testing.T) {
	path := writeToml(t, `
name = "md-viewer"
autostart = true
restart = "on-failure"

[processes.server]
command = "bun --watch run src/server.ts"
port = 3000

[processes.rag]
command = "bun --watch run src/rag-server.ts"
port = 3001
memory_warn_mb = 512
`)
	cfg, err := config.LoadProject(path)
	if err != nil {
		t.Fatalf("LoadProject error: %v", err)
	}
	if cfg.Name != "md-viewer" {
		t.Errorf("Name = %q, want %q", cfg.Name, "md-viewer")
	}
	if len(cfg.Processes) != 2 {
		t.Errorf("Processes len = %d, want 2", len(cfg.Processes))
	}
	if cfg.Processes["server"].Port != 3000 {
		t.Errorf("server port = %d, want 3000", cfg.Processes["server"].Port)
	}
	if cfg.Processes["rag"].MemoryWarnMB != 512 {
		t.Errorf("rag MemoryWarnMB = %d, want 512", cfg.Processes["rag"].MemoryWarnMB)
	}
}

func TestLoadProject_DefaultCommand(t *testing.T) {
	path := writeToml(t, `
name = "my-app"

[ports]
PORT = 8080
`)
	cfg, err := config.LoadProject(path)
	if err != nil {
		t.Fatalf("LoadProject error: %v", err)
	}
	if cfg.Command != "just dev" {
		t.Errorf("Command = %q, want %q", cfg.Command, "just dev")
	}
}

func TestLoadProject_MissingName(t *testing.T) {
	path := writeToml(t, `
[ports]
PORT = 8080
`)
	_, err := config.LoadProject(path)
	if err == nil {
		t.Error("expected error for missing name, got nil")
	}
}

func TestLoadProject_NotFound(t *testing.T) {
	_, err := config.LoadProject("/nonexistent/app-nanny.toml")
	if err == nil {
		t.Error("expected error for missing file, got nil")
	}
}

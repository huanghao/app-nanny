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

func TestLoadProject_SingleProcess(t *testing.T) {
	path := writeToml(t, `
name = "parquet-explorer"
autostart = false
restart = "on-failure"
max_restarts = 5

[processes.main]
command = "npx vite --port ${PORT:-5001}"
port = 5001
`)
	cfg, err := config.LoadProject(path)
	if err != nil {
		t.Fatalf("LoadProject error: %v", err)
	}
	if cfg.Name != "parquet-explorer" {
		t.Errorf("Name = %q, want %q", cfg.Name, "parquet-explorer")
	}
	if len(cfg.Processes) != 1 {
		t.Fatalf("Processes len = %d, want 1", len(cfg.Processes))
	}
	if cfg.Processes["main"].Port != 5001 {
		t.Errorf("main port = %d, want 5001", cfg.Processes["main"].Port)
	}
}

func TestLoadProject_MultipleProcesses(t *testing.T) {
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

func TestLoadProject_RequiresAtLeastOneProcess(t *testing.T) {
	path := writeToml(t, `name = "my-app"`)
	_, err := config.LoadProject(path)
	if err == nil {
		t.Error("expected error when no [processes.*] block is declared, got nil")
	}
}

func TestLoadProject_MissingName(t *testing.T) {
	path := writeToml(t, `
[processes.main]
command = "echo hi"
port = 8080
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

func TestLoadProject_DefaultMaxRestarts(t *testing.T) {
	path := writeToml(t, `
name = "x"
[processes.main]
command = "echo hi"
`)
	cfg, err := config.LoadProject(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MaxRestarts != 5 {
		t.Errorf("MaxRestarts = %d, want 5", cfg.MaxRestarts)
	}
}

func TestLoadProject_DefaultRestart(t *testing.T) {
	path := writeToml(t, `
name = "x"
[processes.main]
command = "echo hi"
`)
	cfg, err := config.LoadProject(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Restart != "on-failure" {
		t.Errorf("Restart = %q, want on-failure", cfg.Restart)
	}
}

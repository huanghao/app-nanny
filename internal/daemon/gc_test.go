// internal/daemon/gc_test.go
package daemon_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/huanghao/app-nanny/internal/config"
	"github.com/huanghao/app-nanny/internal/daemon"
)

// setupGCManager mirrors setupManager but also hands back logDir, which
// GC needs to inspect directly and setupManager's signature doesn't expose.
func setupGCManager(t *testing.T) (m *daemon.Manager, regDir, logDir string) {
	t.Helper()
	regDir = t.TempDir()
	// dataDir/logs mirrors the real daemon.go layout (daemon.go: logDir :=
	// filepath.Join(dataDir, "logs")) so that GC's filepath.Dir(logDir)
	// lookup for daemon.log lands next to logs/, not at some arbitrary
	// t.TempDir() parent.
	dataDir := t.TempDir()
	logDir = filepath.Join(dataDir, "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		t.Fatalf("mkdir logDir: %v", err)
	}
	rt := daemon.NewRuntime(filepath.Join(regDir, "runtime.json"))
	reg := config.NewRegistry(filepath.Join(regDir, "registry.json"))
	m = daemon.NewManager(reg, rt, logDir)
	return m, regDir, logDir
}

func TestGC_RemovesOrphanedLogsNotInRegistry(t *testing.T) {
	m, regDir, logDir := setupGCManager(t)
	projDir := writeProjectToml(t, regDir, `
name = "kept"
command = "sleep 60"
`)
	if err := m.Add("kept", projDir); err != nil {
		t.Fatalf("Add error: %v", err)
	}

	writeFile(t, filepath.Join(logDir, "kept.log"), "still active")
	writeFile(t, filepath.Join(logDir, "removed-project.log"), "orphaned")
	writeFile(t, filepath.Join(logDir, "removed-project.log.1"), "orphaned backup")

	result, err := m.GC(false)
	if err != nil {
		t.Fatalf("GC error: %v", err)
	}
	if len(result.RemovedLogs) != 2 {
		t.Fatalf("RemovedLogs = %v, want 2 entries", result.RemovedLogs)
	}
	if _, err := os.Stat(filepath.Join(logDir, "kept.log")); err != nil {
		t.Errorf("kept.log should survive GC: %v", err)
	}
	if _, err := os.Stat(filepath.Join(logDir, "removed-project.log")); !os.IsNotExist(err) {
		t.Errorf("removed-project.log should have been deleted")
	}
	if _, err := os.Stat(filepath.Join(logDir, "removed-project.log.1")); !os.IsNotExist(err) {
		t.Errorf("removed-project.log.1 should have been deleted")
	}
}

func TestGC_ModeBKeepsEachDeclaredProcessLog(t *testing.T) {
	m, regDir, logDir := setupGCManager(t)
	projDir := writeProjectToml(t, regDir, `
name = "multi"

[processes.api]
command = "sleep 60"

[processes.worker]
command = "sleep 60"
`)
	if err := m.Add("multi", projDir); err != nil {
		t.Fatalf("Add error: %v", err)
	}

	writeFile(t, filepath.Join(logDir, "multi-api.log"), "api")
	writeFile(t, filepath.Join(logDir, "multi-worker.log"), "worker")
	writeFile(t, filepath.Join(logDir, "multi-stale.log"), "renamed away")

	result, err := m.GC(false)
	if err != nil {
		t.Fatalf("GC error: %v", err)
	}
	if len(result.RemovedLogs) != 1 || result.RemovedLogs[0] != "multi-stale.log" {
		t.Fatalf("RemovedLogs = %v, want only multi-stale.log", result.RemovedLogs)
	}
}

func TestGC_DryRunLeavesFilesInPlace(t *testing.T) {
	m, _, logDir := setupGCManager(t)
	writeFile(t, filepath.Join(logDir, "orphan.log"), "orphaned")

	result, err := m.GC(true)
	if err != nil {
		t.Fatalf("GC error: %v", err)
	}
	if len(result.RemovedLogs) != 1 {
		t.Fatalf("RemovedLogs = %v, want 1 entry reported even in dry-run", result.RemovedLogs)
	}
	if _, err := os.Stat(filepath.Join(logDir, "orphan.log")); err != nil {
		t.Errorf("dry-run must not delete files: %v", err)
	}
}

func TestGC_CapsOversizedDaemonLog(t *testing.T) {
	m, _, logDir := setupGCManager(t)

	daemonLogPath := filepath.Join(filepath.Dir(logDir), "daemon.log")
	// Past the 50MB cap: write a recognizable tail marker so we can assert
	// the most recent content survives capping.
	head := make([]byte, 45*1024*1024)
	for i := range head {
		head[i] = 'h'
	}
	tail := []byte("tail-marker")
	padding := make([]byte, 6*1024*1024)
	if err := os.WriteFile(daemonLogPath, append(head, append(padding, tail...)...), 0644); err != nil {
		t.Fatalf("write daemon.log fixture: %v", err)
	}

	result, err := m.GC(false)
	if err != nil {
		t.Fatalf("GC error: %v", err)
	}
	if !result.DaemonLogCapped {
		t.Fatal("expected DaemonLogCapped = true")
	}

	data, err := os.ReadFile(daemonLogPath)
	if err != nil {
		t.Fatalf("read capped daemon.log: %v", err)
	}
	if len(data) >= len(head) {
		t.Errorf("daemon.log not shrunk: len=%d", len(data))
	}
	if string(data[len(data)-len(tail):]) != string(tail) {
		t.Errorf("tail marker lost after capping daemon.log")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

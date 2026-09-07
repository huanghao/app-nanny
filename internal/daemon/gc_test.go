// internal/daemon/gc_test.go
package daemon_test

import (
	"os"
	"path/filepath"
	"strings"
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
	writeOversizedFixture(t, daemonLogPath)

	result, err := m.GC(false)
	if err != nil {
		t.Fatalf("GC error: %v", err)
	}
	assertCapped(t, result, "daemon.log")
	assertTailSurvives(t, daemonLogPath)
}

// TestGC_RemovesOrphanedProjectLogDirWholesale covers the convention (see
// nanny skill docs): a project's own log files it writes itself — not
// through nanny's stdout capture — live under logs/<project>/. If the
// project is no longer registered, the whole directory goes, not just
// individually-recognized filenames (unlike the flat-file case, GC never
// tries to validate what a project named its own files).
func TestGC_RemovesOrphanedProjectLogDirWholesale(t *testing.T) {
	m, _, logDir := setupGCManager(t)
	// No project named "gone-project" is registered.
	projLogDir := filepath.Join(logDir, "gone-project")
	os.MkdirAll(projLogDir, 0755)
	writeFile(t, filepath.Join(projLogDir, "panel.log"), "some content")
	writeFile(t, filepath.Join(projLogDir, "native-host.log"), "more content")

	result, err := m.GC(false)
	if err != nil {
		t.Fatalf("GC error: %v", err)
	}
	if len(result.RemovedLogs) != 1 || result.RemovedLogs[0] != "gone-project/" {
		t.Fatalf("RemovedLogs = %v, want [\"gone-project/\"]", result.RemovedLogs)
	}
	if _, err := os.Stat(projLogDir); !os.IsNotExist(err) {
		t.Errorf("gone-project/ directory should have been removed wholesale")
	}
}

// TestGC_CapsOversizedFileInRegisteredProjectLogDir is the actual bug this
// feature exists for: context-pad's panel.log grows unboundedly because
// it's written by a companion macOS app nanny never spawns, so nanny's
// per-process RotatingFile never sees it. Once context-pad follows the
// convention and writes it under logs/context-pad/, GC caps it exactly
// like daemon.log — nanny doesn't care that it didn't write the file.
func TestGC_CapsOversizedFileInRegisteredProjectLogDir(t *testing.T) {
	m, regDir, logDir := setupGCManager(t)
	projDir := writeProjectToml(t, regDir, `
name = "context-pad"
command = "sleep 60"
`)
	if err := m.Add("context-pad", projDir); err != nil {
		t.Fatalf("Add error: %v", err)
	}

	projLogDir := filepath.Join(logDir, "context-pad")
	os.MkdirAll(projLogDir, 0755)
	panelLogPath := filepath.Join(projLogDir, "panel.log")
	writeOversizedFixture(t, panelLogPath)
	// A normal-sized sibling file must be left alone.
	writeFile(t, filepath.Join(projLogDir, "switches.log"), "small, untouched")

	result, err := m.GC(false)
	if err != nil {
		t.Fatalf("GC error: %v", err)
	}
	assertCapped(t, result, "context-pad/panel.log")
	assertTailSurvives(t, panelLogPath)

	data, err := os.ReadFile(filepath.Join(projLogDir, "switches.log"))
	if err != nil || string(data) != "small, untouched" {
		t.Errorf("switches.log should be untouched, got %q, err=%v", data, err)
	}
	// The registered project's directory itself must survive.
	if _, err := os.Stat(projLogDir); err != nil {
		t.Errorf("context-pad/ directory should survive GC: %v", err)
	}
}

func writeOversizedFixture(t *testing.T, path string) {
	t.Helper()
	head := make([]byte, 45*1024*1024)
	for i := range head {
		head[i] = 'h'
	}
	padding := make([]byte, 6*1024*1024)
	tail := []byte("tail-marker")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir for fixture %s: %v", path, err)
	}
	if err := os.WriteFile(path, append(head, append(padding, tail...)...), 0644); err != nil {
		t.Fatalf("write oversized fixture %s: %v", path, err)
	}
}

// TestGC_CappedLogStartsWithMarkerAndWholeLines covers truncateKeepTail's
// two courtesies: the capped file opens with a "nanny gc: trimmed" marker
// (so a history gap reads as a trim, not as silence), and the first
// surviving record starts at a line boundary rather than mid-line.
func TestGC_CappedLogStartsWithMarkerAndWholeLines(t *testing.T) {
	m, _, logDir := setupGCManager(t)

	daemonLogPath := filepath.Join(filepath.Dir(logDir), "daemon.log")
	// Fixed-width records so any byte-offset cut lands predictably: after
	// realignment the surviving content must be a whole number of records.
	record := "0123456789abcdef\n"
	var sb strings.Builder
	for sb.Len() <= 51*1024*1024 {
		sb.WriteString(record)
	}
	if err := os.WriteFile(daemonLogPath, []byte(sb.String()), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	if _, err := m.GC(false); err != nil {
		t.Fatalf("GC error: %v", err)
	}

	data, err := os.ReadFile(daemonLogPath)
	if err != nil {
		t.Fatalf("read capped file: %v", err)
	}
	firstNewline := strings.IndexByte(string(data), '\n')
	if firstNewline < 0 {
		t.Fatalf("capped file has no marker line: %q...", data[:80])
	}
	marker := string(data[:firstNewline])
	if !strings.Contains(marker, "nanny gc: trimmed") {
		t.Errorf("first line should be the trim marker, got %q", marker)
	}
	rest := data[firstNewline+1:]
	if len(rest)%len(record) != 0 {
		t.Errorf("surviving content should be whole records only, got %d trailing bytes after marker (record=%d)", len(rest), len(record))
	}
}

func assertCapped(t *testing.T, result daemon.GCResult, label string) {
	t.Helper()
	for _, name := range result.CappedLogs {
		if name == label {
			return
		}
	}
	t.Fatalf("CappedLogs = %v, want %q among them", result.CappedLogs, label)
}

func assertTailSurvives(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read capped file %s: %v", path, err)
	}
	if len(data) >= 45*1024*1024 {
		t.Errorf("%s not shrunk: len=%d", path, len(data))
	}
	tail := []byte("tail-marker")
	if string(data[len(data)-len(tail):]) != string(tail) {
		t.Errorf("tail marker lost after capping %s", path)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

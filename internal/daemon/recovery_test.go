// internal/daemon/recovery_test.go
package daemon_test

import (
	"path/filepath"
	"testing"

	"github.com/huanghao/app-nanny/internal/daemon"
)

func TestRuntime_WriteAndRead(t *testing.T) {
	dir := t.TempDir()
	rt := daemon.NewRuntime(filepath.Join(dir, "runtime.json"))

	rt.Set("md-viewer/server", daemon.RuntimeEntry{PID: 1234, PGID: 1234, Port: 3000})
	rt.Set("md-viewer/rag",    daemon.RuntimeEntry{PID: 1235, PGID: 1235, Port: 3001})

	if err := rt.Save(); err != nil {
		t.Fatalf("Save error: %v", err)
	}

	rt2 := daemon.NewRuntime(filepath.Join(dir, "runtime.json"))
	entries := rt2.All()
	if len(entries) != 2 {
		t.Fatalf("entries len = %d, want 2", len(entries))
	}
	if entries["md-viewer/server"].PID != 1234 {
		t.Errorf("PID = %d, want 1234", entries["md-viewer/server"].PID)
	}
}

func TestRuntime_Delete(t *testing.T) {
	dir := t.TempDir()
	rt := daemon.NewRuntime(filepath.Join(dir, "runtime.json"))
	rt.Set("foo", daemon.RuntimeEntry{PID: 42})
	_ = rt.Save()

	rt.Delete("foo")
	_ = rt.Save()

	rt2 := daemon.NewRuntime(filepath.Join(dir, "runtime.json"))
	if _, ok := rt2.All()["foo"]; ok {
		t.Error("deleted entry should not persist")
	}
}

func TestCleanupOrphans_KillsDeadEntry(t *testing.T) {
	dir := t.TempDir()
	rt := daemon.NewRuntime(filepath.Join(dir, "runtime.json"))

	// Use a PID that does not exist
	rt.Set("ghost", daemon.RuntimeEntry{PID: 2147483647, PGID: 2147483647})
	_ = rt.Save()

	killed, err := daemon.CleanupOrphans(rt)
	if err != nil {
		t.Fatalf("CleanupOrphans error: %v", err)
	}
	// PID doesn't exist → no kill needed, but entry should be removed
	if _, ok := rt.All()["ghost"]; ok {
		t.Error("non-existent PID should be cleaned from runtime")
	}
	_ = killed
}

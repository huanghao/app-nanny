// internal/daemon/manager_test.go
package daemon_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/huanghao/app-nanny/internal/config"
	"github.com/huanghao/app-nanny/internal/daemon"
)

func setupManager(t *testing.T) (*daemon.Manager, string) {
	t.Helper()
	dir := t.TempDir()
	rt := daemon.NewRuntime(filepath.Join(dir, "runtime.json"))
	reg := config.NewRegistry(filepath.Join(dir, "registry.json"))
	m := daemon.NewManager(reg, rt, t.TempDir())
	return m, dir
}

func writeProjectToml(t *testing.T, dir, content string) string {
	t.Helper()
	projDir := filepath.Join(dir, "my-project")
	os.MkdirAll(projDir, 0755)
	path := filepath.Join(projDir, "app-nanny.toml")
	os.WriteFile(path, []byte(content), 0644)
	return projDir
}

func TestManager_AddAndStart(t *testing.T) {
	m, dir := setupManager(t)
	projDir := writeProjectToml(t, dir, `
name = "sleeper"
command = "sleep 60"
`)
	if err := m.Add("sleeper", projDir); err != nil {
		t.Fatalf("Add error: %v", err)
	}
	if err := m.Start("sleeper", ""); err != nil {
		t.Fatalf("Start error: %v", err)
	}
	defer m.Stop("sleeper", "")

	infos := m.PS()
	if len(infos) == 0 {
		t.Fatal("PS returned empty")
	}
	found := false
	for _, info := range infos {
		if info.Project == "sleeper" && info.Status == "running" {
			found = true
		}
	}
	if !found {
		t.Error("sleeper not running after Start")
	}
}

func TestManager_PortConflict(t *testing.T) {
	m, dir := setupManager(t)

	proj1Dir := writeProjectToml(t, dir, `
name = "alpha"
command = "sleep 60"
[ports]
PORT = 9090
`)
	proj2Dir := filepath.Join(dir, "proj2")
	os.MkdirAll(proj2Dir, 0755)
	os.WriteFile(filepath.Join(proj2Dir, "app-nanny.toml"), []byte(`
name = "beta"
command = "sleep 60"
[ports]
PORT = 9090
`), 0644)

	_ = m.Add("alpha", proj1Dir)
	_ = m.Add("beta", proj2Dir)
	_ = m.Start("alpha", "")
	defer m.Stop("alpha", "")

	err := m.Start("beta", "")
	if err == nil {
		defer m.Stop("beta", "")
		t.Error("expected port conflict error, got nil")
	}
}

func TestManager_Remove(t *testing.T) {
	m, dir := setupManager(t)
	projDir := writeProjectToml(t, dir, `name = "ephemeral"`)
	_ = m.Add("ephemeral", projDir)
	if err := m.Remove("ephemeral"); err != nil {
		t.Fatalf("Remove error: %v", err)
	}
}

func TestManager_Restart(t *testing.T) {
	m, dir := setupManager(t)
	projDir := writeProjectToml(t, dir, `
name = "rsvc"
command = "sleep 60"
`)
	_ = m.Add("rsvc", projDir)
	_ = m.Start("rsvc", "")
	defer m.Stop("rsvc", "")

	if err := m.Restart("rsvc", ""); err != nil {
		t.Fatalf("Restart error: %v", err)
	}
	time.Sleep(500 * time.Millisecond)

	infos := m.PS()
	found := false
	for _, info := range infos {
		if info.Project == "rsvc" && info.Status == "running" {
			found = true
		}
	}
	if !found {
		t.Error("service should be running after restart")
	}
}

func TestManager_StartUnknown(t *testing.T) {
	m, _ := setupManager(t)
	err := m.Start("ghost", "")
	if err == nil {
		t.Error("starting unknown project should return error")
	}
}

func TestManager_RemoveWhileRunning(t *testing.T) {
	m, dir := setupManager(t)
	projDir := writeProjectToml(t, dir, `name = "blocker"
command = "sleep 60"`)
	_ = m.Add("blocker", projDir)
	_ = m.Start("blocker", "")
	defer m.Stop("blocker", "")

	err := m.Remove("blocker")
	if err == nil {
		t.Error("removing a running project should return error")
	}
}

var _ = time.Second // keep time import used

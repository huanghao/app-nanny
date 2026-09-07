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

// stubPortDetector lets PS()'s ActualPorts field be exercised without
// shelling out to lsof/pgrep against a real process.
type stubPortDetector struct{ ports []int }

func (s stubPortDetector) ActualPorts(pid, pgid int) []int { return s.ports }

func writeProjectToml(t *testing.T, dir, content string) string {
	t.Helper()
	projDir := filepath.Join(dir, "my-project")
	os.MkdirAll(projDir, 0755)
	path := filepath.Join(projDir, "app-nanny.toml")
	os.WriteFile(path, []byte(content), 0644)
	return projDir
}

// A real regression this stands in for: app-nanny's own daemon and web
// console show ports correctly (they use ActualPorts, which walks into
// child processes), but a naive PID-only port lookup misses a process
// whose real listener is a child (e.g. `uvicorn --reload`) — this test
// exercises that PS()/DetailedStatus() report whatever PortDetector says,
// without needing a real child-process/lsof setup to prove it.
func TestManager_PSUsesInjectedPortDetector(t *testing.T) {
	m, dir := setupManager(t)
	m.SetPortDetector(stubPortDetector{ports: []int{4567}})

	projDir := writeProjectToml(t, dir, `
name = "webby"
[processes.main]
command = "sleep 60"
`)
	_ = m.Add("webby", projDir)
	_ = m.Start("webby", "")
	defer m.Stop("webby", "")

	infos := m.PS()
	found := false
	for _, info := range infos {
		if info.Project != "webby" {
			continue
		}
		found = true
		if len(info.ActualPorts) != 1 || info.ActualPorts[0] != 4567 {
			t.Errorf("ActualPorts = %v, want [4567]", info.ActualPorts)
		}
		if info.Key != "webby/main" {
			t.Errorf("Key = %q, want %q", info.Key, "webby/main")
		}
	}
	if !found {
		t.Fatal("webby not found in PS() output")
	}

	status := m.DetailedStatus("webby")
	if len(status.Processes) != 1 || len(status.Processes[0].ActualPorts) != 1 || status.Processes[0].ActualPorts[0] != 4567 {
		t.Errorf("DetailedStatus ActualPorts = %+v, want [4567]", status.Processes)
	}
}

// Regression: a project whose config declares several processes (e.g.
// frontend + backend) used to lose the never-started ones from PS() the
// moment any sibling was started — step 2 skipped the whole project once
// one subprocess was tracked, so the web dashboard showed only the
// started server.
func TestManager_PSShowsUnstartedSiblings(t *testing.T) {
	m, dir := setupManager(t)
	projDir := writeProjectToml(t, dir, `
name = "duo"
[processes.frontend]
command = "sleep 60"
port = 3101
[processes.backend]
command = "sleep 60"
port = 3102
`)
	if err := m.Add("duo", projDir); err != nil {
		t.Fatalf("Add error: %v", err)
	}
	if err := m.Start("duo", "backend"); err != nil {
		t.Fatalf("Start error: %v", err)
	}
	defer m.Stop("duo", "")

	infos := m.PS()
	statuses := make(map[string]string)
	for _, info := range infos {
		if info.Project == "duo" {
			statuses[info.Key] = info.Status
		}
	}
	if statuses["duo/backend"] != "running" {
		t.Errorf("duo/backend status = %q, want running", statuses["duo/backend"])
	}
	if statuses["duo/frontend"] != "stopped" {
		t.Errorf("duo/frontend status = %q, want stopped (must stay visible)", statuses["duo/frontend"])
	}
}

func TestManager_AddAndStart(t *testing.T) {
	m, dir := setupManager(t)
	projDir := writeProjectToml(t, dir, `
name = "sleeper"
[processes.main]
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
[processes.main]
command = "sleep 60"
port = 9090
`)
	proj2Dir := filepath.Join(dir, "proj2")
	os.MkdirAll(proj2Dir, 0755)
	os.WriteFile(filepath.Join(proj2Dir, "app-nanny.toml"), []byte(`
name = "beta"
[processes.main]
command = "sleep 60"
port = 9090
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
	projDir := writeProjectToml(t, dir, `
name = "ephemeral"
[processes.main]
command = "sleep 60"
`)
	_ = m.Add("ephemeral", projDir)
	if err := m.Remove("ephemeral"); err != nil {
		t.Fatalf("Remove error: %v", err)
	}
}

func TestManager_Restart(t *testing.T) {
	m, dir := setupManager(t)
	projDir := writeProjectToml(t, dir, `
name = "rsvc"
[processes.main]
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
	projDir := writeProjectToml(t, dir, `
name = "blocker"
[processes.main]
command = "sleep 60"
`)
	_ = m.Add("blocker", projDir)
	_ = m.Start("blocker", "")
	defer m.Stop("blocker", "")

	err := m.Remove("blocker")
	if err == nil {
		t.Error("removing a running project should return error")
	}
}

var _ = time.Second // keep time import used

func TestManager_CrashLoopGivesUpAtMaxRestarts(t *testing.T) {
	m, dir := setupManager(t)
	projDir := writeProjectToml(t, dir, `
name = "crasher"
max_restarts = 2
[processes.main]
command = "exit 1"
`)
	if err := m.Add("crasher", projDir); err != nil {
		t.Fatalf("Add error: %v", err)
	}
	if err := m.Start("crasher", ""); err != nil {
		t.Fatalf("Start error: %v", err)
	}
	defer m.Stop("crasher", "")

	// Backoff: restart #1 after 1s, #2 after another 2s, then give up.
	restarts := func() int {
		for _, info := range m.PS() {
			if info.Project == "crasher" {
				return info.Restarts
			}
		}
		return -1
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if restarts() >= 2 {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if got := restarts(); got != 2 {
		t.Fatalf("restarts = %d, want 2 after backoff window", got)
	}
	// Stay past another backoff window: a 3rd restart must not happen.
	time.Sleep(5 * time.Second)
	if got := restarts(); got != 2 {
		t.Fatalf("restarts = %d, want 2 — kept restarting past max_restarts", got)
	}
}

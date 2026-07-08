package daemon_test

import (
	"os"
	"testing"

	"github.com/huanghao/app-nanny/internal/daemon"
)

func TestMetrics_SampleSelf(t *testing.T) {
	m := daemon.NewMetrics()
	pid := os.Getpid()
	m.Update("self", pid)

	snap := m.Get("self")
	if snap.MemMB <= 0 {
		t.Errorf("expected MemMB > 0 for own process, got %f", snap.MemMB)
	}
	if snap.CPUPercent < 0 {
		t.Errorf("expected CPUPercent >= 0 for own process, got %f", snap.CPUPercent)
	}
}

func TestMetrics_MissingKey(t *testing.T) {
	m := daemon.NewMetrics()
	snap := m.Get("nonexistent")
	if snap.MemMB != 0 || snap.CPUPercent != 0 {
		t.Errorf("expected zero snapshot for missing key, got %+v", snap)
	}
}

func TestMetrics_ZeroPID(t *testing.T) {
	m := daemon.NewMetrics()
	m.Update("dead", 0)
	snap := m.Get("dead")
	if snap.MemMB != 0 || snap.CPUPercent != 0 {
		t.Errorf("expected zero MemMB/CPUPercent for pid=0, got %+v", snap)
	}
}

func TestMetrics_PIDChangeResetsBaseline(t *testing.T) {
	m := daemon.NewMetrics()
	// First process under this key.
	m.Update("proc", os.Getpid())
	first := m.Get("proc")
	if first.CPUPercent < 0 {
		t.Fatalf("expected non-negative CPUPercent, got %f", first.CPUPercent)
	}
	// A different pid reusing the same key (process restarted) must not diff
	// its cpu-time against the old process's baseline.
	m.Update("proc", 1)
	second := m.Get("proc")
	if second.CPUPercent < 0 {
		t.Errorf("expected non-negative CPUPercent after pid change, got %f", second.CPUPercent)
	}
}

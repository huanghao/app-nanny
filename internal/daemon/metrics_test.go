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
}

func TestMetrics_MissingKey(t *testing.T) {
	m := daemon.NewMetrics()
	snap := m.Get("nonexistent")
	if snap.MemMB != 0 {
		t.Errorf("expected zero snapshot for missing key, got %+v", snap)
	}
}

func TestMetrics_ZeroPID(t *testing.T) {
	m := daemon.NewMetrics()
	m.Update("dead", 0)
	snap := m.Get("dead")
	if snap.MemMB != 0 {
		t.Errorf("expected zero MemMB for pid=0, got %f", snap.MemMB)
	}
}

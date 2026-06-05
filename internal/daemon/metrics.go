package daemon

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Snapshot holds one RSS sample for a process.
type Snapshot struct {
	UpdatedAt time.Time
	MemMB     float64
}

// Metrics holds the latest RSS snapshot per process key.
type Metrics struct {
	mu        sync.Mutex
	snapshots map[string]Snapshot
}

// NewMetrics returns an empty Metrics store.
func NewMetrics() *Metrics {
	return &Metrics{snapshots: make(map[string]Snapshot)}
}

// Update samples RSS for pid and stores it under key.
func (m *Metrics) Update(key string, pid int) {
	memMB := sampleRSS(pid)
	m.mu.Lock()
	m.snapshots[key] = Snapshot{UpdatedAt: time.Now(), MemMB: memMB}
	m.mu.Unlock()
}

// Get returns the most recent snapshot for key, or a zero Snapshot if not found.
func (m *Metrics) Get(key string) Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.snapshots[key]
}

// sampleRSS reads resident set size for pid via `ps -o rss= -p <pid>`.
func sampleRSS(pid int) float64 {
	if pid == 0 {
		return 0
	}
	out, err := exec.Command("ps", "-o", "rss=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return 0
	}
	var kb int
	fmt.Sscanf(strings.TrimSpace(string(out)), "%d", &kb)
	return float64(kb) / 1024.0
}

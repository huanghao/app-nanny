package daemon

import (
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Snapshot holds the latest sample for a process.
//
// CPUPercent is the AVERAGE CPU utilization over the interval since the
// previous sample for this key (not an instantaneous reading). It is
// derived from the OS-reported cumulative CPU time, so it correctly
// reflects any usage that happened between two on-demand checks — e.g. if
// you glance at the dashboard, walk away for an hour, and come back, this
// is the average over that whole hour, not just "whatever ps sees right
// now." This only works because sampling is on-demand (see Update callers)
// rather than a fixed background tick, so there is no idle-time cost and
// no risk of the interval "resetting" on its own.
type Snapshot struct {
	UpdatedAt  time.Time
	MemMB      float64
	CPUPercent float64
}

type cpuBaseline struct {
	pid      int
	at       time.Time
	cpuTimeS float64
}

// Metrics holds the latest sample per process key, sampled on demand.
type Metrics struct {
	mu        sync.Mutex
	snapshots map[string]Snapshot
	baselines map[string]cpuBaseline
}

// NewMetrics returns an empty Metrics store.
func NewMetrics() *Metrics {
	return &Metrics{
		snapshots: make(map[string]Snapshot),
		baselines: make(map[string]cpuBaseline),
	}
}

// Update samples pid and stores the result under key. Call this on demand
// (e.g. when serving /api/ps or `nanny ps`) rather than on a background
// timer — nobody's paying for a ps(1) fork when nobody's looking, and the
// computed CPUPercent naturally covers however long it's been since the
// last call.
func (m *Metrics) Update(key string, pid int) {
	memMB, cpuTimeS, instantPct := sampleProcess(pid)
	now := time.Now()

	m.mu.Lock()
	defer m.mu.Unlock()

	var cpuPercent float64
	base, ok := m.baselines[key]
	switch {
	case pid == 0:
		cpuPercent = 0
	case !ok || base.pid != pid:
		// First sample for this pid (daemon just started, or the process was
		// restarted under the same key) — no baseline to diff against yet,
		// fall back to the instantaneous reading just this once.
		cpuPercent = instantPct
	default:
		elapsed := now.Sub(base.at).Seconds()
		if elapsed < 0.5 {
			// Called again too soon to get a meaningful delta (e.g. two API
			// requests within the same tick) — keep reporting the previous value.
			cpuPercent = m.snapshots[key].CPUPercent
		} else {
			delta := cpuTimeS - base.cpuTimeS
			if delta < 0 {
				delta = 0 // guard against a parse hiccup going backwards
			}
			cpuPercent = delta / elapsed * 100
		}
	}

	m.baselines[key] = cpuBaseline{pid: pid, at: now, cpuTimeS: cpuTimeS}
	m.snapshots[key] = Snapshot{UpdatedAt: now, MemMB: memMB, CPUPercent: cpuPercent}
}

// Get returns the most recent snapshot for key, or a zero Snapshot if not found.
func (m *Metrics) Get(key string) Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.snapshots[key]
}

// sampleProcess reads RSS, cumulative CPU time, and instantaneous %CPU for
// pid in a single `ps` call. cpuTimeS is the total CPU seconds consumed
// since the process started (monotonically increasing, used to compute an
// interval average across calls). instantPct is macOS's live %CPU reading,
// used only as a fallback for the very first sample of a process.
func sampleProcess(pid int) (memMB float64, cpuTimeS float64, instantPct float64) {
	if pid == 0 {
		return 0, 0, 0
	}
	out, err := exec.Command("ps", "-o", "rss=,pcpu=,time=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return 0, 0, 0
	}
	fields := strings.Fields(strings.TrimSpace(string(out)))
	if len(fields) < 3 {
		return 0, 0, 0
	}
	kb, _ := strconv.ParseFloat(fields[0], 64)
	pct, _ := strconv.ParseFloat(fields[1], 64)
	cpuTimeS = parseClock(fields[2])
	return kb / 1024.0, cpuTimeS, pct
}

// parseClock parses ps(1)'s BSD clock formats into seconds:
//
//	"SS.ss", "MM:SS.ss", "HH:MM:SS.ss", "DD-HH:MM:SS(.ss)?"
func parseClock(s string) float64 {
	days := 0.0
	if idx := strings.IndexByte(s, '-'); idx >= 0 {
		days, _ = strconv.ParseFloat(s[:idx], 64)
		s = s[idx+1:]
	}
	var total float64
	for _, part := range strings.Split(s, ":") {
		v, _ := strconv.ParseFloat(part, 64)
		total = total*60 + v
	}
	return days*86400 + total
}

# app-nanny Plan 2: Observability Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Capture stdout/stderr from every managed process, detect errors, sample memory, and expose logs/errors/metrics via CLI commands (`nanny logs`, `nanny errors`, `nanny status`).

**Architecture:** Each process gets a `Logger` (an `io.Writer` that writes to a rotating log file, maintains a 500-line ring buffer, and detects error patterns). The `Manager` holds one `Logger` per process, one global `ErrorRing`, and a `Metrics` snapshot map refreshed every 15 seconds. New IPC handlers (`logs`, `errors`, `status`, `logpath`) expose this data. CLI `nanny logs -f` follows the log file directly (no IPC streaming needed).

**Tech Stack:** Go standard library only. No new dependencies.

**Out of scope:** Web console (Plan 3), CPU% (keep RSS only for simplicity), adaptive sampling (fixed 15s).

---

## File Map

```
internal/daemon/
  rotator.go          # RotatingFile: append-only writer, rotates at 50 MB
  rotator_test.go
  logger.go           # Logger: io.Writer → file + 500-line ring + error detection
  logger_test.go
  errors.go           # ErrorEvent, ErrorRing (50-event circular buffer), pattern matching
  errors_test.go
  metrics.go          # Metrics: RSS sampling every 15s via `ps`
  metrics_test.go
  process.go          # MODIFY: add stdioW field + SetStdio()
  manager.go          # MODIFY: add logDir/loggers/errRing/metrics, wire loggers per process
  daemon.go           # MODIFY: pass logDir to Manager, add logs/errors/status/logpath handlers
internal/ipc/
  types.go            # MODIFY: add LogsParams, LogsResult, ErrorsParams, ErrorsResult,
                      #         StatusResult, ProcessStatus, ErrorEvent
cmd/
  logs.go             # nanny logs <name>[/process] [-f] [-n N]
  errors.go           # nanny errors <name>[/process] [--last] [--copy]
  status.go           # nanny status <name>
```

---

## Task 1: RotatingFile

**Files:**
- Create: `internal/daemon/rotator.go`
- Create: `internal/daemon/rotator_test.go`

- [ ] **Step 1: Write failing tests**

```go
// internal/daemon/rotator_test.go
package daemon_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/huanghao/app-nanny/internal/daemon"
)

func TestRotatingFile_WriteAndRead(t *testing.T) {
	dir := t.TempDir()
	rf, err := daemon.NewRotatingFile(filepath.Join(dir, "test.log"), 1024, 3)
	if err != nil {
		t.Fatal(err)
	}
	defer rf.Close()

	msg := "hello world\n"
	if _, err := rf.Write([]byte(msg)); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "test.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "hello world") {
		t.Errorf("expected file to contain %q, got %q", "hello world", string(data))
	}
}

func TestRotatingFile_Rotates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	// maxSize = 100 bytes, maxFiles = 3
	rf, err := daemon.NewRotatingFile(path, 100, 3)
	if err != nil {
		t.Fatal(err)
	}
	defer rf.Close()

	// Write enough data to trigger rotation
	line := strings.Repeat("x", 50) + "\n"
	for i := 0; i < 5; i++ {
		if _, err := rf.Write([]byte(line)); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}

	// Current file should exist
	if _, err := os.Stat(path); err != nil {
		t.Error("current log file should exist")
	}
	// At least one backup should exist
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Error("backup .1 should exist after rotation")
	}
}

func TestRotatingFile_MaxFilesEnforced(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	rf, err := daemon.NewRotatingFile(path, 50, 2) // only 2 files kept
	if err != nil {
		t.Fatal(err)
	}
	defer rf.Close()

	line := strings.Repeat("y", 30) + "\n"
	for i := 0; i < 8; i++ {
		rf.Write([]byte(line)) //nolint:errcheck
	}

	// .3 should NOT exist since maxFiles=2
	if _, err := os.Stat(path + ".3"); !os.IsNotExist(err) {
		t.Error("file .3 should not exist with maxFiles=2")
	}
}
```

- [ ] **Step 2: Run to confirm they fail**

```bash
go test ./internal/daemon/... -v -run TestRotatingFile -timeout 10s
```

Expected: compile error — `NewRotatingFile` not defined.

- [ ] **Step 3: Implement rotator.go**

```go
// internal/daemon/rotator.go
package daemon

import (
	"fmt"
	"os"
	"sync"
)

// RotatingFile is an append-only log file that rotates when size exceeds maxSize.
// It keeps at most maxFiles backup copies (app.log, app.log.1, app.log.2, …).
type RotatingFile struct {
	mu       sync.Mutex
	path     string
	maxSize  int64
	maxFiles int
	file     *os.File
	size     int64
}

// NewRotatingFile opens (or creates) the file at path and returns a RotatingFile.
func NewRotatingFile(path string, maxSizeBytes int64, maxFiles int) (*RotatingFile, error) {
	f, size, err := openAppend(path)
	if err != nil {
		return nil, err
	}
	return &RotatingFile{
		path:     path,
		maxSize:  maxSizeBytes,
		maxFiles: maxFiles,
		file:     f,
		size:     size,
	}, nil
}

func (r *RotatingFile) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.size+int64(len(p)) > r.maxSize {
		if err := r.rotate(); err != nil {
			return 0, fmt.Errorf("rotate: %w", err)
		}
	}
	n, err := r.file.Write(p)
	r.size += int64(n)
	return n, err
}

func (r *RotatingFile) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.file.Close()
}

func (r *RotatingFile) rotate() error {
	r.file.Close()

	// Shift backups: .N-1 → .N (oldest first, so iterate from max down to 1)
	for i := r.maxFiles - 1; i >= 1; i-- {
		src := fmt.Sprintf("%s.%d", r.path, i)
		dst := fmt.Sprintf("%s.%d", r.path, i+1)
		// Remove the destination if it exists (enforces maxFiles)
		os.Remove(dst)
		os.Rename(src, dst) //nolint:errcheck
	}
	// Rename current → .1
	os.Rename(r.path, r.path+".1") //nolint:errcheck

	f, _, err := openAppend(r.path)
	if err != nil {
		return err
	}
	r.file = f
	r.size = 0
	return nil
}

func openAppend(path string) (*os.File, int64, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, 0, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, err
	}
	return f, info.Size(), nil
}
```

- [ ] **Step 4: Run tests to confirm they pass**

```bash
go test ./internal/daemon/... -v -run TestRotatingFile -timeout 10s
```

Expected: all 3 PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/daemon/rotator.go internal/daemon/rotator_test.go
git commit -m "feat: rotating log file writer"
```

---

## Task 2: ErrorRing

**Files:**
- Create: `internal/daemon/errors.go`
- Create: `internal/daemon/errors_test.go`

- [ ] **Step 1: Write failing tests**

```go
// internal/daemon/errors_test.go
package daemon_test

import (
	"testing"
	"time"

	"github.com/huanghao/app-nanny/internal/daemon"
)

func TestErrorRing_AddAndRecent(t *testing.T) {
	r := daemon.NewErrorRing()

	r.Add(daemon.ErrorEvent{Time: time.Now(), Key: "svc/a", Lines: []string{"err1"}})
	r.Add(daemon.ErrorEvent{Time: time.Now(), Key: "svc/b", Lines: []string{"err2"}})

	all := r.RecentForKey("", 10)
	if len(all) != 2 {
		t.Fatalf("expected 2 events, got %d", len(all))
	}
}

func TestErrorRing_FilterByKey(t *testing.T) {
	r := daemon.NewErrorRing()
	r.Add(daemon.ErrorEvent{Time: time.Now(), Key: "alpha", Lines: []string{"a"}})
	r.Add(daemon.ErrorEvent{Time: time.Now(), Key: "beta",  Lines: []string{"b"}})
	r.Add(daemon.ErrorEvent{Time: time.Now(), Key: "alpha", Lines: []string{"c"}})

	got := r.RecentForKey("alpha", 10)
	if len(got) != 2 {
		t.Fatalf("expected 2 alpha events, got %d", len(got))
	}
	for _, e := range got {
		if e.Key != "alpha" {
			t.Errorf("expected key=alpha, got %q", e.Key)
		}
	}
}

func TestErrorRing_CapAt50(t *testing.T) {
	r := daemon.NewErrorRing()
	for i := 0; i < 60; i++ {
		r.Add(daemon.ErrorEvent{Time: time.Now(), Key: "k", Lines: []string{"x"}})
	}
	got := r.RecentForKey("k", 100)
	if len(got) > 50 {
		t.Errorf("ring should cap at 50, got %d", len(got))
	}
}

func TestMatchesError(t *testing.T) {
	tests := []struct {
		line string
		want bool
	}{
		{"GET /api 500 3ms", true},
		{"GET /api 200 3ms", false},
		{"Traceback (most recent call last):", true},
		{"Error: something failed", true},
		{"TypeError: cannot read property", true},
		{"panic: runtime error", true},
		{"INFO: server started", false},
		{"FATAL: disk full", true},
	}
	for _, tt := range tests {
		got := daemon.MatchesError(tt.line, nil)
		if got != tt.want {
			t.Errorf("MatchesError(%q) = %v, want %v", tt.line, got, tt.want)
		}
	}
}
```

- [ ] **Step 2: Run to confirm they fail**

```bash
go test ./internal/daemon/... -v -run "TestErrorRing|TestMatchesError" -timeout 10s
```

Expected: compile error — `NewErrorRing` not defined.

- [ ] **Step 3: Implement errors.go**

```go
// internal/daemon/errors.go
package daemon

import (
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/huanghao/app-nanny/internal/config"
)

var http5xxRe = regexp.MustCompile(`\b5\d{2}\b`)

// ErrorEvent is one captured error occurrence with surrounding context lines.
type ErrorEvent struct {
	Time  time.Time `json:"time"`
	Key   string    `json:"key"`
	Lines []string  `json:"lines"` // up to 35 lines of context
}

// ErrorRing is a thread-safe circular buffer holding the last 50 error events.
type ErrorRing struct {
	mu     sync.Mutex
	events [50]ErrorEvent
	pos    int
	count  int
}

// NewErrorRing returns an empty ErrorRing.
func NewErrorRing() *ErrorRing { return &ErrorRing{} }

// Add inserts an event into the ring.
func (r *ErrorRing) Add(e ErrorEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events[r.pos] = e
	r.pos = (r.pos + 1) % 50
	if r.count < 50 {
		r.count++
	}
}

// RecentForKey returns up to n recent events matching key.
// key="" returns all events; key="proj" matches "proj" and "proj/process".
func (r *ErrorRing) RecentForKey(key string, n int) []ErrorEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []ErrorEvent
	for i := 0; i < r.count && len(out) < n; i++ {
		idx := (r.pos - 1 - i + 50) % 50
		e := r.events[idx]
		if key == "" || e.Key == key || strings.HasPrefix(e.Key, key+"/") {
			out = append(out, e)
		}
	}
	return out
}

// MatchesError reports whether line should trigger error capture.
// Built-in patterns cover HTTP 5xx, Python tracebacks, JS/Go errors.
// Extra custom patterns from app-nanny.toml [[error_patterns]] are also checked.
func MatchesError(line string, extra []config.ErrorPattern) bool {
	if http5xxRe.MatchString(line) {
		return true
	}
	triggers := []string{
		"Traceback (most recent call last)",
		"Error:",
		"TypeError:",
		"ReferenceError:",
		"SyntaxError:",
		"panic:",
		"FATAL",
		"CRITICAL",
	}
	upper := strings.ToUpper(line)
	for _, t := range triggers {
		if strings.Contains(line, t) || strings.Contains(upper, strings.ToUpper(t)) {
			return true
		}
	}
	for _, p := range extra {
		if p.Match != "" && strings.Contains(line, p.Match) {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run tests to confirm they pass**

```bash
go test ./internal/daemon/... -v -run "TestErrorRing|TestMatchesError" -timeout 10s
```

Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/daemon/errors.go internal/daemon/errors_test.go
git commit -m "feat: error ring and pattern matching"
```

---

## Task 3: Logger

**Files:**
- Create: `internal/daemon/logger.go`
- Create: `internal/daemon/logger_test.go`

- [ ] **Step 1: Write failing tests**

```go
// internal/daemon/logger_test.go
package daemon_test

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/huanghao/app-nanny/internal/daemon"
)

// nopCloser wraps an io.Writer with a no-op Close.
type nopCloser struct{ io.Writer }
func (nopCloser) Close() error { return nil }

func TestLogger_CapturesLines(t *testing.T) {
	var buf bytes.Buffer
	er := daemon.NewErrorRing()
	lg := daemon.NewLogger(nopCloser{&buf}, er, "svc", nil)

	lg.Write([]byte("line one\nline two\n")) //nolint:errcheck

	lines := lg.TailLines(10)
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 lines in ring, got %d", len(lines))
	}
	if !strings.Contains(lines[len(lines)-2], "line one") {
		t.Errorf("expected 'line one' in ring, got %v", lines)
	}
}

func TestLogger_HandlesPartialWrite(t *testing.T) {
	var buf bytes.Buffer
	er := daemon.NewErrorRing()
	lg := daemon.NewLogger(nopCloser{&buf}, er, "svc", nil)

	lg.Write([]byte("hel")) //nolint:errcheck
	lg.Write([]byte("lo\n")) //nolint:errcheck

	lines := lg.TailLines(5)
	found := false
	for _, l := range lines {
		if strings.Contains(l, "hello") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'hello' assembled from partial writes, ring=%v", lines)
	}
}

func TestLogger_DetectsError(t *testing.T) {
	var buf bytes.Buffer
	er := daemon.NewErrorRing()
	lg := daemon.NewLogger(nopCloser{&buf}, er, "svc", nil)

	lg.Write([]byte("GET /api 500 3ms\n")) //nolint:errcheck

	events := er.RecentForKey("svc", 5)
	if len(events) == 0 {
		t.Error("expected error event after 500 line, got none")
	}
}

func TestLogger_RingCapAt500(t *testing.T) {
	var buf bytes.Buffer
	er := daemon.NewErrorRing()
	lg := daemon.NewLogger(nopCloser{&buf}, er, "svc", nil)

	for i := 0; i < 600; i++ {
		lg.Write([]byte("line\n")) //nolint:errcheck
	}

	lines := lg.TailLines(1000)
	if len(lines) > 500 {
		t.Errorf("ring should cap at 500, got %d", len(lines))
	}
}
```

- [ ] **Step 2: Run to confirm they fail**

```bash
go test ./internal/daemon/... -v -run TestLogger -timeout 10s
```

Expected: compile error — `NewLogger` not defined.

- [ ] **Step 3: Implement logger.go**

```go
// internal/daemon/logger.go
package daemon

import (
	"bytes"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/huanghao/app-nanny/internal/config"
)

const ringCap = 500

// Logger is an io.Writer that:
//   - writes timestamped lines to a backing writer (typically a RotatingFile)
//   - keeps the last 500 lines in a ring buffer for `nanny logs`
//   - detects error patterns and records events to an ErrorRing
type Logger struct {
	mu       sync.Mutex
	out      io.WriteCloser
	errRing  *ErrorRing
	key      string
	patterns []config.ErrorPattern

	buf []byte // partial line accumulator

	ring    [ringCap]string
	ringPos int
	ringLen int
}

// NewLogger creates a Logger that writes to out.
func NewLogger(out io.WriteCloser, errRing *ErrorRing, key string, patterns []config.ErrorPattern) *Logger {
	return &Logger{out: out, errRing: errRing, key: key, patterns: patterns}
}

// Write implements io.Writer. It splits p into lines and processes each one.
func (l *Logger) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	data := append(l.buf, p...)
	l.buf = nil

	for {
		idx := bytes.IndexByte(data, '\n')
		if idx < 0 {
			break
		}
		line := string(data[:idx])
		data = data[idx+1:]
		l.processLineLocked(line)
	}
	if len(data) > 0 {
		l.buf = append(l.buf, data...)
	}
	return len(p), nil
}

func (l *Logger) processLineLocked(line string) {
	ts := time.Now().Format("15:04:05")
	fmt.Fprintf(l.out, "%s %s\n", ts, line)

	// Add to ring
	l.ring[l.ringPos] = line
	l.ringPos = (l.ringPos + 1) % ringCap
	if l.ringLen < ringCap {
		l.ringLen++
	}

	// Check error patterns
	if MatchesError(line, l.patterns) {
		context := l.tailLocked(35)
		l.errRing.Add(ErrorEvent{
			Time:  time.Now(),
			Key:   l.key,
			Lines: context,
		})
	}
}

// tailLocked returns the last n lines from the ring. Caller must hold l.mu.
func (l *Logger) tailLocked(n int) []string {
	if l.ringLen == 0 {
		return nil
	}
	actual := n
	if actual > l.ringLen {
		actual = l.ringLen
	}
	out := make([]string, actual)
	for i := 0; i < actual; i++ {
		idx := (l.ringPos - actual + i + ringCap) % ringCap
		out[i] = l.ring[idx]
	}
	return out
}

// TailLines returns the last n lines from the in-memory ring buffer.
func (l *Logger) TailLines(n int) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.tailLocked(n)
}

// Close flushes any partial line and closes the backing writer.
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.buf) > 0 {
		l.processLineLocked(string(l.buf))
		l.buf = nil
	}
	return l.out.Close()
}
```

- [ ] **Step 4: Run tests to confirm they pass**

```bash
go test ./internal/daemon/... -v -run TestLogger -timeout 10s
```

Expected: all 4 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/daemon/logger.go internal/daemon/logger_test.go
git commit -m "feat: logger with ring buffer and error detection"
```

---

## Task 4: Metrics (RSS Sampling)

**Files:**
- Create: `internal/daemon/metrics.go`
- Create: `internal/daemon/metrics_test.go`

- [ ] **Step 1: Write failing tests**

```go
// internal/daemon/metrics_test.go
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
```

- [ ] **Step 2: Run to confirm they fail**

```bash
go test ./internal/daemon/... -v -run TestMetrics -timeout 10s
```

Expected: compile error.

- [ ] **Step 3: Implement metrics.go**

```go
// internal/daemon/metrics.go
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
	MemMB     float64 // resident set size in MiB
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
// A zero pid is silently ignored (sets MemMB=0).
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
// Returns 0 on any error or if pid==0.
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
```

- [ ] **Step 4: Run tests to confirm they pass**

```bash
go test ./internal/daemon/... -v -run TestMetrics -timeout 10s
```

Expected: all 3 PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/daemon/metrics.go internal/daemon/metrics_test.go
git commit -m "feat: RSS metrics sampling"
```

---

## Task 5: Wire Logger and Metrics into Process + Manager

**Files:**
- Modify: `internal/daemon/process.go`
- Modify: `internal/daemon/manager.go`
- Modify: `internal/daemon/daemon.go`

- [ ] **Step 1: Add `SetStdio` to Process**

In `internal/daemon/process.go`, add one field and one method:

```go
// In the Process struct (after the onCrash field):
stdioW io.Writer // stdout+stderr are both piped here when set

// New method (add after SetOnCrash):
// SetStdio sets the writer that receives all stdout and stderr output.
// Must be called before Start().
func (p *Process) SetStdio(w io.Writer) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stdioW = w
}
```

Also add `"io"` to the import block at the top of `process.go`.

In `Start()`, after `cmd.SysProcAttr = ...` and before `cmd.Start()`, add:

```go
if p.stdioW != nil {
    cmd.Stdout = p.stdioW
    cmd.Stderr = p.stdioW
}
```

- [ ] **Step 2: Add Logger/Metrics fields to Manager**

Replace the current `NewManager` signature and struct in `internal/daemon/manager.go`:

```go
// Add to imports: "os", "path/filepath", "io"  (io is likely already imported)

type Manager struct {
	mu        sync.Mutex
	registry  *config.Registry
	runtime   *Runtime
	processes map[string]*Process
	configs   map[string]*config.ProjectConfig
	logDir    string
	loggers   map[string]*Logger
	errRing   *ErrorRing
	metrics   *Metrics
}

func NewManager(reg *config.Registry, rt *Runtime, logDir string) *Manager {
	m := &Manager{
		registry:  reg,
		runtime:   rt,
		processes: make(map[string]*Process),
		configs:   make(map[string]*config.ProjectConfig),
		logDir:    logDir,
		loggers:   make(map[string]*Logger),
		errRing:   NewErrorRing(),
		metrics:   NewMetrics(),
	}
	m.startMetricsLoop()
	return m
}

// startMetricsLoop samples RSS for all running processes every 15 seconds.
func (m *Manager) startMetricsLoop() {
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			m.mu.Lock()
			type kp struct{ key string; pid int }
			var running []kp
			for key, proc := range m.processes {
				if proc.Status() == StatusRunning {
					running = append(running, kp{key, proc.PID()})
				}
			}
			m.mu.Unlock()
			for _, r := range running {
				m.metrics.Update(r.key, r.pid)
			}
		}
	}()
}
```

- [ ] **Step 3: Add helpers to Manager**

Add these methods to `internal/daemon/manager.go`:

```go
// logPath returns the log file path for a given process key.
func (m *Manager) logPath(key string) string {
	// Replace "/" with "-" for filesystem safety
	sanitized := strings.ReplaceAll(key, "/", "-")
	return filepath.Join(m.logDir, sanitized+".log")
}

// LogPath returns the log file path for a key (exported for IPC handler).
func (m *Manager) LogPath(key string) string {
	return m.logPath(key)
}

// LogLines returns the last n lines from the in-memory ring buffer for key.
func (m *Manager) LogLines(key string, n int) []string {
	m.mu.Lock()
	logger, ok := m.loggers[key]
	m.mu.Unlock()
	if !ok {
		return nil
	}
	return logger.TailLines(n)
}

// RecentErrors returns the most recent error events for key (or all if key="").
func (m *Manager) RecentErrors(key string, n int) []ErrorEvent {
	return m.errRing.RecentForKey(key, n)
}
```

- [ ] **Step 4: Create logger in startModeA and startModeB**

In `startModeA`, after `proc := NewProcess(...)` and before `proc.SetEnv(...)`, add:

```go
if err := os.MkdirAll(m.logDir, 0755); err == nil {
    if rf, err := NewRotatingFile(m.logPath(name), 50*1024*1024, 3); err == nil {
        lg := NewLogger(rf, m.errRing, name, cfg.ErrorPatterns)
        proc.SetStdio(lg)
        m.mu.Lock()
        m.loggers[name] = lg
        m.mu.Unlock()
    }
}
```

In `startModeB`, after `proc := NewProcess(key, pCfg, workDir)` and before `proc.SetEnv(...)`, add:

```go
if err := os.MkdirAll(m.logDir, 0755); err == nil {
    if rf, err := NewRotatingFile(m.logPath(key), 50*1024*1024, 3); err == nil {
        lg := NewLogger(rf, m.errRing, key, cfg.ErrorPatterns)
        proc.SetStdio(lg)
        m.mu.Lock()
        m.loggers[key] = lg
        m.mu.Unlock()
    }
}
```

Also add `"os"` to the imports of `manager.go` if not already present.

Also clean up the logger on Remove: inside `Manager.Remove`, after deleting from `m.processes`, add:

```go
for key := range m.loggers {
    if strings.HasPrefix(key, name) {
        delete(m.loggers, key)
    }
}
```

- [ ] **Step 5: Update daemon.go to pass logDir**

In `internal/daemon/daemon.go`, change:

```go
// Old:
mgr := NewManager(reg, rt)

// New:
logDir := filepath.Join(dataDir, "logs")
mgr := NewManager(reg, rt, logDir)
```

Also add `metrics` info to `PS()` — update the `ipc.ProcessInfo` builder in `Manager.PS()`:

```go
snap := m.metrics.Get(key)
out = append(out, ipc.ProcessInfo{
    Project:     project,
    Process:     process,
    Status:      string(proc.Status()),
    PID:         proc.PID(),
    Uptime:      uptime,
    Restarts:    proc.Restarts(),
    ActualPorts: actualPorts,
    MemMB:       snap.MemMB,   // ← add this
})
```

Add `MemMB float64 json:"mem_mb"` to `ipc.ProcessInfo` in `internal/ipc/types.go`.

- [ ] **Step 6: Fix manager_test.go — update NewManager calls**

The test calls `daemon.NewManager(reg, rt)`. Update to `daemon.NewManager(reg, rt, t.TempDir())` in all three test setups.

- [ ] **Step 7: Verify everything compiles and tests pass**

```bash
go test ./... -timeout 30s
```

Expected: all packages compile and all tests pass.

- [ ] **Step 8: Commit**

```bash
git add internal/daemon/process.go internal/daemon/manager.go internal/daemon/daemon.go internal/ipc/types.go internal/daemon/manager_test.go
git commit -m "feat: wire log capture and metrics into process manager"
```

---

## Task 6: IPC Types for Observability

**Files:**
- Modify: `internal/ipc/types.go`

- [ ] **Step 1: Add observability types to types.go**

Append the following to `internal/ipc/types.go`:

```go
// --- Observability params ---

type LogsParams struct {
	Name    string `json:"name"`
	Process string `json:"process,omitempty"`
	Lines   int    `json:"lines"` // 0 = default 100
}

type LogsResult struct {
	Lines []string `json:"lines"`
	Path  string   `json:"path"` // log file path for follow mode
}

type ErrorsParams struct {
	Name    string `json:"name"`
	Process string `json:"process,omitempty"`
	Last    bool   `json:"last"` // return only the most recent event
}

type ErrorsResult struct {
	Events []ErrorEvent `json:"events"`
}

// ErrorEvent mirrors daemon.ErrorEvent for JSON transport.
type ErrorEvent struct {
	Time  string   `json:"time"`
	Key   string   `json:"key"`
	Lines []string `json:"lines"`
}

// StatusResult is the response to "status <name>".
type StatusResult struct {
	Processes []ProcessStatus `json:"processes"`
}

// ProcessStatus is the detailed view of one process.
type ProcessStatus struct {
	Key         string  `json:"key"`
	Status      string  `json:"status"`
	PID         int     `json:"pid"`
	Uptime      string  `json:"uptime"`
	Restarts    int     `json:"restarts"`
	MemMB       float64 `json:"mem_mb"`
	ActualPorts []int   `json:"actual_ports"`
	ErrorCount  int     `json:"error_count"` // events in ring for this key
	LogPath     string  `json:"log_path"`
}
```

- [ ] **Step 2: Verify it compiles**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add internal/ipc/types.go
git commit -m "feat: IPC types for logs/errors/status"
```

---

## Task 7: Daemon IPC Handlers

**Files:**
- Modify: `internal/daemon/daemon.go`
- Modify: `internal/daemon/manager.go` (add DetailedStatus)

- [ ] **Step 1: Add DetailedStatus to Manager**

Add to `internal/daemon/manager.go`:

```go
// DetailedStatus returns per-process status for a named project.
func (m *Manager) DetailedStatus(projectName string) ipc.StatusResult {
	m.mu.Lock()
	defer m.mu.Unlock()

	var statuses []ipc.ProcessStatus
	for key, proc := range m.processes {
		parts := strings.SplitN(key, "/", 2)
		if parts[0] != projectName {
			continue
		}
		snap := m.metrics.Get(key)
		errCount := len(m.errRing.RecentForKey(key, 50))
		uptime := ""
		if proc.Status() == StatusRunning {
			uptime = formatDuration(time.Since(proc.StartedAt()))
		}
		statuses = append(statuses, ipc.ProcessStatus{
			Key:         key,
			Status:      string(proc.Status()),
			PID:         proc.PID(),
			Uptime:      uptime,
			Restarts:    proc.Restarts(),
			MemMB:       snap.MemMB,
			ActualPorts: ActualPorts(proc.PID()),
			ErrorCount:  errCount,
			LogPath:     m.logPath(key),
		})
	}
	return ipc.StatusResult{Processes: statuses}
}
```

- [ ] **Step 2: Add IPC handlers in daemon.go**

In `registerHandlers`, add these handlers before the closing brace:

```go
srv.Handle("logs", func(params json.RawMessage) (any, error) {
    var p ipc.LogsParams
    if err := json.Unmarshal(params, &p); err != nil {
        return nil, err
    }
    key := p.Name
    if p.Process != "" {
        key = p.Name + "/" + p.Process
    }
    n := p.Lines
    if n <= 0 {
        n = 100
    }
    return ipc.LogsResult{
        Lines: mgr.LogLines(key, n),
        Path:  mgr.LogPath(key),
    }, nil
})

srv.Handle("errors", func(params json.RawMessage) (any, error) {
    var p ipc.ErrorsParams
    if err := json.Unmarshal(params, &p); err != nil {
        return nil, err
    }
    key := p.Name
    if p.Process != "" {
        key = p.Name + "/" + p.Process
    }
    n := 10
    if p.Last {
        n = 1
    }
    raw := mgr.RecentErrors(key, n)
    events := make([]ipc.ErrorEvent, len(raw))
    for i, e := range raw {
        events[i] = ipc.ErrorEvent{
            Time:  e.Time.Format("15:04:05"),
            Key:   e.Key,
            Lines: e.Lines,
        }
    }
    return ipc.ErrorsResult{Events: events}, nil
})

srv.Handle("status", func(params json.RawMessage) (any, error) {
    var p ipc.StatusParams
    if err := json.Unmarshal(params, &p); err != nil {
        return nil, err
    }
    return mgr.DetailedStatus(p.Name), nil
})
```

- [ ] **Step 3: Verify it compiles and all tests pass**

```bash
go test ./... -timeout 30s
```

Expected: all pass.

- [ ] **Step 4: Commit**

```bash
git add internal/daemon/daemon.go internal/daemon/manager.go
git commit -m "feat: logs/errors/status IPC handlers"
```

---

## Task 8: CLI — nanny logs

**Files:**
- Create: `cmd/logs.go`

- [ ] **Step 1: Implement logs.go**

```go
// cmd/logs.go
package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/huanghao/app-nanny/internal/ipc"
	"github.com/spf13/cobra"
)

var logsFollow bool
var logsLines int

var logsCmd = &cobra.Command{
	Use:   "logs <name>[/process]",
	Short: "Show captured stdout/stderr logs",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		project, process := splitTarget(args[0])

		client := ipc.NewClient(SocketPath())
		resp, err := client.Call("logs", ipc.LogsParams{
			Name:    project,
			Process: process,
			Lines:   logsLines,
		})
		if err != nil {
			return err
		}
		var result ipc.LogsResult
		if err := json.Unmarshal(resp.Result, &result); err != nil {
			return err
		}

		// Print historical lines
		for _, l := range result.Lines {
			fmt.Println(l)
		}

		if !logsFollow {
			return nil
		}

		// Follow mode: tail the log file directly
		if result.Path == "" {
			return fmt.Errorf("no log file path returned by daemon")
		}
		return tailFollow(result.Path)
	},
}

func init() {
	logsCmd.Flags().BoolVarP(&logsFollow, "follow", "f", false, "Follow log output")
	logsCmd.Flags().IntVarP(&logsLines, "lines", "n", 100, "Number of historical lines to show")
	rootCmd.AddCommand(logsCmd)
}

// tailFollow polls the file at path for new content and writes it to stdout.
// This is a simple tail-f implementation: seek to end after printing history,
// then poll every 200ms for new bytes.
func tailFollow(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open log file %s: %w", path, err)
	}
	defer f.Close()

	// Seek to end of file (history already printed above)
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		return err
	}

	buf := make([]byte, 4096)
	for {
		n, err := f.Read(buf)
		if n > 0 {
			os.Stdout.Write(buf[:n]) //nolint:errcheck
		}
		if err == io.EOF {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		if err != nil {
			return err
		}
	}
}
```

- [ ] **Step 2: Verify it compiles**

```bash
go build ./...
```

- [ ] **Step 3: Commit**

```bash
git add cmd/logs.go
git commit -m "feat: nanny logs with follow mode"
```

---

## Task 9: CLI — nanny errors

**Files:**
- Create: `cmd/errors.go`

- [ ] **Step 1: Implement errors.go**

```go
// cmd/errors.go
package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/huanghao/app-nanny/internal/ipc"
	"github.com/spf13/cobra"
)

var errorsLast bool
var errorsCopy bool

var errorsCmd = &cobra.Command{
	Use:   "errors <name>[/process]",
	Short: "Show captured error events",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		project, process := splitTarget(args[0])

		client := ipc.NewClient(SocketPath())
		resp, err := client.Call("errors", ipc.ErrorsParams{
			Name:    project,
			Process: process,
			Last:    errorsLast,
		})
		if err != nil {
			return err
		}
		var result ipc.ErrorsResult
		if err := json.Unmarshal(resp.Result, &result); err != nil {
			return err
		}

		if len(result.Events) == 0 {
			fmt.Println("no error events recorded")
			return nil
		}

		var sb strings.Builder
		for _, e := range result.Events {
			fmt.Fprintf(&sb, "── %s [%s] ──\n", e.Time, e.Key)
			for _, l := range e.Lines {
				fmt.Fprintf(&sb, "  %s\n", l)
			}
			fmt.Fprintln(&sb)
		}

		output := sb.String()
		fmt.Print(output)

		if errorsCopy {
			return copyToClipboard(output)
		}
		return nil
	},
}

func init() {
	errorsCmd.Flags().BoolVar(&errorsLast, "last", false, "Show only the most recent error event")
	errorsCmd.Flags().BoolVar(&errorsCopy, "copy", false, "Copy the last error event to clipboard (macOS pbcopy)")
	rootCmd.AddCommand(errorsCmd)
}

func copyToClipboard(text string) error {
	cmd := exec.Command("pbcopy")
	cmd.Stdin = strings.NewReader(text)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("pbcopy failed: %w", err)
	}
	fmt.Println("(copied to clipboard)")
	return nil
}
```

- [ ] **Step 2: Verify it compiles**

```bash
go build ./...
```

- [ ] **Step 3: Commit**

```bash
git add cmd/errors.go
git commit -m "feat: nanny errors with --last and --copy"
```

---

## Task 10: CLI — nanny status + Supplemental Tests

**Files:**
- Create: `cmd/status.go`
- Modify: `internal/daemon/manager_test.go` (add coverage)
- Modify: `internal/daemon/process_test.go` (add coverage)

- [ ] **Step 1: Implement status.go**

```go
// cmd/status.go
package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/huanghao/app-nanny/internal/ipc"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status <name>",
	Short: "Show detailed status for a project",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client := ipc.NewClient(SocketPath())
		resp, err := client.Call("status", ipc.StatusParams{Name: args[0]})
		if err != nil {
			return err
		}
		var result ipc.StatusResult
		if err := json.Unmarshal(resp.Result, &result); err != nil {
			return err
		}
		if len(result.Processes) == 0 {
			fmt.Printf("no processes found for %q\n", args[0])
			return nil
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "KEY\tSTATUS\tPID\tUPTIME\tMEM\tERRORS\tLOG")
		for _, p := range result.Processes {
			mem := fmt.Sprintf("%.0fM", p.MemMB)
			if p.MemMB == 0 {
				mem = "-"
			}
			fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%s\t%d\t%s\n",
				p.Key, p.Status, p.PID, p.Uptime, mem, p.ErrorCount, p.LogPath)
		}
		return w.Flush()
	},
}

func init() {
	rootCmd.AddCommand(statusCmd)
}
```

- [ ] **Step 2: Add supplemental process tests**

Add to `internal/daemon/process_test.go`:

```go
func TestProcess_StopIdempotent(t *testing.T) {
	// Stopping an already-stopped process should not error
	proc := daemon.NewProcess("idle", config.ProcessConfig{Command: "sleep 60"}, t.TempDir())
	if err := proc.Stop(); err != nil {
		t.Errorf("Stop on stopped process should return nil, got %v", err)
	}
}

func TestProcess_StartAlreadyRunning(t *testing.T) {
	proc := daemon.NewProcess("dup", config.ProcessConfig{Command: "sleep 60"}, t.TempDir())
	if err := proc.Start(); err != nil {
		t.Fatal(err)
	}
	defer proc.Stop()

	err := proc.Start()
	if err == nil {
		t.Error("starting an already-running process should return error")
	}
}
```

- [ ] **Step 3: Add supplemental manager tests**

Add to `internal/daemon/manager_test.go`:

```go
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
```

- [ ] **Step 4: Add supplemental config tests**

Add to `internal/config/project_test.go`:

```go
func TestLoadProject_DefaultMaxRestarts(t *testing.T) {
	path := writeToml(t, `name = "x"`)
	cfg, err := config.LoadProject(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MaxRestarts != 5 {
		t.Errorf("MaxRestarts = %d, want 5", cfg.MaxRestarts)
	}
}

func TestLoadProject_DefaultRestart(t *testing.T) {
	path := writeToml(t, `name = "x"`)
	cfg, err := config.LoadProject(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Restart != "on-failure" {
		t.Errorf("Restart = %q, want on-failure", cfg.Restart)
	}
}

func TestLoadProject_IsModeB(t *testing.T) {
	path := writeToml(t, `
name = "svc"
[processes.a]
command = "echo a"
port = 3000
`)
	cfg, _ := config.LoadProject(path)
	if !cfg.IsModeB() {
		t.Error("expected IsModeB=true with processes defined")
	}
}

func TestLoadProject_IsModeBFalseForModeA(t *testing.T) {
	path := writeToml(t, `
name = "svc"
[ports]
PORT = 8080
`)
	cfg, _ := config.LoadProject(path)
	if cfg.IsModeB() {
		t.Error("expected IsModeB=false for Mode A config")
	}
}
```

- [ ] **Step 5: Run all tests**

```bash
go test ./... -timeout 30s -v 2>&1 | tail -30
```

Expected: all tests PASS.

- [ ] **Step 6: Build and do a quick smoke test of new commands**

```bash
just build
./nanny daemon start
sleep 1
mkdir -p /tmp/nanny-log-test
cat > /tmp/nanny-log-test/app-nanny.toml << 'EOF'
name = "log-test"
command = "sh -c 'while true; do echo hello; sleep 1; done'"
EOF
./nanny add /tmp/nanny-log-test
./nanny start log-test
sleep 3
./nanny logs log-test -n 5
./nanny status log-test
./nanny stop log-test
./nanny daemon stop
rm -rf /tmp/nanny-log-test
```

Expected: `nanny logs` shows "hello" lines, `nanny status` shows MemMB > 0.

- [ ] **Step 7: Commit**

```bash
git add cmd/status.go internal/daemon/process_test.go internal/daemon/manager_test.go internal/config/project_test.go
git commit -m "feat: nanny status + supplemental tests"
```

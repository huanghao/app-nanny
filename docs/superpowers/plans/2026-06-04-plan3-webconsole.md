# app-nanny Plan 3: Web Console Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a browser-based dashboard at `http://localhost:7070` showing all services, real-time log streaming, and start/stop/restart controls — served directly by the daemon with no separate build step.

**Architecture:** The daemon's `Run()` launches a second goroutine serving HTTP on port 7070. Static files (one `index.html`) are embedded in the binary via Go `embed`. REST endpoints (`GET /api/ps`, `GET /api/status/:name`, `POST /api/:name/:action`) share the same `Manager` as IPC. Log streaming uses SSE (`GET /api/logs/:key/stream`). An `Origin` check middleware rejects cross-origin requests.

**Tech Stack:** Go standard library (`net/http`, `embed`), vanilla JavaScript (no framework). Requires Plan 2 to be implemented first (Manager must have loggers/errRing/metrics).

**Prerequisite:** Plan 2 must be complete (Manager has `LogLines`, `RecentErrors`, `DetailedStatus`, `LogPath`).

---

## File Map

```
internal/web/
  server.go              # HTTP server: NewServer, Start, origin middleware
  server_test.go
  handlers.go            # REST handlers: ps, status, start/stop/restart
  handlers_test.go
  sse.go                 # SSE log streaming endpoint
  sse_test.go
  static/
    index.html           # Web console (single file, embedded)
internal/daemon/
  daemon.go              # MODIFY: start HTTP server alongside IPC server
cmd/
  dashboard.go           # nanny dashboard — opens browser to localhost:7070
```

---

## Task 1: HTTP Server with Origin Check

**Files:**
- Create: `internal/web/server.go`
- Create: `internal/web/server_test.go`

- [ ] **Step 1: Write failing tests**

```go
// internal/web/server_test.go
package web_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/huanghao/app-nanny/internal/web"
)

func TestOriginCheck_AllowsLocalhost(t *testing.T) {
	handler := web.OriginMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for _, origin := range []string{"http://localhost:7070", "http://127.0.0.1:7070", ""} {
		req := httptest.NewRequest("GET", "/", nil)
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		req.Host = "localhost:7070"
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Errorf("origin %q: expected 200, got %d", origin, rr.Code)
		}
	}
}

func TestOriginCheck_BlocksCrossOrigin(t *testing.T) {
	handler := web.OriginMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/api/start", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	req.Host = "localhost:7070"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403 for cross-origin POST, got %d", rr.Code)
	}
}

func TestOriginCheck_AllowsCrossOriginGET(t *testing.T) {
	// GET requests are read-only; allow even with foreign Origin for browser navigation
	handler := web.OriginMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/ps", nil)
	req.Header.Set("Origin", "https://other.example.com")
	req.Host = "localhost:7070"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 for cross-origin GET, got %d", rr.Code)
	}
}
```

- [ ] **Step 2: Run to confirm they fail**

```bash
go test ./internal/web/... -v -run TestOriginCheck -timeout 10s
```

Expected: compile error — package not found.

- [ ] **Step 3: Implement server.go**

```go
// internal/web/server.go
package web

import (
	"log"
	"net/http"
	"strings"
)

// OriginMiddleware rejects cross-origin mutating requests (non-GET/HEAD).
// This prevents malicious web pages from controlling local services via CSRF.
// GET/HEAD requests are allowed from any origin (read-only, no side effects).
func OriginMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && r.Method != http.MethodGet && r.Method != http.MethodHead {
			if !isLocalOrigin(origin) {
				http.Error(w, "forbidden: cross-origin request", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func isLocalOrigin(origin string) bool {
	return strings.HasPrefix(origin, "http://localhost") ||
		strings.HasPrefix(origin, "http://127.0.0.1") ||
		strings.HasPrefix(origin, "http://[::1]")
}

// Server is the HTTP server serving the web console.
type Server struct {
	addr    string
	handler http.Handler
}

// NewServer constructs a Server with the given handler and address.
func NewServer(addr string, handler http.Handler) *Server {
	return &Server{addr: addr, handler: OriginMiddleware(handler)}
}

// Start listens and serves in the foreground. Returns when the server stops.
func (s *Server) Start() error {
	log.Printf("web: listening on %s", s.addr)
	return http.ListenAndServe(s.addr, s.handler)
}
```

- [ ] **Step 4: Run tests to confirm they pass**

```bash
go test ./internal/web/... -v -run TestOriginCheck -timeout 10s
```

Expected: all 3 PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/web/server.go internal/web/server_test.go
git commit -m "feat: HTTP server with Origin middleware"
```

---

## Task 2: REST API Handlers

**Files:**
- Create: `internal/web/handlers.go`
- Create: `internal/web/handlers_test.go`

The handlers need a `ManagerIface` interface so tests can use a stub without starting a real daemon.

- [ ] **Step 1: Write failing tests**

```go
// internal/web/handlers_test.go
package web_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/huanghao/app-nanny/internal/ipc"
	"github.com/huanghao/app-nanny/internal/web"
)

// stubManager implements web.ManagerIface with hard-coded data.
type stubManager struct {
	psResult     []ipc.ProcessInfo
	startErr     error
	stopErr      error
	restartErr   error
}

func (s *stubManager) PS() []ipc.ProcessInfo               { return s.psResult }
func (s *stubManager) Start(n, p string) error             { return s.startErr }
func (s *stubManager) Stop(n, p string) error              { return s.stopErr }
func (s *stubManager) Restart(n, p string) error           { return s.restartErr }
func (s *stubManager) LogLines(key string, n int) []string { return []string{"line1", "line2"} }

func TestHandlePS(t *testing.T) {
	stub := &stubManager{psResult: []ipc.ProcessInfo{
		{Project: "myapp", Status: "running", PID: 1234},
	}}
	mux := web.NewMux(stub)

	req := httptest.NewRequest("GET", "/api/ps", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var result ipc.PSResult
	if err := json.Unmarshal(rr.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Processes) != 1 || result.Processes[0].Project != "myapp" {
		t.Errorf("unexpected ps result: %+v", result)
	}
}

func TestHandleAction_Start(t *testing.T) {
	stub := &stubManager{}
	mux := web.NewMux(stub)

	req := httptest.NewRequest("POST", "/api/myapp/start", strings.NewReader(""))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHandleAction_UnknownAction(t *testing.T) {
	stub := &stubManager{}
	mux := web.NewMux(stub)

	req := httptest.NewRequest("POST", "/api/myapp/explode", strings.NewReader(""))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for unknown action, got %d", rr.Code)
	}
}
```

- [ ] **Step 2: Run to confirm they fail**

```bash
go test ./internal/web/... -v -run TestHandle -timeout 10s
```

Expected: compile error — `NewMux` not defined.

- [ ] **Step 3: Implement handlers.go**

```go
// internal/web/handlers.go
package web

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/huanghao/app-nanny/internal/ipc"
)

// ManagerIface is the subset of daemon.Manager needed by the web handlers.
type ManagerIface interface {
	PS() []ipc.ProcessInfo
	Start(projectName, processName string) error
	Stop(projectName, processName string) error
	Restart(projectName, processName string) error
	LogLines(key string, n int) []string
}

// NewMux returns an http.ServeMux with all web console API routes registered.
func NewMux(mgr ManagerIface) *http.ServeMux {
	mux := http.NewServeMux()

	// GET /api/ps → list all processes
	mux.HandleFunc("/api/ps", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, ipc.PSResult{Processes: mgr.PS()})
	})

	// POST /api/<name>[/process]/start|stop|restart
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// Path: /api/<name>/action  or  /api/<name>/<process>/action
		path := strings.TrimPrefix(r.URL.Path, "/api/")
		parts := strings.Split(path, "/")

		var project, process, action string
		switch len(parts) {
		case 2: // /api/name/action
			project, action = parts[0], parts[1]
		case 3: // /api/name/process/action
			project, process, action = parts[0], parts[1], parts[2]
		default:
			http.Error(w, "invalid path", http.StatusBadRequest)
			return
		}

		var err error
		switch action {
		case "start":
			err = mgr.Start(project, process)
		case "stop":
			err = mgr.Stop(project, process)
		case "restart":
			err = mgr.Restart(project, process)
		default:
			http.Error(w, "unknown action: "+action, http.StatusBadRequest)
			return
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]string{"ok": "true"})
	})

	return mux
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}
```

- [ ] **Step 4: Run tests to confirm they pass**

```bash
go test ./internal/web/... -v -run TestHandle -timeout 10s
```

Expected: all 3 PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/web/handlers.go internal/web/handlers_test.go
git commit -m "feat: web console REST API handlers"
```

---

## Task 3: SSE Log Streaming

**Files:**
- Create: `internal/web/sse.go`
- Create: `internal/web/sse_test.go`

- [ ] **Step 1: Write failing tests**

```go
// internal/web/sse_test.go
package web_test

import (
	"bufio"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/huanghao/app-nanny/internal/web"
)

func TestSSEHandler_StreamsLines(t *testing.T) {
	// The SSE handler polls LogLines every 200ms and writes new lines as events.
	// We use a stub that returns 2 lines and a ResponseRecorder that supports flushing.
	stub := &stubManager{}

	req := httptest.NewRequest("GET", "/api/logs/myapp/stream", nil)
	rr := httptest.NewRecorder()

	// Run the SSE handler in a goroutine and cancel after short time
	done := make(chan struct{})
	go func() {
		defer close(done)
		web.SSELogsHandler(stub, "myapp", rr, req)
	}()

	// Give the handler time to emit at least one event
	time.Sleep(500 * time.Millisecond)

	// SSE format: "data: <line>\n\n"
	body := rr.Body.String()
	if !strings.Contains(body, "data:") {
		t.Errorf("expected SSE data lines, got: %q", body)
	}
}

func TestSSEHeaders(t *testing.T) {
	stub := &stubManager{}
	req := httptest.NewRequest("GET", "/api/logs/myapp/stream", nil)
	rr := httptest.NewRecorder()

	go web.SSELogsHandler(stub, "myapp", rr, req)
	time.Sleep(50 * time.Millisecond)

	ct := rr.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/event-stream") {
		t.Errorf("expected Content-Type text/event-stream, got %q", ct)
	}
}

// Verify SSE lines are well-formed (each data line followed by blank line).
func TestSSEFormat(t *testing.T) {
	stub := &stubManager{}
	req := httptest.NewRequest("GET", "/api/logs/x/stream", nil)
	rr := httptest.NewRecorder()

	go web.SSELogsHandler(stub, "x", rr, req)
	time.Sleep(400 * time.Millisecond)

	scanner := bufio.NewScanner(strings.NewReader(rr.Body.String()))
	for scanner.Scan() {
		line := scanner.Text()
		if line != "" && !strings.HasPrefix(line, "data:") && !strings.HasPrefix(line, ":") {
			t.Errorf("unexpected SSE line: %q", line)
		}
	}
}
```

- [ ] **Step 2: Run to confirm they fail**

```bash
go test ./internal/web/... -v -run TestSSE -timeout 10s
```

Expected: compile error — `SSELogsHandler` not defined.

- [ ] **Step 3: Implement sse.go**

```go
// internal/web/sse.go
package web

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

// SSELogsHandler streams log lines for key as Server-Sent Events.
// It polls LogLines every 200ms and emits new lines as they arrive.
// The stream ends when the client disconnects.
func SSELogsHandler(mgr ManagerIface, key string, w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Send a keep-alive comment immediately so the browser sees the connection
	fmt.Fprintf(w, ": connected\n\n")
	flusher.Flush()

	ctx := r.Context()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	var sent int // how many lines we've sent so far

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			lines := mgr.LogLines(key, 500)
			if len(lines) > sent {
				for _, line := range lines[sent:] {
					escaped := strings.ReplaceAll(line, "\n", " ")
					fmt.Fprintf(w, "data: %s\n\n", escaped)
				}
				sent = len(lines)
				flusher.Flush()
			}
		}
	}
}

// RegisterSSERoute adds the /api/logs/:key/stream SSE route to mux.
func RegisterSSERoute(mux *http.ServeMux, mgr ManagerIface) {
	mux.HandleFunc("/api/logs/", func(w http.ResponseWriter, r *http.Request) {
		// Path: /api/logs/<key>/stream
		path := strings.TrimPrefix(r.URL.Path, "/api/logs/")
		path = strings.TrimSuffix(path, "/stream")
		key := strings.ReplaceAll(path, "/", "-") // normalize "proj/proc" → "proj-proc"
		if key == "" {
			http.Error(w, "missing key", http.StatusBadRequest)
			return
		}
		SSELogsHandler(mgr, key, w, r)
	})
}
```

- [ ] **Step 4: Run tests to confirm they pass**

```bash
go test ./internal/web/... -v -run TestSSE -timeout 10s
```

Expected: all 3 PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/web/sse.go internal/web/sse_test.go
git commit -m "feat: SSE log streaming endpoint"
```

---

## Task 4: Static Web Console

**Files:**
- Create: `internal/web/static/index.html`
- Create: `internal/web/embed.go`

- [ ] **Step 1: Create the HTML/JS console**

Create `internal/web/static/index.html`:

```html
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <title>nanny</title>
  <style>
    body { font-family: monospace; background: #1a1a1a; color: #d4d4d4; margin: 0; padding: 0; }
    header { background: #252526; padding: 12px 20px; border-bottom: 1px solid #3c3c3c; display: flex; align-items: center; gap: 12px; }
    header h1 { margin: 0; font-size: 16px; color: #9cdcfe; }
    #status-dot { width: 8px; height: 8px; border-radius: 50%; background: #4ec9b0; display: inline-block; }
    table { width: 100%; border-collapse: collapse; font-size: 13px; }
    th { text-align: left; padding: 8px 12px; background: #252526; color: #858585; font-weight: normal; border-bottom: 1px solid #3c3c3c; }
    td { padding: 6px 12px; border-bottom: 1px solid #2d2d2d; }
    tr:hover td { background: #2a2a2a; }
    .running { color: #4ec9b0; }
    .stopped { color: #858585; }
    .crashed { color: #f48771; }
    .btn { background: #3c3c3c; border: none; color: #d4d4d4; padding: 3px 10px; cursor: pointer; border-radius: 3px; font-family: monospace; font-size: 12px; margin: 0 2px; }
    .btn:hover { background: #505050; }
    #log-panel { position: fixed; bottom: 0; left: 0; right: 0; height: 240px; background: #1e1e1e; border-top: 1px solid #3c3c3c; display: flex; flex-direction: column; }
    #log-header { background: #252526; padding: 6px 12px; font-size: 12px; color: #858585; display: flex; justify-content: space-between; }
    #log-body { flex: 1; overflow-y: auto; padding: 8px 12px; font-size: 12px; line-height: 1.6; }
    #log-body .err { color: #f48771; }
    .main { padding-bottom: 260px; }
  </style>
</head>
<body>
<header>
  <span id="status-dot"></span>
  <h1>nanny</h1>
  <span id="daemon-status" style="color:#858585;font-size:12px">loading...</span>
</header>
<div class="main">
  <table id="proc-table">
    <thead><tr>
      <th>PROJECT</th><th>PROCESS</th><th>STATUS</th><th>PID</th>
      <th>UPTIME</th><th>MEM</th><th>PORTS</th><th>ACTIONS</th>
    </tr></thead>
    <tbody id="proc-body"></tbody>
  </table>
</div>
<div id="log-panel">
  <div id="log-header">
    <span id="log-title">logs — select a service</span>
    <span><button class="btn" onclick="clearLog()">clear</button></span>
  </div>
  <div id="log-body"></div>
</div>
<script>
let eventSource = null;
let logKey = null;

async function loadPS() {
  try {
    const r = await fetch('/api/ps');
    const data = await r.json();
    const tbody = document.getElementById('proc-body');
    tbody.innerHTML = '';
    (data.processes || []).forEach(p => {
      const key = p.process ? p.project + '/' + p.process : p.project;
      const statusClass = p.status;
      const mem = p.mem_mb > 0 ? p.mem_mb.toFixed(0) + 'M' : '-';
      const ports = (p.actual_ports || []).join(',') || '-';
      const tr = document.createElement('tr');
      tr.innerHTML = `
        <td>${p.project}</td>
        <td>${p.process || '-'}</td>
        <td class="${statusClass}">${p.status}</td>
        <td>${p.pid || '-'}</td>
        <td>${p.uptime || '-'}</td>
        <td>${mem}</td>
        <td>${ports}</td>
        <td>
          <button class="btn" onclick="action('${key}','start')">▶</button>
          <button class="btn" onclick="action('${key}','stop')">■</button>
          <button class="btn" onclick="action('${key}','restart')">↺</button>
          <button class="btn" onclick="streamLogs('${key}')">logs</button>
        </td>`;
      tbody.appendChild(tr);
    });
    document.getElementById('daemon-status').textContent = 'daemon: running';
    document.getElementById('status-dot').style.background = '#4ec9b0';
  } catch(e) {
    document.getElementById('daemon-status').textContent = 'daemon: error';
    document.getElementById('status-dot').style.background = '#f48771';
  }
}

async function action(key, act) {
  const parts = key.split('/');
  const url = parts.length === 2
    ? `/api/${parts[0]}/${parts[1]}/${act}`
    : `/api/${key}/${act}`;
  await fetch(url, { method: 'POST' });
  setTimeout(loadPS, 500);
}

function streamLogs(key) {
  if (eventSource) eventSource.close();
  logKey = key;
  document.getElementById('log-title').textContent = 'logs — ' + key;
  const logBody = document.getElementById('log-body');
  logBody.innerHTML = '';

  // Fetch historical lines first
  fetch('/api/ps').then(() => {
    const sseKey = key.replace('/', '-');
    eventSource = new EventSource('/api/logs/' + sseKey + '/stream');
    eventSource.onmessage = e => {
      const line = document.createElement('div');
      const isErr = /\b5\d{2}\b|Error:|panic:|FATAL/i.test(e.data);
      if (isErr) line.className = 'err';
      line.textContent = e.data;
      logBody.appendChild(line);
      logBody.scrollTop = logBody.scrollHeight;
    };
  });
}

function clearLog() {
  document.getElementById('log-body').innerHTML = '';
}

// Refresh process table every 3 seconds
loadPS();
setInterval(loadPS, 3000);
</script>
</body>
</html>
```

- [ ] **Step 2: Create embed.go**

```go
// internal/web/embed.go
package web

import (
	"embed"
	"net/http"
)

//go:embed static
var staticFiles embed.FS

// StaticHandler returns an http.Handler serving the embedded static files.
// Files are served under / with the "static/" prefix stripped.
func StaticHandler() http.Handler {
	return http.FileServer(http.FS(staticFiles))
}
```

- [ ] **Step 3: Register static handler in NewMux**

In `internal/web/handlers.go`, at the end of `NewMux`, add:

```go
// Serve static files: /static/index.html → accessible at /
mux.Handle("/", StaticHandler())
```

And add a redirect at `/` → `/static/index.html`:

Actually, simpler: serve the static directory at `/static/` and redirect `/` to `/static/index.html`:

```go
// In NewMux, add:
mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
    if r.URL.Path == "/" {
        http.Redirect(w, r, "/static/index.html", http.StatusFound)
        return
    }
    http.NotFound(w, r)
})
mux.Handle("/static/", StaticHandler())
```

- [ ] **Step 4: Verify it compiles**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 5: Commit**

```bash
git add internal/web/static/index.html internal/web/embed.go internal/web/handlers.go
git commit -m "feat: embedded web console UI"
```

---

## Task 5: Wire HTTP Server into Daemon + nanny dashboard

**Files:**
- Modify: `internal/daemon/daemon.go`
- Create: `cmd/dashboard.go`
- Create: `internal/web/server_integration_test.go`

- [ ] **Step 1: Wire HTTP server into daemon.Run()**

In `internal/daemon/daemon.go`, add import:

```go
"github.com/huanghao/app-nanny/internal/web"
```

After `go srv.Serve(ln)` (the IPC server goroutine), add:

```go
// Start web console HTTP server
webMux := web.NewMux(mgr)
web.RegisterSSERoute(webMux, mgr)
webSrv := web.NewServer(":7070", webMux)
go func() {
    if err := webSrv.Start(); err != nil {
        log.Printf("web: server error: %v", err)
    }
}()
```

- [ ] **Step 2: Create cmd/dashboard.go**

```go
// cmd/dashboard.go
package cmd

import (
	"fmt"
	"os/exec"

	"github.com/spf13/cobra"
)

var dashboardCmd = &cobra.Command{
	Use:   "dashboard",
	Short: "Open the web console in the browser (http://localhost:7070)",
	RunE: func(cmd *cobra.Command, args []string) error {
		url := "http://localhost:7070/static/index.html"
		fmt.Printf("Opening %s\n", url)
		return exec.Command("open", url).Start()
	},
}

func init() {
	rootCmd.AddCommand(dashboardCmd)
}
```

- [ ] **Step 3: Write integration test for web server + manager**

```go
// internal/web/server_integration_test.go
package web_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/huanghao/app-nanny/internal/ipc"
	"github.com/huanghao/app-nanny/internal/web"
)

func TestFullStack_PSEndpoint(t *testing.T) {
	stub := &stubManager{psResult: []ipc.ProcessInfo{
		{Project: "demo", Status: "running", PID: 9999, MemMB: 32.5},
	}}
	mux := web.NewMux(stub)
	web.RegisterSSERoute(mux, stub)
	srv := httptest.NewServer(web.OriginMiddleware(mux))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/ps")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result ipc.PSResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if len(result.Processes) != 1 || result.Processes[0].Project != "demo" {
		t.Errorf("unexpected result: %+v", result)
	}
}

func TestFullStack_StartAction(t *testing.T) {
	stub := &stubManager{}
	mux := web.NewMux(stub)
	srv := httptest.NewServer(web.OriginMiddleware(mux))
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/demo/start", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestFullStack_StaticRedirect(t *testing.T) {
	stub := &stubManager{}
	mux := web.NewMux(stub)
	// Do NOT follow redirects for this test
	client := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := client.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		t.Errorf("expected 302 redirect from /, got %d", resp.StatusCode)
	}
}
```

- [ ] **Step 4: Run all tests**

```bash
go test ./... -timeout 30s 2>&1 | tail -20
```

Expected: all packages pass.

- [ ] **Step 5: Build and smoke test web console**

```bash
just build

# Start daemon (includes web server on :7070)
./nanny daemon start
sleep 1

# Verify web console is reachable
curl -s http://localhost:7070/api/ps | python3 -m json.tool

# Open dashboard (macOS)
./nanny dashboard

./nanny daemon stop
```

Expected: curl returns JSON with `processes` key. Browser opens to dashboard.

- [ ] **Step 6: Commit**

```bash
git add internal/daemon/daemon.go cmd/dashboard.go internal/web/server_integration_test.go
git commit -m "feat: wire web console into daemon, nanny dashboard command"
```

---

## Self-Review

- [x] **Origin check**: blocks cross-origin POST (Task 1), allows GET — covered by tests
- [x] **REST API**: GET /api/ps, POST /api/:name/:action — covered by handler tests
- [x] **SSE streaming**: text/event-stream, data: format, polls LogLines — covered by sse tests
- [x] **Static files**: index.html embedded via `//go:embed`, served at /static/ — compiles
- [x] **Dashboard command**: opens browser to :7070 — implemented
- [x] **Daemon integration**: HTTP server goroutine added to daemon.Run() — wired
- [x] **ManagerIface**: web handlers depend on interface, not concrete Manager — testable
- [x] **Design §9 (web console)**: all functional requirements covered (ps, start/stop/restart, logs)
- [x] **Design §9 (Origin check)**: OriginMiddleware blocks unsafe cross-origin requests

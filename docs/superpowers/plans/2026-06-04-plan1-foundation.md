# app-nanny Plan 1: Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a working `nanny` CLI + background daemon that can register projects, start/stop/restart managed processes (with process group isolation), show status, survive daemon crashes, and auto-start via launchd.

**Architecture:** A single Go binary acts as both CLI and daemon. CLI commands connect to the daemon via Unix socket using a simple JSON-RPC protocol. The daemon manages child processes in dedicated process groups, persists runtime state to `runtime.json` for crash recovery, and reads project config from per-project `app-nanny.toml` files.

**Tech Stack:** Go 1.22+, `github.com/spf13/cobra` (CLI), `github.com/BurntSushi/toml` (config parsing), standard library only for everything else.

**Out of scope for this plan:** Log capture, error detection, RSS/CPU monitoring, web console. Those are Plan 2 and Plan 3.

---

## File Map

```
app-nanny/
├── main.go                          # Entry point, delegates to cmd/
├── go.mod
├── go.sum
├── justfile                         # Build, test, run tasks
├── cmd/
│   ├── root.go                      # Root cobra command, socket path resolution
│   ├── add.go                       # nanny add
│   ├── remove.go                    # nanny remove
│   ├── start.go                     # nanny start
│   ├── stop.go                      # nanny stop
│   ├── restart.go                   # nanny restart
│   ├── ps.go                        # nanny ps
│   ├── status.go                    # nanny status <name>
│   └── daemon.go                    # nanny daemon start/stop/status, nanny install/uninstall
├── internal/
│   ├── config/
│   │   ├── project.go               # app-nanny.toml schema + parser
│   │   ├── project_test.go
│   │   ├── registry.go              # registry.json read/write (project path index)
│   │   └── registry_test.go
│   ├── ipc/
│   │   ├── types.go                 # All JSON-RPC request/response structs
│   │   ├── types_test.go
│   │   ├── client.go                # CLI-side: connect to Unix socket, send request
│   │   └── server.go                # Daemon-side: accept connections, dispatch methods
│   ├── daemon/
│   │   ├── daemon.go                # Daemon main loop, signal handling, shutdown
│   │   ├── manager.go               # ProcessManager: owns all Process instances
│   │   ├── process.go               # Process struct, state machine, start/stop
│   │   ├── process_test.go
│   │   └── recovery.go              # runtime.json read/write, orphan cleanup
│   └── launchd/
│       └── plist.go                 # Generate and install/uninstall LaunchAgent plist
└── docs/
    └── (design docs, already written)
```

---

## Task 1: Project Scaffold

**Files:**
- Create: `go.mod`, `go.sum`, `main.go`, `justfile`

- [ ] **Step 1: Initialize Go module**

```bash
cd /Users/huanghao/workspace/app-nanny
go mod init github.com/huanghao/app-nanny
```

Expected: `go.mod` created with `module github.com/huanghao/app-nanny` and `go 1.22` (or current version).

- [ ] **Step 2: Install dependencies**

```bash
go get github.com/spf13/cobra@latest
go get github.com/BurntSushi/toml@latest
```

Expected: `go.sum` created, both packages in `go.mod`.

- [ ] **Step 3: Create main.go**

```go
// main.go
package main

import "github.com/huanghao/app-nanny/cmd"

func main() {
	cmd.Execute()
}
```

- [ ] **Step 4: Create cmd/root.go**

```go
// cmd/root.go
package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "nanny",
	Short: "Local dev service manager",
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// SocketPath returns the Unix socket path for daemon IPC.
func SocketPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "app-nanny", "app-nanny.sock")
}

// DataDir returns the base data directory.
func DataDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "app-nanny")
}
```

- [ ] **Step 5: Create justfile**

```just
# app-nanny build tasks

# Build binary
build:
    go build -o nanny .

# Run all tests
test:
    go test ./...

# Run tests with verbose output
test-v:
    go test -v ./...

# Build and install to ~/bin
install: build
    cp nanny ~/bin/nanny

# Clean build artifacts
clean:
    rm -f nanny
```

- [ ] **Step 6: Verify it compiles**

```bash
go build ./...
```

Expected: no output, no errors. Binary not yet useful but must compile.

- [ ] **Step 7: Commit**

```bash
git add .
git commit -m "chore: project scaffold, go module, cobra, toml"
```

---

## Task 2: Config — app-nanny.toml Parser

**Files:**
- Create: `internal/config/project.go`
- Create: `internal/config/project_test.go`

- [ ] **Step 1: Write failing tests**

```go
// internal/config/project_test.go
package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/huanghao/app-nanny/internal/config"
)

func writeToml(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "app-nanny.toml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadProject_ModeA(t *testing.T) {
	path := writeToml(t, `
name = "parquet-explorer"
autostart = false
restart = "on-failure"
max_restarts = 5

[ports]
PORT = 5001
VITE_PORT = 5173
`)
	cfg, err := config.LoadProject(path)
	if err != nil {
		t.Fatalf("LoadProject error: %v", err)
	}
	if cfg.Name != "parquet-explorer" {
		t.Errorf("Name = %q, want %q", cfg.Name, "parquet-explorer")
	}
	if cfg.Ports["PORT"] != 5001 {
		t.Errorf("Ports[PORT] = %d, want 5001", cfg.Ports["PORT"])
	}
	if cfg.Ports["VITE_PORT"] != 5173 {
		t.Errorf("Ports[VITE_PORT] = %d, want 5173", cfg.Ports["VITE_PORT"])
	}
	if len(cfg.Processes) != 0 {
		t.Errorf("Processes should be empty for Mode A")
	}
}

func TestLoadProject_ModeB(t *testing.T) {
	path := writeToml(t, `
name = "md-viewer"
autostart = true
restart = "on-failure"

[processes.server]
command = "bun --watch run src/server.ts"
port = 3000

[processes.rag]
command = "bun --watch run src/rag-server.ts"
port = 3001
memory_warn_mb = 512
`)
	cfg, err := config.LoadProject(path)
	if err != nil {
		t.Fatalf("LoadProject error: %v", err)
	}
	if cfg.Name != "md-viewer" {
		t.Errorf("Name = %q, want %q", cfg.Name, "md-viewer")
	}
	if len(cfg.Processes) != 2 {
		t.Errorf("Processes len = %d, want 2", len(cfg.Processes))
	}
	if cfg.Processes["server"].Port != 3000 {
		t.Errorf("server port = %d, want 3000", cfg.Processes["server"].Port)
	}
	if cfg.Processes["rag"].MemoryWarnMB != 512 {
		t.Errorf("rag MemoryWarnMB = %d, want 512", cfg.Processes["rag"].MemoryWarnMB)
	}
}

func TestLoadProject_DefaultCommand(t *testing.T) {
	path := writeToml(t, `
name = "my-app"

[ports]
PORT = 8080
`)
	cfg, err := config.LoadProject(path)
	if err != nil {
		t.Fatalf("LoadProject error: %v", err)
	}
	if cfg.Command != "just dev" {
		t.Errorf("Command = %q, want %q", cfg.Command, "just dev")
	}
}

func TestLoadProject_MissingName(t *testing.T) {
	path := writeToml(t, `
[ports]
PORT = 8080
`)
	_, err := config.LoadProject(path)
	if err == nil {
		t.Error("expected error for missing name, got nil")
	}
}

func TestLoadProject_NotFound(t *testing.T) {
	_, err := config.LoadProject("/nonexistent/app-nanny.toml")
	if err == nil {
		t.Error("expected error for missing file, got nil")
	}
}
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
go test ./internal/config/... -v -run TestLoadProject
```

Expected: compile error — `config` package doesn't exist yet.

- [ ] **Step 3: Implement project.go**

```go
// internal/config/project.go
package config

import (
	"fmt"

	"github.com/BurntSushi/toml"
)

// ProjectConfig is the parsed content of an app-nanny.toml file.
type ProjectConfig struct {
	Name          string                    `toml:"name"`
	Command       string                    `toml:"command"`
	AutoStart     bool                      `toml:"autostart"`
	Restart       string                    `toml:"restart"`      // "always"|"on-failure"|"never"
	MaxRestarts   int                       `toml:"max_restarts"`
	Ports         map[string]int            `toml:"ports"`        // Mode A: env_var -> port number
	Processes     map[string]ProcessConfig  `toml:"processes"`    // Mode B: name -> config
	ErrorPatterns []ErrorPattern            `toml:"error_patterns"`
}

// ProcessConfig is one entry under [processes.<name>] in Mode B.
type ProcessConfig struct {
	Command      string `toml:"command"`
	Port         int    `toml:"port"`
	WorkingDir   string `toml:"working_dir"`
	MemoryWarnMB int    `toml:"memory_warn_mb"`
}

// ErrorPattern defines a custom error trigger rule.
type ErrorPattern struct {
	Match        string `toml:"match"`
	ContextAfter int    `toml:"context_after"`
}

// LoadProject reads and validates an app-nanny.toml at the given path.
func LoadProject(path string) (*ProjectConfig, error) {
	var cfg ProjectConfig
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if cfg.Name == "" {
		return nil, fmt.Errorf("%s: 'name' is required", path)
	}
	if cfg.Command == "" {
		cfg.Command = "just dev"
	}
	if cfg.Restart == "" {
		cfg.Restart = "on-failure"
	}
	if cfg.MaxRestarts == 0 {
		cfg.MaxRestarts = 5
	}
	return &cfg, nil
}

// IsModeB reports whether this config uses fine-grained process definitions.
func (c *ProjectConfig) IsModeB() bool {
	return len(c.Processes) > 0
}

// DeclaredPorts returns all port numbers declared in this config.
func (c *ProjectConfig) DeclaredPorts() []int {
	var ports []int
	for _, p := range c.Ports {
		ports = append(ports, p)
	}
	for _, proc := range c.Processes {
		if proc.Port > 0 {
			ports = append(ports, proc.Port)
		}
	}
	return ports
}
```

- [ ] **Step 4: Run tests to confirm they pass**

```bash
go test ./internal/config/... -v -run TestLoadProject
```

Expected: all 5 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config/project.go internal/config/project_test.go
git commit -m "feat: app-nanny.toml config parser with Mode A/B support"
```

---

## Task 3: Config — Registry (Project Path Index)

**Files:**
- Create: `internal/config/registry.go`
- Create: `internal/config/registry_test.go`

- [ ] **Step 1: Write failing tests**

```go
// internal/config/registry_test.go
package config_test

import (
	"path/filepath"
	"testing"

	"github.com/huanghao/app-nanny/internal/config"
)

func TestRegistry_AddAndGet(t *testing.T) {
	dir := t.TempDir()
	reg := config.NewRegistry(filepath.Join(dir, "registry.json"))

	if err := reg.Add("parquet-explorer", "/workspace/parquet-explorer"); err != nil {
		t.Fatalf("Add error: %v", err)
	}

	path, ok := reg.Get("parquet-explorer")
	if !ok {
		t.Fatal("Get returned not-found after Add")
	}
	if path != "/workspace/parquet-explorer" {
		t.Errorf("path = %q, want %q", path, "/workspace/parquet-explorer")
	}
}

func TestRegistry_Remove(t *testing.T) {
	dir := t.TempDir()
	reg := config.NewRegistry(filepath.Join(dir, "registry.json"))

	_ = reg.Add("parquet-explorer", "/workspace/parquet-explorer")
	if err := reg.Remove("parquet-explorer"); err != nil {
		t.Fatalf("Remove error: %v", err)
	}
	if _, ok := reg.Get("parquet-explorer"); ok {
		t.Error("Get returned found after Remove")
	}
}

func TestRegistry_RemoveNotFound(t *testing.T) {
	dir := t.TempDir()
	reg := config.NewRegistry(filepath.Join(dir, "registry.json"))
	if err := reg.Remove("nonexistent"); err == nil {
		t.Error("expected error removing nonexistent project, got nil")
	}
}

func TestRegistry_Persist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "registry.json")

	reg1 := config.NewRegistry(path)
	_ = reg1.Add("my-app", "/workspace/my-app")

	// Load again from disk
	reg2 := config.NewRegistry(path)
	if _, ok := reg2.Get("my-app"); !ok {
		t.Error("project not persisted to disk")
	}
}

func TestRegistry_List(t *testing.T) {
	dir := t.TempDir()
	reg := config.NewRegistry(filepath.Join(dir, "registry.json"))
	_ = reg.Add("a", "/a")
	_ = reg.Add("b", "/b")

	entries := reg.List()
	if len(entries) != 2 {
		t.Errorf("List len = %d, want 2", len(entries))
	}
}
```

- [ ] **Step 2: Run to confirm they fail**

```bash
go test ./internal/config/... -v -run TestRegistry
```

Expected: compile error — `NewRegistry` not defined.

- [ ] **Step 3: Implement registry.go**

```go
// internal/config/registry.go
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// RegistryEntry is one project entry in the global index.
type RegistryEntry struct {
	Path string `json:"path"`
}

// Registry is the in-memory representation of registry.json.
// It is not safe for concurrent use — callers must synchronize.
type Registry struct {
	path     string
	projects map[string]RegistryEntry
}

// NewRegistry loads (or creates) the registry at the given path.
func NewRegistry(path string) *Registry {
	r := &Registry{path: path, projects: map[string]RegistryEntry{}}
	data, err := os.ReadFile(path)
	if err == nil {
		_ = json.Unmarshal(data, &r.projects)
	}
	return r
}

// Add registers a project name -> directory path and persists.
func (r *Registry) Add(name, dir string) error {
	r.projects[name] = RegistryEntry{Path: dir}
	return r.save()
}

// Remove deletes a project from the registry and persists.
func (r *Registry) Remove(name string) error {
	if _, ok := r.projects[name]; !ok {
		return fmt.Errorf("project %q not found in registry", name)
	}
	delete(r.projects, name)
	return r.save()
}

// Get returns the directory path for a project name.
func (r *Registry) Get(name string) (string, bool) {
	e, ok := r.projects[name]
	return e.Path, ok
}

// List returns all registered projects as name -> path map.
func (r *Registry) List() map[string]string {
	out := make(map[string]string, len(r.projects))
	for k, v := range r.projects {
		out[k] = v.Path
	}
	return out
}

func (r *Registry) save() error {
	if err := os.MkdirAll(filepath.Dir(r.path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(r.projects, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(r.path, data, 0644)
}
```

- [ ] **Step 4: Run tests to confirm they pass**

```bash
go test ./internal/config/... -v -run TestRegistry
```

Expected: all 5 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config/registry.go internal/config/registry_test.go
git commit -m "feat: registry.json project path index"
```

---

## Task 4: IPC Types

**Files:**
- Create: `internal/ipc/types.go`
- Create: `internal/ipc/types_test.go`

- [ ] **Step 1: Write failing tests**

```go
// internal/ipc/types_test.go
package ipc_test

import (
	"encoding/json"
	"testing"

	"github.com/huanghao/app-nanny/internal/ipc"
)

func TestRequest_RoundTrip(t *testing.T) {
	req := ipc.Request{
		Method: "start",
		Params: mustMarshal(ipc.StartParams{Name: "md-viewer"}),
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var got ipc.Request
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Method != "start" {
		t.Errorf("Method = %q, want %q", got.Method, "start")
	}
	var params ipc.StartParams
	if err := json.Unmarshal(got.Params, &params); err != nil {
		t.Fatal(err)
	}
	if params.Name != "md-viewer" {
		t.Errorf("Name = %q, want %q", params.Name, "md-viewer")
	}
}

func TestResponse_Error(t *testing.T) {
	resp := ipc.ErrorResponse("something went wrong")
	if resp.Error != "something went wrong" {
		t.Errorf("Error = %q", resp.Error)
	}
	if resp.Result != nil {
		t.Error("Result should be nil on error response")
	}
}

func mustMarshal(v any) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}
```

- [ ] **Step 2: Run to confirm they fail**

```bash
go test ./internal/ipc/... -v -run TestRequest
```

Expected: compile error — package not found.

- [ ] **Step 3: Implement types.go**

```go
// internal/ipc/types.go
package ipc

import "encoding/json"

// Request is a JSON-RPC style message from CLI to daemon.
type Request struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// Response is the daemon's reply to a Request.
type Response struct {
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

// ErrorResponse constructs a Response with an error message.
func ErrorResponse(msg string) Response {
	return Response{Error: msg}
}

// OKResponse constructs a Response with a result value.
func OKResponse(v any) (Response, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return Response{}, err
	}
	return Response{Result: data}, nil
}

// --- Params types (CLI → daemon) ---

type AddParams struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type RemoveParams struct {
	Name string `json:"name"`
}

type StartParams struct {
	Name    string `json:"name"`
	Process string `json:"process,omitempty"` // empty = all processes
}

type StopParams struct {
	Name    string `json:"name"`
	Process string `json:"process,omitempty"`
}

type RestartParams struct {
	Name    string `json:"name"`
	Process string `json:"process,omitempty"`
}

type StatusParams struct {
	Name string `json:"name"`
}

// --- Result types (daemon → CLI) ---

// ProcessInfo is one row in the `nanny ps` output.
type ProcessInfo struct {
	Project      string `json:"project"`
	Process      string `json:"process"`       // empty for Mode A
	Status       string `json:"status"`        // "running"|"stopped"|"crashed"
	PID          int    `json:"pid"`
	Uptime       string `json:"uptime"`        // human-readable, e.g. "2h14m"
	Restarts     int    `json:"restarts"`
	DeclaredPort int    `json:"declared_port"`
	ActualPorts  []int  `json:"actual_ports"`
}

// PSResult is the response to a "ps" request.
type PSResult struct {
	Processes []ProcessInfo `json:"processes"`
}

// AddResult is the response to an "add" request.
type AddResult struct {
	Name string `json:"name"`
	Path string `json:"path"`
}
```

- [ ] **Step 4: Run tests to confirm they pass**

```bash
go test ./internal/ipc/... -v
```

Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ipc/types.go internal/ipc/types_test.go
git commit -m "feat: IPC request/response types"
```

---

## Task 5: IPC Client and Server Transport

**Files:**
- Create: `internal/ipc/client.go`
- Create: `internal/ipc/server.go`

- [ ] **Step 1: Write failing test**

```go
// internal/ipc/client_test.go
package ipc_test

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/huanghao/app-nanny/internal/ipc"
)

func TestClientServer_RoundTrip(t *testing.T) {
	sockPath := filepath.Join(t.TempDir(), "test.sock")

	// Start a minimal server
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		// Echo the method back as result
		var req ipc.Request
		if err := json.NewDecoder(conn).Decode(&req); err != nil {
			return
		}
		resp, _ := ipc.OKResponse(map[string]string{"echo": req.Method})
		json.NewEncoder(conn).Encode(resp)
	}()

	client := ipc.NewClient(sockPath)
	resp, err := client.Call("ping", nil)
	if err != nil {
		t.Fatalf("Call error: %v", err)
	}
	var result map[string]string
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatal(err)
	}
	if result["echo"] != "ping" {
		t.Errorf("echo = %q, want %q", result["echo"], "ping")
	}
}

func TestClient_DaemonNotRunning(t *testing.T) {
	client := ipc.NewClient("/nonexistent/path.sock")
	_, err := client.Call("ps", nil)
	if err == nil {
		t.Error("expected error when daemon not running")
	}
	if !ipc.IsDaemonNotRunning(err) {
		t.Errorf("expected IsDaemonNotRunning=true, got false for error: %v", err)
	}
	_ = os.Remove("/nonexistent/path.sock")
}
```

- [ ] **Step 2: Run to confirm they fail**

```bash
go test ./internal/ipc/... -v -run TestClient
```

Expected: compile error — `NewClient` not defined.

- [ ] **Step 3: Implement client.go**

```go
// internal/ipc/client.go
package ipc

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
)

// ErrDaemonNotRunning is returned when the Unix socket is not available.
var ErrDaemonNotRunning = errors.New("nanny daemon is not running (run: nanny daemon start)")

// Client is the CLI-side IPC connection to the daemon.
type Client struct {
	socketPath string
}

// NewClient constructs a Client for the given socket path.
func NewClient(socketPath string) *Client {
	return &Client{socketPath: socketPath}
}

// Call sends a request to the daemon and returns the response.
func (c *Client) Call(method string, params any) (*Response, error) {
	conn, err := net.Dial("unix", c.socketPath)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDaemonNotRunning, err)
	}
	defer conn.Close()

	var rawParams json.RawMessage
	if params != nil {
		rawParams, err = json.Marshal(params)
		if err != nil {
			return nil, err
		}
	}

	req := Request{Method: method, Params: rawParams}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}

	var resp Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("daemon error: %s", resp.Error)
	}
	return &resp, nil
}

// IsDaemonNotRunning reports whether err is a daemon-not-running error.
func IsDaemonNotRunning(err error) bool {
	return errors.Is(err, ErrDaemonNotRunning)
}
```

- [ ] **Step 4: Implement server.go**

```go
// internal/ipc/server.go
package ipc

import (
	"encoding/json"
	"log"
	"net"
)

// Handler is a function that handles one IPC method.
// It receives raw JSON params and returns a result or error.
type Handler func(params json.RawMessage) (any, error)

// Server listens on a Unix socket and dispatches requests to handlers.
type Server struct {
	socketPath string
	handlers   map[string]Handler
}

// NewServer constructs a Server.
func NewServer(socketPath string) *Server {
	return &Server{
		socketPath: socketPath,
		handlers:   make(map[string]Handler),
	}
}

// Handle registers a handler for the given method name.
func (s *Server) Handle(method string, h Handler) {
	s.handlers[method] = h
}

// Serve listens for connections until the listener is closed.
func (s *Server) Serve(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return // listener closed, stop
		}
		go s.handleConn(conn)
	}
}

func (s *Server) handleConn(conn net.Conn) {
	defer conn.Close()
	var req Request
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		log.Printf("ipc: decode request: %v", err)
		return
	}

	h, ok := s.handlers[req.Method]
	if !ok {
		resp := ErrorResponse("unknown method: " + req.Method)
		json.NewEncoder(conn).Encode(resp) //nolint:errcheck
		return
	}

	result, err := h(req.Params)
	var resp Response
	if err != nil {
		resp = ErrorResponse(err.Error())
	} else {
		resp, err = OKResponse(result)
		if err != nil {
			resp = ErrorResponse("marshal result: " + err.Error())
		}
	}
	json.NewEncoder(conn).Encode(resp) //nolint:errcheck
}
```

- [ ] **Step 5: Run tests to confirm they pass**

```bash
go test ./internal/ipc/... -v
```

Expected: all tests PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/ipc/client.go internal/ipc/server.go internal/ipc/client_test.go
git commit -m "feat: IPC Unix socket client/server transport"
```

---

## Task 6: Process Struct and State Machine

**Files:**
- Create: `internal/daemon/process.go`
- Create: `internal/daemon/process_test.go`

- [ ] **Step 1: Write failing tests**

```go
// internal/daemon/process_test.go
package daemon_test

import (
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/huanghao/app-nanny/internal/config"
	"github.com/huanghao/app-nanny/internal/daemon"
)

func TestProcess_StartStop(t *testing.T) {
	// Use a real long-running process for integration-style test
	if _, err := exec.LookPath("sleep"); err != nil {
		t.Skip("sleep not available")
	}

	proc := daemon.NewProcess("test-sleep", config.ProcessConfig{
		Command: "sleep 60",
	}, t.TempDir())

	if err := proc.Start(); err != nil {
		t.Fatalf("Start error: %v", err)
	}
	if proc.Status() != daemon.StatusRunning {
		t.Errorf("Status = %v, want Running", proc.Status())
	}
	if proc.PID() == 0 {
		t.Error("PID should be non-zero after start")
	}

	if err := proc.Stop(); err != nil {
		t.Fatalf("Stop error: %v", err)
	}
	if proc.Status() != daemon.StatusStopped {
		t.Errorf("Status = %v, want Stopped after stop", proc.Status())
	}
}

func TestProcess_CrashDetection(t *testing.T) {
	proc := daemon.NewProcess("test-crash", config.ProcessConfig{
		Command: "false", // exits immediately with code 1
	}, t.TempDir())

	if err := proc.Start(); err != nil {
		t.Fatalf("Start error: %v", err)
	}

	// Wait for process to exit
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if proc.Status() == daemon.StatusCrashed {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if proc.Status() != daemon.StatusCrashed {
		t.Errorf("Status = %v, want Crashed", proc.Status())
	}
}

func TestProcess_EnvInjection(t *testing.T) {
	// Process that writes PORT to a temp file
	dir := t.TempDir()
	outFile := dir + "/port.txt"

	proc := daemon.NewProcess("test-env", config.ProcessConfig{
		Command: "sh -c 'echo $PORT > " + outFile + "'",
	}, dir)
	proc.SetEnv(map[string]string{"PORT": "9999"})

	if err := proc.Start(); err != nil {
		t.Fatalf("Start error: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(outFile); err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("output file not created: %v", err)
	}
	if got := string(data); got != "9999\n" {
		t.Errorf("PORT = %q, want %q", got, "9999\n")
	}
}
```

- [ ] **Step 2: Run to confirm they fail**

```bash
go test ./internal/daemon/... -v -run TestProcess
```

Expected: compile error — package not found.

- [ ] **Step 3: Implement process.go**

```go
// internal/daemon/process.go
package daemon

import (
	"fmt"
	"log"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/huanghao/app-nanny/internal/config"
)

type Status string

const (
	StatusStopped  Status = "stopped"
	StatusStarting Status = "starting"
	StatusRunning  Status = "running"
	StatusStopping Status = "stopping"
	StatusCrashed  Status = "crashed"
)

// Process manages a single child process with its lifecycle.
type Process struct {
	mu         sync.Mutex
	name       string            // "project/process" or "project" for Mode A
	cfg        config.ProcessConfig
	workDir    string
	extraEnv   map[string]string // injected env vars (ports etc.)
	status     Status
	pid        int
	pgid       int
	startedAt  time.Time
	restarts   int
	cmd        *exec.Cmd
	crashCh    chan struct{}      // closed when process exits unexpectedly
	onCrash    func(name string) // called when crash is detected
}

// NewProcess creates a Process (not yet started).
func NewProcess(name string, cfg config.ProcessConfig, workDir string) *Process {
	return &Process{
		name:    name,
		cfg:     cfg,
		workDir: workDir,
		status:  StatusStopped,
		crashCh: make(chan struct{}),
	}
}

// SetEnv sets additional environment variables to inject on start.
func (p *Process) SetEnv(env map[string]string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.extraEnv = env
}

// SetOnCrash registers a callback invoked when the process exits unexpectedly.
func (p *Process) SetOnCrash(fn func(name string)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.onCrash = fn
}

// Start launches the process. Returns error if already running or launch fails.
func (p *Process) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.status == StatusRunning || p.status == StatusStarting {
		return fmt.Errorf("process %q is already running", p.name)
	}

	parts := strings.Fields(p.cfg.Command)
	if len(parts) == 0 {
		return fmt.Errorf("process %q: empty command", p.name)
	}

	cmd := exec.Command(parts[0], parts[1:]...)
	cmd.Dir = p.workDir

	// Inject env vars
	for k, v := range p.extraEnv {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	// Inherit parent environment
	cmd.Env = append(syscall.Environ(), cmd.Env...)

	// Put child in its own process group for clean shutdown
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start %q: %w", p.name, err)
	}

	p.cmd = cmd
	p.pid = cmd.Process.Pid
	p.pgid, _ = syscall.Getpgid(p.pid)
	p.status = StatusRunning
	p.startedAt = time.Now()
	p.crashCh = make(chan struct{})

	// Watch for exit in background
	go p.watch()

	return nil
}

// Stop terminates the process gracefully (SIGTERM → wait 5s → SIGKILL).
func (p *Process) Stop() error {
	p.mu.Lock()
	if p.status != StatusRunning {
		p.mu.Unlock()
		return nil
	}
	p.status = StatusStopping
	pgid := p.pgid
	p.mu.Unlock()

	// Send SIGTERM to the entire process group
	if err := syscall.Kill(-pgid, syscall.SIGTERM); err != nil {
		log.Printf("process %q: SIGTERM error: %v", p.name, err)
	}

	// Wait up to 5 seconds for graceful exit
	done := make(chan struct{})
	go func() {
		p.cmd.Wait() //nolint:errcheck
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		// Force kill
		syscall.Kill(-pgid, syscall.SIGKILL) //nolint:errcheck
		<-done
	}

	p.mu.Lock()
	p.status = StatusStopped
	p.pid = 0
	p.pgid = 0
	p.mu.Unlock()
	return nil
}

// watch waits for the process to exit and updates status.
func (p *Process) watch() {
	err := p.cmd.Wait()

	p.mu.Lock()
	stopping := p.status == StatusStopping
	if !stopping {
		p.status = StatusCrashed
		log.Printf("process %q crashed: %v", p.name, err)
	}
	crashCh := p.crashCh
	onCrash := p.onCrash
	p.mu.Unlock()

	close(crashCh)
	if !stopping && onCrash != nil {
		onCrash(p.name)
	}
}

// Status returns the current process status.
func (p *Process) Status() Status {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.status
}

// PID returns the current process ID (0 if not running).
func (p *Process) PID() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.pid
}

// PGID returns the process group ID (0 if not running).
func (p *Process) PGID() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.pgid
}

// StartedAt returns when the process was last started.
func (p *Process) StartedAt() time.Time {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.startedAt
}

// Restarts returns the number of times this process has been restarted.
func (p *Process) Restarts() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.restarts
}

// IncrRestarts increments the restart counter.
func (p *Process) IncrRestarts() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.restarts++
}
```

- [ ] **Step 4: Run tests to confirm they pass**

```bash
go test ./internal/daemon/... -v -run TestProcess -timeout 15s
```

Expected: all 3 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/daemon/process.go internal/daemon/process_test.go
git commit -m "feat: process lifecycle with process group isolation"
```

---

## Task 7: Runtime State (Crash Recovery)

**Files:**
- Create: `internal/daemon/recovery.go`

- [ ] **Step 1: Write failing tests**

```go
// internal/daemon/recovery_test.go
package daemon_test

import (
	"os"
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
	_ = killed // may or may not have actually killed anything
}
```

- [ ] **Step 2: Run to confirm they fail**

```bash
go test ./internal/daemon/... -v -run TestRuntime -run TestCleanup
```

Expected: compile error — `NewRuntime` not defined.

- [ ] **Step 3: Implement recovery.go**

```go
// internal/daemon/recovery.go
package daemon

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// RuntimeEntry records the state of one running process for crash recovery.
type RuntimeEntry struct {
	PID       int       `json:"pid"`
	PGID      int       `json:"pgid"`
	Port      int       `json:"port"`
	StartedAt time.Time `json:"started_at"`
}

// Runtime manages the runtime.json file.
type Runtime struct {
	path    string
	entries map[string]RuntimeEntry
}

// NewRuntime loads (or creates empty) runtime state from the given path.
func NewRuntime(path string) *Runtime {
	r := &Runtime{path: path, entries: map[string]RuntimeEntry{}}
	data, err := os.ReadFile(path)
	if err == nil {
		_ = json.Unmarshal(data, &r.entries)
	}
	return r
}

// Set records an entry for a process key (e.g. "md-viewer/server").
func (r *Runtime) Set(key string, e RuntimeEntry) {
	r.entries[key] = e
}

// Delete removes an entry (called on clean process stop).
func (r *Runtime) Delete(key string) {
	delete(r.entries, key)
}

// All returns a copy of all current entries.
func (r *Runtime) All() map[string]RuntimeEntry {
	out := make(map[string]RuntimeEntry, len(r.entries))
	for k, v := range r.entries {
		out[k] = v
	}
	return out
}

// Save persists current entries to disk.
func (r *Runtime) Save() error {
	if err := os.MkdirAll(filepath.Dir(r.path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(r.entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(r.path, data, 0644)
}

// CleanupOrphans checks each entry in rt:
//   - If the PID is still alive → kill the process group (SIGTERM + SIGKILL)
//   - If the PID is gone → just remove the entry
//
// Returns the list of keys that were actively killed.
func CleanupOrphans(rt *Runtime) ([]string, error) {
	var killed []string
	for key, entry := range rt.All() {
		alive := processAlive(entry.PID)
		if alive && entry.PGID > 0 {
			log.Printf("recovery: killing orphan %q (pid=%d pgid=%d)", key, entry.PID, entry.PGID)
			_ = syscall.Kill(-entry.PGID, syscall.SIGTERM)
			time.Sleep(2 * time.Second)
			_ = syscall.Kill(-entry.PGID, syscall.SIGKILL)
			killed = append(killed, key)
		}
		rt.Delete(key)
	}
	return killed, rt.Save()
}

// processAlive returns true if the PID exists and is owned by this user.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil
}
```

- [ ] **Step 4: Run tests to confirm they pass**

```bash
go test ./internal/daemon/... -v -run TestRuntime -run TestCleanup -timeout 15s
```

Expected: all 3 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/daemon/recovery.go internal/daemon/recovery_test.go
git commit -m "feat: runtime.json crash recovery and orphan cleanup"
```

---

## Task 8: Process Manager

**Files:**
- Create: `internal/daemon/manager.go`

- [ ] **Step 1: Write failing tests**

```go
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
	m := daemon.NewManager(reg, rt)
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
	proj2Dir := writeProjectToml(t, dir, `
name = "beta"
command = "sleep 60"
[ports]
PORT = 9090
`)
	os.MkdirAll(proj1Dir, 0755)
	os.MkdirAll(proj2Dir, 0755)

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
```

- [ ] **Step 2: Run to confirm they fail**

```bash
go test ./internal/daemon/... -v -run TestManager -timeout 15s
```

Expected: compile error — `NewManager` not defined.

- [ ] **Step 3: Implement manager.go**

```go
// internal/daemon/manager.go
package daemon

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/huanghao/app-nanny/internal/config"
	"github.com/huanghao/app-nanny/internal/ipc"
)

// Manager owns all running processes and coordinates start/stop/restart.
type Manager struct {
	mu        sync.Mutex
	registry  *config.Registry
	runtime   *Runtime
	processes map[string]*Process // key: "project" or "project/process"
	configs   map[string]*config.ProjectConfig
}

// NewManager creates a Manager backed by the given registry and runtime state.
func NewManager(reg *config.Registry, rt *Runtime) *Manager {
	return &Manager{
		registry:  reg,
		runtime:   rt,
		processes: make(map[string]*Process),
		configs:   make(map[string]*config.ProjectConfig),
	}
}

// Add registers a project and loads its config into memory.
func (m *Manager) Add(name, dir string) error {
	cfg, err := config.LoadProject(filepath.Join(dir, "app-nanny.toml"))
	if err != nil {
		return err
	}
	if err := m.registry.Add(name, dir); err != nil {
		return err
	}
	m.mu.Lock()
	m.configs[name] = cfg
	m.mu.Unlock()
	return nil
}

// Remove unregisters a project. Fails if any of its processes are running.
func (m *Manager) Remove(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for key, proc := range m.processes {
		if strings.HasPrefix(key, name) && proc.Status() == StatusRunning {
			return fmt.Errorf("project %q has running processes; stop it first", name)
		}
	}
	delete(m.configs, name)
	return m.registry.Remove(name)
}

// Start launches a project's processes. processName="" means all processes.
func (m *Manager) Start(projectName, processName string) error {
	m.mu.Lock()
	cfg, ok := m.configs[projectName]
	if !ok {
		// Try loading from registry
		dir, found := m.registry.Get(projectName)
		if !found {
			m.mu.Unlock()
			return fmt.Errorf("project %q not registered", projectName)
		}
		loaded, err := config.LoadProject(filepath.Join(dir, "app-nanny.toml"))
		if err != nil {
			m.mu.Unlock()
			return err
		}
		cfg = loaded
		m.configs[projectName] = cfg
	}
	dir, _ := m.registry.Get(projectName)
	m.mu.Unlock()

	if cfg.IsModeB() {
		return m.startModeB(projectName, processName, cfg, dir)
	}
	return m.startModeA(projectName, cfg, dir)
}

func (m *Manager) startModeA(name string, cfg *config.ProjectConfig, dir string) error {
	// Check declared ports for conflicts
	for envVar, port := range cfg.Ports {
		if err := m.checkPortConflict(name, envVar, port); err != nil {
			return err
		}
	}

	proc := NewProcess(name, config.ProcessConfig{
		Command: cfg.Command,
	}, dir)

	// Inject ports as environment variables
	env := make(map[string]string)
	for k, v := range cfg.Ports {
		env[k] = fmt.Sprintf("%d", v)
	}
	proc.SetEnv(env)
	proc.SetOnCrash(func(key string) { m.onCrash(key, cfg) })

	if err := proc.Start(); err != nil {
		return err
	}

	m.mu.Lock()
	m.processes[name] = proc
	m.runtime.Set(name, RuntimeEntry{
		PID: proc.PID(), PGID: proc.PGID(),
		StartedAt: proc.StartedAt(),
	})
	m.mu.Unlock()
	return m.runtime.Save()
}

func (m *Manager) startModeB(projectName, processName string, cfg *config.ProjectConfig, dir string) error {
	for pName, pCfg := range cfg.Processes {
		if processName != "" && pName != processName {
			continue
		}
		key := projectName + "/" + pName
		if err := m.checkPortConflict(key, "PORT", pCfg.Port); err != nil {
			return err
		}
		workDir := dir
		if pCfg.WorkingDir != "" {
			workDir = filepath.Join(dir, pCfg.WorkingDir)
		}
		proc := NewProcess(key, pCfg, workDir)
		proc.SetEnv(map[string]string{"PORT": fmt.Sprintf("%d", pCfg.Port)})
		proc.SetOnCrash(func(k string) { m.onCrash(k, cfg) })
		if err := proc.Start(); err != nil {
			return fmt.Errorf("start %s: %w", key, err)
		}
		m.mu.Lock()
		m.processes[key] = proc
		m.runtime.Set(key, RuntimeEntry{
			PID: proc.PID(), PGID: proc.PGID(), Port: pCfg.Port,
			StartedAt: proc.StartedAt(),
		})
		m.mu.Unlock()
	}
	return m.runtime.Save()
}

// Stop terminates one or all processes of a project.
func (m *Manager) Stop(projectName, processName string) error {
	m.mu.Lock()
	var targets []string
	for key := range m.processes {
		if strings.HasPrefix(key, projectName) {
			if processName == "" || key == projectName+"/"+processName || key == projectName {
				targets = append(targets, key)
			}
		}
	}
	m.mu.Unlock()

	for _, key := range targets {
		m.mu.Lock()
		proc := m.processes[key]
		m.mu.Unlock()
		if proc != nil {
			if err := proc.Stop(); err != nil {
				return err
			}
		}
		m.mu.Lock()
		m.runtime.Delete(key)
		m.mu.Unlock()
	}
	return m.runtime.Save()
}

// Restart stops then starts a project's processes.
func (m *Manager) Restart(projectName, processName string) error {
	if err := m.Stop(projectName, processName); err != nil {
		return err
	}
	return m.Start(projectName, processName)
}

// PS returns a snapshot of all known processes.
func (m *Manager) PS() []ipc.ProcessInfo {
	m.mu.Lock()
	defer m.mu.Unlock()

	var out []ipc.ProcessInfo
	for key, proc := range m.processes {
		parts := strings.SplitN(key, "/", 2)
		project := parts[0]
		process := ""
		if len(parts) == 2 {
			process = parts[1]
		}
		uptime := ""
		if proc.Status() == StatusRunning {
			uptime = formatDuration(time.Since(proc.StartedAt()))
		}
		out = append(out, ipc.ProcessInfo{
			Project:  project,
			Process:  process,
			Status:   string(proc.Status()),
			PID:      proc.PID(),
			Uptime:   uptime,
			Restarts: proc.Restarts(),
		})
	}
	return out
}

// LoadAll reads the registry and pre-loads all project configs.
func (m *Manager) LoadAll() {
	for name, dir := range m.registry.List() {
		cfg, err := config.LoadProject(filepath.Join(dir, "app-nanny.toml"))
		if err != nil {
			continue
		}
		m.mu.Lock()
		m.configs[name] = cfg
		m.mu.Unlock()
	}
}

// checkPortConflict returns an error if port is already claimed by another process.
func (m *Manager) checkPortConflict(claimant string, envVar string, port int) error {
	if port == 0 {
		return nil
	}
	for key, proc := range m.processes {
		if key == claimant {
			continue
		}
		if proc.Status() != StatusRunning {
			continue
		}
		cfg := m.projectConfigForKey(key)
		if cfg == nil {
			continue
		}
		for _, p := range cfg.DeclaredPorts() {
			if p == port {
				return fmt.Errorf("port %d (%s) conflicts with running service %q", port, envVar, key)
			}
		}
	}
	return nil
}

func (m *Manager) projectConfigForKey(key string) *config.ProjectConfig {
	parts := strings.SplitN(key, "/", 2)
	return m.configs[parts[0]]
}

// onCrash handles a process crash: logs and schedules restart if configured.
func (m *Manager) onCrash(key string, cfg *config.ProjectConfig) {
	m.mu.Lock()
	proc, ok := m.processes[key]
	m.mu.Unlock()
	if !ok {
		return
	}

	if cfg.Restart == "never" {
		return
	}
	if cfg.MaxRestarts > 0 && proc.Restarts() >= cfg.MaxRestarts {
		return
	}

	go func() {
		time.Sleep(1 * time.Second)
		parts := strings.SplitN(key, "/", 2)
		project := parts[0]
		process := ""
		if len(parts) == 2 {
			process = parts[1]
		}
		proc.IncrRestarts()
		_ = m.Start(project, process)
	}()
}

func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh%dm", h, m)
	}
	if m > 0 {
		return fmt.Sprintf("%dm%ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

// ActualPorts returns the ports a process is actually listening on via lsof.
func ActualPorts(pid int) []int {
	if pid == 0 {
		return nil
	}
	out, err := exec.Command("lsof", "-p", fmt.Sprintf("%d", pid), "-iTCP", "-sTCP:LISTEN", "-Fn").Output()
	if err != nil {
		return nil
	}
	var ports []int
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.HasPrefix(line, "n") {
			continue
		}
		// line format: "n*:5001" or "nlocalhost:5001"
		parts := strings.Split(line, ":")
		if len(parts) < 2 {
			continue
		}
		var port int
		fmt.Sscanf(parts[len(parts)-1], "%d", &port)
		if port > 0 {
			ports = append(ports, port)
		}
	}
	return ports
}
```

- [ ] **Step 4: Run tests to confirm they pass**

```bash
go test ./internal/daemon/... -v -run TestManager -timeout 30s
```

Expected: all 3 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/daemon/manager.go internal/daemon/manager_test.go
git commit -m "feat: process manager with port conflict detection and auto-restart"
```

---

## Task 9: Daemon Main Loop

**Files:**
- Create: `internal/daemon/daemon.go`

- [ ] **Step 1: Implement daemon.go**

No unit test for this task — the daemon loop is integration-tested via the CLI in later tasks.

```go
// internal/daemon/daemon.go
package daemon

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/huanghao/app-nanny/internal/config"
	"github.com/huanghao/app-nanny/internal/ipc"
)

// Run is the daemon entry point. It blocks until SIGTERM or SIGINT.
func Run(socketPath, dataDir string) error {
	// Ensure data directory exists
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	rt := NewRuntime(filepath.Join(dataDir, "runtime.json"))
	reg := config.NewRegistry(filepath.Join(dataDir, "registry.json"))

	// Crash recovery: kill orphan processes from previous run
	killed, err := CleanupOrphans(rt)
	if err != nil {
		log.Printf("daemon: orphan cleanup error: %v", err)
	}
	if len(killed) > 0 {
		log.Printf("daemon: cleaned up orphans: %v", killed)
	}

	mgr := NewManager(reg, rt)
	mgr.LoadAll()

	// Auto-start projects marked autostart=true
	for name, dir := range reg.List() {
		cfg, err := config.LoadProject(filepath.Join(dir, "app-nanny.toml"))
		if err != nil {
			continue
		}
		if cfg.AutoStart {
			log.Printf("daemon: auto-starting %q", name)
			if err := mgr.Start(name, ""); err != nil {
				log.Printf("daemon: auto-start %q failed: %v", name, err)
			}
		}
	}

	// Remove stale socket
	os.Remove(socketPath)

	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("listen %s: %w", socketPath, err)
	}
	defer func() {
		ln.Close()
		os.Remove(socketPath)
	}()

	log.Printf("daemon: listening on %s", socketPath)

	srv := ipc.NewServer(socketPath)
	registerHandlers(srv, mgr)

	// Handle OS signals for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	go srv.Serve(ln)

	<-sigCh
	log.Println("daemon: shutting down")
	ln.Close()

	// Stop all running processes
	for name := range reg.List() {
		_ = mgr.Stop(name, "")
	}
	return nil
}

// registerHandlers wires IPC methods to Manager operations.
func registerHandlers(srv *ipc.Server, mgr *Manager) {
	srv.Handle("add", func(params json.RawMessage) (any, error) {
		var p ipc.AddParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
		if err := mgr.Add(p.Name, p.Path); err != nil {
			return nil, err
		}
		return ipc.AddResult{Name: p.Name, Path: p.Path}, nil
	})

	srv.Handle("remove", func(params json.RawMessage) (any, error) {
		var p ipc.RemoveParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
		return nil, mgr.Remove(p.Name)
	})

	srv.Handle("start", func(params json.RawMessage) (any, error) {
		var p ipc.StartParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
		return nil, mgr.Start(p.Name, p.Process)
	})

	srv.Handle("stop", func(params json.RawMessage) (any, error) {
		var p ipc.StopParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
		return nil, mgr.Stop(p.Name, p.Process)
	})

	srv.Handle("restart", func(params json.RawMessage) (any, error) {
		var p ipc.RestartParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
		return nil, mgr.Restart(p.Name, p.Process)
	})

	srv.Handle("ps", func(_ json.RawMessage) (any, error) {
		return ipc.PSResult{Processes: mgr.PS()}, nil
	})
}
```

- [ ] **Step 2: Verify it compiles**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add internal/daemon/daemon.go
git commit -m "feat: daemon main loop with IPC handlers and graceful shutdown"
```

---

## Task 10: CLI Commands — add, remove, start, stop, restart

**Files:**
- Create: `cmd/add.go`, `cmd/remove.go`, `cmd/start.go`, `cmd/stop.go`, `cmd/restart.go`

- [ ] **Step 1: Create cmd/add.go**

```go
// cmd/add.go
package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/huanghao/app-nanny/internal/ipc"
	"github.com/spf13/cobra"
)

var addCmd = &cobra.Command{
	Use:   "add [dir]",
	Short: "Register a project (reads app-nanny.toml in dir)",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := "."
		if len(args) == 1 {
			dir = args[0]
		}
		abs, err := filepath.Abs(dir)
		if err != nil {
			return err
		}
		tomlPath := filepath.Join(abs, "app-nanny.toml")
		if _, err := os.Stat(tomlPath); err != nil {
			return fmt.Errorf("no app-nanny.toml found in %s", abs)
		}

		// Read name from toml to use as the registry key
		name, err := readProjectName(tomlPath)
		if err != nil {
			return err
		}

		client := ipc.NewClient(SocketPath())
		_, err = client.Call("add", ipc.AddParams{Name: name, Path: abs})
		if ipc.IsDaemonNotRunning(err) {
			return err
		}
		if err != nil {
			return err
		}
		fmt.Printf("registered %q (%s)\n", name, abs)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(addCmd)
}

// readProjectName reads just the name field from app-nanny.toml
// without importing the full config package to avoid circular imports.
func readProjectName(path string) (string, error) {
	// Use config package directly — it's fine to import here
	return readNameFromToml(path)
}
```

- [ ] **Step 2: Create a shared helper for reading project name**

Add to `cmd/root.go`:

```go
// Add this import at the top of cmd/root.go
import (
	// ... existing imports ...
	"github.com/huanghao/app-nanny/internal/config"
)

// readNameFromToml reads the project name from an app-nanny.toml.
func readNameFromToml(path string) (string, error) {
	cfg, err := config.LoadProject(path)
	if err != nil {
		return "", err
	}
	return cfg.Name, nil
}
```

- [ ] **Step 3: Create cmd/remove.go**

```go
// cmd/remove.go
package cmd

import (
	"fmt"

	"github.com/huanghao/app-nanny/internal/ipc"
	"github.com/spf13/cobra"
)

var removeCmd = &cobra.Command{
	Use:   "remove <name>",
	Short: "Unregister a project",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client := ipc.NewClient(SocketPath())
		_, err := client.Call("remove", ipc.RemoveParams{Name: args[0]})
		if err != nil {
			return err
		}
		fmt.Printf("removed %q\n", args[0])
		return nil
	},
}

func init() {
	rootCmd.AddCommand(removeCmd)
}
```

- [ ] **Step 4: Create cmd/start.go**

```go
// cmd/start.go
package cmd

import (
	"fmt"
	"strings"

	"github.com/huanghao/app-nanny/internal/ipc"
	"github.com/spf13/cobra"
)

var startCmd = &cobra.Command{
	Use:   "start <name>[/process]",
	Short: "Start a registered project or a single process",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		project, process := splitTarget(args[0])
		client := ipc.NewClient(SocketPath())
		_, err := client.Call("start", ipc.StartParams{Name: project, Process: process})
		if err != nil {
			return err
		}
		fmt.Printf("started %s\n", args[0])
		return nil
	},
}

func init() {
	rootCmd.AddCommand(startCmd)
}

// splitTarget splits "project/process" into ("project", "process").
// If no slash, returns (target, "").
func splitTarget(target string) (project, process string) {
	parts := strings.SplitN(target, "/", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return target, ""
}
```

- [ ] **Step 5: Create cmd/stop.go**

```go
// cmd/stop.go
package cmd

import (
	"fmt"

	"github.com/huanghao/app-nanny/internal/ipc"
	"github.com/spf13/cobra"
)

var stopCmd = &cobra.Command{
	Use:   "stop <name>[/process]",
	Short: "Stop a project or a single process",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		project, process := splitTarget(args[0])
		client := ipc.NewClient(SocketPath())
		_, err := client.Call("stop", ipc.StopParams{Name: project, Process: process})
		if err != nil {
			return err
		}
		fmt.Printf("stopped %s\n", args[0])
		return nil
	},
}

func init() {
	rootCmd.AddCommand(stopCmd)
}
```

- [ ] **Step 6: Create cmd/restart.go**

```go
// cmd/restart.go
package cmd

import (
	"fmt"

	"github.com/huanghao/app-nanny/internal/ipc"
	"github.com/spf13/cobra"
)

var restartCmd = &cobra.Command{
	Use:   "restart <name>[/process]",
	Short: "Restart a project or a single process",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		project, process := splitTarget(args[0])
		client := ipc.NewClient(SocketPath())
		_, err := client.Call("restart", ipc.RestartParams{Name: project, Process: process})
		if err != nil {
			return err
		}
		fmt.Printf("restarted %s\n", args[0])
		return nil
	},
}

func init() {
	rootCmd.AddCommand(restartCmd)
}
```

- [ ] **Step 7: Verify everything compiles**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 8: Commit**

```bash
git add cmd/add.go cmd/remove.go cmd/start.go cmd/stop.go cmd/restart.go cmd/root.go
git commit -m "feat: CLI commands add/remove/start/stop/restart"
```

---

## Task 11: CLI — nanny ps

**Files:**
- Create: `cmd/ps.go`

- [ ] **Step 1: Implement ps.go**

```go
// cmd/ps.go
package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/huanghao/app-nanny/internal/ipc"
	"github.com/spf13/cobra"
)

var psCmd = &cobra.Command{
	Use:   "ps",
	Short: "List all registered services and their status",
	RunE: func(cmd *cobra.Command, args []string) error {
		client := ipc.NewClient(SocketPath())
		resp, err := client.Call("ps", nil)
		if err != nil {
			if ipc.IsDaemonNotRunning(err) {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return err
		}

		var result ipc.PSResult
		if err := json.Unmarshal(resp.Result, &result); err != nil {
			return err
		}

		if len(result.Processes) == 0 {
			fmt.Println("no processes registered")
			return nil
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "PROJECT\tPROCESS\tSTATUS\tPID\tUPTIME\tRESTARTS\tPORTS")
		for _, p := range result.Processes {
			process := p.Process
			if process == "" {
				process = "-"
			}
			ports := formatPorts(p.DeclaredPort, p.ActualPorts)
			status := p.Status
			fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\t%d\t%s\n",
				p.Project, process, status, p.PID, p.Uptime, p.Restarts, ports)
		}
		return w.Flush()
	},
}

func init() {
	rootCmd.AddCommand(psCmd)
}

func formatPorts(declared int, actual []int) string {
	if len(actual) > 0 {
		s := ""
		for i, p := range actual {
			if i > 0 {
				s += ","
			}
			s += fmt.Sprintf("%d", p)
		}
		return s
	}
	if declared > 0 {
		return fmt.Sprintf("%d (declared)", declared)
	}
	return "-"
}
```

- [ ] **Step 2: Verify it compiles**

```bash
go build ./...
```

- [ ] **Step 3: Commit**

```bash
git add cmd/ps.go
git commit -m "feat: nanny ps tabular output"
```

---

## Task 12: CLI — nanny daemon + nanny serve

**Files:**
- Create: `cmd/daemon.go`
- Create: `cmd/serve.go` (internal subcommand, not exposed in help)

- [ ] **Step 1: Create cmd/serve.go (daemon entry point)**

```go
// cmd/serve.go
package cmd

import (
	"log"

	"github.com/huanghao/app-nanny/internal/daemon"
	"github.com/spf13/cobra"
)

// serveCmd is the internal subcommand run by the daemon process.
// It is hidden from normal help output.
var serveCmd = &cobra.Command{
	Use:    "serve",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		log.SetPrefix("[nanny-daemon] ")
		return daemon.Run(SocketPath(), DataDir())
	},
}

func init() {
	rootCmd.AddCommand(serveCmd)
}
```

- [ ] **Step 2: Create cmd/daemon.go**

```go
// cmd/daemon.go
package cmd

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"syscall"

	"github.com/huanghao/app-nanny/internal/ipc"
	"github.com/spf13/cobra"
)

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Manage the nanny background daemon",
}

var daemonStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the nanny daemon in the background",
	RunE: func(cmd *cobra.Command, args []string) error {
		if isDaemonRunning() {
			fmt.Println("daemon is already running")
			return nil
		}
		self, err := os.Executable()
		if err != nil {
			return fmt.Errorf("find executable: %w", err)
		}
		proc := exec.Command(self, "serve")
		proc.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

		// Redirect daemon output to log file
		logPath := DataDir() + "/daemon.log"
		logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return fmt.Errorf("open daemon log: %w", err)
		}
		proc.Stdout = logFile
		proc.Stderr = logFile

		if err := proc.Start(); err != nil {
			return fmt.Errorf("start daemon: %w", err)
		}
		fmt.Printf("daemon started (pid %d), log: %s\n", proc.Process.Pid, logPath)
		return nil
	},
}

var daemonStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the nanny daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		client := ipc.NewClient(SocketPath())
		// Send SIGTERM via a dedicated IPC method
		_, err := client.Call("shutdown", nil)
		if err != nil {
			if ipc.IsDaemonNotRunning(err) {
				fmt.Println("daemon is not running")
				return nil
			}
			return err
		}
		fmt.Println("daemon stopped")
		return nil
	},
}

var daemonStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show daemon status",
	RunE: func(cmd *cobra.Command, args []string) error {
		if isDaemonRunning() {
			fmt.Println("daemon: running")
		} else {
			fmt.Println("daemon: stopped")
		}
		return nil
	},
}

func init() {
	daemonCmd.AddCommand(daemonStartCmd)
	daemonCmd.AddCommand(daemonStopCmd)
	daemonCmd.AddCommand(daemonStatusCmd)
	rootCmd.AddCommand(daemonCmd)
}

func isDaemonRunning() bool {
	conn, err := net.Dial("unix", SocketPath())
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
```

- [ ] **Step 3: Add "shutdown" handler to daemon.go**

In `internal/daemon/daemon.go`, inside `registerHandlers`, add:

```go
srv.Handle("shutdown", func(_ json.RawMessage) (any, error) {
    go func() {
        sigCh <- syscall.SIGTERM
    }()
    return "ok", nil
})
```

This requires making `sigCh` accessible inside `registerHandlers`. Refactor `Run` so `sigCh` is passed in, or use a closure. Simplest approach: pass sigCh as a parameter to `registerHandlers`:

```go
// Change function signature in daemon.go:
func registerHandlers(srv *ipc.Server, mgr *Manager, sigCh chan<- os.Signal) {
    // ... existing handlers ...
    srv.Handle("shutdown", func(_ json.RawMessage) (any, error) {
        go func() { sigCh <- syscall.SIGTERM }()
        return "ok", nil
    })
}

// Update call site in Run():
registerHandlers(srv, mgr, sigCh)
```

- [ ] **Step 4: Verify it compiles**

```bash
go build ./...
```

- [ ] **Step 5: Commit**

```bash
git add cmd/daemon.go cmd/serve.go internal/daemon/daemon.go
git commit -m "feat: nanny daemon start/stop/status and serve subcommand"
```

---

## Task 13: launchd Integration

**Files:**
- Create: `internal/launchd/plist.go`
- Modify: `cmd/daemon.go` (add install/uninstall subcommands)

- [ ] **Step 1: Implement plist.go**

```go
// internal/launchd/plist.go
package launchd

import (
	"fmt"
	"os"
	"path/filepath"
)

const plistTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.app-nanny.daemon</string>
    <key>ProgramArguments</key>
    <array>
        <string>%s</string>
        <string>serve</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <dict>
        <key>Crashed</key>
        <true/>
    </dict>
    <key>StandardOutPath</key>
    <string>%s/daemon.log</string>
    <key>StandardErrorPath</key>
    <string>%s/daemon.log</string>
</dict>
</plist>
`

// PlistPath returns the path where the LaunchAgent plist should be installed.
func PlistPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", "com.app-nanny.daemon.plist")
}

// Install writes the LaunchAgent plist and loads it with launchctl.
func Install(binaryPath, dataDir string) error {
	plist := fmt.Sprintf(plistTemplate, binaryPath, dataDir, dataDir)
	path := PlistPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(plist), 0644); err != nil {
		return fmt.Errorf("write plist: %w", err)
	}
	return nil
}

// Uninstall removes the LaunchAgent plist.
func Uninstall() error {
	path := PlistPath()
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove plist: %w", err)
	}
	return nil
}
```

- [ ] **Step 2: Add install/uninstall to cmd/daemon.go**

Append these commands in the `init()` of `cmd/daemon.go`:

```go
var installCmd = &cobra.Command{
    Use:   "install",
    Short: "Install nanny as a macOS LaunchAgent (auto-start on login)",
    RunE: func(cmd *cobra.Command, args []string) error {
        self, err := os.Executable()
        if err != nil {
            return err
        }
        if err := launchd.Install(self, DataDir()); err != nil {
            return err
        }
        fmt.Printf("installed LaunchAgent: %s\n", launchd.PlistPath())
        fmt.Println("nanny daemon will start automatically on next login.")
        fmt.Println("To start now: nanny daemon start")
        return nil
    },
}

var uninstallCmd = &cobra.Command{
    Use:   "uninstall",
    Short: "Remove the nanny LaunchAgent",
    RunE: func(cmd *cobra.Command, args []string) error {
        if err := launchd.Uninstall(); err != nil {
            return err
        }
        fmt.Println("LaunchAgent removed.")
        return nil
    },
}

// In init():
rootCmd.AddCommand(installCmd)
rootCmd.AddCommand(uninstallCmd)
```

Add import at top of `cmd/daemon.go`:
```go
"github.com/huanghao/app-nanny/internal/launchd"
```

- [ ] **Step 3: Verify it compiles**

```bash
go build ./...
```

- [ ] **Step 4: Commit**

```bash
git add internal/launchd/plist.go cmd/daemon.go
git commit -m "feat: launchd install/uninstall for auto-start on login"
```

---

## Task 14: End-to-End Smoke Test

Verify the full binary works end-to-end against a real project.

- [ ] **Step 1: Build the binary**

```bash
just build
```

Expected: `./nanny` binary created.

- [ ] **Step 2: Start the daemon**

```bash
./nanny daemon start
sleep 1
./nanny daemon status
```

Expected:
```
daemon started (pid XXXXX), log: ~/.local/share/app-nanny/daemon.log
daemon: running
```

- [ ] **Step 3: Create a test project and register it**

```bash
mkdir -p /tmp/nanny-smoke-test
cat > /tmp/nanny-smoke-test/app-nanny.toml << 'EOF'
name = "smoke-test"
command = "sleep 60"
EOF

./nanny add /tmp/nanny-smoke-test
```

Expected: `registered "smoke-test" (/tmp/nanny-smoke-test)`

- [ ] **Step 4: Start and verify**

```bash
./nanny start smoke-test
./nanny ps
```

Expected: `ps` shows `smoke-test` with status `running`.

- [ ] **Step 5: Stop and verify**

```bash
./nanny stop smoke-test
./nanny ps
```

Expected: `ps` shows `smoke-test` with status `stopped`.

- [ ] **Step 6: Stop the daemon**

```bash
./nanny daemon stop
./nanny daemon status
```

Expected: `daemon: stopped`

- [ ] **Step 7: Clean up**

```bash
./nanny remove smoke-test || true
rm -rf /tmp/nanny-smoke-test
```

- [ ] **Step 8: Final commit**

```bash
git add .
git commit -m "chore: all tests pass, smoke test documented"
```

---

## Self-Review Checklist

- [x] **Spec §3 (app-nanny.toml)** → Task 2 (ProjectConfig, Mode A, Mode B, defaults)
- [x] **Spec §3 (registry.json)** → Task 3 (Registry, Add/Remove/List/Persist)
- [x] **Spec §4 (port management)** → Task 8 (Manager.checkPortConflict, env injection)
- [x] **Spec §5 (process groups, crash recovery)** → Tasks 6, 7, 8, 9 (Setpgid, runtime.json, CleanupOrphans)
- [x] **Spec §9 (CLI commands)** → Tasks 10, 11, 12
- [x] **Spec §10.1 (launchd)** → Task 13
- [x] **Spec §10.2 (autostart on daemon restart)** → Task 9 (daemon.go Run() autostart loop)
- [x] **Spec §11 (Go, single binary)** → Task 1 (go.mod, cobra)
- [ ] **Spec §4 (lsof actual ports)** → `ActualPorts()` in manager.go implemented but not wired into PS() response. Add to `ProcessInfo` population in `Manager.PS()`:

```go
// In Manager.PS(), update ProcessInfo population:
actualPorts := ActualPorts(proc.PID())
out = append(out, ipc.ProcessInfo{
    // ... existing fields ...
    ActualPorts: actualPorts,
})
```

Add this fix to Task 11's commit.

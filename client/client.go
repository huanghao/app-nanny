// Package client is a typed Go binding for app-nanny's HTTP API
// (internal/web, routes under /api/v1) — the stable, versioned surface
// other local tools should depend on instead of reverse-engineering the
// wire format (as context-pad's internal/appnanny package originally did
// against the Unix-socket IPC protocol, which is CLI-internal and was
// never meant to be consumed externally).
//
// This package deliberately does not import anything under app-nanny's own
// internal/ — not because it's disallowed (same module, it would compile),
// but because depending on it would silently reintroduce exactly the
// coupling this package exists to avoid. The types below re-declare the
// wire shapes instead; keeping them in sync with internal/ipc when the API
// changes is a deliberate, visible step, not an accident.
//
// Compatibility: additive response fields/new endpoints are safe to expect
// (VersionInfo.APIVersion tells you which contract version you're talking
// to); a field being removed or changing type/meaning would only happen
// under a new API version (see internal/web's apiPrefix doc comment).
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// DefaultBaseURL points at the daemon's default bind address — loopback
// only, matching app-nanny's "this machine only" design (see
// docs/2026-06-04-design.md §13).
const DefaultBaseURL = "http://127.0.0.1:7070/api/v1"

// Process mirrors ipc.Process's JSON shape — see the package doc for why
// this is a separate, re-declared type rather than an import.
type Process struct {
	Key           string  `json:"key"`
	Project       string  `json:"project"`
	Process       string  `json:"process"`
	Status        string  `json:"status"`
	PID           int     `json:"pid"`
	Uptime        string  `json:"uptime"`
	Restarts      int     `json:"restarts"`
	DeclaredPort  int     `json:"declared_port"`
	ActualPorts   []int   `json:"actual_ports"`
	OtelService   string  `json:"otel_service,omitempty"`
	MemMB         float64 `json:"mem_mb"`
	CPUPercent    float64 `json:"cpu_percent"`
	WorkDir       string  `json:"work_dir"`
	ErrorCount    int     `json:"error_count"`
	LastErrorTime string  `json:"last_error_time"`
	LastLogTime   string  `json:"last_log_time"`
	LogPath       string  `json:"log_path"`
}

// VersionInfo is the response to Version(). APIVersion is the contract
// version to check compatibility against; Version/Commit are build
// identity, not part of the compatibility contract.
type VersionInfo struct {
	Version    string `json:"version"`
	Commit     string `json:"commit"`
	APIVersion string `json:"api_version"`
}

// LogsResult is the response to Logs().
type LogsResult struct {
	Lines   []string `json:"lines"`
	Path    string   `json:"path"`     // log file path, "" if this key has no single file (see SubKeys)
	SubKeys []string `json:"sub_keys"` // populated when Path is empty
}

// ErrorEvent is one captured error/crash event, as returned by Errors().
type ErrorEvent struct {
	Time  string   `json:"time"`
	Key   string   `json:"key"`
	Lines []string `json:"lines"`
}

// APIError is returned for any non-2xx response — Code is a small fixed
// set ("bad_request", "not_found", "internal" as of API v1.0), Message is
// human-readable and not part of the compatibility contract.
type APIError struct {
	Status  int
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *APIError) Error() string {
	return fmt.Sprintf("app-nanny: %s (%s)", e.Message, e.Code)
}

// Client talks to a running app-nanny daemon's HTTP API.
type Client struct {
	baseURL string
	http    *http.Client
}

// New returns a Client pointed at DefaultBaseURL with a 5s request timeout.
func New() *Client {
	return &Client{baseURL: DefaultBaseURL, http: &http.Client{Timeout: 5 * time.Second}}
}

// NewWithBaseURL is New, but pointed at a different daemon address/prefix —
// mainly for tests (an httptest.Server) or a non-default port.
func NewWithBaseURL(baseURL string) *Client {
	return &Client{baseURL: baseURL, http: &http.Client{Timeout: 5 * time.Second}}
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	return c.do(req, out)
}

func (c *Client) post(ctx context.Context, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	return c.do(req, out)
}

func (c *Client) do(req *http.Request, out any) error {
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var apiErr APIError
		if jsonErr := json.Unmarshal(data, &apiErr); jsonErr != nil {
			return fmt.Errorf("app-nanny: unexpected response (status %d): %s", resp.StatusCode, string(data))
		}
		apiErr.Status = resp.StatusCode
		return &apiErr
	}

	if out == nil {
		return nil
	}
	return json.Unmarshal(data, out)
}

// Version reports the daemon's build identity and API contract version.
func (c *Client) Version(ctx context.Context) (VersionInfo, error) {
	var v VersionInfo
	err := c.get(ctx, "/version", &v)
	return v, err
}

// PS lists every process app-nanny knows about, across all registered
// projects (running, stopped, and crashed).
func (c *Client) PS(ctx context.Context) ([]Process, error) {
	var result struct {
		Processes []Process `json:"processes"`
	}
	err := c.get(ctx, "/ps", &result)
	return result.Processes, err
}

// Status returns detailed process records for one project (a subset of
// PS(), scoped to name).
func (c *Client) Status(ctx context.Context, name string) ([]Process, error) {
	var result struct {
		Processes []Process `json:"processes"`
	}
	err := c.get(ctx, "/status/"+url.PathEscape(name), &result)
	return result.Processes, err
}

// Start starts a registered project, or one named process within it
// (process == "" starts every process the project declares).
func (c *Client) Start(ctx context.Context, project, process string) error {
	return c.post(ctx, actionPath(project, process, "start"), nil, nil)
}

// Stop stops a project or one process within it.
func (c *Client) Stop(ctx context.Context, project, process string) error {
	return c.post(ctx, actionPath(project, process, "stop"), nil, nil)
}

// Restart restarts a project or one process within it.
func (c *Client) Restart(ctx context.Context, project, process string) error {
	return c.post(ctx, actionPath(project, process, "restart"), nil, nil)
}

func actionPath(project, process, action string) string {
	if process == "" {
		return "/" + url.PathEscape(project) + "/" + action
	}
	return "/" + url.PathEscape(project) + "/" + url.PathEscape(process) + "/" + action
}

// Add registers a project directory (must contain an app-nanny.toml).
func (c *Client) Add(ctx context.Context, name, path string) error {
	return c.post(ctx, "/add", map[string]string{"name": name, "path": path}, nil)
}

// Remove unregisters a project. Fails if it has running processes.
func (c *Client) Remove(ctx context.Context, name string) error {
	return c.post(ctx, "/remove", map[string]string{"name": name}, nil)
}

// Logs returns up to lines recent log lines for key ("project" or
// "project/process"). lines <= 0 uses the daemon's default (100).
func (c *Client) Logs(ctx context.Context, key string, lines int) (LogsResult, error) {
	path := "/logs/" + url.PathEscape(key)
	if lines > 0 {
		path += fmt.Sprintf("?lines=%d", lines)
	}
	var result LogsResult
	err := c.get(ctx, path, &result)
	return result, err
}

// Errors returns recent captured error/crash events for key, or just the
// most recent one if last is true.
func (c *Client) Errors(ctx context.Context, key string, last bool) ([]ErrorEvent, error) {
	path := "/errors/" + url.PathEscape(key)
	if last {
		path += "?last=true"
	}
	var result struct {
		Events []ErrorEvent `json:"events"`
	}
	err := c.get(ctx, path, &result)
	return result.Events, err
}

// internal/web/handlers.go
package web

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/huanghao/app-nanny/internal/ipc"
)

// apiPrefix is the versioned base path for the public API — see
// docs/2026-06-04-design.md and the "API v1" note added alongside this
// change for the versioning policy: additive changes (new fields, new
// routes) don't bump this; removing/renaming a field or changing its type
// or meaning does, and gets a new prefix (/api/v2) added alongside this one
// rather than breaking it in place.
const apiPrefix = "/api/v1"

// APIVersion is this contract's version — bump it (and start a parallel
// /api/v2 rather than editing routes/types in place) only on a breaking
// change per the policy above. First release of the API, hence "1.0".
const APIVersion = "1.0"

// ManagerIface is the subset of daemon.Manager needed by the web handlers.
// Structurally identical in spirit to daemon.ProcessManager but declared
// independently here — daemon.go imports this package (for NewMux), so
// this package can't import daemon.ProcessManager without a cycle. Both
// interfaces are satisfied by the same *daemon.Manager regardless.
type ManagerIface interface {
	Add(name, dir string) error
	Remove(name string) error
	Start(projectName, processName string) error
	Stop(projectName, processName string) error
	Restart(projectName, processName string) error
	PS() []ipc.Process
	DetailedStatus(projectName string) ipc.StatusResult
	LogPath(key string) string
	SubProcessKeys(project string) []string
	LogLines(key string, n int) []string
	RecentErrorEvents(key string, n int) []ipc.ErrorEvent
	ProjectToml(name string) (string, error)
	ProjectTomlActive(name string) (content string, loadedAt time.Time)
	ProjectTomlDiskMtime(name string) time.Time
}

// VersionInfo is reported at GET /api/v1/version. Version/Commit identify
// the build (informational); APIVersion is the contract version described
// above — the thing a client actually wants to check compatibility against.
type VersionInfo struct {
	Version    string
	Commit     string
	APIVersion string
}

// NewMux returns an http.ServeMux with all /api/v1 routes registered.
// Caller must also call RegisterSSERoute and register a static file handler.
func NewMux(mgr ManagerIface, info VersionInfo) *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc(apiPrefix+"/version", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]string{
			"version":     info.Version,
			"commit":      info.Commit,
			"api_version": info.APIVersion,
		})
	})

	mux.HandleFunc(apiPrefix+"/ps", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeAPIError(w, http.StatusMethodNotAllowed, errCodeBadRequest, "method not allowed")
			return
		}
		writeJSON(w, ipc.PSResult{Processes: mgr.PS()})
	})

	// GET /api/v1/status/<name> — per-project detail, same Process shape as ps.
	mux.HandleFunc(apiPrefix+"/status/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeAPIError(w, http.StatusMethodNotAllowed, errCodeBadRequest, "method not allowed")
			return
		}
		name := strings.TrimPrefix(r.URL.Path, apiPrefix+"/status/")
		if name == "" {
			writeAPIError(w, http.StatusBadRequest, errCodeBadRequest, "missing name")
			return
		}
		writeJSON(w, mgr.DetailedStatus(name))
	})

	// GET /api/v1/config/:name — return toml info for a project
	// Response: {"disk":"...","active":"...","stale":bool}
	// disk   = current file on disk
	// active = what was loaded at last Start() — empty if never started
	// stale  = disk != active (restart needed to apply changes)
	mux.HandleFunc(apiPrefix+"/config/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, apiPrefix+"/config/")
		if name == "" {
			writeAPIError(w, http.StatusBadRequest, errCodeBadRequest, "missing name")
			return
		}
		disk, err := mgr.ProjectToml(name)
		if err != nil {
			writeAPIError(w, http.StatusNotFound, errCodeNotFound, err.Error())
			return
		}
		active, loadedAt := mgr.ProjectTomlActive(name)
		diskMtime := mgr.ProjectTomlDiskMtime(name)
		stale := active != "" && active != disk
		fmtTime := func(t time.Time) string {
			if t.IsZero() {
				return ""
			}
			return t.Format("01-02 15:04:05")
		}
		writeJSON(w, map[string]any{
			"disk":       disk,
			"active":     active,
			"stale":      stale,
			"loaded_at":  fmtTime(loadedAt),
			"disk_mtime": fmtTime(diskMtime),
		})
	})

	// POST /api/v1/add {"name":"...","path":"..."}
	mux.HandleFunc(apiPrefix+"/add", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeAPIError(w, http.StatusMethodNotAllowed, errCodeBadRequest, "method not allowed")
			return
		}
		var p ipc.AddParams
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			writeAPIError(w, http.StatusBadRequest, errCodeBadRequest, err.Error())
			return
		}
		if err := mgr.Add(p.Name, p.Path); err != nil {
			writeAPIError(w, http.StatusInternalServerError, errCodeInternal, err.Error())
			return
		}
		writeJSON(w, ipc.AddResult{Name: p.Name, Path: p.Path})
	})

	// POST /api/v1/remove {"name":"..."}
	mux.HandleFunc(apiPrefix+"/remove", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeAPIError(w, http.StatusMethodNotAllowed, errCodeBadRequest, "method not allowed")
			return
		}
		var p ipc.RemoveParams
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			writeAPIError(w, http.StatusBadRequest, errCodeBadRequest, err.Error())
			return
		}
		if err := mgr.Remove(p.Name); err != nil {
			writeAPIError(w, http.StatusInternalServerError, errCodeInternal, err.Error())
			return
		}
		writeJSON(w, map[string]string{"ok": "true"})
	})

	// GET /api/v1/logs/<name-or-name/process>[?lines=100 | /stream] is
	// registered by RegisterSSERoute (internal/web/sse.go) — both the
	// paginated snapshot and the SSE stream live under the same prefix, so
	// they have to be one registration (net/http.ServeMux panics on two
	// handlers for the same pattern); see that file for why.

	// GET /api/v1/errors/<name-or-name/process>?last=true
	mux.HandleFunc(apiPrefix+"/errors/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeAPIError(w, http.StatusMethodNotAllowed, errCodeBadRequest, "method not allowed")
			return
		}
		key := strings.TrimPrefix(r.URL.Path, apiPrefix+"/errors/")
		if key == "" {
			writeAPIError(w, http.StatusBadRequest, errCodeBadRequest, "missing key")
			return
		}
		n := 10
		if r.URL.Query().Get("last") == "true" {
			n = 1
		}
		writeJSON(w, ipc.ErrorsResult{Events: mgr.RecentErrorEvents(key, n)})
	})

	// POST /api/v1/<name>/action  or  POST /api/v1/<name>/<process>/action
	mux.HandleFunc(apiPrefix+"/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeAPIError(w, http.StatusMethodNotAllowed, errCodeBadRequest, "method not allowed")
			return
		}
		path := strings.TrimPrefix(r.URL.Path, apiPrefix+"/")
		parts := strings.Split(path, "/")

		var project, process, action string
		switch len(parts) {
		case 2:
			project, action = parts[0], parts[1]
		case 3:
			project, process, action = parts[0], parts[1], parts[2]
		default:
			writeAPIError(w, http.StatusBadRequest, errCodeBadRequest, "invalid path")
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
			writeAPIError(w, http.StatusBadRequest, errCodeBadRequest, "unknown action: "+action)
			return
		}
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, errCodeInternal, err.Error())
			return
		}
		writeJSON(w, map[string]string{"ok": "true"})
	})

	// Root redirect
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/static/index.html", http.StatusFound)
			return
		}
		http.NotFound(w, r)
	})

	mux.Handle("/static/", StaticHandler())

	return mux
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}

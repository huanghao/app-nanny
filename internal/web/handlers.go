// internal/web/handlers.go
package web

import (
	"encoding/json"
	"fmt"
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
	ProjectToml(name string) (string, error)
}

// NewMux returns an http.ServeMux with all web console API routes registered.
// Caller must also call RegisterSSERoute and register a static file handler.
func NewMux(mgr ManagerIface) *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/ps", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, ipc.PSResult{Processes: mgr.PS()})
	})

	// GET /api/config/:name — return raw app-nanny.toml for a project
	mux.HandleFunc("/api/config/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/api/config/")
		if name == "" {
			http.Error(w, "missing name", http.StatusBadRequest)
			return
		}
		content, err := mgr.ProjectToml(name)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprint(w, content)
	})

	// POST /api/<name>/action  or  POST /api/<name>/<process>/action
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/api/")
		parts := strings.Split(path, "/")

		var project, process, action string
		switch len(parts) {
		case 2:
			project, action = parts[0], parts[1]
		case 3:
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

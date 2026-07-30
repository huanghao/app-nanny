// internal/web/sse.go
package web

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/huanghao/app-nanny/internal/ipc"
)

// SSELogsHandler streams log lines for key as Server-Sent Events.
// It polls LogLines every 200ms and emits new lines.
// The stream ends when the client disconnects (context done).
func SSELogsHandler(mgr ManagerIface, key string, w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	fmt.Fprintf(w, ": connected\n\n")
	flusher.Flush()

	ctx := r.Context()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	var sent int

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

// RegisterSSERoute adds the /api/v1/logs/:key route to mux — both the
// paginated snapshot (GET .../logs/<key>?lines=100) and the streamed tail
// (GET .../logs/<key>/stream) live under the same prefix pattern, so they
// have to share one registration: net/http.ServeMux panics if you register
// two handlers for the same pattern, and a variable key segment followed by
// a fixed "/stream" suffix isn't expressible as two separate patterns in
// the classic ServeMux matching this project uses.
func RegisterSSERoute(mux *http.ServeMux, mgr ManagerIface) {
	mux.HandleFunc(apiPrefix+"/logs/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeAPIError(w, http.StatusMethodNotAllowed, errCodeBadRequest, "method not allowed")
			return
		}
		path := strings.TrimPrefix(r.URL.Path, apiPrefix+"/logs/")
		if strings.HasSuffix(path, "/stream") {
			// Path may contain "/" for "project/process" — matches the
			// "-"-joined key convention the log files themselves use.
			key := strings.ReplaceAll(strings.TrimSuffix(path, "/stream"), "/", "-")
			if key == "" {
				writeAPIError(w, http.StatusBadRequest, errCodeBadRequest, "missing key")
				return
			}
			SSELogsHandler(mgr, key, w, r)
			return
		}

		key := strings.TrimSuffix(path, "/")
		if key == "" {
			writeAPIError(w, http.StatusBadRequest, errCodeBadRequest, "missing key")
			return
		}
		n := 100
		if raw := r.URL.Query().Get("lines"); raw != "" {
			if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
				n = parsed
			}
		}
		logPath := mgr.LogPath(key)
		var subKeys []string
		if logPath == "" {
			subKeys = mgr.SubProcessKeys(key)
		}
		writeJSON(w, ipc.LogsResult{
			Lines:   mgr.LogLines(key, n),
			Path:    logPath,
			SubKeys: subKeys,
		})
	})
}

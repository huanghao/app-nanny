// internal/web/sse.go
package web

import (
	"fmt"
	"net/http"
	"strings"
	"time"
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

// RegisterSSERoute adds the /api/logs/:key/stream SSE route to mux.
func RegisterSSERoute(mux *http.ServeMux, mgr ManagerIface) {
	mux.HandleFunc("/api/logs/", func(w http.ResponseWriter, r *http.Request) {
		// Path: /api/logs/<key>/stream  (key may contain "-" for "proj-process")
		path := strings.TrimPrefix(r.URL.Path, "/api/logs/")
		path = strings.TrimSuffix(path, "/stream")
		key := strings.ReplaceAll(path, "/", "-")
		if key == "" {
			http.Error(w, "missing key", http.StatusBadRequest)
			return
		}
		SSELogsHandler(mgr, key, w, r)
	})
}

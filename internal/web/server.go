// internal/web/server.go
package web

import (
	"log"
	"net/http"
	"strings"
)

// OriginMiddleware rejects cross-origin mutating requests (non-GET/HEAD).
// GET/HEAD are allowed from any origin (read-only).
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

// Server is the HTTP server for the web console.
type Server struct {
	addr    string
	handler http.Handler
}

// NewServer constructs a Server wrapping handler with OriginMiddleware.
func NewServer(addr string, handler http.Handler) *Server {
	return &Server{addr: addr, handler: OriginMiddleware(handler)}
}

// Start listens and serves. Blocks until error.
func (s *Server) Start() error {
	log.Printf("web: listening on %s", s.addr)
	return http.ListenAndServe(s.addr, s.handler)
}

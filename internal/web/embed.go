// internal/web/embed.go
package web

import (
	"embed"
	"net/http"
)

//go:embed static
var staticFiles embed.FS

// StaticHandler returns an http.Handler serving the embedded static files.
func StaticHandler() http.Handler {
	return http.FileServer(http.FS(staticFiles))
}

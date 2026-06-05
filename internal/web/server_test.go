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
		req := httptest.NewRequest("POST", "/api/start", nil)
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

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

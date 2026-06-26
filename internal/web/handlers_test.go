// internal/web/handlers_test.go
package web_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/huanghao/app-nanny/internal/ipc"
	"github.com/huanghao/app-nanny/internal/web"
)

// stubManager implements web.ManagerIface for testing.
type stubManager struct {
	psResult   []ipc.ProcessInfo
	startErr   error
	stopErr    error
	restartErr error
}

func (s *stubManager) PS() []ipc.ProcessInfo               { return s.psResult }
func (s *stubManager) Start(n, p string) error             { return s.startErr }
func (s *stubManager) Stop(n, p string) error              { return s.stopErr }
func (s *stubManager) Restart(n, p string) error           { return s.restartErr }
func (s *stubManager) LogLines(key string, n int) []string    { return []string{"line1", "line2"} }
func (s *stubManager) ProjectToml(name string) (string, error) {
	return `name = "` + name + `"` + "\n" + `command = "just dev"` + "\n", nil
}
func (s *stubManager) ProjectTomlActive(name string) (string, time.Time) { return "", time.Time{} }
func (s *stubManager) ProjectTomlDiskMtime(name string) time.Time        { return time.Time{} }

func TestHandlePS(t *testing.T) {
	stub := &stubManager{psResult: []ipc.ProcessInfo{
		{Project: "myapp", Status: "running", PID: 1234},
	}}
	mux := web.NewMux(stub)

	req := httptest.NewRequest("GET", "/api/ps", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var result ipc.PSResult
	if err := json.Unmarshal(rr.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Processes) != 1 || result.Processes[0].Project != "myapp" {
		t.Errorf("unexpected ps result: %+v", result)
	}
}

func TestHandleAction_Start(t *testing.T) {
	stub := &stubManager{}
	mux := web.NewMux(stub)

	req := httptest.NewRequest("POST", "/api/myapp/start", strings.NewReader(""))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHandleAction_UnknownAction(t *testing.T) {
	stub := &stubManager{}
	mux := web.NewMux(stub)

	req := httptest.NewRequest("POST", "/api/myapp/explode", strings.NewReader(""))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for unknown action, got %d", rr.Code)
	}
}

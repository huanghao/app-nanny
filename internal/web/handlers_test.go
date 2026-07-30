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
	psResult       []ipc.Process
	statusResult   ipc.StatusResult
	startErr       error
	stopErr        error
	restartErr     error
	addErr         error
	removeErr      error
	logPathResult  string
	subKeysResult  []string
	logLinesResult []string
	errorsResult   []ipc.ErrorEvent
}

func (s *stubManager) Add(name, dir string) error                 { return s.addErr }
func (s *stubManager) Remove(name string) error                   { return s.removeErr }
func (s *stubManager) PS() []ipc.Process                          { return s.psResult }
func (s *stubManager) DetailedStatus(name string) ipc.StatusResult { return s.statusResult }
func (s *stubManager) Start(n, p string) error                    { return s.startErr }
func (s *stubManager) Stop(n, p string) error                     { return s.stopErr }
func (s *stubManager) Restart(n, p string) error                  { return s.restartErr }
func (s *stubManager) LogPath(key string) string                  { return s.logPathResult }
func (s *stubManager) SubProcessKeys(project string) []string     { return s.subKeysResult }
func (s *stubManager) LogLines(key string, n int) []string {
	if s.logLinesResult != nil {
		return s.logLinesResult
	}
	return []string{"line1", "line2"}
}
func (s *stubManager) RecentErrorEvents(key string, n int) []ipc.ErrorEvent { return s.errorsResult }
func (s *stubManager) ProjectToml(name string) (string, error) {
	return `name = "` + name + `"` + "\n" + `command = "just dev"` + "\n", nil
}
func (s *stubManager) ProjectTomlActive(name string) (string, time.Time) { return "", time.Time{} }
func (s *stubManager) ProjectTomlDiskMtime(name string) time.Time        { return time.Time{} }

func testMux(stub *stubManager) *http.ServeMux {
	mux := web.NewMux(stub, web.VersionInfo{Version: "test", Commit: "abc", APIVersion: web.APIVersion})
	web.RegisterSSERoute(mux, stub)
	return mux
}

func TestHandlePS(t *testing.T) {
	stub := &stubManager{psResult: []ipc.Process{
		{Project: "myapp", Status: "running", PID: 1234},
	}}
	mux := testMux(stub)

	req := httptest.NewRequest("GET", "/api/v1/ps", nil)
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

func TestHandleStatus(t *testing.T) {
	stub := &stubManager{statusResult: ipc.StatusResult{Processes: []ipc.Process{
		{Key: "myapp/backend", Status: "running"},
	}}}
	mux := testMux(stub)

	req := httptest.NewRequest("GET", "/api/v1/status/myapp", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var result ipc.StatusResult
	if err := json.Unmarshal(rr.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Processes) != 1 || result.Processes[0].Key != "myapp/backend" {
		t.Errorf("unexpected status result: %+v", result)
	}
}

func TestHandleVersion(t *testing.T) {
	stub := &stubManager{}
	mux := testMux(stub)

	req := httptest.NewRequest("GET", "/api/v1/version", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	var result map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result["api_version"] != web.APIVersion {
		t.Errorf("api_version = %q, want %q", result["api_version"], web.APIVersion)
	}
}

func TestHandleErrors(t *testing.T) {
	stub := &stubManager{errorsResult: []ipc.ErrorEvent{{Time: "12:00:00", Key: "myapp", Lines: []string{"boom"}}}}
	mux := testMux(stub)

	req := httptest.NewRequest("GET", "/api/v1/errors/myapp", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var result ipc.ErrorsResult
	if err := json.Unmarshal(rr.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Events) != 1 || result.Events[0].Lines[0] != "boom" {
		t.Errorf("unexpected errors result: %+v", result)
	}
}

func TestHandleAction_Start(t *testing.T) {
	stub := &stubManager{}
	mux := testMux(stub)

	req := httptest.NewRequest("POST", "/api/v1/myapp/start", strings.NewReader(""))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHandleAction_UnknownAction(t *testing.T) {
	stub := &stubManager{}
	mux := testMux(stub)

	req := httptest.NewRequest("POST", "/api/v1/myapp/explode", strings.NewReader(""))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for unknown action, got %d", rr.Code)
	}
	var apiErr struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &apiErr); err != nil {
		t.Fatal(err)
	}
	if apiErr.Code != "bad_request" {
		t.Errorf("error code = %q, want bad_request", apiErr.Code)
	}
}

func TestHandleAdd(t *testing.T) {
	stub := &stubManager{}
	mux := testMux(stub)

	body := strings.NewReader(`{"name":"myapp","path":"/tmp/myapp"}`)
	req := httptest.NewRequest("POST", "/api/v1/add", body)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHandleRemove(t *testing.T) {
	stub := &stubManager{}
	mux := testMux(stub)

	body := strings.NewReader(`{"name":"myapp"}`)
	req := httptest.NewRequest("POST", "/api/v1/remove", body)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHandleLogsPaginated(t *testing.T) {
	stub := &stubManager{logLinesResult: []string{"a", "b", "c"}}
	mux := testMux(stub)

	req := httptest.NewRequest("GET", "/api/v1/logs/myapp?lines=3", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var result ipc.LogsResult
	if err := json.Unmarshal(rr.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Lines) != 3 {
		t.Errorf("Lines = %v, want 3 entries", result.Lines)
	}
}

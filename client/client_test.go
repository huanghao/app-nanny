package client_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/huanghao/app-nanny/client"
	"github.com/huanghao/app-nanny/internal/ipc"
	"github.com/huanghao/app-nanny/internal/web"
)

// fakeManager implements web.ManagerIface — used to spin up the *real*
// web.NewMux over httptest, so these tests prove client's requests/response
// decoding actually match the real server, not just a hand-simulated one.
type fakeManager struct {
	psResult     []ipc.Process
	statusResult ipc.StatusResult
	startCalls   []string
	startErr     error
	addCalls     []string
	removeCalls  []string
	logsResult   []string
	errorsResult []ipc.ErrorEvent
}

func (f *fakeManager) Add(name, dir string) error {
	f.addCalls = append(f.addCalls, name+"@"+dir)
	return nil
}
func (f *fakeManager) Remove(name string) error {
	f.removeCalls = append(f.removeCalls, name)
	return nil
}
func (f *fakeManager) PS() []ipc.Process                          { return f.psResult }
func (f *fakeManager) DetailedStatus(name string) ipc.StatusResult { return f.statusResult }
func (f *fakeManager) Start(n, p string) error {
	f.startCalls = append(f.startCalls, n+"/"+p)
	return f.startErr
}
func (f *fakeManager) Stop(n, p string) error                        { return nil }
func (f *fakeManager) Restart(n, p string) error                     { return nil }
func (f *fakeManager) LogPath(key string) string                     { return "" }
func (f *fakeManager) SubProcessKeys(project string) []string        { return nil }
func (f *fakeManager) LogLines(key string, n int) []string           { return f.logsResult }
func (f *fakeManager) RecentErrorEvents(key string, n int) []ipc.ErrorEvent { return f.errorsResult }
func (f *fakeManager) ProjectToml(name string) (string, error)        { return "", nil }
func (f *fakeManager) ProjectTomlActive(name string) (string, time.Time) {
	return "", time.Time{}
}
func (f *fakeManager) ProjectTomlDiskMtime(name string) time.Time { return time.Time{} }

func startTestServer(t *testing.T, mgr *fakeManager) *client.Client {
	t.Helper()
	mux := web.NewMux(mgr, web.VersionInfo{Version: "test", Commit: "abc123", APIVersion: web.APIVersion})
	web.RegisterSSERoute(mux, mgr)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return client.NewWithBaseURL(srv.URL + "/api/v1")
}

func TestClient_Version(t *testing.T) {
	c := startTestServer(t, &fakeManager{})
	v, err := c.Version(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if v.APIVersion != web.APIVersion {
		t.Errorf("APIVersion = %q, want %q", v.APIVersion, web.APIVersion)
	}
	if v.Commit != "abc123" {
		t.Errorf("Commit = %q, want abc123", v.Commit)
	}
}

func TestClient_PS(t *testing.T) {
	mgr := &fakeManager{psResult: []ipc.Process{
		{Key: "demo", Project: "demo", Status: "running", PID: 42, ActualPorts: []int{3000}},
	}}
	c := startTestServer(t, mgr)

	procs, err := c.PS(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(procs) != 1 || procs[0].Project != "demo" || procs[0].PID != 42 {
		t.Errorf("unexpected PS result: %+v", procs)
	}
	if len(procs[0].ActualPorts) != 1 || procs[0].ActualPorts[0] != 3000 {
		t.Errorf("ActualPorts = %v, want [3000]", procs[0].ActualPorts)
	}
}

func TestClient_Status(t *testing.T) {
	mgr := &fakeManager{statusResult: ipc.StatusResult{Processes: []ipc.Process{
		{Key: "demo/backend", Status: "running"},
	}}}
	c := startTestServer(t, mgr)

	procs, err := c.Status(context.Background(), "demo")
	if err != nil {
		t.Fatal(err)
	}
	if len(procs) != 1 || procs[0].Key != "demo/backend" {
		t.Errorf("unexpected Status result: %+v", procs)
	}
}

func TestClient_StartStopRestart(t *testing.T) {
	mgr := &fakeManager{}
	c := startTestServer(t, mgr)

	if err := c.Start(context.Background(), "demo", ""); err != nil {
		t.Fatal(err)
	}
	if err := c.Start(context.Background(), "demo", "backend"); err != nil {
		t.Fatal(err)
	}
	want := []string{"demo/", "demo/backend"}
	if len(mgr.startCalls) != 2 || mgr.startCalls[0] != want[0] || mgr.startCalls[1] != want[1] {
		t.Errorf("startCalls = %v, want %v", mgr.startCalls, want)
	}
}

func TestClient_AddRemove(t *testing.T) {
	mgr := &fakeManager{}
	c := startTestServer(t, mgr)

	if err := c.Add(context.Background(), "demo", "/tmp/demo"); err != nil {
		t.Fatal(err)
	}
	if len(mgr.addCalls) != 1 || mgr.addCalls[0] != "demo@/tmp/demo" {
		t.Errorf("addCalls = %v", mgr.addCalls)
	}

	if err := c.Remove(context.Background(), "demo"); err != nil {
		t.Fatal(err)
	}
	if len(mgr.removeCalls) != 1 || mgr.removeCalls[0] != "demo" {
		t.Errorf("removeCalls = %v", mgr.removeCalls)
	}
}

func TestClient_Logs(t *testing.T) {
	mgr := &fakeManager{logsResult: []string{"line one", "line two"}}
	c := startTestServer(t, mgr)

	result, err := c.Logs(context.Background(), "demo", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Lines) != 2 || result.Lines[0] != "line one" {
		t.Errorf("unexpected Logs result: %+v", result)
	}
}

func TestClient_Errors(t *testing.T) {
	mgr := &fakeManager{errorsResult: []ipc.ErrorEvent{{Time: "12:00:00", Key: "demo", Lines: []string{"boom"}}}}
	c := startTestServer(t, mgr)

	events, err := c.Errors(context.Background(), "demo", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Lines[0] != "boom" {
		t.Errorf("unexpected Errors result: %+v", events)
	}
}

func TestClient_ManagerErrorDecodesAsAPIError(t *testing.T) {
	mgr := &fakeManager{startErr: fmt.Errorf("project %q not registered", "demo")}
	c := startTestServer(t, mgr)

	err := c.Start(context.Background(), "demo", "")
	if err == nil {
		t.Fatal("expected an error")
	}
	apiErr, ok := err.(*client.APIError)
	if !ok {
		t.Fatalf("expected *client.APIError, got %T: %v", err, err)
	}
	if apiErr.Code != "internal" {
		t.Errorf("Code = %q, want internal", apiErr.Code)
	}
	if apiErr.Status != http.StatusInternalServerError {
		t.Errorf("Status = %d, want 500", apiErr.Status)
	}
}

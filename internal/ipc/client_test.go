package ipc_test

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/huanghao/app-nanny/internal/ipc"
)

func TestClientServer_RoundTrip(t *testing.T) {
	sockPath := filepath.Join(t.TempDir(), "test.sock")

	// Start a minimal server
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		// Echo the method back as result
		var req ipc.Request
		if err := json.NewDecoder(conn).Decode(&req); err != nil {
			return
		}
		resp, _ := ipc.OKResponse(map[string]string{"echo": req.Method})
		json.NewEncoder(conn).Encode(resp)
	}()

	client := ipc.NewClient(sockPath)
	resp, err := client.Call("ping", nil)
	if err != nil {
		t.Fatalf("Call error: %v", err)
	}
	var result map[string]string
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatal(err)
	}
	if result["echo"] != "ping" {
		t.Errorf("echo = %q, want %q", result["echo"], "ping")
	}
}

func TestClient_DaemonNotRunning(t *testing.T) {
	client := ipc.NewClient("/nonexistent/path.sock")
	_, err := client.Call("ps", nil)
	if err == nil {
		t.Error("expected error when daemon not running")
	}
	if !ipc.IsDaemonNotRunning(err) {
		t.Errorf("expected IsDaemonNotRunning=true, got false for error: %v", err)
	}
	_ = os.Remove("/nonexistent/path.sock")
}

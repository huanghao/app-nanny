// internal/daemon/process_test.go
package daemon_test

import (
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/huanghao/app-nanny/internal/config"
	"github.com/huanghao/app-nanny/internal/daemon"
)

func TestProcess_StartStop(t *testing.T) {
	if _, err := exec.LookPath("sleep"); err != nil {
		t.Skip("sleep not available")
	}

	proc := daemon.NewProcess("test-sleep", config.ProcessConfig{
		Command: "sleep 60",
	}, t.TempDir())

	if err := proc.Start(); err != nil {
		t.Fatalf("Start error: %v", err)
	}
	if proc.Status() != daemon.StatusRunning {
		t.Errorf("Status = %v, want Running", proc.Status())
	}
	if proc.PID() == 0 {
		t.Error("PID should be non-zero after start")
	}

	if err := proc.Stop(); err != nil {
		t.Fatalf("Stop error: %v", err)
	}
	if proc.Status() != daemon.StatusStopped {
		t.Errorf("Status = %v, want Stopped after stop", proc.Status())
	}
}

func TestProcess_CrashDetection(t *testing.T) {
	proc := daemon.NewProcess("test-crash", config.ProcessConfig{
		Command: "false",
	}, t.TempDir())

	if err := proc.Start(); err != nil {
		t.Fatalf("Start error: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if proc.Status() == daemon.StatusCrashed {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if proc.Status() != daemon.StatusCrashed {
		t.Errorf("Status = %v, want Crashed", proc.Status())
	}
}

func TestProcess_EnvInjection(t *testing.T) {
	dir := t.TempDir()
	outFile := dir + "/port.txt"

	proc := daemon.NewProcess("test-env", config.ProcessConfig{
		Command: "sh -c 'echo $PORT > " + outFile + "'",
	}, dir)
	proc.SetEnv(map[string]string{"PORT": "9999"})

	if err := proc.Start(); err != nil {
		t.Fatalf("Start error: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(outFile); err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("output file not created: %v", err)
	}
	if got := string(data); got != "9999\n" {
		t.Errorf("PORT = %q, want %q", got, "9999\n")
	}
}

func TestProcess_StopIdempotent(t *testing.T) {
	// Stopping an already-stopped process should not error
	proc := daemon.NewProcess("idle", config.ProcessConfig{Command: "sleep 60"}, t.TempDir())
	if err := proc.Stop(); err != nil {
		t.Errorf("Stop on stopped process should return nil, got %v", err)
	}
}

func TestProcess_StartAlreadyRunning(t *testing.T) {
	proc := daemon.NewProcess("dup", config.ProcessConfig{Command: "sleep 60"}, t.TempDir())
	if err := proc.Start(); err != nil {
		t.Fatal(err)
	}
	defer proc.Stop()

	err := proc.Start()
	if err == nil {
		t.Error("starting an already-running process should return error")
	}
}

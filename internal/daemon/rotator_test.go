// internal/daemon/rotator_test.go
package daemon_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/huanghao/app-nanny/internal/daemon"
)

func TestRotatingFile_WriteAndRead(t *testing.T) {
	dir := t.TempDir()
	rf, err := daemon.NewRotatingFile(filepath.Join(dir, "test.log"), 1024, 3)
	if err != nil {
		t.Fatal(err)
	}
	defer rf.Close()

	msg := "hello world\n"
	if _, err := rf.Write([]byte(msg)); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "test.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "hello world") {
		t.Errorf("expected file to contain %q, got %q", "hello world", string(data))
	}
}

func TestRotatingFile_Rotates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	// maxSize = 100 bytes, maxFiles = 3
	rf, err := daemon.NewRotatingFile(path, 100, 3)
	if err != nil {
		t.Fatal(err)
	}
	defer rf.Close()

	// Write enough data to trigger rotation
	line := strings.Repeat("x", 50) + "\n"
	for i := 0; i < 5; i++ {
		if _, err := rf.Write([]byte(line)); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}

	// Current file should exist
	if _, err := os.Stat(path); err != nil {
		t.Error("current log file should exist")
	}
	// At least one backup should exist
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Error("backup .1 should exist after rotation")
	}
}

func TestRotatingFile_MaxFilesEnforced(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	rf, err := daemon.NewRotatingFile(path, 50, 2) // only 2 files kept
	if err != nil {
		t.Fatal(err)
	}
	defer rf.Close()

	line := strings.Repeat("y", 30) + "\n"
	for i := 0; i < 8; i++ {
		rf.Write([]byte(line)) //nolint:errcheck
	}

	// .3 should NOT exist since maxFiles=2
	if _, err := os.Stat(path + ".3"); !os.IsNotExist(err) {
		t.Error("file .3 should not exist with maxFiles=2")
	}
}

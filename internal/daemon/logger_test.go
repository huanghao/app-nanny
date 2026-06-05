// internal/daemon/logger_test.go
package daemon_test

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/huanghao/app-nanny/internal/daemon"
)

// nopCloser wraps an io.Writer with a no-op Close.
type nopCloser struct{ io.Writer }

func (nopCloser) Close() error { return nil }

func TestLogger_CapturesLines(t *testing.T) {
	var buf bytes.Buffer
	er := daemon.NewErrorRing()
	lg := daemon.NewLogger(nopCloser{&buf}, er, "svc", nil)

	lg.Write([]byte("line one\nline two\n")) //nolint:errcheck

	lines := lg.TailLines(10)
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 lines in ring, got %d", len(lines))
	}
	if !strings.Contains(lines[len(lines)-2], "line one") {
		t.Errorf("expected 'line one' in ring, got %v", lines)
	}
}

func TestLogger_HandlesPartialWrite(t *testing.T) {
	var buf bytes.Buffer
	er := daemon.NewErrorRing()
	lg := daemon.NewLogger(nopCloser{&buf}, er, "svc", nil)

	lg.Write([]byte("hel")) //nolint:errcheck
	lg.Write([]byte("lo\n")) //nolint:errcheck

	lines := lg.TailLines(5)
	found := false
	for _, l := range lines {
		if strings.Contains(l, "hello") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'hello' assembled from partial writes, ring=%v", lines)
	}
}

func TestLogger_DetectsError(t *testing.T) {
	var buf bytes.Buffer
	er := daemon.NewErrorRing()
	lg := daemon.NewLogger(nopCloser{&buf}, er, "svc", nil)

	lg.Write([]byte("GET /api 500 3ms\n")) //nolint:errcheck

	events := er.RecentForKey("svc", 5)
	if len(events) == 0 {
		t.Error("expected error event after 500 line, got none")
	}
}

func TestLogger_RingCapAt500(t *testing.T) {
	var buf bytes.Buffer
	er := daemon.NewErrorRing()
	lg := daemon.NewLogger(nopCloser{&buf}, er, "svc", nil)

	for i := 0; i < 600; i++ {
		lg.Write([]byte("line\n")) //nolint:errcheck
	}

	lines := lg.TailLines(1000)
	if len(lines) > 500 {
		t.Errorf("ring should cap at 500, got %d", len(lines))
	}
}

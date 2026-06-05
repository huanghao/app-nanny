// internal/daemon/errors_test.go
package daemon_test

import (
	"testing"
	"time"

	"github.com/huanghao/app-nanny/internal/daemon"
)

func TestErrorRing_AddAndRecent(t *testing.T) {
	r := daemon.NewErrorRing()

	r.Add(daemon.ErrorEvent{Time: time.Now(), Key: "svc/a", Lines: []string{"err1"}})
	r.Add(daemon.ErrorEvent{Time: time.Now(), Key: "svc/b", Lines: []string{"err2"}})

	all := r.RecentForKey("", 10)
	if len(all) != 2 {
		t.Fatalf("expected 2 events, got %d", len(all))
	}
}

func TestErrorRing_FilterByKey(t *testing.T) {
	r := daemon.NewErrorRing()
	r.Add(daemon.ErrorEvent{Time: time.Now(), Key: "alpha", Lines: []string{"a"}})
	r.Add(daemon.ErrorEvent{Time: time.Now(), Key: "beta", Lines: []string{"b"}})
	r.Add(daemon.ErrorEvent{Time: time.Now(), Key: "alpha", Lines: []string{"c"}})

	got := r.RecentForKey("alpha", 10)
	if len(got) != 2 {
		t.Fatalf("expected 2 alpha events, got %d", len(got))
	}
	for _, e := range got {
		if e.Key != "alpha" {
			t.Errorf("expected key=alpha, got %q", e.Key)
		}
	}
}

func TestErrorRing_CapAt50(t *testing.T) {
	r := daemon.NewErrorRing()
	for i := 0; i < 60; i++ {
		r.Add(daemon.ErrorEvent{Time: time.Now(), Key: "k", Lines: []string{"x"}})
	}
	got := r.RecentForKey("k", 100)
	if len(got) > 50 {
		t.Errorf("ring should cap at 50, got %d", len(got))
	}
}

func TestMatchesError(t *testing.T) {
	tests := []struct {
		line string
		want bool
	}{
		{"GET /api 500 3ms", true},
		{"GET /api 200 3ms", false},
		{"Traceback (most recent call last):", true},
		{"Error: something failed", true},
		{"TypeError: cannot read property", true},
		{"panic: runtime error", true},
		{"INFO: server started", false},
		{"FATAL: disk full", true},
	}
	for _, tt := range tests {
		got := daemon.MatchesError(tt.line, nil)
		if got != tt.want {
			t.Errorf("MatchesError(%q) = %v, want %v", tt.line, got, tt.want)
		}
	}
}

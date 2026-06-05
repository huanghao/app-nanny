// internal/daemon/errors.go
package daemon

import (
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/huanghao/app-nanny/internal/config"
)

var http5xxRe = regexp.MustCompile(`\b5\d{2}\b`)

// ErrorEvent is one captured error occurrence with surrounding context lines.
type ErrorEvent struct {
	Time  time.Time `json:"time"`
	Key   string    `json:"key"`
	Lines []string  `json:"lines"`
}

// ErrorRing is a thread-safe circular buffer holding the last 50 error events.
type ErrorRing struct {
	mu     sync.Mutex
	events [50]ErrorEvent
	pos    int
	count  int
}

// NewErrorRing returns an empty ErrorRing.
func NewErrorRing() *ErrorRing { return &ErrorRing{} }

// Add inserts an event into the ring.
func (r *ErrorRing) Add(e ErrorEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events[r.pos] = e
	r.pos = (r.pos + 1) % 50
	if r.count < 50 {
		r.count++
	}
}

// RecentForKey returns up to n recent events matching key.
// key="" returns all events; key="proj" matches "proj" and "proj/process".
func (r *ErrorRing) RecentForKey(key string, n int) []ErrorEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []ErrorEvent
	for i := 0; i < r.count && len(out) < n; i++ {
		idx := (r.pos - 1 - i + 50) % 50
		e := r.events[idx]
		if key == "" || e.Key == key || strings.HasPrefix(e.Key, key+"/") {
			out = append(out, e)
		}
	}
	return out
}

// MatchesError reports whether line should trigger error capture.
func MatchesError(line string, extra []config.ErrorPattern) bool {
	if http5xxRe.MatchString(line) {
		return true
	}
	triggers := []string{
		"Traceback (most recent call last)",
		"Error:",
		"TypeError:",
		"ReferenceError:",
		"SyntaxError:",
		"panic:",
		"FATAL",
		"CRITICAL",
	}
	for _, t := range triggers {
		if strings.Contains(line, t) {
			return true
		}
	}
	for _, p := range extra {
		if p.Match != "" && strings.Contains(line, p.Match) {
			return true
		}
	}
	return false
}

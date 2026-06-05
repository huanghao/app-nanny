// internal/daemon/logger.go
package daemon

import (
	"bytes"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/huanghao/app-nanny/internal/config"
)

const ringCap = 500

// Logger is an io.Writer that:
//   - writes timestamped lines to a backing writer (typically a RotatingFile)
//   - keeps the last 500 lines in a ring buffer for `nanny logs`
//   - detects error patterns and records events to an ErrorRing
type Logger struct {
	mu       sync.Mutex
	out      io.WriteCloser
	errRing  *ErrorRing
	key      string
	patterns []config.ErrorPattern

	buf []byte // partial line accumulator

	ring    [ringCap]string
	ringPos int
	ringLen int
}

// NewLogger creates a Logger that writes to out.
func NewLogger(out io.WriteCloser, errRing *ErrorRing, key string, patterns []config.ErrorPattern) *Logger {
	return &Logger{out: out, errRing: errRing, key: key, patterns: patterns}
}

// Write implements io.Writer. It splits p into lines and processes each one.
func (l *Logger) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	data := append(l.buf, p...)
	l.buf = nil

	for {
		idx := bytes.IndexByte(data, '\n')
		if idx < 0 {
			break
		}
		line := string(data[:idx])
		data = data[idx+1:]
		l.processLineLocked(line)
	}
	if len(data) > 0 {
		l.buf = append(l.buf, data...)
	}
	return len(p), nil
}

func (l *Logger) processLineLocked(line string) {
	ts := time.Now().Format("15:04:05")
	fmt.Fprintf(l.out, "%s %s\n", ts, line)

	// Add to ring
	l.ring[l.ringPos] = line
	l.ringPos = (l.ringPos + 1) % ringCap
	if l.ringLen < ringCap {
		l.ringLen++
	}

	// Check error patterns
	if MatchesError(line, l.patterns) {
		context := l.tailLocked(35)
		l.errRing.Add(ErrorEvent{
			Time:  time.Now(),
			Key:   l.key,
			Lines: context,
		})
	}
}

// tailLocked returns the last n lines from the ring. Caller must hold l.mu.
func (l *Logger) tailLocked(n int) []string {
	if l.ringLen == 0 {
		return nil
	}
	actual := n
	if actual > l.ringLen {
		actual = l.ringLen
	}
	out := make([]string, actual)
	for i := 0; i < actual; i++ {
		idx := (l.ringPos - actual + i + ringCap) % ringCap
		out[i] = l.ring[idx]
	}
	return out
}

// TailLines returns the last n lines from the in-memory ring buffer.
func (l *Logger) TailLines(n int) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.tailLocked(n)
}

// Close flushes any partial line and closes the backing writer.
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.buf) > 0 {
		l.processLineLocked(string(l.buf))
		l.buf = nil
	}
	return l.out.Close()
}

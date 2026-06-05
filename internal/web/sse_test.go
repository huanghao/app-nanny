// internal/web/sse_test.go
package web_test

import (
	"bufio"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/huanghao/app-nanny/internal/web"
)

func TestSSEHandler_StreamsLines(t *testing.T) {
	stub := &stubManager{}
	req := httptest.NewRequest("GET", "/api/logs/myapp/stream", nil)
	rr := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		web.SSELogsHandler(stub, "myapp", rr, req)
	}()

	time.Sleep(500 * time.Millisecond)

	body := rr.Body.String()
	if !strings.Contains(body, "data:") {
		t.Errorf("expected SSE data lines, got: %q", body)
	}
}

func TestSSEHeaders(t *testing.T) {
	stub := &stubManager{}
	req := httptest.NewRequest("GET", "/api/logs/myapp/stream", nil)
	rr := httptest.NewRecorder()

	go web.SSELogsHandler(stub, "myapp", rr, req)
	time.Sleep(50 * time.Millisecond)

	ct := rr.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/event-stream") {
		t.Errorf("expected Content-Type text/event-stream, got %q", ct)
	}
}

func TestSSEFormat(t *testing.T) {
	stub := &stubManager{}
	req := httptest.NewRequest("GET", "/api/logs/x/stream", nil)
	rr := httptest.NewRecorder()

	go web.SSELogsHandler(stub, "x", rr, req)
	time.Sleep(400 * time.Millisecond)

	scanner := bufio.NewScanner(strings.NewReader(rr.Body.String()))
	for scanner.Scan() {
		line := scanner.Text()
		if line != "" && !strings.HasPrefix(line, "data:") && !strings.HasPrefix(line, ":") {
			t.Errorf("unexpected SSE line: %q", line)
		}
	}
}

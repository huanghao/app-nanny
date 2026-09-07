package ipc

import "encoding/json"

type Request struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

type Response struct {
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

func ErrorResponse(msg string) Response {
	return Response{Error: msg}
}

func OKResponse(v any) (Response, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return Response{}, err
	}
	return Response{Result: data}, nil
}

type AddParams struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type RemoveParams struct {
	Name string `json:"name"`
}

type StartParams struct {
	Name    string `json:"name"`
	Process string `json:"process,omitempty"`
}

type StopParams struct {
	Name    string `json:"name"`
	Process string `json:"process,omitempty"`
}

type RestartParams struct {
	Name    string `json:"name"`
	Process string `json:"process,omitempty"`
}

type StatusParams struct {
	Name string `json:"name"`
}

// Process is the one canonical record of a tracked (running, stopped, or
// crashed) process — used by both "ps" (PSResult) and "status"
// (StatusResult). These used to be two separate, field-divergent types
// (ProcessInfo / ProcessStatus) that drifted from each other; unifying them
// is part of stabilizing this as a real API surface, not just an
// implementation detail two call sites happened to each shape their own way.
//
// Not to be confused with daemon.Process (internal/daemon/process.go), the
// live in-process tracker this type is a point-in-time snapshot of.
type Process struct {
	// Key is Project (Mode A) or "Project/Process" (Mode B) — the same
	// identifier accepted back as StartParams.Name/.Process etc.
	Key           string  `json:"key"`
	Project       string  `json:"project"`
	Process       string  `json:"process"`
	Status        string  `json:"status"`
	PID           int     `json:"pid"`
	Uptime        string  `json:"uptime"`
	Restarts      int     `json:"restarts"`
	DeclaredPort  int     `json:"declared_port"`
	ActualPorts   []int   `json:"actual_ports"`
	OtelService   string  `json:"otel_service,omitempty"` // declared otel_service_name, "" if not opted in
	MemMB         float64 `json:"mem_mb"`
	CPUPercent    float64 `json:"cpu_percent"`
	WorkDir       string  `json:"work_dir"`
	ErrorCount    int     `json:"error_count"`
	LastErrorTime string  `json:"last_error_time"` // RFC3339, empty if no errors
	LastLogTime   string  `json:"last_log_time"`   // RFC3339, time of last log line
	LogPath       string  `json:"log_path"`        // "" for a Mode B project key (no single file — see SubKeys)
}

type PSResult struct {
	Processes []Process `json:"processes"`
}

type AddResult struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// --- Observability params ---

type LogsParams struct {
	Name    string `json:"name"`
	Process string `json:"process,omitempty"`
	Lines   int    `json:"lines"` // 0 = default 100
}

type LogsResult struct {
	Lines   []string `json:"lines"`
	Path    string   `json:"path"`    // log file path for -f mode; empty = can't follow (multi-process)
	SubKeys []string `json:"sub_keys"` // populated when Path is empty: the per-process keys to use
}

type ErrorsParams struct {
	Name    string `json:"name"`
	Process string `json:"process,omitempty"`
	Last    bool   `json:"last"` // return only the most recent event
}

type ErrorsResult struct {
	Events []ErrorEvent `json:"events"`
}

// ErrorEvent mirrors daemon.ErrorEvent for JSON transport.
type ErrorEvent struct {
	Time  string   `json:"time"`
	Key   string   `json:"key"`
	Lines []string `json:"lines"`
}

// StatusResult is the response to "status <name>".
type StatusResult struct {
	Processes []Process `json:"processes"`
}

// GCParams requests cleanup of nanny-managed disk state that isn't tied to
// any currently registered project (orphaned log files) or has grown past
// its cap without an active rotator (daemon.log — see internal/daemon/gc.go).
type GCParams struct {
	DryRun bool `json:"dry_run"` // report what would be removed without touching disk
}

type GCResult struct {
	RemovedLogs         []string `json:"removed_logs"`           // basenames under logs/ with no owning registered project/process
	FreedBytes          int64    `json:"freed_bytes"`            // total size of RemovedLogs
	DaemonLogCapped     bool     `json:"daemon_log_capped"`      // whether daemon.log was over the cap
	DaemonLogFreedBytes int64    `json:"daemon_log_freed_bytes"` // bytes trimmed from daemon.log (0 if not capped)
}

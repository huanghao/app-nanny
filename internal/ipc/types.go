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

type ProcessInfo struct {
	Project      string  `json:"project"`
	Process      string  `json:"process"`
	Status       string  `json:"status"`
	PID          int     `json:"pid"`
	Uptime       string  `json:"uptime"`
	Restarts     int     `json:"restarts"`
	DeclaredPort int     `json:"declared_port"`
	ActualPorts  []int   `json:"actual_ports"`
	MemMB        float64 `json:"mem_mb"`
}

type PSResult struct {
	Processes []ProcessInfo `json:"processes"`
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
	Processes []ProcessStatus `json:"processes"`
}

// ProcessStatus is the detailed view of one process.
type ProcessStatus struct {
	Key         string  `json:"key"`
	Status      string  `json:"status"`
	PID         int     `json:"pid"`
	Uptime      string  `json:"uptime"`
	Restarts    int     `json:"restarts"`
	MemMB       float64 `json:"mem_mb"`
	ActualPorts []int   `json:"actual_ports"`
	ErrorCount  int     `json:"error_count"`
	LogPath     string  `json:"log_path"`
}

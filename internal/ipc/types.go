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

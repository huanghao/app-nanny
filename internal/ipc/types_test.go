package ipc_test

import (
	"encoding/json"
	"testing"

	"github.com/huanghao/app-nanny/internal/ipc"
)

func TestRequest_RoundTrip(t *testing.T) {
	req := ipc.Request{
		Method: "start",
		Params: mustMarshal(ipc.StartParams{Name: "md-viewer"}),
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var got ipc.Request
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Method != "start" {
		t.Errorf("Method = %q, want %q", got.Method, "start")
	}
	var params ipc.StartParams
	if err := json.Unmarshal(got.Params, &params); err != nil {
		t.Fatal(err)
	}
	if params.Name != "md-viewer" {
		t.Errorf("Name = %q, want %q", params.Name, "md-viewer")
	}
}

func TestResponse_Error(t *testing.T) {
	resp := ipc.ErrorResponse("something went wrong")
	if resp.Error != "something went wrong" {
		t.Errorf("Error = %q", resp.Error)
	}
	if resp.Result != nil {
		t.Error("Result should be nil on error response")
	}
}

func mustMarshal(v any) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}

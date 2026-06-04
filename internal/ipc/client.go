package ipc

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
)

var ErrDaemonNotRunning = errors.New("nanny daemon is not running (run: nanny daemon start)")

type Client struct {
	socketPath string
}

func NewClient(socketPath string) *Client {
	return &Client{socketPath: socketPath}
}

func (c *Client) Call(method string, params any) (*Response, error) {
	conn, err := net.Dial("unix", c.socketPath)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDaemonNotRunning, err)
	}
	defer conn.Close()

	var rawParams json.RawMessage
	if params != nil {
		rawParams, err = json.Marshal(params)
		if err != nil {
			return nil, err
		}
	}

	req := Request{Method: method, Params: rawParams}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}

	var resp Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("daemon error: %s", resp.Error)
	}
	return &resp, nil
}

func IsDaemonNotRunning(err error) bool {
	return errors.Is(err, ErrDaemonNotRunning)
}

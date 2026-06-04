package ipc

import (
	"encoding/json"
	"log"
	"net"
)

type Handler func(params json.RawMessage) (any, error)

type Server struct {
	socketPath string
	handlers   map[string]Handler
}

func NewServer(socketPath string) *Server {
	return &Server{
		socketPath: socketPath,
		handlers:   make(map[string]Handler),
	}
}

func (s *Server) Handle(method string, h Handler) {
	s.handlers[method] = h
}

func (s *Server) Serve(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go s.handleConn(conn)
	}
}

func (s *Server) handleConn(conn net.Conn) {
	defer conn.Close()
	var req Request
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		log.Printf("ipc: decode request: %v", err)
		return
	}

	h, ok := s.handlers[req.Method]
	if !ok {
		resp := ErrorResponse("unknown method: " + req.Method)
		json.NewEncoder(conn).Encode(resp) //nolint:errcheck
		return
	}

	result, err := h(req.Params)
	var resp Response
	if err != nil {
		resp = ErrorResponse(err.Error())
	} else {
		resp, err = OKResponse(result)
		if err != nil {
			resp = ErrorResponse("marshal result: " + err.Error())
		}
	}
	json.NewEncoder(conn).Encode(resp) //nolint:errcheck
}

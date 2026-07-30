// internal/web/errors.go
package web

import (
	"encoding/json"
	"net/http"
)

// apiError is the JSON body for any /api/v1 error response — a small fixed
// set of machine-readable codes (below) plus a human message, so a client
// can branch on code instead of string-matching Error(). Deliberately
// coarse for now: Manager's own errors are plain `error` values with no
// code of their own, so this can't yet distinguish e.g. "already running"
// from "config invalid" — codeForStatus below maps only by HTTP status.
// Finer-grained codes can follow once Manager's errors carry their own
// classification.
type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

const (
	errCodeBadRequest = "bad_request"
	errCodeNotFound   = "not_found"
	errCodeInternal   = "internal"
)

func writeAPIError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(apiError{Code: code, Message: message}) //nolint:errcheck
}

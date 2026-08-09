package api

import (
	"encoding/json"
	"log"
	"net/http"
)

// writeJSON encodes v as the response body - any encode failure is
// logged (the headers/status are already sent by this point, so there's
// nothing left to report to the client).
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("failed to encode JSON response: %v", err)
	}
}

// errorResponse is every failed handler's response shape - Code is a
// small, stable enum (see statusCode) a client can branch on
// programmatically, distinct from Error's human-readable message text
// (which is free to change wording without breaking anything that
// switches on Code).
type errorResponse struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}

// writeError writes {"error": message, "code": <derived from status>} -
// deliberately derived from status rather than threaded through as an
// extra parameter at every one of this package's ~30 call sites: every
// call site already picks the right http.Status* for what happened, so
// the code falls out of that for free, consistently, without touching
// any of them.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorResponse{Error: message, Code: statusCode(status)})
}

// statusCode maps an HTTP status to errorResponse's stable Code string.
func statusCode(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "bad_request"
	case http.StatusUnauthorized:
		return "unauthorized"
	case http.StatusForbidden:
		return "forbidden"
	case http.StatusNotFound:
		return "not_found"
	case http.StatusConflict:
		return "conflict"
	case http.StatusTooManyRequests:
		return "rate_limited"
	case http.StatusInternalServerError:
		return "internal_error"
	default:
		return "error"
	}
}

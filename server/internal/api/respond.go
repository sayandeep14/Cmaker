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

type errorResponse struct {
	Error string `json:"error"`
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorResponse{Error: message})
}

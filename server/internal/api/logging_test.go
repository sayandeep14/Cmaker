package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLoggingMiddlewarePassesThroughResponse(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte("hello"))
	})

	req := httptest.NewRequest(http.MethodGet, "/anything", nil)
	w := httptest.NewRecorder()
	loggingMiddleware(inner).ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d", w.Code, http.StatusCreated)
	}
	if w.Body.String() != "hello" {
		t.Errorf("body = %q, want hello", w.Body.String())
	}
}

func TestStatusCapturingWriterDefaultsTo200(t *testing.T) {
	rec := httptest.NewRecorder()
	w := &statusCapturingWriter{ResponseWriter: rec, status: http.StatusOK}
	w.Write([]byte("no explicit WriteHeader call"))
	if w.status != http.StatusOK {
		t.Errorf("status = %d, want %d (default)", w.status, http.StatusOK)
	}
}

func TestStatusCapturingWriterCapturesExplicitStatus(t *testing.T) {
	rec := httptest.NewRecorder()
	w := &statusCapturingWriter{ResponseWriter: rec, status: http.StatusOK}
	w.WriteHeader(http.StatusNotFound)
	if w.status != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.status, http.StatusNotFound)
	}
}

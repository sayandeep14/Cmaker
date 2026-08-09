package api

import (
	"log"
	"net/http"
	"time"
)

// loggingMiddleware logs one line per request - method, path, resulting
// status, and duration. Basic (no request IDs, no structured/JSON log
// output) is deliberate for the POC: Fly.io already captures stdout, and
// this is meant to answer "is anything hitting this server, and is it
// erroring" at a glance, not feed a log aggregation pipeline.
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		lw := &statusCapturingWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(lw, r)
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, lw.status, time.Since(start))
	})
}

// statusCapturingWriter wraps http.ResponseWriter purely to observe which
// status code a handler actually sent - WriteHeader is often never called
// explicitly for a 200 (the first Write call implies it), but every
// handler in this package always goes through writeJSON, which does call
// WriteHeader explicitly every time, so that implicit-200 case never
// actually arises here in practice.
type statusCapturingWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusCapturingWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

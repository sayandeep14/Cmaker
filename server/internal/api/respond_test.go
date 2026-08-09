package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteErrorIncludesCode(t *testing.T) {
	cases := []struct {
		status int
		want   string
	}{
		{http.StatusBadRequest, "bad_request"},
		{http.StatusUnauthorized, "unauthorized"},
		{http.StatusForbidden, "forbidden"},
		{http.StatusNotFound, "not_found"},
		{http.StatusConflict, "conflict"},
		{http.StatusTooManyRequests, "rate_limited"},
		{http.StatusInternalServerError, "internal_error"},
		{http.StatusTeapot, "error"}, // an unmapped status still gets a fallback code
	}
	for _, c := range cases {
		w := httptest.NewRecorder()
		writeError(w, c.status, "something went wrong")

		var resp errorResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("status %d: failed to decode response: %v", c.status, err)
		}
		if resp.Code != c.want {
			t.Errorf("status %d: Code = %q, want %q", c.status, resp.Code, c.want)
		}
		if resp.Error != "something went wrong" {
			t.Errorf("status %d: Error = %q, unexpected", c.status, resp.Error)
		}
	}
}

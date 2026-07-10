package main

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestStatusRecorderCapturesStatus(t *testing.T) {
	rec := httptest.NewRecorder()
	sr := &statusRecorder{ResponseWriter: rec, status: http.StatusOK}
	sr.WriteHeader(http.StatusTeapot)
	if sr.status != http.StatusTeapot {
		t.Errorf("captured=%d want %d", sr.status, http.StatusTeapot)
	}
	if rec.Code != http.StatusTeapot {
		t.Errorf("passthrough Code=%d want %d", rec.Code, http.StatusTeapot)
	}
}

func TestStatusRecorderFlushes(t *testing.T) {
	rec := httptest.NewRecorder()
	sr := &statusRecorder{ResponseWriter: rec, status: http.StatusOK}
	// should not panic when underlying supports Flusher (httptest.ResponseRecorder does)
	sr.Flush()
}

func TestHasAuth(t *testing.T) {
	h := http.Header{}
	if hasAuth(h) {
		t.Error("empty header should report no auth")
	}
	h.Set("Authorization", "Bearer secret")
	if !hasAuth(h) {
		t.Error("Authorization present should report auth")
	}
}

func TestRequestLogRedactsKey(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setUpstream(r.Context(), "wss://host/path")
		w.WriteHeader(200)
	})
	req := httptest.NewRequest("POST", "/v1/messages", nil)
	req.Header.Set("Authorization", "Bearer super-secret-key")
	rec := httptest.NewRecorder()

	withRequestLog(h).ServeHTTP(rec, req)

	out := buf.String()
	if !contains(out, "auth=true") {
		t.Errorf("log missing auth=true: %q", out)
	}
	if bytes.Contains(buf.Bytes(), []byte("super-secret-key")) {
		t.Errorf("log leaked key value: %q", out)
	}
	if !contains(out, "wss://host/path") {
		t.Errorf("log missing upstream url: %q", out)
	}
}

func contains(s, sub string) bool {
	return bytes.Contains([]byte(s), []byte(sub))
}

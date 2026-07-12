package core

import (
	"context"
	"net/http"
	"time"
)

type logCtxKey struct{}

type requestLog struct {
	upstream string
	auth     bool
}

// statusRecorder wraps http.ResponseWriter to capture the status code while
// still flushing through (important for SSE streaming). The committed flag is
// set on the first WriteHeader or Write, so the proxy can tell whether headers
// are still mutable (e.g. to emit a 504 on an upstream read timeout before any
// frame was flushed) or already sent.
type statusRecorder struct {
	http.ResponseWriter
	status    int
	committed bool
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.committed = true
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	s.committed = true
	return s.ResponseWriter.Write(b)
}

func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// withRequestLog wraps a handler so every request emits one line:
// method path status latency upstream=<url> auth=<bool>. Auth values are never logged.
func withRequestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sr := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		rl := &requestLog{auth: hasAuth(r.Header)}
		ctx := context.WithValue(r.Context(), logCtxKey{}, rl)
		next.ServeHTTP(sr, r.WithContext(ctx))
		logf(LevelInfo, "%s %s %d %s upstream=%s auth=%v",
			r.Method, r.URL.RequestURI(), sr.status, time.Since(start), rl.upstream, rl.auth)
	})
}

func hasAuth(h http.Header) bool {
	return h.Get("Authorization") != "" || h.Get("x-api-key") != ""
}

// setUpstream stashes the dialed upstream URL into the request log context.
// Defined as a var so proxy.go can reference it without import cycles.
var setUpstream = func(ctx context.Context, upstream string) {
	if rl, ok := ctx.Value(logCtxKey{}).(*requestLog); ok {
		rl.upstream = upstream
	}
}

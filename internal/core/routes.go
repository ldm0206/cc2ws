package core

import "net/http"

// newRouter wires the supported routes. Proxy paths are POST-only (Go 1.22 method
// routing returns 405 for other methods). Unmatched paths fall through to 404.
//
// Frame modes are decided by route (never by sniffing frame content):
//   /v1/responses, /v1/responses/compact → typed-JSON frames
//   /v1/chat/completions, /v1/messages, /anthropic/v1/messages → SSE-bytes frames
func newRouter(cfg Config) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","version":"` + Version + `"}`))
	})
	mux.Handle("POST /v1/chat/completions", newProxyHandler(cfg, FrameModeSSEBytes))
	mux.Handle("POST /v1/responses", newProxyHandler(cfg, FrameModeTypedJSON))
	mux.Handle("POST /v1/responses/compact", newProxyHandler(cfg, FrameModeTypedJSON))
	mux.Handle("POST /v1/messages", newProxyHandler(cfg, FrameModeSSEBytes))
	mux.Handle("POST /anthropic/v1/messages", newProxyHandler(cfg, FrameModeSSEBytes))
	// No "/" catch-all: a subtree-matching "/" handler would shadow Go 1.22's
	// native 405 (it matches every method/path, so method-mismatched requests to
	// known POST routes would fall through to it as 404 instead of 405). Letting
	// the mux return its default 404 / 405 preserves the method-routing behavior.
	return mux
}

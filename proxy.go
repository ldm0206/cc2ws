package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/gorilla/websocket"
)

// authForwardHeaders is the allowlist forwarded on the upstream WS handshake.
// Never logged by value (logging.go records presence only).
var authForwardHeaders = []string{
	"Authorization",
	"x-api-key",
	"anthropic-version",
	"anthropic-beta",
	"OpenAI-Organization",
	"OpenAI-Project",
}

// newProxyHandler builds a per-request HTTP handler that dials one upstream WS,
// sends the raw JSON body as a single text message, and pumps frames back.
func newProxyHandler(cfg Config, mode FrameMode) http.HandlerFunc {
	dialer := &websocket.Dialer{
		TLSClientConfig:  &tls.Config{InsecureSkipVerify: cfg.InsecureSkipTLSVerify},
		HandshakeTimeout: cfg.ConnectTimeout,
	}
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeProxyError(w, http.StatusBadRequest, "read request body failed: "+err.Error())
			return
		}
		if !json.Valid(body) {
			writeProxyError(w, http.StatusBadRequest, "invalid json body")
			return
		}
		stream := detectStream(body, mode)
		upURL := cfg.upstreamURL(r.URL)
		setUpstream(r.Context(), upURL)

		ctx, cancel := context.WithTimeout(r.Context(), cfg.ConnectTimeout)
		defer cancel()
		conn, _, err := dialer.DialContext(ctx, upURL, forwardHeaders(r.Header))
		if err != nil {
			status := http.StatusBadGateway
			if errors.Is(err, context.DeadlineExceeded) {
				status = http.StatusGatewayTimeout
			}
			writeProxyError(w, status, "upstream websocket dial failed: "+err.Error())
			log.Printf("dial %s: %v", upURL, err)
			return
		}
		defer conn.Close()
		_ = conn.SetReadDeadline(time.Now().Add(cfg.IdleTimeout))

		if err := conn.WriteMessage(websocket.TextMessage, body); err != nil {
			writeProxyError(w, http.StatusBadGateway, "upstream write failed: "+err.Error())
			return
		}

		var pumpErr error
		switch mode {
		case FrameModeTypedJSON:
			pumpErr = pumpTypedJSON(w, conn, stream)
		default:
			pumpErr = pumpSSEBytes(w, conn, stream)
		}
		if pumpErr != nil {
			log.Printf("pump %s: %v", r.URL.Path, pumpErr)
		}
	}
}

// detectStream reads only the top-level "stream" field (body is not mutated).
// Responses default to streaming (the WS is itself an event stream); chat/messages
// default off. An explicit stream field always wins.
func detectStream(body []byte, mode FrameMode) bool {
	var probe struct {
		Stream *bool `json:"stream"`
	}
	_ = json.Unmarshal(body, &probe)
	if probe.Stream != nil {
		return *probe.Stream
	}
	return mode == FrameModeTypedJSON
}

func (c Config) upstreamURL(u *url.URL) string {
	out := c.UpstreamWS + u.Path
	if u.RawQuery != "" {
		out += "?" + u.RawQuery
	}
	return out
}

func forwardHeaders(h http.Header) http.Header {
	out := http.Header{}
	for _, k := range authForwardHeaders {
		if v := h.Get(k); v != "" {
			out.Set(k, v)
		}
	}
	return out
}

// writeProxyError emits the canonical proxy error JSON envelope.
func writeProxyError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	b, _ := json.Marshal(map[string]any{
		"error": map[string]any{"message": msg, "type": "proxy_error"},
	})
	_, _ = w.Write(b)
}

// setUpstream is replaced in logging.go (Task 4). Temporary no-op stub.
var setUpstream = func(ctx context.Context, upstream string) {}

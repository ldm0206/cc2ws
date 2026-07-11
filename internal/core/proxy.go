package core

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
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

// idleConn refreshes the read deadline before every ReadMessage so IDLE_TIMEOUT
// acts as a true per-read idle timeout, not an absolute wall-clock cap on the
// whole stream (a 600s default would otherwise kill any legitimate stream that
// runs longer than 10 minutes).
type idleConn struct {
	*websocket.Conn
	idle time.Duration
}

func (c *idleConn) ReadMessage() (int, []byte, error) {
	_ = c.Conn.SetReadDeadline(time.Now().Add(c.idle))
	return c.Conn.ReadMessage()
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
			if isTimeoutErr(err) { // context deadline OR net handshake read-timeout
				status = http.StatusGatewayTimeout
			}
			writeProxyError(w, status, "upstream websocket dial failed: "+err.Error())
			logf(LevelWarn, "dial %s: %v", upURL, err)
			return
		}
		defer conn.Close()

		// Write the request body with a deadline (ConnectTimeout) so a hung upstream
		// socket can't stall the request indefinitely; clear it again right away so
		// it does not affect any later writes.
		_ = conn.SetWriteDeadline(time.Now().Add(cfg.ConnectTimeout))
		writeErr := conn.WriteMessage(websocket.TextMessage, body)
		_ = conn.SetWriteDeadline(time.Time{})
		if writeErr != nil {
			writeProxyError(w, http.StatusBadGateway, "upstream write failed: "+writeErr.Error())
			return
		}

		// IDLE_TIMEOUT is enforced per-read by idleConn (deadline refreshed before
		// every ReadMessage), not as an absolute cap set once here.
		reader := &idleConn{Conn: conn, idle: cfg.IdleTimeout}

		var pumpErr error
		switch mode {
		case FrameModeTypedJSON:
			pumpErr = pumpTypedJSON(w, reader, stream)
		default:
			pumpErr = pumpSSEBytes(w, reader, stream)
		}
		if pumpErr != nil {
			if errors.Is(pumpErr, errReadTimeout) {
				logf(LevelWarn, "upstream read timeout %s", r.URL.Path)
				// If the pump already committed headers (mid-stream frame was flushed),
				// the status code is already sent — nothing more we can do. Otherwise
				// emit a 504 so the client sees the timeout.
				if sr, ok := w.(*statusRecorder); ok {
					if !sr.committed {
						writeProxyError(w, http.StatusGatewayTimeout, "upstream read timeout")
					}
				} else {
					// No statusRecorder wrapping the handler (e.g. direct unit-test
					// calls). Best-effort 504; http.ResponseWriter ignores WriteHeader
					// after body bytes have started, so this is safe either way.
					writeProxyError(w, http.StatusGatewayTimeout, "upstream read timeout")
				}
			} else {
				logf(LevelWarn, "pump %s: %v", r.URL.Path, pumpErr)
			}
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

// forwardHeaders copies the allowlisted request headers through to the upstream
// WS handshake, preserving every value for keys that carry multiple (notably
// anthropic-beta, which may legitimately carry several comma-separated values).
func forwardHeaders(h http.Header) http.Header {
	out := http.Header{}
	for _, k := range authForwardHeaders {
		if vals := h.Values(k); len(vals) > 0 {
			out[http.CanonicalHeaderKey(k)] = vals
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

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
)

// FrameMode selects how upstream WS text frames are turned into an HTTP response.
// Decided by route, never by sniffing frame content.
type FrameMode int

const (
	FrameModeSSEBytes FrameMode = iota // chat completions / anthropic messages
	FrameModeTypedJSON                 // openai responses (/responses, /responses/compact)
)

// messageReader abstracts *websocket.Conn.ReadMessage so pumps are unit-testable
// with a fake reader instead of a live WS connection.
type messageReader interface {
	ReadMessage() (messageType int, p []byte, err error)
}

// terminalResponseEvents ends a typed-JSON event stream when seen.
var terminalResponseEvents = map[string]bool{
	"response.completed":  true,
	"response.failed":     true,
	"response.cancelled":  true,
	"response.incomplete": true,
	"error":               true,
}

// errReadTimeout signals that an upstream WS read timed out (net.Error.Timeout()
// or context.DeadlineExceeded). The proxy turns this into 504 (or an SSE error
// frame if headers are already committed mid-stream). Clean closes / EOF return
// nil from classifyReadError and are treated as a normal end of stream.
var errReadTimeout = errors.New("upstream read timeout")

// isTimeoutErr reports whether err is a genuine timeout: a net.Error whose
// Timeout() is true, or context.DeadlineExceeded. Shared by the upstream read
// path (pumps) and the dial path (proxy) so that a dial handshake read-timeout
// (a net.Error, not context.DeadlineExceeded) maps to 504 the same way a
// context-deadline dial timeout does.
func isTimeoutErr(err error) bool {
	if err == nil {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	return errors.Is(err, context.DeadlineExceeded)
}

// classifyReadError returns errReadTimeout for genuine timeouts (see isTimeoutErr);
// nil for clean closes / EOF / other errors (treated as a normal end of stream).
func classifyReadError(err error) error {
	if isTimeoutErr(err) {
		return errReadTimeout
	}
	return nil
}

// mapErrorStatus extracts a numeric HTTP status from a buffered upstream JSON
// error body. Returns 502 (Bad Gateway) as the fallback when no parseable
// 4xx/5xx code is present (e.g. string codes like "insufficient_quota"). It
// checks error.status, error.code, and a top-level status, in that order.
func mapErrorStatus(raw []byte) int {
	var p struct {
		Error struct {
			Status json.RawMessage `json:"status"`
			Code   json.RawMessage `json:"code"`
		} `json:"error"`
		Status json.RawMessage `json:"status"`
	}
	if json.Unmarshal(raw, &p) != nil {
		return http.StatusBadGateway
	}
	for _, v := range []json.RawMessage{p.Error.Status, p.Error.Code, p.Status} {
		if s := numericStatus(v); s != 0 {
			return s
		}
	}
	return http.StatusBadGateway
}

// numericStatus returns v as an HTTP status if it unmarshals as a number in
// [400,599]; otherwise 0 (string codes like "insufficient_quota" are not statuses).
func numericStatus(v json.RawMessage) int {
	if len(v) == 0 {
		return 0
	}
	var n float64
	if json.Unmarshal(v, &n) == nil && n >= 400 && n <= 599 {
		return int(n)
	}
	return 0
}

func setSSEHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
}

func flush(w http.ResponseWriter) {
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

// writeSSETimeoutError emits a best-effort SSE error frame on an already-streaming
// response whose upstream read timed out. Used only after the first frame has been
// flushed (headers committed), so the status code can no longer be changed.
func writeSSETimeoutError(w http.ResponseWriter) {
	_, _ = fmt.Fprintf(w, "event: error\ndata: %s\n\n",
		`{"error":{"type":"proxy_error","message":"upstream read timeout"}}`)
	flush(w)
}

// pumpSSEBytes copies each upstream text message verbatim into the response.
// stream=true  → text/event-stream live (one frame = one SSE chunk, written as-is).
//                On an upstream read timeout after at least one emitted frame, a
//                best-effort SSE error frame is written before returning errReadTimeout.
//                A timeout before any frame returns errReadTimeout without writing
//                (so the proxy can still emit 504).
// stream=false → buffer to a single application/json body; map upstream error
//                status code when present (4xx/5xx), else 502.
func pumpSSEBytes(w http.ResponseWriter, r messageReader, stream bool) error {
	if stream {
		setSSEHeaders(w)
		emitted := false
		for {
			_, data, err := r.ReadMessage()
			if err != nil {
				if e := classifyReadError(err); e != nil {
					if emitted {
						writeSSETimeoutError(w)
					}
					return e
				}
				return nil // upstream closed; end stream
			}
			if _, err := w.Write(data); err != nil {
				return err
			}
			flush(w)
			emitted = true
		}
	}
	var buf []byte
	for {
		_, data, err := r.ReadMessage()
		if err != nil {
			if e := classifyReadError(err); e != nil {
				return e // do NOT write buffered body — let the proxy emit 504
			}
			break
		}
		buf = append(buf, data...)
	}
	status := http.StatusOK
	var probe map[string]json.RawMessage
	if json.Unmarshal(buf, &probe) == nil {
		if _, ok := probe["error"]; ok {
			status = mapErrorStatus(buf)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(buf)
	return nil
}

// pumpTypedJSON turns each upstream typed-JSON frame into SSE (stream) or
// aggregates to one JSON body (non-stream).
// stream=true  → emit "event: <type>\ndata: <json>\n\n" per frame; stop at terminal event.
//                On an upstream read timeout after at least one emitted frame, a
//                best-effort SSE error frame is written before returning errReadTimeout.
// stream=false → aggregate until terminal; return response.completed.response (200)
//                or the terminal failure event body (status mapped from code, else 502).
func pumpTypedJSON(w http.ResponseWriter, r messageReader, stream bool) error {
	if stream {
		setSSEHeaders(w)
		emitted := false
		for {
			_, data, err := r.ReadMessage()
			if err != nil {
				if e := classifyReadError(err); e != nil {
					if emitted {
						writeSSETimeoutError(w)
					}
					return e
				}
				return nil
			}
			var head struct {
				Type string `json:"type"`
			}
			_ = json.Unmarshal(data, &head)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", head.Type, data)
			flush(w)
			emitted = true
			if terminalResponseEvents[head.Type] {
				return nil
			}
		}
	}
	var lastResponse json.RawMessage
	var failedBody json.RawMessage
	for {
		_, data, err := r.ReadMessage()
		if err != nil {
			if e := classifyReadError(err); e != nil {
				return e // do NOT write buffered body — let the proxy emit 504
			}
			break
		}
		var head struct {
			Type     string          `json:"type"`
			Response json.RawMessage `json:"response"`
		}
		if json.Unmarshal(data, &head) != nil {
			continue
		}
		switch head.Type {
		case "response.completed":
			lastResponse = head.Response
		case "response.failed", "response.cancelled", "response.incomplete", "error":
			failedBody = data
		}
		if terminalResponseEvents[head.Type] {
			break
		}
	}
	status := http.StatusOK
	if failedBody != nil {
		status = mapErrorStatus(failedBody)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	switch {
	case failedBody != nil:
		_, _ = w.Write(failedBody)
	case lastResponse != nil:
		_, _ = w.Write(lastResponse)
	default:
		_, _ = w.Write([]byte("{}"))
	}
	return nil
}

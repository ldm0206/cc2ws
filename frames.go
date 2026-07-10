package main

import (
	"encoding/json"
	"fmt"
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

// pumpSSEBytes copies each upstream text message verbatim into the response.
// stream=true  → text/event-stream live (one frame = one SSE chunk, written as-is).
// stream=false → buffer to a single application/json body; 502 if it parses as {"error":...}.
func pumpSSEBytes(w http.ResponseWriter, r messageReader, stream bool) error {
	if stream {
		setSSEHeaders(w)
		for {
			_, data, err := r.ReadMessage()
			if err != nil {
				return nil // upstream closed; end stream
			}
			if _, err := w.Write(data); err != nil {
				return err
			}
			flush(w)
		}
	}
	var buf []byte
	for {
		_, data, err := r.ReadMessage()
		if err != nil {
			break
		}
		buf = append(buf, data...)
	}
	status := http.StatusOK
	var probe map[string]json.RawMessage
	if json.Unmarshal(buf, &probe) == nil {
		if _, ok := probe["error"]; ok {
			status = http.StatusBadGateway
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
// stream=false → aggregate until terminal; return response.completed.response (200)
//                or the terminal failure event body (502).
func pumpTypedJSON(w http.ResponseWriter, r messageReader, stream bool) error {
	if stream {
		setSSEHeaders(w)
		for {
			_, data, err := r.ReadMessage()
			if err != nil {
				return nil
			}
			var head struct {
				Type string `json:"type"`
			}
			_ = json.Unmarshal(data, &head)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", head.Type, data)
			flush(w)
			if terminalResponseEvents[head.Type] {
				return nil
			}
		}
	}
	var lastResponse json.RawMessage
	var failedBody json.RawMessage
	status := http.StatusOK
	for {
		_, data, err := r.ReadMessage()
		if err != nil {
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
			status = http.StatusBadGateway
		}
		if terminalResponseEvents[head.Type] {
			break
		}
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

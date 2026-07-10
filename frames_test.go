package main

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeReader struct {
	msgs [][]byte
	i    int
}

func (f *fakeReader) ReadMessage() (int, []byte, error) {
	if f.i >= len(f.msgs) {
		return 0, nil, io.EOF
	}
	m := f.msgs[f.i]
	f.i++
	return 1, m, nil // 1 == TextMessage
}

func TestPumpSSEBytesStreamWritesVerbatim(t *testing.T) {
	fr := &fakeReader{msgs: [][]byte{
		[]byte("data: {\"a\":1}\n\n"),
		[]byte("data: {\"a\":2}\n\n"),
	}}
	rec := httptest.NewRecorder()
	if err := pumpSSEBytes(rec, fr, true); err != nil {
		t.Fatal(err)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type=%q want text/event-stream", ct)
	}
	want := "data: {\"a\":1}\n\ndata: {\"a\":2}\n\n"
	if rec.Body.String() != want {
		t.Errorf("body=%q want %q", rec.Body.String(), want)
	}
}

func TestPumpSSEBytesNonStreamReturnsJSON(t *testing.T) {
	fr := &fakeReader{msgs: [][]byte{[]byte(`{"id":"chatcmpl-1","choices":[]}`)}}
	rec := httptest.NewRecorder()
	if err := pumpSSEBytes(rec, fr, false); err != nil {
		t.Fatal(err)
	}
	if rec.Code != 200 {
		t.Errorf("status=%d want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type=%q want application/json", ct)
	}
	if !strings.Contains(rec.Body.String(), "chatcmpl-1") {
		t.Errorf("body=%q missing payload", rec.Body.String())
	}
}

func TestPumpSSEBytesNonStreamErrorMaps502(t *testing.T) {
	fr := &fakeReader{msgs: [][]byte{[]byte(`{"error":{"message":"bad"}}`)}}
	rec := httptest.NewRecorder()
	if err := pumpSSEBytes(rec, fr, false); err != nil {
		t.Fatal(err)
	}
	if rec.Code != 502 {
		t.Errorf("status=%d want 502", rec.Code)
	}
}

func TestPumpTypedJSONStreamEmitsSSE(t *testing.T) {
	fr := &fakeReader{msgs: [][]byte{
		[]byte(`{"type":"response.created"}`),
		[]byte(`{"type":"response.output_text.delta","delta":"hi"}`),
		[]byte(`{"type":"response.completed","response":{"id":"r1"}}`),
		[]byte(`{"type":"response.completed","response":{"id":"r1"}}`), // should NOT appear (terminal stops)
	}}
	rec := httptest.NewRecorder()
	if err := pumpTypedJSON(rec, fr, true); err != nil {
		t.Fatal(err)
	}
	body := rec.Body.String()
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type=%q want text/event-stream", ct)
	}
	for _, want := range []string{
		"event: response.created\ndata: {\"type\":\"response.created\"}",
		"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"hi\"}",
		"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"r1\"}}",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing:\n  %s\ngot:\n%s", want, body)
		}
	}
	if occurrences := strings.Count(body, "event: response.completed"); occurrences != 1 {
		t.Errorf("expected terminal emitted once, got %d", occurrences)
	}
}

func TestPumpTypedJSONNonStreamReturnsResponseObject(t *testing.T) {
	fr := &fakeReader{msgs: [][]byte{
		[]byte(`{"type":"response.created"}`),
		[]byte(`{"type":"response.output_text.delta","delta":"hi"}`),
		[]byte(`{"type":"response.completed","response":{"id":"r1","status":"completed"}}`),
	}}
	rec := httptest.NewRecorder()
	if err := pumpTypedJSON(rec, fr, false); err != nil {
		t.Fatal(err)
	}
	if rec.Code != 200 {
		t.Errorf("status=%d want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type=%q want application/json", ct)
	}
	want := `{"id":"r1","status":"completed"}`
	if rec.Body.String() != want {
		t.Errorf("body=%q want %q", rec.Body.String(), want)
	}
}

func TestPumpTypedJSONNonStreamErrorMaps502(t *testing.T) {
	fr := &fakeReader{msgs: [][]byte{
		[]byte(`{"type":"response.failed","error":{"message":"boom"}}`),
	}}
	rec := httptest.NewRecorder()
	if err := pumpTypedJSON(rec, fr, false); err != nil {
		t.Fatal(err)
	}
	if rec.Code != 502 {
		t.Errorf("status=%d want 502", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "boom") {
		t.Errorf("body=%q missing error payload", rec.Body.String())
	}
}

package core

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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

// fakeTimeoutReader returns the given messages normally, then a net.Error-style
// timeout on the next read (instead of EOF).
type fakeTimeoutReader struct {
	msgs [][]byte
	i    int
}

func (f *fakeTimeoutReader) ReadMessage() (int, []byte, error) {
	if f.i < len(f.msgs) {
		m := f.msgs[f.i]
		f.i++
		return 1, m, nil
	}
	return 0, nil, timeoutErr{}
}

// timeoutErr is a minimal net.Error whose Timeout()==true, so classifyReadError
// treats it as an upstream read timeout.
type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

func TestWriteErrorFrameOpenAI(t *testing.T) {
	rec := httptest.NewRecorder()
	writeErrorFrame(rec, DialectOpenAI, "upstream read timeout", 0, time.Time{})
	body := rec.Body.String()
	if !strings.Contains(body, "event: error\n") {
		t.Errorf("missing SSE error event: %q", body)
	}
	if !strings.Contains(body, `"type":"proxy_error"`) {
		t.Errorf("missing type: %q", body)
	}
	if !strings.Contains(body, `"code":"upstream_timeout"`) {
		t.Errorf("missing code: %q", body)
	}
	if !strings.Contains(body, `"message":"upstream read timeout"`) {
		t.Errorf("missing message: %q", body)
	}
	if strings.Contains(body, `"response"`) {
		t.Errorf("OpenAI frame must not carry response object: %q", body)
	}
}

func TestWriteErrorFrameAnthropic(t *testing.T) {
	rec := httptest.NewRecorder()
	writeErrorFrame(rec, DialectAnthropic, "upstream read timeout", 0, time.Time{})
	body := rec.Body.String()
	if !strings.Contains(body, "event: error\n") {
		t.Errorf("missing SSE error event: %q", body)
	}
	if !strings.Contains(body, `"type":"error"`) {
		t.Errorf("missing Anthropic type:error wrapper: %q", body)
	}
	if !strings.Contains(body, `"type":"proxy_error"`) {
		t.Errorf("missing inner type: %q", body)
	}
	if !strings.Contains(body, `"message":"upstream read timeout"`) {
		t.Errorf("missing message: %q", body)
	}
	if strings.Contains(body, `"code"`) {
		t.Errorf("Anthropic frame must not carry code: %q", body)
	}
}

func TestWriteErrorFrameResponses(t *testing.T) {
	start := time.Unix(1700000000, 0)
	rec := httptest.NewRecorder()
	writeErrorFrame(rec, DialectResponses, "upstream read timeout", 3, start)
	body := rec.Body.String()
	if !strings.Contains(body, "event: response.failed\n") {
		t.Errorf("missing response.failed event: %q", body)
	}
	if !strings.Contains(body, `"sequence_number":3`) {
		t.Errorf("missing sequence_number=3: %q", body)
	}
	if !strings.Contains(body, `"created_at":1700000000`) {
		t.Errorf("missing created_at: %q", body)
	}
	if !strings.Contains(body, `"status":"failed"`) {
		t.Errorf("missing status failed: %q", body)
	}
	if !strings.Contains(body, `"type":"proxy_error"`) || !strings.Contains(body, `"code":"upstream_timeout"`) {
		t.Errorf("missing error type/code: %q", body)
	}
	if !strings.Contains(body, `"message":"upstream read timeout"`) {
		t.Errorf("missing message: %q", body)
	}
}

func TestPumpSSEBytesStreamWritesVerbatim(t *testing.T) {
	fr := &fakeReader{msgs: [][]byte{
		[]byte("data: {\"a\":1}\n\n"),
		[]byte("data: {\"a\":2}\n\n"),
	}}
	rec := httptest.NewRecorder()
	if err := pumpSSEBytes(rec, fr, true, DialectOpenAI); err != nil {
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
	if err := pumpSSEBytes(rec, fr, false, DialectOpenAI); err != nil {
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
	if err := pumpSSEBytes(rec, fr, false, DialectOpenAI); err != nil {
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
	if err := pumpTypedJSON(rec, fr, true, DialectResponses, time.Unix(0, 0)); err != nil {
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
	if err := pumpTypedJSON(rec, fr, false, DialectResponses, time.Unix(0, 0)); err != nil {
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
	if err := pumpTypedJSON(rec, fr, false, DialectResponses, time.Unix(0, 0)); err != nil {
		t.Fatal(err)
	}
	if rec.Code != 502 {
		t.Errorf("status=%d want 502", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "boom") {
		t.Errorf("body=%q missing error payload", rec.Body.String())
	}
}

// After at least one frame has been flushed on a stream, an upstream read
// timeout must emit a best-effort SSE error frame and return errReadTimeout so
// the proxy can log it (the status code is already committed and cannot change).
func TestPumpSSEBytesStreamTimeoutWritesSSEError(t *testing.T) {
	fr := &fakeTimeoutReader{msgs: [][]byte{
		[]byte("data: {\"a\":1}\n\n"),
	}}
	rec := httptest.NewRecorder()
	err := pumpSSEBytes(rec, fr, true, DialectOpenAI)
	if !errors.Is(err, errReadTimeout) {
		t.Errorf("err=%v want errReadTimeout", err)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "event: error") {
		t.Errorf("body missing SSE error event: %q", body)
	}
	if !strings.Contains(body, "upstream read timeout") {
		t.Errorf("body missing timeout message: %q", body)
	}
}

// On a non-stream response, an upstream read timeout before any body is written
// must return errReadTimeout and write nothing, so the proxy can still emit 504.
func TestPumpSSEBytesNonStreamTimeoutReturns504Path(t *testing.T) {
	fr := &fakeTimeoutReader{} // immediate timeout, no frames
	rec := httptest.NewRecorder()
	err := pumpSSEBytes(rec, fr, false, DialectOpenAI)
	if !errors.Is(err, errReadTimeout) {
		t.Errorf("err=%v want errReadTimeout", err)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("body=%q want empty so proxy can emit 504", rec.Body.String())
	}
	if rec.Code != http.StatusOK {
		t.Errorf("code=%d want 200 (default; pump must not call WriteHeader)", rec.Code)
	}
}

// SSEBytes pump 超时 + Anthropic 方言 → 写出 Anthropic error 事件(无 code)。
func TestPumpSSEBytesStreamTimeoutAnthropicDialect(t *testing.T) {
	fr := &fakeTimeoutReader{msgs: [][]byte{
		[]byte("data: {\"a\":1}\n\n"),
	}}
	rec := httptest.NewRecorder()
	err := pumpSSEBytes(rec, fr, true, DialectAnthropic)
	if !errors.Is(err, errReadTimeout) {
		t.Errorf("err=%v want errReadTimeout", err)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "event: error\n") {
		t.Errorf("missing SSE error event: %q", body)
	}
	if !strings.Contains(body, `"type":"error"`) {
		t.Errorf("missing Anthropic type:error wrapper: %q", body)
	}
	if !strings.Contains(body, `"type":"proxy_error"`) {
		t.Errorf("missing inner type: %q", body)
	}
	if !strings.Contains(body, `"message":"upstream read timeout"`) {
		t.Errorf("missing message: %q", body)
	}
	if strings.Contains(body, `"code"`) {
		t.Errorf("Anthropic frame must not carry code: %q", body)
	}
}

// TypedJSON pump 超时 + Responses 方言 → 写出 response.failed 事件。
func TestPumpTypedJSONStreamTimeoutResponsesDialect(t *testing.T) {
	fr := &fakeTimeoutReader{msgs: [][]byte{
		[]byte(`{"type":"response.created"}`),
		[]byte(`{"type":"response.output_text.delta","delta":"hi"}`),
	}}
	rec := httptest.NewRecorder()
	start := time.Unix(1700000000, 0)
	err := pumpTypedJSON(rec, fr, true, DialectResponses, start)
	if !errors.Is(err, errReadTimeout) {
		t.Errorf("err=%v want errReadTimeout", err)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "event: response.failed\n") {
		t.Errorf("missing response.failed event: %q", body)
	}
	if !strings.Contains(body, `"sequence_number":2`) { // two frames emitted before timeout
		t.Errorf("missing sequence_number=2: %q", body)
	}
	if !strings.Contains(body, `"created_at":1700000000`) {
		t.Errorf("missing created_at: %q", body)
	}
	if !strings.Contains(body, `"status":"failed"`) {
		t.Errorf("missing status failed: %q", body)
	}
}

func TestMapErrorStatus(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want int
	}{
		{"numeric error.status", `{"error":{"status":429}}`, 429},
		{"string error.code falls back to 502", `{"error":{"code":"insufficient_quota"}}`, 502},
		{"string error.status falls back to 502", `{"error":{"status":"rate_limit"}}`, 502},
		{"top-level status", `{"status":402}`, 402},
		{"no status fields -> 502", `{}`, 502},
		{"error.status 500", `{"error":{"status":500}}`, 500},
		{"error.code numeric in range", `{"error":{"code":503}}`, 503},
		{"status below 400 ignored -> 502", `{"error":{"status":200}}`, 502},
		{"invalid json -> 502", `not json`, 502},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := mapErrorStatus([]byte(c.raw))
			if got != c.want {
				t.Errorf("mapErrorStatus(%s)=%d want %d", c.raw, got, c.want)
			}
		})
	}
}

// isTimeoutErr underpins both the pump read-timeout path and the dial-timeout
// path. Verify deterministically (the integration TestProxyDialTimeoutReturns504
// depends on host network behavior; this test does not).
func TestIsTimeoutErr(t *testing.T) {
	if !isTimeoutErr(context.DeadlineExceeded) {
		t.Error("context.DeadlineExceeded should be a timeout")
	}
	if !isTimeoutErr(timeoutErr{}) {
		t.Error("net.Error with Timeout()==true should be a timeout")
	}
	if isTimeoutErr(errors.New("connection refused")) {
		t.Error("plain non-timeout error should not classify as timeout")
	}
	if isTimeoutErr(nil) {
		t.Error("nil should not classify as timeout")
	}
}

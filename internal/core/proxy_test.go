package core

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// startStubUpstream launches a local WS server that upgrades, reads one body
// message, then calls fn to emit frames. Returns the ws:// URL for the proxy to dial.
func startStubUpstream(t *testing.T, fn func(c *websocket.Conn, body []byte)) string {
	t.Helper()
	up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer c.Close()
		_, body, err := c.ReadMessage()
		if err != nil {
			return
		}
		fn(c, body)
	}))
	t.Cleanup(srv.Close)
	wsURL, err := swapScheme(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	return wsURL
}

func testCfg(upstreamWS string) Config {
	return Config{
		UpstreamWS:     upstreamWS,
		ConnectTimeout: 2 * time.Second,
		IdleTimeout:    2 * time.Second,
	}
}

func TestProxySSEBytesStreamEndToEnd(t *testing.T) {
	upstream := startStubUpstream(t, func(c *websocket.Conn, body []byte) {
		_ = c.WriteMessage(websocket.TextMessage, []byte("data: {\"ok\":true}\n\n"))
		_ = c.WriteMessage(websocket.TextMessage, []byte("data: [DONE]\n\n"))
	})
	srv := httptest.NewServer(newProxyHandler(testCfg(upstream), FrameModeSSEBytes))
	t.Cleanup(srv.Close)

	resp, err := http.Post(srv.URL+"/v1/chat/completions", "application/json",
		strings.NewReader(`{"model":"x","stream":true,"messages":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type=%q", ct)
	}
	b, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(b), "data: {\"ok\":true}") {
		t.Errorf("body=%q", b)
	}
}

func TestProxyTypedJSONNonStreamEndToEnd(t *testing.T) {
	upstream := startStubUpstream(t, func(c *websocket.Conn, body []byte) {
		_ = c.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.created"}`))
		_ = c.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.completed","response":{"id":"r1"}}`))
	})
	srv := httptest.NewServer(newProxyHandler(testCfg(upstream), FrameModeTypedJSON))
	t.Cleanup(srv.Close)

	resp, err := http.Post(srv.URL+"/v1/responses", "application/json",
		strings.NewReader(`{"model":"x","stream":false,"input":"hi"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	b, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(b), `"id":"r1"`) {
		t.Errorf("body=%q", b)
	}
}

func TestProxyForwardsAuthHeaders(t *testing.T) {
	gotAuth := make(chan string, 1)
	gotCustom := make(chan string, 1)
	up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth <- r.Header.Get("Authorization") // capture before Upgrade
		gotCustom <- r.Header.Get("X-Not-Forwarded")
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		_, _, _ = c.ReadMessage()
		_ = c.WriteMessage(websocket.TextMessage, []byte("{}"))
	}))
	t.Cleanup(srv.Close)
	wsURL, _ := swapScheme(srv.URL)

	proxySrv := httptest.NewServer(newProxyHandler(testCfg(wsURL), FrameModeSSEBytes))
	t.Cleanup(proxySrv.Close)

	req, _ := http.NewRequest("POST", proxySrv.URL+"/v1/messages", strings.NewReader(`{"model":"x","messages":[]}`))
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("X-Not-Forwarded", "nope")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	select {
case a := <-gotAuth:
	if a != "Bearer secret" {
		t.Errorf("upstream got Authorization=%q want 'Bearer secret'", a)
	}
case <-time.After(time.Second):
	t.Fatal("upstream never received request")
}

	select {
case c := <-gotCustom:
	if c != "nope" {
		t.Errorf("upstream got X-Not-Forwarded=%q want 'nope' (all non-WS headers must forward)", c)
	}
case <-time.After(time.Second):
	t.Fatal("upstream never received request")
}
}

func TestProxyDialFailureReturns502(t *testing.T) {
	// port 1 is reserved/unreachable → dial fails fast
	srv := httptest.NewServer(newProxyHandler(testCfg("ws://127.0.0.1:1"), FrameModeSSEBytes))
	t.Cleanup(srv.Close)

	resp, err := http.Post(srv.URL+"/v1/messages", "application/json",
		strings.NewReader(`{"model":"x","messages":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 502 {
		t.Errorf("status=%d want 502", resp.StatusCode)
	}
}

func TestProxyInvalidJSONReturns400(t *testing.T) {
	srv := httptest.NewServer(newProxyHandler(testCfg("ws://127.0.0.1:1"), FrameModeSSEBytes))
	t.Cleanup(srv.Close)

	resp, err := http.Post(srv.URL+"/v1/messages", "application/json",
		strings.NewReader(`not json`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Errorf("status=%d want 400", resp.StatusCode)
	}
}

// Dialing a non-routable address with a short ConnectTimeout must surface a
// gateway-level error. The context deadline fires and yields 504; if the OS
// instead returns "no route to host" immediately, the dial fails as a plain
// 502. Both are acceptable per spec §7; 504 is the preferred/ideal outcome.
func TestProxyDialTimeoutReturns504(t *testing.T) {
	cfg := Config{
		UpstreamWS:     "ws://10.255.255.1:1", // non-routable, packets dropped
		ConnectTimeout: 250 * time.Millisecond,
		IdleTimeout:    time.Second,
	}
	srv := httptest.NewServer(newProxyHandler(cfg, FrameModeSSEBytes))
	t.Cleanup(srv.Close)

	resp, err := http.Post(srv.URL+"/v1/messages", "application/json",
		strings.NewReader(`{"model":"x","messages":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 504 && resp.StatusCode != 502 {
		t.Errorf("status=%d want 504 (or 502 if dial failed fast)", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type=%q want application/json", ct)
	}
}

// Multi-value forwarded headers (anthropic-beta can carry several values) must
// survive forwarding with each value intact, not be collapsed by Set/Get.
func TestProxyForwardsMultiValueHeader(t *testing.T) {
	gotBeta := make(chan []string, 1)
	up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBeta <- r.Header["Anthropic-Beta"]
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		_, _, _ = c.ReadMessage()
		_ = c.WriteMessage(websocket.TextMessage, []byte("{}"))
	}))
	t.Cleanup(srv.Close)
	wsURL, _ := swapScheme(srv.URL)

	proxySrv := httptest.NewServer(newProxyHandler(testCfg(wsURL), FrameModeSSEBytes))
	t.Cleanup(proxySrv.Close)

	req, _ := http.NewRequest("POST", proxySrv.URL+"/v1/messages", strings.NewReader(`{"model":"x","messages":[]}`))
	req.Header.Add("anthropic-beta", "prompt-caching-2024-07-31")
	req.Header.Add("anthropic-beta", "max-tokens-3-5-sonnet-2024-07-15")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	select {
	case vals := <-gotBeta:
		want := []string{"prompt-caching-2024-07-31", "max-tokens-3-5-sonnet-2024-07-15"}
		if len(vals) != 2 || vals[0] != want[0] || vals[1] != want[1] {
			t.Errorf("upstream anthropic-beta=%v want exactly %v", vals, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("upstream never received request")
	}
}

func TestDetectStream(t *testing.T) {
	cases := []struct {
		body []byte
		mode FrameMode
		want bool
	}{
		{[]byte(`{"stream":true}`), FrameModeSSEBytes, true},
		{[]byte(`{"stream":false}`), FrameModeSSEBytes, false},
		{[]byte(`{}`), FrameModeSSEBytes, false}, // chat defaults off
		{[]byte(`{}`), FrameModeTypedJSON, true}, // responses default on
		{[]byte(`{"stream":false}`), FrameModeTypedJSON, false},
	}
	for i, c := range cases {
		got := detectStream(c.body, c.mode)
		if got != c.want {
			t.Errorf("case %d: detectStream=%v want %v", i, got, c.want)
		}
	}
}

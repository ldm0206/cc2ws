# WS 错误帧按端点方言输出 — 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让代理在流中途自身故障(上游读超时)时,按入站路由的方言发出客户端能解析的终止错误帧(OpenAI / Anthropic / Responses),替代当前统一 OpenAI 形状。

**Architecture:** 新增 `ErrorDialect` 枚举,与现有 `FrameMode` 正交。`FrameMode` 决定正常数据怎么 pump(不变);`ErrorDialect` 决定代理自身错误帧的形状。路由表是方言唯一真相源,经 `newProxyHandler` 透传到 pump。删除统一形状的 `writeSSETimeoutError`,替换为按方言输出的 `writeErrorFrame`。上游推来的帧一律逐字节透传,不拦截不改包。

**Tech Stack:** Go 1.22+(`net/http`,Go 1.22 method routing),`encoding/json`,无新依赖。

## Global Constraints

- 错误内容只来自代理自身故障(读超时);`type` 固定 `"proxy_error"`,`code` 固定 `"upstream_timeout"`,仅 `message` 随调用方变化。
- Anthropic 方言不带 `code`(贴合其 error 事件形状);OpenAI 与 Responses 带 `code`。
- 正常数据 pump 路径(`pumpSSEBytes`/`pumpTypedJSON` 的透传循环)零改动,只改超时分支调用。
- 范围 3 端点家族;不含 Gemini;不含 dial/write/未 committed 路径的 `writeProxyError`。
- 普通包代码可用 `time.Now()`(`time` 限制只针对 workflow 脚本,与此无关)。
- 提交粒度:每个 Task 末尾一次 commit。中文/英文 commit 风格沿用仓库现有(`refactor(core):`/`feat(core):` 前缀)。

## File Structure

- `internal/core/frames.go`(改)— 新增 `ErrorDialect` 枚举与 `writeErrorFrame`;删除 `writeSSETimeoutError`;`pumpSSEBytes`/`pumpTypedJSON` 加 `dialect` 参数(及 seq/start 时间上下文),超时分支改调 `writeErrorFrame`。
- `internal/core/proxy.go`(改)— `newProxyHandler` 加 `dialect` 参数,透传到 pump。
- `internal/core/routes.go`(改)— 每条路由注册时按表传 `dialect`。
- `internal/core/frames_test.go`(改)— 更新现有超时测试的 pump 调用签名;新增按方言断言的测试。

---

## Task 1: 新增 `ErrorDialect` 与 `writeErrorFrame`

新增按方言输出错误帧的函数,暂不接入 pump(保留旧 `writeSSETimeoutError` 直到 Task 2 切换,保证本 Task 末尾构建通过)。

**Files:**
- Modify: `internal/core/frames.go`(在 `writeSSETimeoutError` 之后新增类型与函数)
- Test: `internal/core/frames_test.go`

**Interfaces:**
- Produces: `type ErrorDialect int`;常量 `DialectOpenAI`、`DialectAnthropic`、`DialectResponses`;`func writeErrorFrame(w http.ResponseWriter, d ErrorDialect, msg string, seq int, startedAt time.Time)`。`seq`/`startedAt` 仅 Responses 方言使用(填 `sequence_number` 与 `response.created_at`);OpenAI/Anthropic 忽略。

- [ ] **Step 1: 写失败测试(三种方言)**

追加到 `internal/core/frames_test.go`(文件已 `import` `"net/http/httptest"`、`"strings"`、`"testing"`;需补 `"time"` import——见 Step 1b):

```go
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
	if !strings.Contains(body, `{"type":"error","error":{"type":"proxy_error","message":"upstream read timeout"}}`) {
		t.Errorf("missing Anthropic inner payload: %q", body)
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
```

- [ ] **Step 1b: 补 `"time"` import**

在 `frames_test.go` 的 import 块加入 `"time"`(若已有则跳过)。现有 import 块:

```go
import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)
```

改为:

```go
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
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/core/ -run TestWriteErrorFrame -v`
Expected: 编译失败——`DialectOpenAI` / `writeErrorFrame` undefined。

- [ ] **Step 3: 实现 `ErrorDialect` 与 `writeErrorFrame`**

在 `internal/core/frames.go` 顶部 import 块补 `"time"`(若已有则跳过)。当前 import:

```go
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
)
```

改为:

```go
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"
)
```

在 `writeSSETimeoutError` 函数**之后**(即 `pumpSSEBytes` 之前)新增:

```go
// ErrorDialect selects the shape of the terminal error frame the proxy emits
// for its OWN faults (e.g. upstream read timeout) on an already-streaming
// response. Decided by route, orthogonal to FrameMode (which selects the data
// pump). Upstream-pushed frames are always passed through verbatim — this only
// governs proxy-generated terminal frames.
type ErrorDialect int

const (
	DialectOpenAI ErrorDialect = iota // /v1/chat/completions
	DialectAnthropic                   // /v1/messages, /anthropic/v1/messages
	DialectResponses                   // /v1/responses, /v1/responses/compact
)

// writeErrorFrame emits the proxy's own terminal error frame on an
// already-streaming response, in the dialect the client's route speaks.
// msg is the human message; type is always "proxy_error" and code
// "upstream_timeout" (the only current caller is the read-timeout path).
// seq and startedAt fill the Responses response.failed frame and are ignored
// by the other dialects.
func writeErrorFrame(w http.ResponseWriter, d ErrorDialect, msg string, seq int, startedAt time.Time) {
	switch d {
	case DialectAnthropic:
		payload := map[string]any{
			"type":  "error",
			"error": map[string]any{"type": "proxy_error", "message": msg},
		}
		b, _ := json.Marshal(payload)
		_, _ = fmt.Fprintf(w, "event: error\ndata: %s\n\n", b)
	case DialectResponses:
		payload := map[string]any{
			"type":            "response.failed",
			"sequence_number": seq,
			"response": map[string]any{
				"id":         "resp_proxy_error",
				"object":     "response",
				"created_at": startedAt.Unix(),
				"model":      "",
				"output":     []any{},
				"status":     "failed",
				"error":      map[string]any{"type": "proxy_error", "code": "upstream_timeout", "message": msg},
			},
		}
		b, _ := json.Marshal(payload)
		_, _ = fmt.Fprintf(w, "event: response.failed\ndata: %s\n\n", b)
	default: // DialectOpenAI
		payload := map[string]any{
			"error": map[string]any{"type": "proxy_error", "code": "upstream_timeout", "message": msg},
		}
		b, _ := json.Marshal(payload)
		_, _ = fmt.Fprintf(w, "event: error\ndata: %s\n\n", b)
	}
	flush(w)
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/core/ -run TestWriteErrorFrame -v`
Expected: PASS(三个用例全过)。`writeSSETimeoutError` 仍存在(被 pump 引用),`go build ./...` 通过。

- [ ] **Step 5: 提交**

```bash
git add internal/core/frames.go internal/core/frames_test.go
git commit -m "$(cat <<'EOF'
feat(core): add per-dialect writeErrorFrame

New ErrorDialect (OpenAI/Anthropic/Responses) selects the shape of the
proxy's own terminal error frame. Not yet wired into the pumps; the old
writeSSETimeoutError stays until the next change swaps the call sites.
EOF
)"
```

---

## Task 2: 把 `dialect` 接入 pump 与 handler,删除 `writeSSETimeoutError`

切换错误路径到 `writeErrorFrame`,删除旧的统一形状函数。

**Files:**
- Modify: `internal/core/frames.go`(`pumpSSEBytes`、`pumpTypedJSON` 签名 + 超时分支)
- Modify: `internal/core/proxy.go:54`(`newProxyHandler` 签名 + pump 调用)
- Modify: `internal/core/routes.go`(5 条路由按表传 dialect)
- Modify: `internal/core/frames_test.go`(更新现有 pump 调用签名 + 新增方言断言用例)

**Interfaces:**
- Consumes: `ErrorDialect`, `writeErrorFrame`, `Dialect*`(Task 1)。
- Produces: `pumpSSEBytes(w http.ResponseWriter, r messageReader, stream bool, dialect ErrorDialect) error`;`pumpTypedJSON(w http.ResponseWriter, r messageReader, stream bool, dialect ErrorDialect, startedAt time.Time) error`;`newProxyHandler(cfg Config, mode FrameMode, dialect ErrorDialect) http.HandlerFunc`。

> 说明:`pumpTypedJSON` 额外接 `startedAt` 用于 Responses 的 `created_at`;`pumpSSEBytes` 不需要(OpenAI/Anthropic 帧不用时间),但两个 pump 都需要 `dialect`。

- [ ] **Step 1: 改 `pumpSSEBytes` 签名与超时分支**

`internal/core/frames.go` 中 `pumpSSEBytes` 的签名:

```go
func pumpSSEBytes(w http.ResponseWriter, r messageReader, stream bool) error {
```

改为:

```go
func pumpSSEBytes(w http.ResponseWriter, r messageReader, stream bool, dialect ErrorDialect) error {
```

其 `stream==true` 分支里,把:

```go
				if e := classifyReadError(err); e != nil {
					if emitted {
						writeSSETimeoutError(w)
					}
					return e
				}
```

改为:

```go
				if e := classifyReadError(err); e != nil {
					if emitted {
						writeErrorFrame(w, dialect, "upstream read timeout", 0, time.Time{})
					}
					return e
				}
```

(`pumpSSEBytes` 的非流式分支不受影响——它返回 errReadTimeout 由 proxy 发 504,不发帧。)

- [ ] **Step 2: 改 `pumpTypedJSON` 签名、加帧计数器与超时分支**

`pumpTypedJSON` 签名:

```go
func pumpTypedJSON(w http.ResponseWriter, r messageReader, stream bool) error {
```

改为:

```go
func pumpTypedJSON(w http.ResponseWriter, r messageReader, stream bool, dialect ErrorDialect, startedAt time.Time) error {
```

其 `stream==true` 分支,把 `emitted := false` 改为帧计数器,并在超时处用它:

原:

```go
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
```

改为:

```go
		setSSEHeaders(w)
		emitted := 0
		for {
			_, data, err := r.ReadMessage()
			if err != nil {
				if e := classifyReadError(err); e != nil {
					if emitted > 0 {
						writeErrorFrame(w, dialect, "upstream read timeout", emitted, startedAt)
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
			emitted++
			if terminalResponseEvents[head.Type] {
				return nil
			}
		}
```

(非流式分支不受影响——它返回 errReadTimeout 由 proxy 发 504。)

- [ ] **Step 3: 删除 `writeSSETimeoutError`**

删除 `internal/core/frames.go` 中整个函数:

```go
// writeSSETimeoutError emits a best-effort SSE error frame on an already-streaming
// response whose upstream read timed out. Used only after the first frame has been
// flushed (headers committed), so the status code can no longer be changed.
func writeSSETimeoutError(w http.ResponseWriter) {
	_, _ = fmt.Fprintf(w, "event: error\ndata: %s\n\n",
		`{"error":{"type":"proxy_error","message":"upstream read timeout"}}`)
	flush(w)
}
```

- [ ] **Step 4: 改 `newProxyHandler` 签名与 pump 调用**

`internal/core/proxy.go:54`,`newProxyHandler` 签名:

```go
func newProxyHandler(cfg Config, mode FrameMode) http.HandlerFunc {
```

改为:

```go
func newProxyHandler(cfg Config, mode FrameMode, dialect ErrorDialect) http.HandlerFunc {
```

同文件 pump 调用块(`proxy.go:97-103`):

```go
		var pumpErr error
		switch mode {
		case FrameModeTypedJSON:
			pumpErr = pumpTypedJSON(w, reader, true)
		default:
			pumpErr = pumpSSEBytes(w, reader, true)
		}
```

改为:

```go
		var pumpErr error
		switch mode {
		case FrameModeTypedJSON:
			pumpErr = pumpTypedJSON(w, reader, true, dialect, time.Now())
		default:
			pumpErr = pumpSSEBytes(w, reader, true, dialect)
		}
```

在 `proxy.go` import 块补 `"time"`(若已有则跳过)。当前 import:

```go
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
```

`"time"` 已存在,无需改动。

- [ ] **Step 5: 改路由表按表传 dialect**

`internal/core/routes.go`,把 5 条 `newProxyHandler` 调用改为带 dialect。整段替换:

```go
	mux.Handle("POST /v1/chat/completions", newProxyHandler(cfg, FrameModeSSEBytes))
	mux.Handle("POST /v1/responses", newProxyHandler(cfg, FrameModeTypedJSON))
	mux.Handle("POST /v1/responses/compact", newProxyHandler(cfg, FrameModeTypedJSON))
	mux.Handle("POST /v1/messages", newProxyHandler(cfg, FrameModeSSEBytes))
	mux.Handle("POST /anthropic/v1/messages", newProxyHandler(cfg, FrameModeSSEBytes))
```

改为:

```go
	mux.Handle("POST /v1/chat/completions", newProxyHandler(cfg, FrameModeSSEBytes, DialectOpenAI))
	mux.Handle("POST /v1/responses", newProxyHandler(cfg, FrameModeTypedJSON, DialectResponses))
	mux.Handle("POST /v1/responses/compact", newProxyHandler(cfg, FrameModeTypedJSON, DialectResponses))
	mux.Handle("POST /v1/messages", newProxyHandler(cfg, FrameModeSSEBytes, DialectAnthropic))
	mux.Handle("POST /anthropic/v1/messages", newProxyHandler(cfg, FrameModeSSEBytes, DialectAnthropic))
```

同步更新 `routes.go` 顶部注释(第 8-10 行附近)对方言的说明:

```go
// Frame modes are decided by route (never by sniffing frame content):
//   /v1/responses, /v1/responses/compact → typed-JSON frames
//   /v1/chat/completions, /v1/messages, /anthropic/v1/messages → SSE-bytes frames
```

在其后追加一行:

```go
// Error dialect (proxy self-fault terminal frame) is decided by route too:
//   /v1/chat/completions → OpenAI ; /v1/messages, /anthropic/v1/messages → Anthropic
//   /v1/responses, /v1/responses/compact → Responses
```

- [ ] **Step 6: 更新现有超时测试的 pump 调用签名**

`internal/core/frames_test.go` 的 `TestPumpSSEBytesStreamTimeoutWritesSSEError`(第 167 行附近)调用:

```go
	err := pumpSSEBytes(rec, fr, true)
```

改为:

```go
	err := pumpSSEBytes(rec, fr, true, DialectOpenAI)
```

断言不变(OpenAI 形状仍含 `event: error` 与 `upstream read timeout`)。`TestPumpSSEBytesNonStreamTimeoutReturns504Path` 里的 `pumpSSEBytes(rec, fr, false)` 同样补 `, DialectOpenAI`。

`TestPumpSSEBytesStreamWritesVerbatim`、`TestPumpSSEBytesNonStreamReturnsJSON`、`TestPumpSSEBytesNonStreamErrorMaps502` 中的 `pumpSSEBytes(rec, fr, …)` 调用全部补 `, DialectOpenAI`(具体值不影响这些用例,OpenAI 即可)。

`TestPumpTypedJSONStreamEmitsSSE`、`TestPumpTypedJSONNonStreamReturnsResponseObject`、`TestPumpTypedJSONNonStreamErrorMaps502` 中的 `pumpTypedJSON(rec, fr, …)` 调用全部补 `, DialectResponses, time.Unix(0,0)`(这些用例不触发错误帧,dialect 与时间均不影响断言;`time` import 已在 Task 1 Step 1b 加入)。

- [ ] **Step 7: 新增按方言断言的超时测试**

追加到 `frames_test.go`:

```go
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
	if !strings.Contains(body, `{"type":"error","error":{"type":"proxy_error","message":"upstream read timeout"}}`) {
		t.Errorf("missing Anthropic payload: %q", body)
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
```

- [ ] **Step 8: 全量测试 + vet**

Run: `go vet ./... && go test ./...`
Expected: vet 干净;全部测试 PASS(含新增 2 个 + Task 1 的 3 个 + 更新签名后的既有用例)。

- [ ] **Step 9: 提交**

```bash
git add internal/core/frames.go internal/core/proxy.go internal/core/routes.go internal/core/frames_test.go
git commit -m "$(cat <<'EOF'
refactor(core): emit per-dialect error frames on stream timeout

Pumps now carry an ErrorDialect and call writeErrorFrame instead of the
old OpenAI-only writeSSETimeoutError (deleted). Routes bind each dialect:
chat/completions→OpenAI, messages→Anthropic, responses→Responses, so
Claude Code and Codex receive a terminal frame they can parse.
EOF
)"
```

---

## Task 3: 收尾校验

**Files:** 无改动。

- [ ] **Step 1: 再次全量测试**

Run: `go test ./...`
Expected: PASS。

- [ ] **Step 2: 确认 `writeSSETimeoutError` 已无残留引用**

Run(Grep 工具):搜索 `writeSSETimeoutError`
Expected: 0 命中。

- [ ] **Step 3: 手动冒烟(可选,需本机上游)**

若本机有可用的上游 WS,启动 `cc2ws`,用 Claude Code 打 `/v1/messages`、用 Codex 打 `/v1/responses`,分别在流中途掐断上游触发读超时,确认客户端收到对应方言的终止帧而非 OpenAI 形状。无上游则跳过,依赖 Step 1 的测试。

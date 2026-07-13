# WS 错误帧按端点方言输出 — 设计

日期: 2026-07-13
状态: 设计

## 背景与根因

cc2ws 是 HTTP→WS 反向代理。客户端按路由走不同的 LLM 协议方言:

- `/v1/chat/completions` — OpenAI Chat Completions(SSE)
- `/v1/responses`、`/v1/responses/compact` — OpenAI Responses API(typed-JSON 事件流,Codex 用)
- `/v1/messages`、`/anthropic/v1/messages` — Anthropic Messages(事件流,Claude Code 用)

正常数据流是**逐字节透传**上游 WS 帧,这条路径是对的、不动。

问题出在**代理自身出错时**(目前唯一场景:上游读超时)。`frames.go:119` 的 `writeSSETimeoutError` 把错误帧**统一**写成 OpenAI 形状:

```
event: error
data: {"error":{"type":"proxy_error","message":"upstream read timeout"}}
```

于是说 Anthropic 方言的 Claude Code、说 Responses 方言的 Codex,在流中途超时时收到一个它们解析不了的形状——**客户端收不到/收不全响应**。这是根因。

## 范围

3 个端点家族(`/v1/chat/completions`、`/v1/responses(+compact)`、`/v1/messages`+`/anthropic/v1/messages`)。

**不含 Gemini**:`routes.go` 目前没有 Gemini 路由,Gemini 留待将来单独处理。

**不含上游错误帧改包**:上游 WS 主动推来的错误帧属于正常数据,逐字节透传,代理不拦截、不改包。代理只为自己生成的故障(读超时/写失败)发错误帧。

## 设计

### 正交双维度

- `FrameMode`(现有,2 值 `SSEBytes` / `TypedJSON`)— 决定**正常数据怎么 pump**。本设计不改它。
- `ErrorDialect`(新增,3 值)— 决定**代理自身出错时发什么形状的终止帧**。

两个维度正交:同一个 `FrameModeSSEBytes`,在 OpenAI 路由下出错说 OpenAI 方言,在 Anthropic 路由下出错说 Anthropic 方言。

### ErrorDialect 枚举(新增于 `frames.go`)

```go
type ErrorDialect int
const (
    DialectOpenAI ErrorDialect = iota // /v1/chat/completions
    DialectAnthropic                   // /v1/messages, /anthropic/v1/messages
    DialectResponses                   // /v1/responses, /v1/responses/compact
)
```

### 路由表是方言唯一真相源(`routes.go`)

`newProxyHandler` 签名加一个 `dialect ErrorDialect` 参数,每条路由在注册时同时指定 `mode` 与 `dialect`:

| 路由 | FrameMode | ErrorDialect |
|---|---|---|
| `POST /v1/chat/completions` | SSEBytes | OpenAI |
| `POST /v1/responses` | TypedJSON | Responses |
| `POST /v1/responses/compact` | TypedJSON | Responses |
| `POST /v1/messages` | SSEBytes | Anthropic |
| `POST /anthropic/v1/messages` | SSEBytes | Anthropic |

### 删除 `writeSSETimeoutError`,替换为 `writeErrorFrame`

`frames.go` 删掉统一形状的 `writeSSETimeoutError`,新增:

```go
func writeErrorFrame(w http.ResponseWriter, d ErrorDialect, msg string)
```

`writeErrorFrame` 按 dialect 产出(详见"各端点错误帧"一节)。`msg` 为代理生成的消息文案(如 `"upstream read timeout"`),`type` 统一用 `"proxy_error"`(Anthropic/OpenAI),Responses 里放进 `error.type`。

### pump 透传 dialect

`pumpSSEBytes` 与 `pumpTypedJSON` 各加一个 `dialect ErrorDialect` 参数。它们在超时分支(原调 `writeSSETimeoutError` 处)改为调 `writeErrorFrame(w, dialect, msg)`。其余逻辑零改动。

`newProxyHandler` 把 `dialect` 透传给对应 pump。

## 各端点错误帧(终止形状)

错误内容:`<m>` = 代理消息文案(如 `"upstream read timeout"`)。`type` 固定 `"proxy_error"`,`code` 固定 `"upstream_timeout"`(`writeErrorFrame` 目前唯一调用方是读超时分支,故 code 准确)。Anthropic 方言不带 `code`(贴合其错误事件形状)。只有 `message` 随调用方传入变化。

### OpenAI(`/v1/chat/completions`)— SSE error 事件

```
event: error
data: {"error":{"type":"proxy_error","code":"upstream_timeout","message":"<m>"}}

```

(SSE,与正常数据同一流。)

### Anthropic(`/v1/messages`, `/anthropic/v1/messages`)— SSE error 事件(Anthropic 内层)

```
event: error
data: {"type":"error","error":{"type":"proxy_error","message":"<m>"}}

```

Anthropic 官方流里 error 事件以 SSE `event: error\ndata: {...}` 出现,内层是 `{"type":"error","error":{...}}`(无 `code`)。本设计采用该 SSE 外壳 + Anthropic 内层 JSON,既贴合客户端解析器,又与该路由 SSEBytes 的 SSE 字节流一致。

### Responses(`/v1/responses`, `/v1/responses/compact`)— `response.failed` 事件

```
event: response.failed
data: {"type":"response.failed","sequence_number":<N>,"response":{"id":"resp_proxy_error","object":"response","created_at":<N>,"model":"","output":[],"status":"failed","error":{"type":"proxy_error","code":"upstream_timeout","message":"<m>"}}}

```

字段填充规则:
- `sequence_number`: best-effort 单调计数器——pump 每透传一帧 +1,错误帧取当前值。与上游自己的 numbering 无关(帧是逐字节透传的,上游帧内 `sequence_number` 不被改写),只是给合成帧一个单调值。
- `response.id`: 合成占位 `"resp_proxy_error"`;`created_at`: pump 启动时刻(Unix 秒,在 pump 入口捕获传参);`model`: 留空;`output`: `[]`;`status`: `"failed"`;`error`: 含 `type`/`code`/`message`。
- 该路由走 `FrameModeTypedJSON`,SSE 模式下按现有约定包成 `event: response.failed\ndata: <上面 JSON>\n\n`。

> 普通 Go 代码可用 `time.Now()`(`time` 包限制只针对 workflow 脚本)。

## 数据流(错误路径)

```
客户端 POST /v1/messages
  → newProxyHandler(mode=SSEBytes, dialect=Anthropic)
    → dial upstream WS, 写请求 body
    → pumpSSEBytes(w, reader, stream=true, dialect=Anthropic)
       正常: 逐帧透传,flush
       读超时(已 emitted ≥1 帧):
         → writeErrorFrame(w, DialectAnthropic, "upstream read timeout")
         → 返回 errReadTimeout
    → proxy 见 errReadTimeout:
         若 statusRecorder 已 committed → 什么都不做(帧已发)
         否则 → writeProxyError(504)  [未变]
```

dial 失败、写 body 失败、超时但未 committed —— 这些路径**响应尚未开始**,仍走现有 `writeProxyError`(标准 502/504 JSON 信封),**不在本设计范围**。本设计只覆盖"headers 已提交、流进行中、代理要补一个终止帧"这一条路径的形状修正。

## 测试

`frames_test.go` 现有测试保持(行为不变的部分)。新增:

1. `TestWriteErrorFrameOpenAI` — 校验 SSE `event: error` + OpenAI 内层 JSON。
2. `TestWriteErrorFrameAnthropic` — 校验 SSE `event: error` + `{"type":"error","error":{...}}`。
3. `TestWriteErrorFrameResponses` — 校验 `event: response.failed` + 内层 `response.status:"failed"` 与 `error`。
4. 改 `TestPumpSSEBytesStreamTimeoutWritesSSEError` — 断言里加 dialect 参数(默认或显式 OpenAI),验证形状随 dialect 变。
5. 新增 `TestPumpSSEBytesStreamTimeoutAnthropicDialect` — 同一超时场景,传 `DialectAnthropic`,断言写出 Anthropic 形状。
6. 新增 `TestPumpTypedJSONStreamTimeoutResponsesDialect` — TypedJSON 超时,断言 `response.failed`。
7. `TestRouter` / 路由相关测试若有,补 mode+dialect 绑定断言。

`proxy_test.go` 的集成测试不变(它们验证 dial/504 路径,不受影响)。

## 非目标 / 不做

- 不引入 transformer 接口或 per-endpoint 对象(过度设计;正常数据逐字节透传,无需 transform)。
- 不拦截/改包上游错误帧。
- 不加 Gemini。
- 不改 dial/write/未committed 路径的 `writeProxyError`。

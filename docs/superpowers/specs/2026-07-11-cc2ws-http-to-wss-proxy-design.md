# cc2ws — 本地 HTTP→WSS 代理（OpenAI / Anthropic）

- 日期：2026-07-11
- 状态：已评审，待实现

## 1. 目标

写一个轻量本地代理工具（Go 单二进制），行为如下：

1. 本地监听 HTTP（默认 `127.0.0.1:18080`）。
2. 只处理 OpenAI / Anthropic 的模型调用 POST 请求。
3. 把这些请求用 WebSocket（`wss://`，本地调试可用 `ws://`）转发到上游网关（AxonHub 这类同路径支持 WS Upgrade 的网关）。
4. 把上游 WSS 返回的流式事件还原成客户端期望的 HTTP 响应（SSE 或 JSON）。
5. 不做供应商管理、故障转移、模型映射、UI、鉴权存储、格式转换。

### 非目标（明确不做）

- 供应商切换 / profile / UI / 模型改名映射 / Anthropic↔OpenAI 格式转换。
- 请求重试 / 熔断。
- WS 入站（客户端不会用 WS 连本代理；本代理只出站 WSS）。
- 本地 HTTPS 证书（本地明文 HTTP）。
- embeddings / images / audio / models list 等。

## 2. 架构

单 Go 二进制，一个 HTTP server 监听 `LISTEN`。每个到达已知路由的 POST 请求派生一个 goroutine：

```
HTTP POST (client)
  → scheme-swap(UPSTREAM_BASE) + 原 path  → 上游 WSS URL
  → dial WSS，透传鉴权 headers
  → 发 1 条 text message = 原始 JSON body（不改字段）
  → 按路由帧模式读 text frames，回写 HTTP body（SSE 或 JSON）
  → 关闭上游 WS，结束 HTTP 响应
```

不把本地 HTTP 升级成 WS；不把上游 WSS URL 当 HTTP URL 用。v1 每请求一条 WS，串行处理，不做连接复用。

## 3. 组件 / 文件布局

```
cc2ws/
├── main.go          # 入口：解析配置、启动 server、注册路由、信号/超时
├── config.go        # Config 结构 + 环境变量/CLI 解析 + scheme-swap helper
├── proxy.go         # per-request handler：读 body、dial、send、pump、回写
├── frames.go        # pumpSSEBytes / pumpTypedJSON 两个帧泵 + 终止事件判定
├── routes.go        # 路由表：path → 帧模式；/health；404
├── logging.go       # 结构化日志（method/path/status/latency/upstream_url，不打 key）
├── go.mod
├── .goreleaser.yml
├── .github/workflows/release.yml
├── README.md
└── docs/superpowers/specs/2026-07-11-cc2ws-http-to-wss-proxy-design.md
```

### 单元职责

- `config.go`：把环境变量与 CLI flag 合并成 `Config`；`swapScheme(base)` 把 `https`→`wss`、`http`→`ws`；校验 `UPSTREAM_BASE` 合法 origin。
- `proxy.go`：`ProxyHandler(cfg, mode)` 返回 `http.HandlerFunc`，负责编排（读 body、选 Content-Type、dial、发、调帧泵、错误映射）。
- `frames.go`：纯函数化的帧泵，输入 `ws.Conn` + `http.ResponseWriter`，可独立单元测试（用 `httptest` + 内存 WS 桩）。
- `routes.go`：把 path 映射到 `(handler, frameMode)`，未命中返回 404。
- `logging.go`：请求级 status line 日志；鉴权头一律脱敏（只记是否存在，不记值）。

## 4. 配置

环境变量（也可同名 CLI flag 覆盖）：

| 变量 | 默认 | 说明 |
|---|---|---|
| `LISTEN` | `127.0.0.1:18080` | 本地监听地址 |
| `UPSTREAM_BASE` | （必填） | 上游 origin，只写 origin，如 `https://hubcn.jikuixie.me` 或 `http://127.0.0.1:8090` |
| `UPSTREAM_INSECURE_SKIP_TLS_VERIFY` | `false` | 跳过上游 TLS 校验（仅本地自签调试） |
| `CONNECT_TIMEOUT` | `10s` | WS 拨号超时 |
| `IDLE_TIMEOUT` | `600s` | WS 空闲读超时 |
| `LOG_LEVEL` | `info` | debug/info/warn/error |

`UPSTREAM_BASE` 只写 origin，**不在其后再拼 `/v1`**；路径直接复用本地请求 path + query。

## 5. 上游 WebSocket 协议

### 5.1 连接（scheme 替换）

| 本地 HTTP | 上游 WSS |
|---|---|
| `POST .../v1/chat/completions` | `wss://{UPSTREAM_HOST}/v1/chat/completions` |
| `POST .../v1/responses` | `wss://{UPSTREAM_HOST}/v1/responses` |
| `POST .../v1/responses/compact` | `wss://{UPSTREAM_HOST}/v1/responses/compact` |
| `POST .../v1/messages` | `wss://{UPSTREAM_HOST}/v1/messages` |
| `POST .../anthropic/v1/messages` | `wss://{UPSTREAM_HOST}/anthropic/v1/messages` |

### 5.2 鉴权 headers（透传，有则带）

- `Authorization`
- `x-api-key`
- `anthropic-version`
- `anthropic-beta`
- `OpenAI-Organization` / `OpenAI-Project`（若有）

`Content-Type` 仅握手期参考；请求体走 WS message，不在握手头里依赖。代理不存 key。

### 5.3 发送请求

WS 握手成功后，发 **一条 Text 消息**，内容为本地 POST 的 **原始 JSON body 字节**（不改字段）。

### 5.4 帧模式（路由决定，按 route 判定，不嗅探内容）

- **typed-JSON 帧模式**：`/v1/responses`、`/v1/responses/compact`。上游每条 text message = 一个 JSON 对象（含顶层 `type`）。
- **SSE-bytes 帧模式**：`/v1/chat/completions`、`/v1/messages`、`/anthropic/v1/messages`。上游每条 text message = 一整段 SSE 事件字节。

## 6. 接收与回写

### 6.1 流式 vs 非流式判定

解析请求 body 的 `stream` 字段（**只读不改 body**）：

- chat / messages：默认 `stream=false`。`stream:true` → SSE 输出；否则 → JSON 输出。
- responses / compact：默认视为流式（WS 本身是事件流）。只有客户端**显式** `stream:false` 才切聚合模式。

### 6.2 SSE-bytes 帧模式（chat / messages）

- `stream:true`：HTTP 头 `Content-Type: text/event-stream` + `Cache-Control: no-cache` + `Connection: keep-alive`。每条 WS text message 原样写入 body（不再包 `data:`）。WS 关闭即结束。
- `stream:false`：HTTP 头 `Content-Type: application/json`。读取 WS 消息直到关闭（预期单条完整 JSON），原样作为 body 返回。

### 6.3 typed-JSON 帧模式（responses / compact）

- `stream` 缺省或 `true`（默认）：HTTP 头 `Content-Type: text/event-stream`。每条 JSON 转成标准 SSE：

  ```
  event: <json.type>
  data: <整条 json 文本>

  ```

  终止事件（遇到后可继续读到 close）：`response.completed` / `response.failed` / `response.cancelled` / `response.incomplete` / `error`。

- `stream:false`（显式）：聚合 frames 直到终止事件，返回 **`response.completed` 事件中的 `response` 对象**，HTTP 头 `Content-Type: application/json`，状态 200。若终止是 `response.failed` / `error` 等，按错误处理（见 §7）。

## 7. 错误处理

| 场景 | HTTP 响应 |
|---|---|
| 上游 WS 握手失败 | `502` `{"error":{"message":"upstream websocket dial failed: ...","type":"proxy_error"}}` |
| 拨号超时 | `504` `{"error":{...,"type":"proxy_error"}}` |
| 本地请求非 JSON body | `400` `{"error":{"message":"invalid json body","type":"proxy_error"}}` |
| 未知路径 | `404` |
| 上游读超时（头未发） | `504`（`{"error":{...,"type":"proxy_error"}}`） |
| 上游读超时（流式头已发） | 只能结束响应（无法改状态码）；写一条 SSE error 帧尽力提示后关闭 |
| 上游业务 error（流式中） | 按当前帧模式写回，HTTP 200（流式惯例） |
| 上游业务 error（非流） | 能解析错误码则映射 4xx/5xx，否则 502 |

`GET /health` → `200 {"status":"ok","version":"<version>"}`。

## 8. 路由表

| Method | Path | 帧模式 |
|---|---|---|
| POST | `/v1/chat/completions` | SSE-bytes |
| POST | `/v1/responses` | typed-JSON |
| POST | `/v1/responses/compact` | typed-JSON |
| POST | `/v1/messages` | SSE-bytes |
| POST | `/anthropic/v1/messages` | SSE-bytes |
| GET | `/health` | — |
| 其他 | — | 404 |

## 9. 日志

每请求一行：`method path status latency upstream_url`。鉴权头脱敏（只记 `auth=present` 之类，不记值，绝不打印完整 API key）。

## 10. CI/CD（GitHub Actions + goreleaser）

### 10.1 触发

仅 `push` 到 `main` 触发自动发版。

### 10.2 版本自增

- 取最新 `v[0-9]*` tag；无则首个版本为 `v0.1.0`。
- 自增规则：**minor +1，patch 归 0，major 不变**。例：`v0.5.3 → v0.6.0`、`v1.2.0 → v1.3.0`。
- 在同一 workflow 内：创建并推送新 tag，紧接着在同 job 里调用 goreleaser 发版（tag-push 不触发第二个 workflow）。

### 10.3 构建矩阵

goreleaser 单 job 多 target（Go 交叉编译，无 CGO）：

| OS | amd64 | arm64 |
|---|---|---|
| Windows | `windows_amd64` | `windows_arm64` |
| macOS | `darwin_amd64` | `darwin_arm64` |
| Linux | `linux_amd64` | `linux_arm64` |

产物：

- Windows → `.zip`；macOS/Linux → `.tar.gz`。
- 命名：`cc2ws_<version>_<os>_<arch>.{zip|tar.gz}`。
- 附带 `checksums.txt`（SHA256）。
- 版本号通过 ldflags `-X main.version=<version>` 注入二进制；`--version` 与 `/health` 可读。

### 10.4 工作流

- `.github/workflows/release.yml`：`on: push: branches: [main]`，`permissions: contents: write`。
- 步骤：checkout（`fetch-depth: 0` 取全历史）→ setup-go → 计算 next version → `git tag` + `git push` tag → `goreleaser-action` `args: release --clean`（`GITHUB_TOKEN: secrets.GITHUB_TOKEN`）。
- `.goreleaser.yml`：`project_name: cc2ws`、binary `cc2ws`、6 targets、ldflags 注入 version、archive 格式按 OS 切 zip/tar.gz、checksum、`release.draft: false`、`changelog` 用默认或关闭。

## 11. 测试与验收

### 11.1 单元 / 桩测（本地，无需真上游）

- `swapScheme`：`https://x` → `wss://x`、`http://x` → `ws://x`。
- 用 `httptest` + 一个内存 WS 桩 server（gorilla/websocket `Upgrader`）验证：
  - SSE-bytes 模式 stream:true：桩推 3 段 SSE 字节，断言 HTTP body 原样拼接、头为 `text/event-stream`。
  - SSE-bytes 模式 stream:false：桩推 1 条完整 JSON，断言头 `application/json`。
  - typed-JSON 模式 stream:true：桩推 `response.created` / `...delta` / `response.completed`，断言输出为 `event:/data:` SSE 且在 completed 处结束。
  - typed-JSON 模式 stream:false：桩推到 `response.completed`，断言返回其 `response` 对象 JSON。

### 11.2 端到端（真上游，README 记录）

6 个 curl 用例（spec 原文）：

1. Anthropic 非流 → JSON 含 pong。
2. Anthropic 流 → `text/event-stream`，可见 `event:`/`data:`。
3. OpenAI Chat 流 → SSE `data:` 持续输出。
4. OpenAI Responses 流 → SSE 含 `response.output_text.delta` 与 `response.completed`。
5. OpenAI Responses 非流 → 单个 JSON，HTTP 200。
6. 错误 base（`UPSTREAM_BASE` 设错） → 502，错误信息可读。

## 12. 客户端接入示例

### Claude Code

```bash
export ANTHROPIC_BASE_URL=http://127.0.0.1:18080
# 或 http://127.0.0.1:18080/anthropic
export ANTHROPIC_AUTH_TOKEN=<key>
```

### Codex（`config.toml`）

```toml
model_provider = "custom"
[model_providers.custom]
name = "wss-proxy"
base_url = "http://127.0.0.1:18080/v1"
wire_api = "responses"
```

### README 须声明

本代理上游必须是支持**同路径 WSS 入站**的网关（如 AxonHub）；普通只支持 HTTPS 的 OpenAI/Anthropic 官方接口不可用。

## 13. 关键设计约束（写进代码注释）

- 本地客户端（Claude Code / Codex）只发 HTTP POST。
- 上游 AxonHub 对相同 path 提供 GET Upgrade WebSocket。
- 代理职责：HTTP POST → dial WSS（同 path）→ 发 1 条 text 请求（JSON body）→ 读 text frames → 回写 HTTP SSE/JSON → 关闭上游 WS。
- 不把本地 HTTP 升级成 WS；不把上游 WSS URL 当 HTTP URL 用。
- 不修改请求 body 字段。
- 先实现正确，再考虑连接复用。

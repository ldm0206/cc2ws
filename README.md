# cc2ws

A minimal local **HTTP → WebSocket** proxy for OpenAI / Anthropic model calls.

Local clients (Claude Code, Codex, anything speaking the OpenAI/Anthropic HTTP
API) send plain HTTP POST. cc2ws dials a **same-path upstream WebSocket**
(`wss://`), sends the raw JSON body as one text message, and pumps the upstream
frames back to the client as HTTP SSE or JSON.

> The upstream must accept **WebSocket Upgrade on the same path** as the HTTP
> route (e.g. [AxonHub](https://github.com)-style gateways). Plain HTTPS-only
> OpenAI/Anthropic official endpoints will **not** work — they are not WebSocket
> gateways.

## Install

Download a prebuilt binary from [Releases](/releases) (Windows / macOS /
Linux, amd64 + arm64), or build from source:

```bash
go build -o cc2ws .
```

## Run

```bash
export UPSTREAM_BASE=https://hub.example.com   # or http://127.0.0.1:8090 locally
export LISTEN=127.0.0.1:18080
./cc2ws
```

Flags mirror the env vars and win if both are set:

```bash
./ccws -listen 127.0.0.1:18080 -upstream-base https://hub.example.com
./cc2ws -version
```

### Configuration

| Env / Flag | Default | Description |
|---|---|---|
| `LISTEN` / `-listen` | `127.0.0.1:18080` | local HTTP listen address |
| `UPSTREAM_BASE` / `-upstream-base` | (required) | upstream origin only — `https://host` or `http://host:port` |
| `UPSTREAM_INSECURE_SKIP_TLS_VERIFY` | `false` | skip upstream TLS verify (self-signed local debugging only) |
| `CONNECT_TIMEOUT` | `10s` | upstream WS dial timeout |
| `IDLE_TIMEOUT` | `600s` | upstream WS read idle timeout |
| `LOG_LEVEL` | `info` | log level |

## Routes

| Method | Path | Upstream frame mode |
|---|---|---|
| POST | `/v1/chat/completions` | SSE bytes (verbatim) |
| POST | `/v1/responses` | typed-JSON → SSE `event:`/`data:` |
| POST | `/v1/responses/compact` | typed-JSON → SSE |
| POST | `/v1/messages` | SSE bytes |
| POST | `/anthropic/v1/messages` | SSE bytes |
| GET | `/health` | — returns `{"status":"ok","version":"..."}` |

Auth headers (`Authorization`, `x-api-key`, `anthropic-version`,
`anthropic-beta`, `OpenAI-Organization`, `OpenAI-Project`) are forwarded on the
WS handshake when present. cc2ws never stores keys and never logs their values.

## Point your client at it

### Claude Code

```bash
export ANTHROPIC_BASE_URL=http://127.0.0.1:18080
# or http://127.0.0.1:18080/anthropic
export ANTHROPIC_AUTH_TOKEN=<your-key>
```

### Codex (`config.toml`)

```toml
model_provider = "custom"
[model_providers.custom]
name = "wss-proxy"
base_url = "http://127.0.0.1:18080/v1"
wire_api = "responses"
```

## Acceptance checks (live upstream)

```bash
KEY=Bearer <your-key>
BASE=http://127.0.0.1:18080

# 1) Anthropic non-stream
curl -sS $BASE/v1/messages -H "Authorization: $KEY" \
  -H "anthropic-version: 2023-06-01" -H "content-type: application/json" \
  -d '{"model":"glm-5.2","max_tokens":32,"messages":[{"role":"user","content":"Reply with exactly: pong"}]}'

# 2) Anthropic stream
curl -N $BASE/v1/messages -H "Authorization: $KEY" \
  -H "anthropic-version: 2023-06-01" -H "content-type: application/json" \
  -d '{"model":"glm-5.2","stream":true,"max_tokens":32,"messages":[{"role":"user","content":"ping"}]}'

# 3) OpenAI Chat stream
curl -N $BASE/v1/chat/completions -H "Authorization: $KEY" -H "content-type: application/json" \
  -d '{"model":"glm-5.2","stream":true,"messages":[{"role":"user","content":"ping"}]}'

# 4) OpenAI Responses stream
curl -N $BASE/v1/responses -H "Authorization: $KEY" -H "content-type: application/json" \
  -d '{"model":"glm-5.2","stream":true,"input":"Reply with exactly: pong","max_output_tokens":32}'

# 5) OpenAI Responses non-stream → single JSON, HTTP 200
curl -sS $BASE/v1/responses -H "Authorization: $KEY" -H "content-type: application/json" \
  -d '{"model":"glm-5.2","stream":false,"input":"Reply with exactly: pong","max_output_tokens":32}'

# 6) Bad upstream → 502
UPSTREAM_BASE=http://127.0.0.1:1 ./cc2ws &
curl -sS -o /dev/null -w "%{http_code}\n" $BASE/v1/messages ...
# → 502
```

## Design

See `docs/superpowers/specs/2026-07-11-cc2ws-http-to-wss-proxy-design.md`.

```
HTTP POST (client)
  → dial WSS (upstream, same path)
  → send 1 text request message (raw JSON body)
  → read text frames
  → write HTTP SSE/JSON back to client
  → close upstream WS
```

Never upgrades the local HTTP to WS; never treats the upstream WSS URL as HTTP.

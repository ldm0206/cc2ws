# cc2ws

A minimal local **HTTP → WebSocket** proxy for OpenAI / Anthropic model calls.

Local clients (Claude Code, Codex, anything speaking the OpenAI/Anthropic HTTP
API) send plain HTTP POST. cc2ws dials a **same-path upstream WebSocket**
(`wss://`), sends the raw JSON body as one text message, and pumps the upstream
frames back to the client as HTTP SSE or JSON.

> The upstream must accept **WebSocket Upgrade on the same path** as the HTTP
> route (AxonHub-style gateways). Plain HTTPS-only OpenAI/Anthropic official
> endpoints will **not** work — they are not WebSocket gateways.

## Install

Download a prebuilt **raw binary** from
[Releases](https://github.com/ldm0206/cc2ws/releases) — no archives, just the
executable. Assets:

| OS | Asset |
|---|---|
| Windows | `cc2ws-windows-amd64.exe` |
| macOS (Intel) | `cc2ws-darwin-amd64` |
| macOS (Apple silicon) | `cc2ws-darwin-arm64` |
| Linux (amd64) | `cc2ws-linux-amd64` |
| Linux (arm64) | `cc2ws-linux-arm64` |

Each release also ships a `checksums.txt` (SHA256 of every asset); the in-app
updater fetches it to verify downloads before applying.

Builds are **unsigned**.

### macOS first run

Because the binary is unsigned, Gatekeeper quarantines it on first launch.
Clear the attribute:

```bash
xattr -d com.apple.quarantine cc2ws-darwin-*
```

…or right-click → *Open* → *Open anyway* in Finder.

### Build from source

```bash
go build -o cc2ws ./cmd/cc2ws          # Linux (pure Go, no cgo)
CGO_ENABLED=1 go build -o cc2ws ./cmd/cc2ws   # Windows/macOS GUI (needs a C compiler)
```

The Windows/macOS build imports Fyne and requires cgo + a C compiler
(`gcc`/`mingw-w64`). Linux builds are pure Go.

## Run

By default cc2ws opens its **native UI** — the Fyne GUI on Windows/macOS, the
bubbletea TUI on Linux. Pass `-headless` (or set `CC2WS_HEADLESS=true`) for the
windowless mode (stdout logs) used on servers / SSH / CI.

```bash
export UPSTREAM_BASE=https://hub.example.com   # or http://127.0.0.1:8090 locally
export LISTEN=127.0.0.1:18080
./cc2ws                                         # native UI (default)
./cc2ws -headless                               # windowless (servers/SSH/CI)
./cc2ws -version
```

Flags mirror the env vars and win if both are set:

```bash
./cc2ws -listen 127.0.0.1:18080 -upstream-base https://hub.example.com
```

### Windows / macOS GUI (default)

Opens a Fyne window with three tabs:

- **Settings** — Upstream base, listen address, connect/idle timeouts, log
  level, skip-TLS. *Save & Apply* validates, persists to `config.json`, and
  hot-restarts the running proxy. *Start* / *Stop* control the server.
- **Logs** — last 500 connection-log lines (info+).
- **About** — version + in-app updater (see below).

Closing the window stops the proxy and exits.

### Linux TUI (default)

An 80×24 [bubbletea](https://github.com/charmbracelet/bubbletea) TUI with the
same three views: `[1]Settings [2]Logs [3]About`, plus `[s]`tart / `[x]`top /
`[q]`uit. Use `-headless` for windowless servers / SSH.

### In-app update (GUI & TUI)

The **About** tab has a *Check for updates* action. It queries the latest
GitHub Release, verifies the new binary's SHA256 against `checksums.txt`, then
self-applies via [minio/selfupdate](https://github.com/minio/selfupdate).
Checksum verification is **always** enforced before apply — there is no path
that applies an unverified binary. After a successful apply, restart cc2ws to
run the new version.

### Configuration

Precedence (low → high): **defaults < `config.json` < env < flags.** The
Settings UI persists to `UserConfigDir/cc2ws/config.json`
(`%AppData%\cc2ws\config.json` on Windows,
`~/Library/Application Support/cc2ws/config.json` on macOS,
`$XDG_CONFIG_HOME/cc2ws/config.json` — or `~/.config/cc2ws/config.json` —
on Linux); env vars and CLI flags still override it. The file is optional — a
missing or corrupt file is silently ignored.

| Env / Flag | Default | Description |
|---|---|---|
| `LISTEN` / `-listen` | `127.0.0.1:18080` | local HTTP listen address |
| `UPSTREAM_BASE` / `-upstream-base` | (required) | upstream origin only — `https://host` or `http://host:port` |
| `UPSTREAM_INSECURE_SKIP_TLS_VERIFY` / `-insecure-skip-tls-verify` | `false` | skip upstream TLS verify (self-signed local debugging only) |
| `CONNECT_TIMEOUT` / `-connect-timeout` | `10s` | upstream WS dial timeout |
| `IDLE_TIMEOUT` / `-idle-timeout` | `600s` | upstream WS per-read idle timeout |
| `LOG_LEVEL` / `-log-level` | `info` | log level |
| `CC2WS_HEADLESS` / `-headless` | `false` | `true` = no UI (servers/SSH/CI); `false` (default) = open GUI/TUI |

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

# 6) Bad upstream → 502 (run cc2ws against an unreachable upstream, on a separate port)
UPSTREAM_BASE=http://127.0.0.1:1 LISTEN=127.0.0.1:18081 ./cc2ws &
sleep 1
curl -sS -o /dev/null -w "%{http_code}\n" http://127.0.0.1:18081/v1/messages \
  -H "content-type: application/json" \
  -d '{"model":"glm-5.2","max_tokens":32,"messages":[{"role":"user","content":"ping"}]}'
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

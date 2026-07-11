# cc2ws — Managed Desktop App (GUI/TUI, raw-binary distribution, in-app updates)

**Date:** 2026-07-11
**Status:** Approved design (pending implementation plan)
**Supersedes (distribution):** the goreleaser-archive flow described in `2026-07-11-cc2ws-http-to-wss-proxy-design.md`.

## Goal

Turn cc2ws from a headless CLI into a managed desktop application:

1. **Raw-executable distribution** via GitHub Releases (no zip/tar.gz/installer).
2. **Settings UI** to edit proxy config interactively (persisted).
3. **Connection-log view** showing live request/lifecycle logs.
4. **In-app updates** that check GitHub Releases, download, and self-apply.

## Platform matrix

| OS | Frontend | Build |
|---|---|---|
| Windows (amd64) | Fyne GUI | native, cgo |
| macOS (amd64 + arm64) | Fyne GUI | native, cgo |
| Linux (amd64 + arm64) | bubbletea TUI | native, pure Go (no cgo) |

All three platforms retain a `--headless` mode that runs the proxy with no UI (logs to stdout) for servers / SSH / CI.

### Why Fyne + cgo (and why native builds, not cross-compile)

Fyne links system GL via cgo on every platform (mingw on Windows, OpenGL.framework on macOS, GL/libX11 on Linux). The current release builds on one Ubuntu runner with `CGO_ENABLED=0` and cross-compiles — that works for the pure-Go proxy but **cannot cross-compile cgo → macOS** without a fragile osxcross setup. The release workflow therefore becomes a **native build matrix**: each OS builds itself, where cgo "just works".

### Why Linux is TUI, not GUI

Fyne on Linux needs a display server + GL — awkward on headless/SSH. Linux instead gets a bubbletea TUI (pure Go, no cgo) that mirrors the GUI experience in an 80×24 terminal.

## Repository structure

Existing root `package main` files move into an importable core so frontends can share logic.

```
cc2ws/
  cmd/cc2ws/main.go          # entrypoint: parse flags, dispatch --headless vs frontend
  internal/
    core/
      config.go               # Config + LoadConfig + file persistence + precedence
      server.go               # HTTP server start/stop lifecycle (from inline main())
      proxy.go                # moved from root
      routes.go               # moved from root
      frames.go               # moved from root
      logger.go               # rewritten: ring-buffer + fan-out
      updater.go              # GitHub Releases check + download + self-apply
      *_test.go               # moved + new tests
  app/
    frontend.go               # Frontend interface: Run(ctx, core.Handle) error
    gui/                      # //go:build windows || darwin  (Fyne)
      gui.go
    tui/                      # //go:build linux            (bubbletea)
      tui.go
  .github/workflows/{ci,release}.yml
```

- **One binary name per platform** (`cc2ws` / `cc2ws.exe`); build tags in `app/gui` vs `app/tui` select the right frontend per OS.
- **Entrypoint** (`cmd/cc2ws/main.go`): if `--headless` → run core headless (all platforms); otherwise hand off to the build-selected frontend.
- **Core handle** the frontends drive: `Start(cfg)`, `Stop()`, `Config()`, `SubscribeLogs() <-chan LogEntry`, `CheckUpdate() (UpdateInfo, error)`, `ApplyUpdate(UpdateInfo) error`. Frontends stay thin.

## Core subsystems

### Config persistence & precedence

Config file at `os.UserConfigDir()/cc2ws/config.json` (Windows `%AppData%\cc2ws\config.json`; macOS `~/Library/Application Support/cc2ws/config.json`; Linux `~/.config/cc2ws/config.json`). Created on first save.

**Precedence (low → high):** defaults → config file → env vars → CLI flags. Flags win (same as today); the settings UI writes the file. Same fields as the current env/flags table: Listen, UpstreamBase, InsecureSkipTLSVerify, ConnectTimeout, IdleTimeout, LogLevel. **No new config surface** — just persistence + a form. Auth keys are never stored or logged (existing behavior preserved; no key field in the UI).

### Log streaming (the connection-log view)

Replace `withRequestLog`'s `log.Printf` with a **ring-buffered structured logger** (`core/logger.go`). Every log call — request logs (`METHOD path status latency upstream=… auth=…`) and app lifecycle (start/stop/dial/update) — becomes a `LogEntry{Time, Level, Msg, Fields}`.

Entries fan out to **two sinks**:
1. stderr/stdout — preserves current `cc2ws …` console output for headless/CLI users.
2. In-memory ring buffer (cap ~2000 entries; oldest dropped).

Frontends subscribe via `core.SubscribeLogs() <-chan LogEntry` and receive live entries + a buffer snapshot on connect (no polling). The Logs view is a scrollable list with a level filter (debug/info/warn/error) and a Clear button. Auth values stay masked as today.

### In-app updater

- **Source:** the project's own GitHub Releases. Query `https://api.github.com/repos/<owner>/cc2ws/releases/latest`; compare its tag to `main.version` (injected via ldflags).
- **Trigger:** async non-blocking check on app launch (surfaces a badge if newer); plus a manual "Check for updates" button.
- **Download:** distribution is raw binaries, so download the release asset matching `runtime.GOOS`+`runtime.GOARCH` directly (no unzip). Verify the downloaded bytes against the SHA256 listed in the release's `checksums.txt` artifact (CI computes and uploads this alongside the binaries) — fetch `checksums.txt`, parse the line for this asset's filename, compare to `sha256.Sum256` of the downloaded bytes. Mismatch → discard download, fail the update.
- **Apply:** `github.com/minio/selfupdate` — handles cross-platform self-replace; on Windows renames the running `.exe` aside and swaps in the new one (a running Windows binary can't be overwritten in place). Then prompt restart.
- **Failure handling:** any step (network, checksum, write) fails → log the error, leave the running binary untouched, show the error in the UI. Never half-replace.
- **Signing:** builds are unsigned, so the updater verifies the **checksum** (SHA256 from `checksums.txt`), not a cryptographic signature. A bad actor who can publish a release + a matching `checksums.txt` could push a malicious binary — accepted risk for a personal/distrib tool. macOS Gatekeeper / Windows SmartScreen warnings remain a documented limitation.

## Frontends

Both frontends render **Settings, Logs, About/Update** and call the same `core` handle.

### Fyne GUI (Windows / macOS)

Three tabs, a header status line, a start/stop control. Same Material-style controls on both OSes (Fyne draws its own).

```
┌─ cc2ws  v0.3.0                              ● Running ─┐
│ [ Settings ] [ Logs ] [ About ]      [ Stop proxy ]    │
│                                                        │
│  Upstream base   [ https://hub.example.com        ]    │
│  Listen address  [ 127.0.0.1:18080                 ]   │
│  Connect timeout [ 10s      ]   Idle timeout [ 600s ]  │
│  Log level       [ info ▼ ]                            │
│  □ Skip upstream TLS verify (debug only)               │
│                                                        │
│                              [ Revert ] [ Save & Apply]│
└────────────────────────────────────────────────────────┘
```

- **Status dot** (● Running / ○ Stopped / ⚠ Error) reflects real server state via `core` callbacks.
- **Save & Apply** validates (same `swapScheme`/duration parsing as today), writes `config.json`, and **hot-restarts** the server with the new config — no app restart needed. Invalid input → inline error, nothing written.
- **Logs tab:** scrollable list, level filter chips (debug/info/warn/error), auto-scroll toggle, Clear. Live via the log channel.
- **About tab:** current version, latest version + "update available" badge, changelog excerpt, Check / Download & Restart buttons.
- **Window close:** quits the app and stops the proxy (graceful 10s shutdown). `--headless` stays available for windowless runs.

### bubbletea TUI (Linux)

Same three sections, keyboard-driven, fits an 80×24 terminal:

```
┌ cc2ws v0.3.0                          ● Running ─────┐
│ SETTINGS                                            │
│  Upstream base : https://hub.example.com            │
│  Listen        : 127.0.0.1:18080                    │
│  Connect/idle  : 10s / 600s      Level: info        │
│  Skip TLS verify: off                               │
│ ───────────────── CONNECTION LOGS ─────────────────  │
│  14:02:01 POST /v1/messages    200   45ms  auth     │
│  14:02:03 POST /v1/messages    200  1.2s   auth     │
│  14:02:05 POST /v1/chat/comp.. 200  220ms  auth     │
│ ───────────────────────────────────────────────────  │
│ [1]Settings [2]Logs [3]About  [s]Start/[x]Stop       │
│ [e]Edit config  [u]Check update  [q]Quit            │
└─────────────────────────────────────────────────────┘
```

- **Tabs `[1]/[2]/[3]`** switch the main pane (Settings summary / full-height Logs / About+Update).
- **`[e]` edit** opens an inline form (same fields, same validation); Enter saves & applies, Esc reverts.
- **Logs pane** subscribes to the same channel; `[f]` cycles level filter, `[c]` clears.
- **`[u]`** runs the same updater path as the GUI.
- `--headless` skips bubbletea entirely and behaves like the current CLI (stdout logs, no `$TERM` needed — works over SSH / servers).

Both frontends share validation, so a bad value is caught identically regardless of surface.

## Build & distribution

Raw binaries, no archives — built natively with cgo per platform.

### CI (`.github/workflows/ci.yml`)

Matrix over `windows-latest / macos-latest / ubuntu-latest` runs `go vet` + `go test` on every push/PR. On macOS/Windows the Fyne packages compile (cgo), so a broken GUI build fails CI here, not at release time.

### Release (`.github/workflows/release.yml`)

Native build matrix replaces goreleaser:

```
strategy:
  matrix:
    include:
      - { os: windows-latest, goos: windows, goarch: amd64, ext: .exe }
      - { os: macos-latest,   goos: darwin,  goarch: amd64 }
      - { os: macos-latest,   goos: darwin,  goarch: arm64 }
      - { os: ubuntu-latest,  goos: linux,   goarch: amd64 }
      - { os: ubuntu-latest,  goos: linux,   goarch: arm64 }
```

Each job:
1. Checkout (full history for tag discovery).
2. Compute next minor version (same `git describe` logic as today).
3. Build **natively** with cgo: `go build -ldflags "-s -w -X main.version=<ver>"` → a single raw binary.
4. Generate SHA256 for its binary; upload binary + checksum as workflow artifacts.
5. A final **publish job** (depends on all build jobs) creates one GitHub Release, attaches all raw binaries + a combined `checksums.txt`.

Notes:
- macOS arm64 builds on `macos-latest` (Apple Silicon) natively. macOS amd64 builds on `macos-latest` with `GOARCH=amd64` — cgo cross-compile within macOS works (same SDK), unlike Linux→macOS.
- **goreleaser is removed.** The auto-changelog it provided is dropped (keep the workflow simple); optionally replace with a `git log` snippet in the publish job.
- **macOS distribution caveat:** the raw `cc2ws` mach-o binary runs from Terminal. On first run the user does `xattr -d com.apple.quarantine cc2ws` for Gatekeeper, or right-click → Open. A double-clickable `.app` bundle is a documented future option, not in this scope.
- **Windows:** raw `cc2ws.exe` — SmartScreen may warn on first run (unsigned), documented. No installer.

### What the user downloads

One raw executable per platform/arch. No zip, no tar.gz, no installer. `chmod +x` (unix), run it, get the GUI/TUI.

## Error handling

- **Config errors** (bad URL scheme, unparseable duration, missing upstream on a non-editable run): inline error on the Settings form; in headless mode, exit non-zero with a clear `cc2ws:` message (same as today). Proxy never starts with invalid config.
- **Server runtime errors** (dial failures, upstream read timeouts): already mapped to HTTP `502`/`504` by existing proxy code — unchanged. Now also emit a `warn`/`error` log entry so they appear in the Logs view.
- **Updater errors** (network, checksum mismatch, write failure): logged + shown in About tab; running binary left untouched. Never a partial swap.
- **Log subscriber backpressure:** the fan-out channel is non-blocking with a bounded buffer; a slow frontend drops entries (logged once), never blocks the proxy. Proxy performance never depends on UI consumption.
- **Window-close vs `--headless`:** close → graceful server shutdown (existing 10s context). `--headless` has no window, exits on SIGINT/SIGTERM as today.
- **Corrupt `config.json`:** if parse fails on load, fall back to defaults + env + flags (never fail to start because of a bad file), log a warning. Overwritten on next save.

## Testing

- **Core unit tests** move with the code into `internal/core/` — existing `config_test`, `proxy_test`, `frames_test`, `logging_test`, `server_test` keep passing unchanged (refactor is mechanical, behavior-preserving).
- **New `updater_test.go`** — version comparison + asset-name selection (`GOOS`/`GOARCH` → expected filename) with a fake release manifest (no network). Download/apply exercised manually in the verify step, not unit-tested.
- **New `logger_test.go`** — ring buffer bounds, subscriber fan-out, drop-on-overflow (replaces/expands the existing `logging_test.go`).
- **New `config_persist_test.go`** — file save/load round-trip, precedence (defaults < file < env < flags), corrupt-file fallback.
- **Frontends are thin** → light testing: the TUI gets a small state-transition test; the GUI is verified manually (Fyne is hard to unit-test). Both rely on `core` tests for correctness.
- **CI** runs the full suite on the 3-OS matrix, so a build-tag mistake (e.g., TUI code leaking into a Windows build) fails fast.

## Implementation sequencing

1. Refactor root `.go` → `internal/core/` (behavior-preserving; CI green).
2. Config persistence + precedence + tests.
3. Ring-buffer logger + tests; wire into proxy/routes.
4. `core` handle API (`Start`/`Stop`/`SubscribeLogs`) + headless entrypoint (`--headless`).
5. Updater (check + checksum + apply) + tests.
6. TUI (Linux) — first frontend, proves the `core` handle.
7. GUI (Windows/macOS) — Fyne.
8. CI matrix + release workflow rewrite (native builds, raw binaries).

## Out of scope

- Double-clickable `.app` bundle (macOS) — documented future option.
- Code signing / notarization (macOS) / authenticode (Windows) — builds remain unsigned; updater verifies checksum only.
- An installer (MSI/pkg/deb) — distribution stays raw binaries per requirement.
- goreleaser auto-changelog — dropped; optionally replaced with a `git log` snippet.
- Per-platform `.app`/dmg packaging — raw binary only.

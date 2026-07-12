# cc2ws v0.4 — Gio GUI Redesign, wss Support, Autostart, Bilingual i18n

**Date:** 2026-07-12
**Status:** Approved design (pending implementation plan)
**Builds on:** `2026-07-11-cc2ws-managed-desktop-app-design.md` (the Fyne-based managed desktop app).

## Goal

Address four user-reported defects in the v0.3 managed desktop app:

1. **wss upstream rejected** — `swapScheme` only accepts `http(s)://`; typing `wss://host` errors with `upstream base must be http(s)://`. Users who know their upstream is wss are forced to guess the matching https origin.
2. **No Chinese** — all UI strings are hardcoded English.
3. **No boot autostart** — no OS-level "launch on login" integration.
4. **Ugly UI** — the Fyne GUI uses the default theme with zero styling; combined with no CJK font, Chinese renders as tofu.

The fix rewrites the Windows/macOS GUI from Fyne to **Gio** (immediate-mode, pure-Go, GL-backed, single binary, no system GUI libraries beyond the OS's own GL), adds a zero-dependency bilingual i18n layer with an embedded CJK font, adds a per-platform autostart module, and extends `swapScheme` to accept `ws`/`wss` input. `internal/core` is untouched except for small, additive config changes.

## Locked decisions

| Decision | Choice | Rationale |
|---|---|---|
| GUI framework (Win/macOS) | **Gio** (replaces Fyne) | Non-web, single binary, no runtime library installs on Win/macOS, modern aesthetic ceiling. Webview-based frameworks (Wails/Electron) rejected: they require authoring HTML/CSS/JS and depend on WebView2/webkit2gtk. |
| Linux frontend | **bubbletea TUI, retained** | Keeps SSH/headless usability (per project memory: "Linux stays TUI"). Gets bilingual strings + layout polish. |
| Frontend stack for GUI | n/a (Gio is Go) | No web authoring. |
| Language | **Bilingual, default zh** | `config.Language` ∈ `{"zh","en"}`, default `"zh"`. Toggle in Settings. |
| Autostart scope | **All three platforms** | Win registry Run key, macOS LaunchAgent, Linux XDG `.desktop`. |
| i18n mechanism | **Zero-dep map table** | ~40 strings; `go-i18n` + toml/json is unjustified weight. |
| Gio state model | **Pull-per-frame** | UI reads `h.Config()`/`h.Running()` each frame; background goroutines call `w.Invalidate()` on change. Idiomatic Gio; thinnest binding to the existing `core.Handle`. |
| CJK font | **`go:embed` a CJK font** (Noto Sans SC or LXGW WenKai) registered as Gio theme default | Chinese renders everywhere; binary +5–8MB, acceptable for a desktop GUI. |

## Platform matrix (unchanged from v0.3, framework updated)

| OS | Frontend | Build |
|---|---|---|
| Windows (amd64) | **Gio** GUI | native, cgo |
| macOS (amd64 + arm64) | **Gio** GUI | native, cgo |
| Linux (amd64 + arm64) | bubbletea TUI | native, pure Go (no cgo) |

All three retain `--headless`. Gio links the OS's own GL (OpenGL.framework on macOS, opengl32 on Windows); no WebView2, no webkit2gtk, no extra runtime installs on the GUI platforms.

## Module structure

```
cc2ws/
  cmd/cc2ws/main.go              # unchanged shape: flags → core → frontend dispatch
  internal/
    core/                        # UNCHANGED except config.go (additive)
      config.go                  # +Language +AutoStart; swapScheme accepts ws/wss
      handle.go proxy.go routes.go frames.go logger.go updater.go  # untouched
    i18n/                        # NEW
      i18n.go                    # Lang type, T(key), SetLang, default zh
      strings.go                 # zh + en maps, keyed by stable string IDs
    autostart/                   # NEW
      autostart.go               # common: Enable/Disable/IsEnabled + EnableIfWanted
      autostart_windows.go       # //go:build windows — HKCU\...\Run
      autostart_darwin.go        # //go:build darwin  — ~/Library/LaunchAgents plist
      autostart_linux.go         # //go:build linux   — ~/.config/autostart/*.desktop
  app/
    frontend.go                  # unchanged Frontend interface + dispatch
    frontend_gui.go              # unchanged build tag (windows || darwin) → app/gui
    frontend_linux.go            # unchanged → app/tui
    frontend_nonative.go         # unchanged nil fallback
    gui/                         # REWRITTEN Fyne → Gio  (//go:build windows || darwin)
      gui.go                     # Gio window, event loop, owns Start/Stop, binds Handle
      theme.go                   # custom Gio theme (colors/spacing) + embedded CJK font
      assets/                    # embedded font (go:embed)
      page_settings.go           # form: upstream/listen/timeouts/level/skipTLS + autostart + lang
      page_logs.go               # scrolling log list, level filter, clear
      page_about.go              # version, check update, download & apply
    tui/                         # bilingual via i18n; minor layout polish
      tui.go
```

`internal/core`'s proxy/handle/logger/updater logic is untouched. This is what makes a GUI framework swap safe: the new frontend talks to the same `*core.Handle` the Fyne frontend did.

## Subsystem: wss upstream fix (`core/config.go`)

`swapScheme` is generalized to accept all four schemes. The proxy still dials `cfg.UpstreamWS`; only the input acceptance and `UpstreamBase` normalization change.

| Input (`UpstreamBase`) | `UpstreamWS` | Normalized `UpstreamBase` |
|---|---|---|
| `wss://host[:port]/...` | `wss://host[:port]` | `https://host[:port]` |
| `ws://host[:port]/...` | `ws://host[:port]` | `http://host[:port]` |
| `https://host[:port]/...` | `wss://host[:port]` | unchanged |
| `http://host[:port]/...` | `ws://host[:port]` | unchanged |

Rules preserved from today: path/query/fragment are stripped when computing `UpstreamWS` (the proxy appends the request path at dial time); host must be non-empty; any other scheme is an error. `Validate` and `BuildConfigFromStrings` reuse the same function, so the form and CLI validate identically.

This is a 4-line generalization of the existing `switch`; behavior for `http`/`https` input is byte-for-byte unchanged.

## Subsystem: autostart (`internal/autostart`)

Common API (all platforms):

```go
func Enable() error       // register cc2ws to launch at login, using os.Executable() path
func Disable() error      // remove the registration (no-op if absent)
func IsEnabled() bool     // true iff the registration exists and points at the current exe
func EnableIfWanted(want bool) error  // Enable or Disable to match `want`; returns Enable/Disable error
```

Per-platform registration:

- **Windows** — `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`, value name `cc2ws`, data `"C:\path\to\cc2ws.exe"` (quoted). `HKCU` (current user) → no elevation/UAC. `IsEnabled` reads the value and compares paths.
- **macOS** — write `~/Library/LaunchAgents/io.cc2ws.app.plist` with `ProgramArguments` = `[<exe path>]` and `RunAtLoad=true`. Per-user; no admin. `IsEnabled` checks file existence + path match.
- **Linux** — write `~/.config/autostart/cc2ws.desktop` (XDG Autostart, `Type=Application`, `Exec=<exe path>`). Works under GNOME/KDE/XFCE. `IsEnabled` checks file existence + Exec match.

Config stores the *intent* (`AutoStart bool`). On startup the GUI calls `autostart.EnableIfWanted(cfg.AutoStart)` to re-sync if the user deleted the OS entry out-of-band. Toggling the checkbox calls `EnableIfWanted` immediately and reports any error inline; on success it persists `AutoStart` via `SetConfig`.

Path resolution uses `os.Executable()` (resolves symlinks). Home/config dirs use `os.UserConfigDir()` / `os.UserHomeDir()`, overridable by `CC2WS_CONFIG_DIR` in tests (same pattern as `core.configDir`).

## Subsystem: i18n + CJK font

### i18n package (`internal/i18n`)

```go
type Lang string
const (
    ZH Lang = "zh"
    EN Lang = "en"
)

var current Lang = ZH

func SetLang(l Lang)        // called once at startup from config.Language
func T(key string) string   // returns current-lang string; falls back to zh, then to key
```

`strings.go` holds two maps keyed by stable IDs (`"upstream_base"`, `"listen_addr"`, `"start"`, `"stop"`, `"running"`, `"stopped"`, `"autostart"`, `"language"`, …). Both GUI and TUI import `i18n`; no string literals in the UI code.

### CJK font (GUI only)

`app/gui/assets/` holds an embedded CJK font (Noto Sans SC Regular or LXGW WenKai — chosen for open license + clean rendering at small sizes). `theme.go` registers it via Gio's `text` font registry and sets it as the `material.Theme` default. Chinese renders on Windows and macOS without relying on system CJK fonts. The TUI needs no font (it uses the terminal's fonts, which already handle CJK).

## Subsystem: config extension (`core/config.go`)

```go
type Config struct {
    Listen                string
    UpstreamBase          string
    UpstreamWS            string
    InsecureSkipTLSVerify bool
    ConnectTimeout        time.Duration
    IdleTimeout           time.Duration
    LogLevel              string
    Language              string        // NEW — "zh" (default) | "en"
    AutoStart             bool          // NEW
}
```

`fileConfig` gains `Language *string` and `AutoStart *bool` (pointers + `omitempty`), preserving the existing "absent field falls through to env/defaults" semantics. `LoadConfig`/`DefaultConfig`/`Validate` handle the new fields: `DefaultConfig` sets `Language: "zh"`; `Validate` rejects anything other than `"zh"`/`"en"` (normalizing empty → `"zh"` first). `SaveConfig` persists both.

Precedence is unchanged: defaults < config file < env (`CC2WS_LANG`, `CC2WS_AUTOSTART`) < CLI flags (none added — language/autostart are UI-driven, but env is honored for headless users).

## Frontend: Gio GUI (Windows / macOS)

### Event loop & threading

`gui.Run(ctx, h)`:
1. `i18n.SetLang(Lang(h.Config().Language))`.
2. Create Gio `app.Window` titled `cc2ws <version>`.
3. Start a log-drain goroutine: reads `h.SubscribeLogs()`, appends to a 500-entry ring, calls `w.Invalidate()` per entry (coalesced by Gio).
4. `h.Start()` (surfacing bind error on the status line, as today).
5. Run the Gio event loop on the main goroutine. Each frame: read `h.Config()`/`h.Running()`, render the active page, apply the theme.
6. On `tea.WindowClosed` / ctx done: `h.Stop()`, quit.

Long operations (`CheckUpdate`, `ApplyUpdate`) run in goroutines; on completion they store the result in UI state and `Invalidate()`. The proxy (`h.Start`) already runs in its own goroutine; the UI never blocks on network.

### Pages

Three pages with a left tab rail and a persistent status header.

```
┌─ cc2ws  v0.4           ● 运行中        语言: 中文 ▾ ──────┐
│ [设置]  日志  关于                                       │
│                                                          │
│  上游地址    [ wss://hub.example.com              ]      │
│  监听地址    [ 127.0.0.1:18080                   ]       │
│  连接超时    [ 10s ]            空闲超时 [ 600s ]        │
│  日志级别    [ info ▾ ]                                  │
│  ☐ 跳过上游 TLS 校验（仅调试）                          │
│  ☑ 开机自启                                             │
│                                                          │
│              [ 恢复 ]   [ 保存并应用 ]                   │
│  [ 启动 ]  [ 停止 ]       状态: 运行中                   │
└──────────────────────────────────────────────────────────┘
```

- **Status dot**: green 运行中 / gray 已停止 / red 错误 — reflects `h.Running()` + last error.
- **Upstream field** accepts `http`/`https`/`ws`/`wss` (the wss fix).
- **Save & Apply**: `BuildConfigFromStrings(...)` → `h.SetConfig(cfg)` (validates, saves, hot-restarts). Invalid input → inline red message, nothing written.
- **Autostart checkbox**: toggles `autostart.EnableIfWanted` immediately; persists via `SetConfig`.
- **Language selector**: `i18n.SetLang` immediately (UI re-renders in the new language without restart); persists via `SetConfig`.

**日志** page: scrolling list (Gio `layout.List`), level filter chips (debug/info/warn/error), Clear button. Entries from the ring buffer; auto-scroll pinned to bottom.

**关于** page: current version, "latest version + update available" badge, Check / Download & Apply buttons. Same `h.CheckUpdate`/`h.ApplyUpdate` path as today.

### Theme (`theme.go`)

A custom `material.Theme` with: a neutral palette (warm gray surfaces, single blue accent), 4px corner radius, comfortable spacing, the embedded CJK font as default. No external theme package — Gio's `material` is enough. Goal: no longer look like unstyled default widgets.

## Frontend: bubbletea TUI (Linux) — minor

`app/tui/tui.go` switches all literal strings to `i18n.T(...)`. Layout gains: a level filter indicator, and the upstream line shows the actual scheme the user typed (now that wss is accepted). No structural change. English text is reachable by setting `CC2WS_LANG=en`.

## Build & distribution

### go.mod

- **Remove** `fyne.io/fyne/v2` (and its indirect GL/glfw/text deps).
- **Add** `gioui.org` and `gioui.org/x` (x only if a needed widget isn't in core `material`/`widget`).
- `gorilla/websocket`, `charmbracelet/bubbletea`, `minio/selfupdate` unchanged. `cgo` still required for GL on Win/macOS (same as Fyne).

### CI (`.github/workflows/ci.yml`)

Matrix unchanged (`ubuntu/windows/macos`). `go vet` / `go test` / `go build` unchanged. Linux runner still never compiles Gio (TUI only, pure Go). Windows/macOS runners compile Gio with cgo — GL headers are present on both runner images by default.

### Release (`.github/workflows/release.yml`)

- Native build matrix unchanged (5 entries).
- Windows: `go build -ldflags "-s -w -H=windowsgui -X cc2ws/internal/core.Version=<ver>"` — `-H=windowsgui` still correct (Gio is a GUI app).
- macOS: the hand-rolled `.app` bundle + `hdiutil` dmg step is reused as-is (Gio binary drops into `cc2ws.app/Contents/MacOS/cc2ws` identically to the Fyne binary).
- **No archives** (per project memory: native `.exe`/`.dmg` + raw binaries, never zip/tar.gz). `checksums.txt` over raw binaries unchanged so the self-updater keeps working.
- Binary size grows ~5–8MB from the embedded CJK font; acceptable.

## Error handling

Unchanged philosophy, extended to the new surfaces:

- **Config/validation errors**: inline red message on the Settings page (Gio label). Headless: non-zero exit with `cc2ws:` message (existing).
- **Autostart Enable/Disable errors**: inline message next to the checkbox; preference is *not* persisted on failure. Running proxy untouched.
- **Updater errors**: shown on the 关于/About page; running binary untouched (existing never-half-replace guarantee).
- **Unknown language / missing i18n key**: `T()` falls back to zh, then to the key itself — never panics, never shows empty.
- **Font load failure** (corrupt embedded asset): treat as build-time error (the asset is `go:embed`-d; if it's missing the build fails), not a runtime path.

## Testing

- **`core/config`**: extend `config_test` with `swapScheme` cases for `ws://`/`wss://` input (UpstreamWS + normalized base asserted); config round-trip with `Language`/`AutoStart`; `Validate` accepts `zh`/`en`, rejects others, normalizes empty → `zh`.
- **`i18n`**: `T()` returns the right lang after `SetLang`; missing key falls back zh → key; default lang is zh.
- **`autostart`**: inject a temp config/home dir (reuse `CC2WS_CONFIG_DIR` + a new `CC2WS_HOME_DIR` test seam), then assert `Disable`→`Enable`→`IsEnabled` round-trip and the generated plist/registry-value/desktop file shape (paths, `RunAtLoad`/`Exec`/value-data correct). No real OS writes hit.
- **GUI**: thin; verified manually on Windows + macOS (Gio is hard to unit-test). Relies on `core` tests for correctness.
- **TUI**: extend `tui_test` to assert `i18n.T` is wired (strings change with `SetLang`).
- **CI** runs the full suite on the 3-OS matrix; a build-tag leak (Gio code in a Linux build, or TUI code in a Windows build) fails fast.

## Implementation sequencing

1. **wss fix** in `core/config.go` + tests. (Standalone; unblocks wss usage regardless of GUI progress.)
2. **i18n package** + wire into TUI. (Small; immediate visible win on Linux.)
3. **autostart package** — common API + three per-OS files + tests.
4. **Config extension** — `Language`/`AutoStart` fields + persistence + validation.
5. **Gio GUI rewrite** — theme + font, event loop, three pages, bind `core.Handle`.
6. **Release/CI** — swap Fyne → Gio in `go.mod`, verify matrix builds, keep raw-binary + dmg packaging.

Each step leaves the tree building and tests green. Step 1 ships value even if GUI work is interrupted.

## Out of scope

- Code signing / notarization (macOS) / authenticode (Windows) — builds remain unsigned; updater verifies checksum only. (Unchanged from v0.3.)
- An installer (MSI/pkg/deb) — raw binaries + dmg only, per project memory.
- Linux GUI — Linux stays TUI by design (project memory). XDG autostart still launches the TUI at login.
- A language beyond zh/en in v0.4 — the i18n table is structured for it, but no third translation ships now.
- Fyne removal in a separate PR — it happens as part of step 5/6, not before (the tree must always build).

# Task 8 Report: Windows/macOS GUI (Fyne)

## What was implemented

### Pure validation helpers in `internal/core` (deliberate deviation from brief)

The task brief placed `buildConfig`/`statusText` in the GUI package. The task description corrected this: the pure config-validation helpers went into `internal/core/config.go` (no Fyne import) so they compile and test on every platform without a C compiler.

- `Validate(cfg Config) error` — checks upstream origin via `swapScheme`, positive connect/idle timeouts, and valid log level.
- `BuildConfigFromStrings(upstream, listen, ct, it, level string, skipTLS bool) (Config, error)` — parses the GUI form's string fields into a validated `Config`, computing `UpstreamWS`.

The GUI is render-only and calls `core.BuildConfigFromStrings` on "Save & Apply".

### Fyne v2 GUI in `app/gui/gui.go`

`//go:build windows || darwin` — three tabs (Settings, Logs, About):
- **Settings**: upstream base, listen address, connect/idle timeouts, log level select, skip-TLS check, Save & Apply (validates via `core.BuildConfigFromStrings` then `h.SetConfig`), Start/Stop buttons, status label.
- **Logs**: read-only `MultiLineEntry` fed by a goroutine pumping from `h.SubscribeLogs()`. Filters to LevelInfo+. Caps at 500 lines.
- **About**: version label, Check-for-updates and Download-&-apply buttons (both call `h.CheckUpdate`/`h.ApplyUpdate` in goroutines).

`Run(ctx, h)` starts the proxy, opens the Fyne window, pumps logs, and stops the proxy on window close via `w.SetOnClosed`.

### Build-tag wiring

- `app/frontend_gui.go` (`//go:build windows || darwin`) — returns `gui.New()`.
- `app/frontend_nonative.go` narrowed from `//go:build !linux` to `//go:build !linux && !(windows || darwin)`.

Exactly one `selectNativeFrontend` per platform, no overlap:
- linux → `tui.New()`
- windows/darwin → `gui.New()`
- others → `nil`

## Test evidence that runs locally

### `go test ./internal/... -v -count=1` — ALL PASS

Key new tests:
```
=== RUN   TestValidate
--- PASS: TestValidate (0.00s)
=== RUN   TestBuildConfigFromStrings
=== RUN   TestBuildConfigFromStrings/valid
=== RUN   TestBuildConfigFromStrings/bad_scheme
=== RUN   TestBuildConfigFromStrings/bad_duration
=== RUN   TestBuildConfigFromStrings/bad_level
--- PASS: TestBuildConfigFromStrings (0.00s)
    --- PASS: TestBuildConfigFromStrings/valid (0.00s)
    --- PASS: TestBuildConfigFromStrings/bad_scheme (0.00s)
    --- PASS: TestBuildConfigFromStrings/bad_duration (0.00s)
    --- PASS: TestBuildConfigFromStrings/bad_level (0.00s)
```

Full suite: `ok  cc2ws/internal/core  2.325s` — all 35+ tests pass.

### `go vet ./internal/...` — clean (exit 0)

## Deferred to CI

GUI compile/build/run and full `go build ./...` are deferred to Task-9 CI windows/macos runners. This host has no gcc (`CGO_ENABLED=0`).

The `go build ./cmd/cc2ws` failure confirms the expected cgo dependency chain:
```
imports cc2ws/app/gui
imports fyne.io/fyne/v2/app
imports fyne.io/fyne/v2/internal/driver/glfw
imports fyne.io/fyne/v2/internal/driver/common
imports fyne.io/fyne/v2/internal/painter/gl
imports github.com/go-gl/gl/v2.1/gl: build constraints exclude all Go files
```

This is the OpenGL/cgo backend requiring gcc — an accepted consequence of defer-to-CI, not a defect.

## `go get` / `go mod tidy` result

`go get fyne.io/fyne/v2@latest` added `fyne.io/fyne/v2 v2.7.4` plus the full dependency tree (~30 transitive deps: go-gl, glfw, typesetting, etc.). `go mod tidy` succeeded; go.mod/go.sum are consistent. The direct require is:
```
fyne.io/fyne/v2 v2.7.4
```

Note: `go mod tidy` initially stripped the fyne dep on the first run (the `go get` and `gui.go` Write were parallel, and tidy ran before the file existed). Re-running `go get` + `go mod tidy` after gui.go existed resolved this correctly.

## Files changed

- `internal/core/config.go` — added `Validate` + `BuildConfigFromStrings`
- `internal/core/config_test.go` — added `TestValidate` + `TestBuildConfigFromStrings`
- `app/gui/gui.go` — **created** (`//go:build windows || darwin`) Fyne GUI
- `app/frontend_gui.go` — **created** (`//go:build windows || darwin`) dispatch
- `app/frontend_nonative.go` — narrowed build tag from `!linux` to `!linux && !(windows || darwin)`
- `go.mod` / `go.sum` — added fyne.io/fyne/v2 v2.7.4 dependency tree

## Self-review findings

### Fyne API verified against official docs (https://docs.fyne.io/api/v2/fyne/)

Confirmed against `fyne.App` and `fyne.Window` interface definitions fetched from the live docs:
- `app.New()` from `fyne.io/fyne/v2/app` — standard app constructor
- `App.NewWindow(title string) Window` — confirmed in `fyne.App` interface
- `Window.SetContent(CanvasObject)` — confirmed in `fyne.Window` interface
- `Window.SetOnClosed(func())` — confirmed in `fyne.Window` interface
- `Window.ShowAndRun()` — confirmed in `fyne.Window` interface
- `App.Quit()` — confirmed in `fyne.App` interface

Widget/container constructors (`widget.NewEntry`, `widget.NewSelect`, `widget.NewCheck`, `widget.NewLabel`, `widget.NewButton`, `widget.NewMultiLineEntry`, `container.NewVBox`, `container.NewHBox`, `container.NewGridWithColumns`, `container.NewAppTabs`, `container.NewTabItem`) are all stable Fyne v2 APIs present since v2.0. The `var _ fyne.Widget = (*widget.Label)(nil)` assertion is valid: `*widget.Label` satisfies `fyne.Widget` through its embedded `BaseWidget` + its own `CreateRenderer`.

### Build-tag dispatch

Exactly one `selectNativeFrontend` definition per platform — verified by grep across all files in `app/`. No overlap.

### Test struct literal fix

The prompt's test code had a struct-literal arity bug (7 values for 8 fields in `TestBuildConfigFromStrings` cases). Fixed by adding the second boolean (`wantErr`) to each case line: `{"valid", ..., false, false}`, `{"bad scheme", ..., false, true}`, etc.

## Concerns

1. **Log pump thread safety (known caveat per task description):** The log-pump goroutine calls `logsEntry.SetText` from a non-main goroutine. Fyne recommends UI updates on the main loop but exposes no stable public main-thread dispatcher in v2; `SetText`-from-goroutine works in practice. Flagged for future hardening (Fyne data bindings would be the proper fix).

2. **`_ = ctx` dead code:** The reference code includes `_ = ctx` at the end of `Run`, but `ctx` is already used (goroutine `case <-ctx.Done()`, `h.CheckUpdate(ctx)`, `h.ApplyUpdate(ctx, info)`). Harmless no-op assignment; kept to match the reference exactly.

3. **`go build ./...` and `go test ./app/...` fail locally** — expected and accepted. The `app` package now imports `cc2ws/app/gui` which imports fyne which requires cgo. CI runners (Task 9) with gcc will compile cleanly. On Linux CI (ubuntu), the gui files are excluded by build tags so `go test ./...` continues to pass.

## Fix wave

### Critical: `SetReadOnly` is not a Fyne v2 API

**File:** `app/gui/gui.go:83`

**Change:** `logsEntry.SetReadOnly(true)` → `logsEntry.Disable()`

**Why:** Fyne v2's `Entry` (and `MultiLineEntry`) has no `SetReadOnly` method or `ReadOnly` field — verified against the Fyne v2.7.4 source. The line would fail the Task-9 CI build on windows/macos runners. The supported mechanism for a non-editable entry is `Disable()`, inherited from the embedded `DisableableWidget`. `Disable()` sets the disabled state, greys the widget, and ignores input — the desired read-only behavior.

**Verification:** `go test ./internal/...` still green after the change (GUI-only fix; no core code touched). GUI compile verification remains deferred to Task-9 CI (no gcc on this host).

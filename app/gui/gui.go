//go:build windows || darwin

package gui

import (
	"context"
	"fmt"
	"sync"

	"gioui.org/app"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"cc2ws/internal/autostart"
	"cc2ws/internal/core"
	"cc2ws/internal/i18n"
)

// GuiFrontend is the app.Frontend for windows/darwin.
type GuiFrontend struct{}

func New() *GuiFrontend { return &GuiFrontend{} }

type uiState struct {
	mu        sync.Mutex
	logs      []core.LogEntry
	status    string
	updateMsg string
}

func (u *uiState) setStatus(s string)  { u.mu.Lock(); u.status = s; u.mu.Unlock() }
func (u *uiState) setUpdate(s string)   { u.mu.Lock(); u.updateMsg = s; u.mu.Unlock() }
func (u *uiState) Status() string       { u.mu.Lock(); defer u.mu.Unlock(); return u.status }
func (u *uiState) UpdateMsg() string    { u.mu.Lock(); defer u.mu.Unlock(); return u.updateMsg }
func (u *uiState) Logs() []core.LogEntry {
	u.mu.Lock()
	defer u.mu.Unlock()
	out := make([]core.LogEntry, len(u.logs))
	copy(out, u.logs)
	return out
}
func (u *uiState) appendLog(e core.LogEntry) {
	u.mu.Lock()
	u.logs = append(u.logs, e)
	if len(u.logs) > 500 {
		u.logs = u.logs[len(u.logs)-500:]
	}
	u.mu.Unlock()
}
func (u *uiState) clearLogs() { u.mu.Lock(); u.logs = nil; u.mu.Unlock() }

func (g *GuiFrontend) Run(ctx context.Context, h *core.Handle) error {
	i18n.SetLang(i18n.Lang(h.Config().Language))
	_ = autostart.EnableIfWanted(h.Config().AutoStart)

	w := new(app.Window)
	w.Option(app.Title("cc2ws " + core.Version))
	th := loadTheme()
	st := &uiState{}

	logCh, unsub := h.SubscribeLogs()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case e, ok := <-logCh:
				if !ok {
					return
				}
				if e.Level < core.LevelInfo {
					continue
				}
				st.appendLog(e)
				w.Invalidate()
			}
		}
	}()

	if err := h.Start(); err != nil {
		st.setStatus(i18n.T("error_prefix") + ": " + err.Error())
	} else {
		st.setStatus(i18n.T("running"))
	}

	p := newPages(th, h, st, w)

	// On macOS the main thread must own the event loop (app.Main). Run the
	// Gio loop in a goroutine and block here on app.Main on the caller's
	// goroutine (which is the main goroutine under cmd/cc2ws).
	errCh := make(chan error, 1)
	go func() {
		var ops op.Ops
		for {
			e := w.Event()
			select {
			case <-ctx.Done():
				unsub()
				_ = h.Stop()
				errCh <- nil
				return
			default:
			}
			switch e := e.(type) {
			case app.FrameEvent:
				gtx := app.NewContext(&ops, e)
				p.layout(gtx)
				e.Frame(gtx.Ops)
			case app.DestroyEvent:
				unsub()
				_ = h.Stop()
				errCh <- e.Err
				return
			}
		}
	}()
	app.Main()
	return <-errCh
}

// pages holds all widget state and dispatches to the active page.
type pages struct {
	th   *material.Theme
	h    *core.Handle
	st   *uiState
	w    *app.Window
	host tabKind

	nav [3]widget.Clickable

	upstream widget.Editor
	listen   widget.Editor
	ct       widget.Editor
	it       widget.Editor
	level    widget.Enum
	skipTLS  widget.Bool
	autost   widget.Bool
	lang     widget.Enum
	save     widget.Clickable
	revert   widget.Clickable
	start    widget.Clickable
	stop     widget.Clickable

	logList widget.List
	clear   widget.Clickable

	check widget.Clickable
	apply widget.Clickable
}

type tabKind int

const (
	tabSettings tabKind = iota
	tabLogs
	tabAbout
)

func newPages(th *material.Theme, h *core.Handle, st *uiState, w *app.Window) *pages {
	p := &pages{th: th, h: h, st: st, w: w}
	cfg := h.Config()
	p.upstream.SingleLine = true
	p.upstream.SetText(cfg.UpstreamBase)
	p.listen.SingleLine = true
	p.listen.SetText(cfg.Listen)
	p.ct.SingleLine = true
	p.ct.SetText(cfg.ConnectTimeout.String())
	p.it.SingleLine = true
	p.it.SetText(cfg.IdleTimeout.String())
	p.level.Value = cfg.LogLevel
	p.skipTLS.Value = cfg.InsecureSkipTLSVerify
	p.autost.Value = autostart.IsEnabled()
	p.lang.Value = cfg.Language
	p.logList.Axis = layout.Vertical
	return p
}

func (p *pages) layout(gtx layout.Context) layout.Dimensions {
	// Nav rail across the top.
	for i := range p.nav {
		if p.nav[i].Clicked(gtx) {
			p.host = tabKind(i)
		}
	}
	navBar := func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal, Spacing: layout.SpaceStart}.Layout(gtx,
			p.navTab(0, i18n.T("settings")),
			p.navTab(1, i18n.T("logs")),
			p.navTab(2, i18n.T("about")),
		)
	}
	// Status header.
	running := p.h.Running()
	statusText := p.st.Status()
	if running && statusText == "" {
		statusText = i18n.T("running")
	}
	header := func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal, Spacing: layout.SpaceBetween}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return dot(gtx, statusColor(running, statusText))
						})
					}),
					layout.Rigid(material.H6(p.th, "cc2ws "+core.Version).Layout),
				)
			}),
			layout.Rigid(material.Body1(p.th, statusText).Layout),
		)
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(header),
		layout.Rigid(navBar),
		layout.Flexed(1, p.layoutActive),
	)
}

func (p *pages) navTab(i int, label string) layout.FlexChild {
	return layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		btn := material.Button(p.th, &p.nav[i], label)
		if p.host == tabKind(i) {
			btn.Background = p.th.Palette.ContrastBg
		} else {
			btn.Background = p.th.Palette.Bg
		}
		return btn.Layout(gtx)
	})
}

func (p *pages) layoutActive(gtx layout.Context) layout.Dimensions {
	switch p.host {
	case tabLogs:
		return p.layoutLogs(gtx)
	case tabAbout:
		return p.layoutAbout(gtx)
	default:
		return p.layoutSettings(gtx)
	}
}

func (p *pages) layoutSettings(gtx layout.Context) layout.Dimensions {
	// Handle button clicks first.
	if p.save.Clicked(gtx) {
		p.applySave()
	}
	if p.revert.Clicked(gtx) {
		p.revertForm()
	}
	if p.start.Clicked(gtx) {
		if err := p.h.Start(); err != nil {
			p.st.setStatus(i18n.T("error_prefix") + ": " + err.Error())
		} else {
			p.st.setStatus(i18n.T("running"))
		}
	}
	if p.stop.Clicked(gtx) {
		_ = p.h.Stop()
		p.st.setStatus(i18n.T("stopped"))
	}
	if p.lang.Update(gtx) {
		i18n.SetLang(i18n.Lang(p.lang.Value))
		p.w.Invalidate()
	}
	if p.autost.Update(gtx) {
		if err := autostart.EnableIfWanted(p.autost.Value); err != nil {
			p.st.setStatus(i18n.T("error_prefix") + ": " + err.Error())
		}
	}

	status := p.st.Status()
	if p.h.Running() && (status == "" || status == i18n.T("running") || status == i18n.T("stopped")) {
		// keep as-is
	}

	insetLabel := func(text string) layout.FlexChild {
		return layout.Rigid(material.Body1(p.th, text).Layout)
	}
	field := func(e *widget.Editor, hint string) layout.FlexChild {
		return layout.Rigid(material.Editor(p.th, e, hint).Layout)
	}

	form := func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			insetLabel(i18n.T("upstream_base")), field(&p.upstream, "wss://hub.example.com"),
			insetLabel(i18n.T("listen_addr")), field(&p.listen, "127.0.0.1:18080"),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Spacing: layout.SpaceStart}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							insetLabel(i18n.T("connect_timeout")), field(&p.ct, "10s"))
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(16)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							insetLabel(i18n.T("idle_timeout")), field(&p.it, "600s"))
					}),
				)
			}),
			insetLabel(i18n.T("log_level")),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
					layout.Rigid(material.RadioButton(p.th, &p.level, "debug", "debug").Layout),
					layout.Rigid(material.RadioButton(p.th, &p.level, "info", "info").Layout),
					layout.Rigid(material.RadioButton(p.th, &p.level, "warn", "warn").Layout),
					layout.Rigid(material.RadioButton(p.th, &p.level, "error", "error").Layout),
				)
			}),
			layout.Rigid(material.CheckBox(p.th, &p.skipTLS, i18n.T("skip_tls")).Layout),
			layout.Rigid(material.CheckBox(p.th, &p.autost, i18n.T("autostart")).Layout),
			insetLabel(i18n.T("language")),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
					layout.Rigid(material.RadioButton(p.th, &p.lang, "zh", "中文").Layout),
					layout.Rigid(material.RadioButton(p.th, &p.lang, "en", "English").Layout),
				)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
					layout.Rigid(material.Button(p.th, &p.revert, i18n.T("revert")).Layout),
					layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
					layout.Rigid(material.Button(p.th, &p.save, i18n.T("save_apply")).Layout),
				)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
					layout.Rigid(material.Button(p.th, &p.start, i18n.T("start")).Layout),
					layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
					layout.Rigid(material.Button(p.th, &p.stop, i18n.T("stop")).Layout),
					layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
					layout.Rigid(material.Body1(p.th, status).Layout),
				)
			}),
		)
	}
	return layout.UniformInset(unit.Dp(12)).Layout(gtx, form)
}

func (p *pages) applySave() {
	cfg, err := core.BuildConfigFromStrings(
		p.upstream.Text(), p.listen.Text(), p.ct.Text(), p.it.Text(),
		p.level.Value, p.lang.Value, p.skipTLS.Value, p.autost.Value,
	)
	if err != nil {
		p.st.setStatus(i18n.T("invalid") + ": " + err.Error())
		return
	}
	if err := p.h.SetConfig(cfg); err != nil {
		p.st.setStatus(i18n.T("error_prefix") + ": " + err.Error())
		return
	}
	p.upstream.SetText(cfg.UpstreamBase)
	i18n.SetLang(i18n.Lang(cfg.Language))
	if p.h.Running() {
		p.st.setStatus(i18n.T("running"))
	} else {
		p.st.setStatus(i18n.T("stopped"))
	}
}

func (p *pages) revertForm() {
	cfg := p.h.Config()
	p.upstream.SetText(cfg.UpstreamBase)
	p.listen.SetText(cfg.Listen)
	p.ct.SetText(cfg.ConnectTimeout.String())
	p.it.SetText(cfg.IdleTimeout.String())
	p.level.Value = cfg.LogLevel
	p.skipTLS.Value = cfg.InsecureSkipTLSVerify
	p.autost.Value = autostart.IsEnabled()
	p.lang.Value = cfg.Language
}

func (p *pages) layoutLogs(gtx layout.Context) layout.Dimensions {
	if p.clear.Clicked(gtx) {
		p.st.clearLogs()
	}
	header := func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
			layout.Rigid(material.H6(p.th, i18n.T("conn_logs")).Layout),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{} }),
			layout.Rigid(material.Button(p.th, &p.clear, i18n.T("clear")).Layout),
		)
	}
	logs := p.st.Logs()
	list := material.List(p.th, &p.logList)
	return layout.UniformInset(unit.Dp(12)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(header),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return list.Layout(gtx, len(logs), func(gtx layout.Context, idx int) layout.Dimensions {
					e := logs[idx]
					return material.Body2(p.th, fmt.Sprintf("%s %s", e.Time.Format("15:04:05"), e.Msg)).Layout(gtx)
				})
			}),
		)
	})
}

func (p *pages) layoutAbout(gtx layout.Context) layout.Dimensions {
	if p.check.Clicked(gtx) {
		go func() {
			info, err := p.h.CheckUpdate(context.Background())
			if err != nil {
				p.st.setUpdate(i18n.T("error_prefix") + ": " + err.Error())
			} else if info.Version == "" {
				p.st.setUpdate(i18n.T("no_update"))
			} else {
				p.st.setUpdate(i18n.T("update_available") + ": " + info.Version)
			}
			p.w.Invalidate()
		}()
	}
	if p.apply.Clicked(gtx) {
		go func() {
			info, err := p.h.CheckUpdate(context.Background())
			if err != nil {
				p.st.setUpdate(i18n.T("update_failed") + ": " + err.Error())
				p.w.Invalidate()
				return
			}
			if err := p.h.ApplyUpdate(context.Background(), info); err != nil {
				p.st.setUpdate(i18n.T("update_failed") + ": " + err.Error())
			} else {
				p.st.setUpdate(i18n.T("updated_restart"))
			}
			p.w.Invalidate()
		}()
	}
	return layout.UniformInset(unit.Dp(12)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(material.H6(p.th, "cc2ws "+core.Version).Layout),
			layout.Rigid(material.Body1(p.th, i18n.T("current_version")+": "+core.Version).Layout),
			layout.Rigid(material.Body1(p.th, p.st.UpdateMsg()).Layout),
			layout.Rigid(material.Button(p.th, &p.check, i18n.T("check_update")).Layout),
			layout.Rigid(material.Button(p.th, &p.apply, i18n.T("download_apply")).Layout),
		)
	})
}

// Ensure material is referenced for future edits.
var _ = material.NewTheme

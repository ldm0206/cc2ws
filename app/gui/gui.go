//go:build windows || darwin

package gui

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"sync"

	"gioui.org/app"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
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
	th := newTheme()
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
				// Gio windows are transparent until painted; on Windows an
				// unpainted window reads as solid white. Fill the whole frame
				// with the theme background before any widgets draw.
				paint.Fill(gtx.Ops, p.th.Palette.Bg)
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
	skin skin

	nav [3]widget.Clickable

	upstream  widget.Editor
	listen    widget.Editor
	ct        widget.Editor
	it        widget.Editor
	level     widget.Enum
	skipTLS   widget.Bool
	autost    widget.Bool
	lang      widget.Enum
	themeMode widget.Enum
	save      widget.Clickable
	revert    widget.Clickable
	start     widget.Clickable
	stop      widget.Clickable

	logList     widget.List
	settingsList widget.List
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
	p.themeMode.Value = cfg.ThemeMode
	p.skin = skinFor(cfg.ThemeMode)
	applySkin(p.th, p.skin)
	p.logList.Axis = layout.Vertical
	return p
}

func (p *pages) layout(gtx layout.Context) layout.Dimensions {
	for i := range p.nav {
		if p.nav[i].Clicked(gtx) {
			p.host = tabKind(i)
		}
	}
	running := p.h.Running()
	statusText := p.st.Status()
	if running && (statusText == "" || statusText == i18n.T("running")) {
		statusText = i18n.T("running")
	}

	// Left vertical nav rail: brand + status + nav items, full height.
	navRail := func(gtx layout.Context) layout.Dimensions {
		return layout.Stack{Alignment: layout.NW}.Layout(gtx,
			layout.Expanded(func(gtx layout.Context) layout.Dimensions {
				paint.FillShape(gtx.Ops, p.skin.card,
					clip.UniformRRect(image.Rectangle{Max: gtx.Constraints.Min}, 0).Op(gtx.Ops))
				return layout.Dimensions{Size: gtx.Constraints.Min}
			}),
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				return layout.UniformInset(unit.Dp(16)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										return dot(gtx, statusColor(p.skin, running, statusText))
									})
								}),
								layout.Rigid(material.H6(p.th, "cc2ws").Layout),
							)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							l := material.Label(p.th, p.th.TextSize, core.Version)
							l.Color = p.skin.subtle
							return layout.Inset{Top: unit.Dp(2)}.Layout(gtx, l.Layout)
						}),
						vSpace(28),
						p.navItem(0, i18n.T("settings")),
						vSpace(6),
						p.navItem(1, i18n.T("logs")),
						vSpace(6),
						p.navItem(2, i18n.T("about")),
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{} }),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							l := material.Label(p.th, p.th.TextSize, statusText)
							l.Color = statusColor(p.skin, running, statusText)
							return l.Layout(gtx)
						}),
					)
				})
			}),
		)
	}

	return layout.Inset{Top: unit.Dp(10), Bottom: unit.Dp(10), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Max.X = gtx.Dp(180)
					gtx.Constraints.Min.X = gtx.Constraints.Max.X
					return navRail(gtx)
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(10)}.Layout),
				layout.Flexed(1, p.layoutActive),
			)
		})
}

// navItem is a left-aligned pill in the rail; active gets the accent fill.
func (p *pages) navItem(i int, label string) layout.FlexChild {
	return layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		active := p.host == tabKind(i)
		b := material.Button(p.th, &p.nav[i], label)
		b.CornerRadius = 10
		b.Inset = layout.Inset{Top: unit.Dp(10), Bottom: unit.Dp(10), Left: unit.Dp(14), Right: unit.Dp(40)}
		if active {
			b.Background = p.th.Palette.ContrastBg
			b.Color = p.th.Palette.ContrastFg
		} else {
			b.Background = color.NRGBA{} // transparent
			b.Color = p.th.Palette.Fg
		}
		return b.Layout(gtx)
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

func vSpace(dp int) layout.FlexChild {
	return layout.Rigid(layout.Spacer{Height: unit.Dp(dp)}.Layout)
}

func (p *pages) layoutSettings(gtx layout.Context) layout.Dimensions {
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
	if p.themeMode.Update(gtx) {
		p.skin = skinFor(p.themeMode.Value)
		applySkin(p.th, p.skin)
		p.w.Invalidate()
	}
	if p.autost.Update(gtx) {
		if err := autostart.EnableIfWanted(p.autost.Value); err != nil {
			p.st.setStatus(i18n.T("error_prefix") + ": " + err.Error())
		}
	}

	status := p.st.Status()

	editor := func(e *widget.Editor, hint string) layout.Widget {
		return func(gtx layout.Context) layout.Dimensions {
			ed := material.Editor(p.th, e, hint)
			ed.Color = p.th.Palette.Fg
			ed.HintColor = p.skin.hint
			border := widget.Border{
				Color:        p.skin.outline,
				CornerRadius: 10,
				Width:        1,
			}
			return border.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Stack{Alignment: layout.W}.Layout(gtx,
					layout.Expanded(func(gtx layout.Context) layout.Dimensions {
						paint.FillShape(gtx.Ops, p.skin.input,
							clip.UniformRRect(image.Rectangle{Max: gtx.Constraints.Min}, 10).Op(gtx.Ops))
						return layout.Dimensions{Size: gtx.Constraints.Min}
					}),
					layout.Stacked(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Top: unit.Dp(9), Bottom: unit.Dp(9), Left: unit.Dp(14), Right: unit.Dp(14)}.Layout(gtx, ed.Layout)
					}),
				)
			})
		}
	}

	// field: label on top, input fills the row beneath it.
	field := func(label string, w layout.Widget) layout.Widget {
		return func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					l := material.Label(p.th, p.th.TextSize, label)
					l.Color = p.skin.subtle
					return layout.Inset{Bottom: unit.Dp(6)}.Layout(gtx, l.Layout)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Min.X = gtx.Dp(260)
					return w(gtx)
				}),
			)
		}
	}

	// A two-up row of fields (used for connect/idle timeouts).
	twoUp := func(a, b layout.Widget) layout.Widget {
		return func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
				layout.Flexed(1, a),
				layout.Rigid(layout.Spacer{Width: unit.Dp(16)}.Layout),
				layout.Flexed(1, b),
			)
		}
	}

	chips := func(group *widget.Enum, opts [][2]string) layout.Widget {
		return func(gtx layout.Context) layout.Dimensions {
			children := []layout.FlexChild{}
			for i, o := range opts {
				if i > 0 {
					children = append(children, layout.Rigid(layout.Spacer{Width: unit.Dp(10)}.Layout))
				}
				oo := o
				children = append(children, layout.Rigid(material.RadioButton(p.th, group, oo[0], oo[1]).Layout))
			}
			return layout.Flex{Axis: layout.Horizontal}.Layout(gtx, children...)
		}
	}

	// sectionCard: a titled raised panel.
	sectionCard := func(title string, body layout.Widget) layout.Widget {
		return func(gtx layout.Context) layout.Dimensions {
			return p.surface(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.UniformInset(unit.Dp(18)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							l := material.H6(p.th, title)
							return layout.Inset{Bottom: unit.Dp(14)}.Layout(gtx, l.Layout)
						}),
						layout.Rigid(body),
					)
				})
			})
		}
	}

	connBody := func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(field(i18n.T("upstream_base"), editor(&p.upstream, "wss://hub.example.com"))),
			vSpace(14),
			layout.Rigid(field(i18n.T("listen_addr"), editor(&p.listen, "127.0.0.1:18080"))),
			vSpace(14),
			layout.Rigid(twoUp(
				field(i18n.T("connect_timeout"), editor(&p.ct, "10s")),
				field(i18n.T("idle_timeout"), editor(&p.it, "600s")),
			)),
			vSpace(14),
			layout.Rigid(field(i18n.T("log_level"), chips(&p.level, [][2]string{
				{"debug", "debug"}, {"info", "info"}, {"warn", "warn"}, {"error", "error"},
			}))),
			vSpace(10),
			layout.Rigid(material.CheckBox(p.th, &p.skipTLS, i18n.T("skip_tls")).Layout),
		)
	}

	appearanceBody := func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(field(i18n.T("theme_mode"), chips(&p.themeMode, [][2]string{
				{"dark", i18n.T("theme_dark")}, {"light", i18n.T("theme_light")},
			}))),
			vSpace(14),
			layout.Rigid(field(i18n.T("language"), chips(&p.lang, [][2]string{
				{"zh", "中文"}, {"en", "English"},
			}))),
		)
	}

	launchBody := func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(material.CheckBox(p.th, &p.autost, i18n.T("autostart")).Layout),
		)
	}

	scroll := material.List(p.th, &p.settingsList)
	scroll.AnchorStrategy = material.Occupy
	return scroll.Layout(gtx, 1, func(gtx layout.Context, _ int) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(sectionCard(i18n.T("conn_section"), connBody)),
			vSpace(14),
			layout.Rigid(sectionCard(i18n.T("appearance_section"), appearanceBody)),
			vSpace(14),
			layout.Rigid(sectionCard(i18n.T("launch_section"), launchBody)),
			vSpace(18),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
					layout.Rigid(material.Button(p.th, &p.start, i18n.T("start")).Layout),
					layout.Rigid(layout.Spacer{Width: unit.Dp(10)}.Layout),
					layout.Rigid(material.Button(p.th, &p.stop, i18n.T("stop")).Layout),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{} }),
					layout.Rigid(material.Button(p.th, &p.revert, i18n.T("revert")).Layout),
					layout.Rigid(layout.Spacer{Width: unit.Dp(10)}.Layout),
					layout.Rigid(material.Button(p.th, &p.save, i18n.T("save_apply")).Layout),
				)
			}),
			vSpace(10),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if status == "" {
					return layout.Dimensions{}
				}
				l := material.Body2(p.th, status)
				if isLikelyError(status) {
					l.Color = p.th.Palette.ContrastBg
				}
				return l.Layout(gtx)
			}),
		)
	})
}

// isLikelyError is a cheap heuristic: messages we generate for failures start
// with the localized "Error"/"Invalid" prefix. Used only to tint the line.
func isLikelyError(s string) bool {
	return len(s) > 0 && (startsWith(s, i18n.T("error_prefix")) || startsWith(s, i18n.T("invalid")))
}

func startsWith(s, prefix string) bool {
	if len(prefix) > len(s) {
		return false
	}
	return s[:len(prefix)] == prefix
}

func (p *pages) applySave() {
	cfg, err := core.BuildConfigFromStrings(
		p.upstream.Text(), p.listen.Text(), p.ct.Text(), p.it.Text(),
		p.level.Value, p.lang.Value, p.themeMode.Value, p.skipTLS.Value, p.autost.Value,
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
	p.skin = skinFor(cfg.ThemeMode)
	applySkin(p.th, p.skin)
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
	p.themeMode.Value = cfg.ThemeMode
	p.skin = skinFor(cfg.ThemeMode)
	applySkin(p.th, p.skin)
}

func (p *pages) layoutLogs(gtx layout.Context) layout.Dimensions {
	if p.clear.Clicked(gtx) {
		p.st.clearLogs()
	}
	logs := p.st.Logs()
	list := material.List(p.th, &p.logList)
	body := func(gtx layout.Context) layout.Dimensions {
		return layout.UniformInset(unit.Dp(18)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
						layout.Rigid(material.H6(p.th, i18n.T("conn_logs")).Layout),
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{} }),
						layout.Rigid(material.Button(p.th, &p.clear, i18n.T("clear")).Layout),
					)
				}),
				vSpace(12),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					if len(logs) == 0 {
						return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							l := material.Body2(p.th, "—")
							l.Color = color.NRGBA{R: 120, G: 124, B: 130, A: 255}
							return l.Layout(gtx)
						})
					}
					return list.Layout(gtx, len(logs), func(gtx layout.Context, idx int) layout.Dimensions {
						return layout.UniformInset(unit.Dp(4)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							e := logs[idx]
							return material.Body2(p.th, fmt.Sprintf("%s  %s", e.Time.Format("15:04:05"), e.Msg)).Layout(gtx)
						})
					})
				}),
			)
		})
	}
	return layout.UniformInset(unit.Dp(16)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return p.surface(gtx, body)
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
	body := func(gtx layout.Context) layout.Dimensions {
		return layout.UniformInset(unit.Dp(28)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(material.H4(p.th, "cc2ws").Layout),
					vSpace(4),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						l := material.Body1(p.th, core.Version)
						l.Color = color.NRGBA{R: 150, G: 155, B: 165, A: 255}
						return l.Layout(gtx)
					}),
					vSpace(18),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						msg := p.st.UpdateMsg()
						if msg == "" {
							return layout.Dimensions{}
						}
						return material.Body1(p.th, msg).Layout(gtx)
					}),
					vSpace(18),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
							layout.Rigid(material.Button(p.th, &p.check, i18n.T("check_update")).Layout),
							layout.Rigid(layout.Spacer{Width: unit.Dp(12)}.Layout),
							layout.Rigid(material.Button(p.th, &p.apply, i18n.T("download_apply")).Layout),
						)
					}),
				)
			})
		})
	}
	return layout.UniformInset(unit.Dp(16)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return p.surface(gtx, body)
	})
}

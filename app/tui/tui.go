//go:build linux

package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"cc2ws/internal/core"
	"cc2ws/internal/i18n"
)

// TuiFrontend is the app.Frontend for linux.
type TuiFrontend struct{}

func New() *TuiFrontend { return &TuiFrontend{} }

// Run starts the proxy, launches the bubbletea program, pumps log entries
// into the model, and stops the proxy on exit.
func (f *TuiFrontend) Run(ctx context.Context, h *core.Handle) error {
	if err := h.Start(); err != nil {
		return err
	}
	defer h.Stop()
	logCh, unsub := h.SubscribeLogs()
	defer unsub()
	m := newModel(h.Config(), h)
	p := tea.NewProgram(m, tea.WithContext(ctx))
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case e, ok := <-logCh:
				if !ok {
					return
				}
				p.Send(e)
			}
		}
	}()
	_, err := p.Run()
	return err
}

type tab int

const (
	tabSettings tab = iota
	tabLogs
	tabAbout
)

type model struct {
	h        *core.Handle
	cfg      core.Config
	tab      tab
	logs     []core.LogEntry
	levelMin core.Level
	width    int
	height   int
}

func newModel(cfg core.Config, h *core.Handle) model {
	return model{cfg: cfg, h: h, levelMin: core.LevelInfo}
}

func (m model) View() string {
	switch m.tab {
	case tabLogs:
		return m.viewLogs()
	case tabAbout:
		return m.viewAbout()
	default:
		return m.viewSettings()
	}
}

func (m model) viewSettings() string {
	return fmt.Sprintf(
		"cc2ws %s\n\n"+
			"%s\n"+
			"  %s : %s\n"+
			"  %s : %s\n"+
			"  %s / %s   %s: %s\n"+
			"  %s: %v\n\n"+
			"[1]%s [2]%s [3]%s  [s]%s/[x]%s  [q]Quit",
		core.Version,
		i18n.T("settings"),
		i18n.T("upstream_base"), m.cfg.UpstreamBase,
		i18n.T("listen_addr"), m.cfg.Listen,
		fmt.Sprintf("%ds", int64(m.cfg.ConnectTimeout/time.Second)),
		fmt.Sprintf("%ds", int64(m.cfg.IdleTimeout/time.Second)),
		i18n.T("log_level"), m.cfg.LogLevel,
		i18n.T("skip_tls"), m.cfg.InsecureSkipTLSVerify,
		i18n.T("settings"), i18n.T("logs"), i18n.T("about"),
		i18n.T("start"), i18n.T("stop"))
}

func (m model) viewLogs() string {
	var b strings.Builder
	b.WriteString(i18n.T("conn_logs") + "\n\n")
	for _, e := range m.logs {
		if e.Level < m.levelMin {
			continue
		}
		fmt.Fprintf(&b, "%s %s\n", e.Time.Format("15:04:05"), e.Msg)
	}
	b.WriteString("\n[f]" + i18n.T("filter") + "  [c]" + i18n.T("clear") + "  [1]" + i18n.T("settings") + " [2]" + i18n.T("logs") + " [3]" + i18n.T("about") + " [q]Quit")
	return b.String()
}

func (m model) viewAbout() string {
	return fmt.Sprintf("cc2ws %s\n\n%s\n  %s: %s\n\n[u]%s  [q]Quit",
		core.Version,
		i18n.T("about"),
		i18n.T("current_version"), core.Version,
		i18n.T("check_update"))
}

func (m model) Init() tea.Cmd { return nil }

// tabMsg / keyMsg are test seams so tests can drive Update without constructing
// real tea messages.
type tabMsg tab
type keyMsg string

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tabMsg:
		m.tab = tab(msg)
		return m, nil
	case keyMsg:
		return m.handleKey(string(msg))
	case tea.KeyMsg:
		return m.handleKey(msg.String())
	case core.LogEntry:
		m.appendLog(msg)
		return m, nil
	}
	return m, nil
}

func (m model) handleKey(k string) (model, tea.Cmd) {
	switch k {
	case "1":
		m.tab = tabSettings
	case "2":
		m.tab = tabLogs
	case "3":
		m.tab = tabAbout
	case "s":
		if m.h != nil {
			_ = m.h.Start()
		}
	case "x":
		if m.h != nil {
			_ = m.h.Stop()
		}
	case "c":
		m.logs = nil
	case "f":
		m.levelMin++
		if m.levelMin > core.LevelError {
			m.levelMin = core.LevelDebug
		}
	case "q":
		return m, tea.Quit
	}
	return m, nil
}

func (m *model) appendLog(e core.LogEntry) {
	if len(m.logs) > 2000 {
		m.logs = m.logs[1:]
	}
	m.logs = append(m.logs, e)
}

//go:build linux

package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"cc2ws/internal/core"
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
			"SETTINGS\n"+
			"  Upstream base : %s\n"+
			"  Listen        : %s\n"+
			"  Connect/idle  : %s / %s   Level: %s\n"+
			"  Skip TLS verify: %v\n\n"+
			"[1]Settings [2]Logs [3]About  [s]Start/[x]Stop\n"+
			"[e]Edit config  [u]Check update  [q]Quit",
		core.Version, m.cfg.UpstreamBase, m.cfg.Listen,
		m.cfg.ConnectTimeout, m.cfg.IdleTimeout, m.cfg.LogLevel,
		m.cfg.InsecureSkipTLSVerify)
}

func (m model) viewLogs() string {
	var b strings.Builder
	b.WriteString("CONNECTION LOGS\n\n")
	for _, e := range m.logs {
		if e.Level < m.levelMin {
			continue
		}
		fmt.Fprintf(&b, "%s %s\n", e.Time.Format("15:04:05"), e.Msg)
	}
	b.WriteString("\n[f]Filter  [c]Clear  [1]Settings [2]Logs [3]About [q]Quit")
	return b.String()
}

func (m model) viewAbout() string {
	return fmt.Sprintf("cc2ws %s\n\nABOUT\n  Current: %s\n\n[u]Check update  [q]Quit", core.Version, core.Version)
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

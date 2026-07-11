//go:build windows || darwin

package gui

import (
	"context"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"cc2ws/internal/core"
)

// GuiFrontend is the app.Frontend for windows/darwin.
type GuiFrontend struct{}

func New() *GuiFrontend { return &GuiFrontend{} }

// Run starts the proxy, opens the Fyne window, pumps log entries into the Logs
// view, and stops the proxy on window close.
func (g *GuiFrontend) Run(ctx context.Context, h *core.Handle) error {
	a := app.New()
	w := a.NewWindow("cc2ws " + core.Version)

	cfg := h.Config()
	upstream := widget.NewEntry()
	upstream.SetText(cfg.UpstreamBase)
	listen := widget.NewEntry()
	listen.SetText(cfg.Listen)
	ct := widget.NewEntry()
	ct.SetText(cfg.ConnectTimeout.String())
	it := widget.NewEntry()
	it.SetText(cfg.IdleTimeout.String())
	level := widget.NewSelect([]string{"debug", "info", "warn", "error"}, nil)
	level.SetSelected(cfg.LogLevel)
	skipTLS := widget.NewCheck("Skip upstream TLS verify (debug only)", nil)
	skipTLS.SetChecked(cfg.InsecureSkipTLSVerify)

	status := widget.NewLabel("Stopped")
	startBtn := widget.NewButton("Start", func() {
		if err := h.Start(); err != nil {
			status.SetText("Error: " + err.Error())
		} else {
			status.SetText("Running")
		}
	})
	stopBtn := widget.NewButton("Stop", func() {
		_ = h.Stop()
		status.SetText("Stopped")
	})
	saveBtn := widget.NewButton("Save & Apply", func() {
		newCfg, err := core.BuildConfigFromStrings(upstream.Text, listen.Text, ct.Text, it.Text, level.Selected, skipTLS.Checked)
		if err != nil {
			status.SetText("Invalid: " + err.Error())
			return
		}
		if err := h.SetConfig(newCfg); err != nil {
			status.SetText("Error: " + err.Error())
			return
		}
		upstream.SetText(newCfg.UpstreamBase)
		if h.Running() {
			status.SetText("Running")
		} else {
			status.SetText("Stopped")
		}
	})

	settingsTab := container.NewVBox(
		widget.NewLabel("Upstream base"), upstream,
		widget.NewLabel("Listen address"), listen,
		container.NewGridWithColumns(2,
			container.NewVBox(widget.NewLabel("Connect timeout"), ct),
			container.NewVBox(widget.NewLabel("Idle timeout"), it),
		),
		widget.NewLabel("Log level"), level,
		skipTLS,
		saveBtn,
		status,
		container.NewHBox(startBtn, stopBtn),
	)

	logsEntry := widget.NewMultiLineEntry()
	logsEntry.Disable()
	logsTab := container.NewVBox(
		widget.NewLabel("Connection logs"),
		logsEntry,
	)

	aboutLabel := widget.NewLabel("cc2ws " + core.Version)
	updateBtn := widget.NewButton("Check for updates", func() {
		go func() {
			info, err := h.CheckUpdate(ctx)
			if err != nil {
				aboutLabel.SetText("Update check: " + err.Error())
				return
			}
			aboutLabel.SetText("Update available: " + info.Version)
		}()
	})
	applyBtn := widget.NewButton("Download & apply update", func() {
		go func() {
			info, err := h.CheckUpdate(ctx)
			if err != nil {
				aboutLabel.SetText("No update: " + err.Error())
				return
			}
			if err := h.ApplyUpdate(ctx, info); err != nil {
				aboutLabel.SetText("Update failed: " + err.Error())
				return
			}
			aboutLabel.SetText("Updated — please restart")
		}()
	})
	aboutTab := container.NewVBox(aboutLabel, updateBtn, applyBtn)

	tabs := container.NewAppTabs(
		container.NewTabItem("Settings", settingsTab),
		container.NewTabItem("Logs", logsTab),
		container.NewTabItem("About", aboutTab),
	)
	w.SetContent(tabs)

	// Start the proxy + pump logs into the Logs view.
	if err := h.Start(); err != nil {
		return err
	}
	status.SetText("Running")

	var logLines []string
	logCh, unsub := h.SubscribeLogs()
	go func() {
		for {
			select {
			case <-ctx.Done():
				a.Quit()
				return
			case e, ok := <-logCh:
				if !ok {
					return
				}
				if e.Level < core.LevelInfo {
					continue
				}
				logLines = append(logLines, e.Msg)
				if len(logLines) > 500 {
					logLines = logLines[len(logLines)-500:]
				}
				logsEntry.SetText(strings.Join(logLines, "\n"))
			}
		}
	}()

	w.SetOnClosed(func() {
		unsub()
		_ = h.Stop()
	})
	w.ShowAndRun()
	_ = ctx
	return nil
}

// Ensure fyne is referenced even if future edits remove a usage.
var _ fyne.Widget = (*widget.Label)(nil)

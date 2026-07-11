// app/frontend.go
package app

import (
	"context"
	"fmt"

	"cc2ws/internal/core"
)

// Frontend is a UI surface for cc2ws. Implementations: headless (this file),
// GUI (app/gui, build-tagged windows||darwin), TUI (app/tui, build-tagged linux).
type Frontend interface {
	Run(ctx context.Context, h *core.Handle) error
}

// headlessFrontend blocks until ctx is done. The Handle's server is already
// started by the caller; this frontend simply waits for shutdown.
type headlessFrontend struct{}

func (headlessFrontend) Run(ctx context.Context, h *core.Handle) error {
	<-ctx.Done()
	return h.Stop()
}

// selectNativeFrontend is implemented in app/gui or app/tui (build-tagged).
// The fallback here returns nil so non-headless mode fails cleanly on a build
// with no frontend compiled in.
func selectNativeFrontend() Frontend {
	return nil
}

// RunFrontend dispatches to the headless or native frontend. The caller is
// responsible for starting the Handle's server before this returns for the
// headless path; the native frontend (GUI/TUI) owns its own Start/Stop so it
// can reflect server state in its UI.
func RunFrontend(ctx context.Context, h *core.Handle, headless bool) error {
	if headless {
		return headlessFrontend{}.Run(ctx, h)
	}
	f := selectNativeFrontend()
	if f == nil {
		return errNoFrontend
	}
	return f.Run(ctx, h)
}

var errNoFrontend = fmt.Errorf("no GUI/TUI frontend compiled for this build; use -headless")

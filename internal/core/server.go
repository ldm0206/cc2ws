// internal/core/server.go
package core

import (
	"context"
)

// Version is injected at build time via ldflags: -ldflags "-X cc2ws/internal/core.Version=v0.1.0".
var Version = "dev"

// Run starts the HTTP proxy server via a Handle and blocks until ctx is
// canceled. On ctx cancellation the Handle performs a 10s graceful shutdown.
// The caller owns flag parsing and Config construction.
func Run(ctx context.Context, cfg Config) error {
	h := NewHandle(cfg)
	if err := h.Start(); err != nil {
		return err
	}
	<-ctx.Done()
	return h.Stop()
}

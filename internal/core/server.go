// internal/core/server.go
package core

import (
	"context"
	"net/http"
	"time"
)

// Version is injected at build time via ldflags: -ldflags "-X cc2ws/internal/core.Version=v0.1.0".
var Version = "dev"

// Run starts the HTTP proxy server and blocks until ctx is canceled or the
// server errors. On ctx cancellation it performs a 10s graceful shutdown.
// The caller owns flag parsing and Config construction.
func Run(ctx context.Context, cfg Config) error {
	logf(LevelInfo, "cc2ws %s listening on %s, upstream %s (ws=%s)",
		Version, cfg.Listen, cfg.UpstreamBase, cfg.UpstreamWS)

	srv := &http.Server{
		Addr:         cfg.Listen,
		Handler:      withRequestLog(newRouter(cfg)),
		ReadTimeout:  60 * time.Second,
		WriteTimeout: 0,
		IdleTimeout:  120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		logf(LevelInfo, "cc2ws shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}

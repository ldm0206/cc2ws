// internal/core/handle.go
package core

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"
)

// Handle owns the HTTP server lifecycle and exposes the config + log surface
// frontends drive. All methods are safe for concurrent use.
type Handle struct {
	mu     sync.Mutex
	cfg    Config
	srv    *http.Server
	cancel context.CancelFunc // cancels the server's run goroutine
	done   chan struct{}      // closed when the server has stopped
}

// NewHandle constructs a handle around cfg. The server is NOT started.
func NewHandle(cfg Config) *Handle {
	return &Handle{cfg: cfg}
}

func (h *Handle) Config() Config {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.cfg
}

func (h *Handle) Running() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.srv != nil && h.cancel != nil
}

func (h *Handle) newServer() *http.Server {
	return &http.Server{
		Addr:         h.cfg.Listen,
		Handler:      withRequestLog(newRouter(h.cfg)),
		ReadTimeout:  60 * time.Second,
		WriteTimeout: 0,
		IdleTimeout:  120 * time.Second,
	}
}

// Start launches the HTTP server. Idempotent: returns nil if already running.
// The listener is bound synchronously so a bind failure (e.g. port in use)
// returns immediately instead of being swallowed by a goroutine race.
func (h *Handle) Start() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.srv != nil {
		return nil
	}
	// Synchronous bind: net.Listen returns the bind error immediately, or a
	// ready listener. This eliminates the race where a delayed bind error would
	// be missed by a sleep-based "assume success" wait.
	ln, err := net.Listen("tcp", h.cfg.Listen)
	if err != nil {
		return err // bind failed — synchronous, no race
	}
	srv := h.newServer()
	cancelCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer cancel()
		err := srv.Serve(ln)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		_ = err
	}()
	_ = cancelCtx // held for lifecycle; cancel fires on Serve return or Stop
	logf(LevelInfo, "cc2ws %s listening on %s, upstream %s (ws=%s)",
		Version, h.cfg.Listen, h.cfg.UpstreamBase, h.cfg.UpstreamWS)
	h.srv = srv
	h.cancel = cancel
	h.done = done
	return nil
}

// Stop performs a 10s graceful shutdown. Idempotent.
func (h *Handle) Stop() error {
	h.mu.Lock()
	srv := h.srv
	cancel := h.cancel
	done := h.done
	h.srv = nil
	h.cancel = nil
	h.done = nil
	h.mu.Unlock()
	if srv == nil {
		return nil
	}
	cancel()
	shutdownCtx, sCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer sCancel()
	logf(LevelInfo, "cc2ws shutting down")
	err := srv.Shutdown(shutdownCtx)
	<-done
	return err
}

// SetConfig validates cfg, saves it to the config file, and — if the server is
// running — hot-restarts it with the new config. On validation or save error
// the running server and current config are left untouched.
//
// UpstreamWS is always recomputed from UpstreamBase so a caller that leaves
// UpstreamWS empty or stale cannot break the proxy after a hot-restart.
func (h *Handle) SetConfig(cfg Config) error {
	// Validate: reuse swapScheme to check the upstream origin. Fix 2: also
	// store the resulting WebSocket URL onto cfg.UpstreamWS so callers that
	// pass an empty/stale UpstreamWS cannot break the proxy after hot-restart.
	ws, err := swapScheme(cfg.UpstreamBase)
	if err != nil {
		return err
	}
	cfg.UpstreamWS = ws
	if err := SaveConfig(cfg); err != nil {
		return err
	}
	h.mu.Lock()
	running := h.srv != nil
	h.cfg = cfg
	h.mu.Unlock()
	if running {
		_ = h.Stop()
		return h.Start()
	}
	return nil
}

// SubscribeLogs returns a log channel (snapshot + live) and an unsubscribe
// closure. Delegates to the package-global Logger.
func (h *Handle) SubscribeLogs() (<-chan LogEntry, func()) {
	return Log.Subscribe()
}

// CheckUpdate queries GitHub Releases for a newer cc2ws version.
func (h *Handle) CheckUpdate(ctx context.Context) (UpdateInfo, error) {
	return NewUpdater().Check(ctx)
}

// ApplyUpdate downloads, verifies, and applies the given update.
func (h *Handle) ApplyUpdate(ctx context.Context, info UpdateInfo) error {
	return NewUpdater().Apply(ctx, info)
}

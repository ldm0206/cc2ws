// app/frontend_test.go
package app

import (
	"context"
	"testing"
	"time"

	"cc2ws/internal/core"
)

// freshConfig uses a free port so any incidental Start bind won't collide.
func freshConfig() core.Config {
	return core.Config{
		Listen:         "127.0.0.1:0",
		UpstreamBase:   "https://x.example.com",
		UpstreamWS:     "wss://x.example.com",
		ConnectTimeout: time.Second,
		IdleTimeout:    time.Second,
	}
}

// RunFrontend(headless=true) blocks until ctx is canceled, then returns nil.
// Stop is idempotent on a never-started handle, so we don't bother starting.
func TestRunFrontend_HeadlessReturnsNilOnCtxCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	h := core.NewHandle(freshConfig())

	// Cancel after a short delay in a separate goroutine so Run blocks first.
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	if err := RunFrontend(ctx, h, true); err != nil {
		t.Fatalf("RunFrontend(headless=true) = %v, want nil", err)
	}
}

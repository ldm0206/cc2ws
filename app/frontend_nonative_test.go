//go:build !linux && !(windows || darwin)

package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"cc2ws/internal/core"
)

func TestRunFrontend_NoNativeReturnsErrNoFrontend(t *testing.T) {
	cfg := core.Config{Listen: "127.0.0.1:0", UpstreamBase: "https://x.example.com", UpstreamWS: "wss://x.example.com", ConnectTimeout: time.Second, IdleTimeout: time.Second}
	h := core.NewHandle(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := RunFrontend(ctx, h, false)
	if !errors.Is(err, errNoFrontend) {
		t.Fatalf("RunFrontend(false)=%v, want errNoFrontend", err)
	}
}

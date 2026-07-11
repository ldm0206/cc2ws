package core

import (
	"testing"
	"time"
)

func TestHandleStartStop(t *testing.T) {
	cfg := Config{
		Listen:         "127.0.0.1:18081",
		UpstreamBase:   "https://hub.example.com",
		UpstreamWS:     "wss://hub.example.com",
		ConnectTimeout: time.Second,
		IdleTimeout:    time.Second,
	}
	t.Setenv("CC2WS_CONFIG_DIR", t.TempDir())
	h := NewHandle(cfg)
	if h.Running() {
		t.Fatal("should not be running before Start")
	}
	if err := h.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !h.Running() {
		t.Fatal("should be running after Start")
	}
	if err := h.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if h.Running() {
		t.Fatal("should not be running after Stop")
	}
}

func TestHandleSetConfigHotRestart(t *testing.T) {
	cfg := Config{
		Listen:         "127.0.0.1:18082",
		UpstreamBase:   "https://hub.example.com",
		UpstreamWS:     "wss://hub.example.com",
		ConnectTimeout: time.Second,
		IdleTimeout:    time.Second,
		LogLevel:       "info",
	}
	t.Setenv("CC2WS_CONFIG_DIR", t.TempDir())
	h := NewHandle(cfg)
	if err := h.Start(); err != nil {
		t.Fatal(err)
	}
	defer h.Stop()
	next := cfg
	next.Listen = "127.0.0.1:18083"
	if err := h.SetConfig(next); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}
	if h.Config().Listen != "127.0.0.1:18083" {
		t.Errorf("Config().Listen=%q want 18083", h.Config().Listen)
	}
	if !h.Running() {
		t.Error("should still be running after hot restart")
	}
}

func TestHandleSetConfigRejectsZeroTimeout(t *testing.T) {
	cfg := Config{Listen: "127.0.0.1:0", UpstreamBase: "https://hub.example.com", UpstreamWS: "wss://hub.example.com", ConnectTimeout: time.Second, IdleTimeout: time.Second}
	t.Setenv("CC2WS_CONFIG_DIR", t.TempDir())
	h := NewHandle(cfg)
	bad := cfg
	bad.ConnectTimeout = 0
	if err := h.SetConfig(bad); err == nil {
		t.Fatal("SetConfig should reject ConnectTimeout=0")
	}
	if h.Config().ConnectTimeout != time.Second {
		t.Error("config should be unchanged after rejected SetConfig")
	}
}

func TestHandleSetConfigRejectsInvalid(t *testing.T) {
	cfg := Config{
		Listen:         "127.0.0.1:18084",
		UpstreamBase:   "https://hub.example.com",
		UpstreamWS:     "wss://hub.example.com",
		ConnectTimeout: time.Second,
		IdleTimeout:    time.Second,
	}
	t.Setenv("CC2WS_CONFIG_DIR", t.TempDir())
	h := NewHandle(cfg)
	bad := cfg
	bad.UpstreamBase = "ftp://bad" // invalid scheme
	if err := h.SetConfig(bad); err == nil {
		t.Fatal("SetConfig should reject invalid upstream scheme")
	}
	if h.Config().UpstreamBase != "https://hub.example.com" {
		t.Error("config should be unchanged after rejected SetConfig")
	}
}

func TestHandleSubscribeLogs(t *testing.T) {
	cfg := Config{
		Listen:         "127.0.0.1:18085",
		UpstreamBase:   "https://hub.example.com",
		UpstreamWS:     "wss://hub.example.com",
		ConnectTimeout: time.Second,
		IdleTimeout:    time.Second,
	}
	t.Setenv("CC2WS_CONFIG_DIR", t.TempDir())
	h := NewHandle(cfg)
	ch, unsub := h.SubscribeLogs()
	defer unsub()
	_ = ch // Start will emit a log line; subscribers receive it
	if err := h.Start(); err != nil {
		t.Fatal(err)
	}
	defer h.Stop()
	// The Start emits "cc2ws ... listening ..."; wait for one entry.
	select {
	case e := <-ch:
		if e.Level < LevelInfo {
			t.Errorf("entry level=%v want >= info", e.Level)
		}
	case <-time.After(time.Second):
		t.Fatal("no log entry received on SubscribeLogs")
	}
}

package core

import (
	"strings"
	"testing"
	"time"
)

func TestLoggerRingBufferBounds(t *testing.T) {
	l := newLogger(4, LevelDebug)
	for i := 0; i < 10; i++ {
		l.logf(LevelInfo, "msg%d", i)
	}
	snap := l.Snapshot()
	if len(snap) != 4 {
		t.Fatalf("ring cap=4, got %d entries", len(snap))
	}
	// Oldest dropped; last 4 kept (msg6..msg9).
	if snap[0].Msg != "msg6" {
		t.Errorf("oldest=%q want msg6", snap[0].Msg)
	}
	if snap[3].Msg != "msg9" {
		t.Errorf("newest=%q want msg9", snap[3].Msg)
	}
}

func TestLoggerLevelFilter(t *testing.T) {
	l := newLogger(10, LevelWarn)
	l.logf(LevelDebug, "debug")
	l.logf(LevelInfo, "info")
	l.logf(LevelWarn, "warn")
	l.logf(LevelError, "err")
	snap := l.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("want 2 entries (warn+error), got %d", len(snap))
	}
}

func TestLoggerSubscribeSnapshotThenLive(t *testing.T) {
	l := newLogger(10, LevelDebug)
	l.logf(LevelInfo, "existing")
	ch, unsub := l.Subscribe()
	defer unsub()
	// Snapshot delivered first.
	got := readN(ch, 1)
	if got[0].Msg != "existing" {
		t.Errorf("snapshot first=%q want existing", got[0].Msg)
	}
	// Then live entries.
	l.logf(LevelInfo, "live")
	got = readN(ch, 1)
	if got[0].Msg != "live" {
		t.Errorf("live=%q want live", got[0].Msg)
	}
}

func TestLoggerSubscriberDropDoesNotBlock(t *testing.T) {
	l := newLogger(10, LevelDebug)
	// Subscriber that never reads — must not block emit.
	_, unsub := l.Subscribe()
	defer unsub()
	for i := 0; i < 1000; i++ {
		l.logf(LevelInfo, "msg%d", i)
	}
	// If we got here, emit never blocked.
}

func TestLogfEmitsToStdout(t *testing.T) {
	// Log is the package global; its stdout writer preserves the existing
	// "cc2ws ..." console format for headless users. Verify a request-log
	// line round-trips through logf into stdout with key fields.
	l := newLogger(10, LevelDebug)
	var buf strings.Builder
	l.stdout = &buf
	l.logf(LevelInfo, "%s %s %d %s upstream=%s auth=%v",
		"POST", "/v1/messages", 200, "45ms", "wss://hub", true)
	if !strings.Contains(buf.String(), "POST /v1/messages 200") {
		t.Errorf("stdout=%q missing request line", buf.String())
	}
}

func readN(ch <-chan LogEntry, n int) []LogEntry {
	out := make([]LogEntry, 0, n)
	for len(out) < n {
		select {
		case e := <-ch:
			out = append(out, e)
		case <-time.After(time.Second):
			panic("readN timeout")
		}
	}
	return out
}

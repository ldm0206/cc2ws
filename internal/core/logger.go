package core

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

// LogEntry is one structured log record. Fields holds the key=value extras
// (e.g. upstream=, auth=) so frontends can render them without re-parsing.
type LogEntry struct {
	Time   time.Time
	Level  Level
	Msg    string
	Fields map[string]string
}

// Logger fans every entry to stdout (preserving the existing console format for
// headless users) and to an in-memory ring buffer. Subscribers get a snapshot
// of the ring followed by live entries; a slow subscriber's channel is dropped
// (logged once) and never blocks the emit path.
type Logger struct {
	mu      sync.Mutex
	ring    []LogEntry
	head    int // index of oldest entry
	count   int
	cap     int
	level   Level
	stdout  io.Writer
	subs    map[chan LogEntry]struct{}
	dropped map[chan LogEntry]bool
}

// newLogger creates a Logger with the given ring capacity and level threshold.
// ringCap is a count of entries (NOT a Level), level gates which entries emit.
func newLogger(ringCap int, level Level) *Logger {
	return &Logger{
		ring:    make([]LogEntry, ringCap),
		cap:     ringCap,
		level:   level,
		stdout:  os.Stdout,
		subs:    make(map[chan LogEntry]struct{}),
		dropped: make(map[chan LogEntry]bool),
	}
}

// Log is the package-global logger, initialized at info level, 2000-entry ring.
var Log = newLogger(2000, LevelInfo)

// logf formats and emits one entry at level. Lower levels than l.level are
// dropped before any fan-out.
func (l *Logger) logf(level Level, format string, args ...any) {
	if level < l.level {
		return
	}
	entry := LogEntry{
		Time:  time.Now(),
		Level: level,
		Msg:   fmt.Sprintf(format, args...),
	}
	l.emit(entry)
}

// emit appends to the ring, writes to stdout, and fans out to subscribers.
func (l *Logger) emit(e LogEntry) {
	l.mu.Lock()
	l.push(e)
	fmt.Fprintln(l.stdout, e.Msg) // preserve "cc2ws ..." console format for headless
	for ch := range l.subs {
		select {
		case ch <- e:
		default:
			if !l.dropped[ch] {
				l.dropped[ch] = true
				// log the drop to stdout only (not re-entrant via emit)
				fmt.Fprintln(l.stdout, "cc2ws: log subscriber dropped (slow consumer)")
			}
		}
	}
	l.mu.Unlock()
}

func (l *Logger) push(e LogEntry) {
	if l.cap == 0 {
		return
	}
	l.ring[l.head] = e
	l.head = (l.head + 1) % l.cap
	if l.count < l.cap {
		l.count++
	}
}

// snapshotLocked returns a copy of the ring without acquiring l.mu. The caller
// MUST already hold l.mu. Extracted from Snapshot so Subscribe can call it
// under its own lock acquisition without a reentrant-lock deadlock.
func (l *Logger) snapshotLocked() []LogEntry {
	out := make([]LogEntry, l.count)
	if l.cap == 0 {
		return out
	}
	start := (l.head - l.count + l.cap) % l.cap
	for i := 0; i < l.count; i++ {
		out[i] = l.ring[(start+i)%l.cap]
	}
	return out
}

// Snapshot returns a copy of the current ring contents in chronological order.
func (l *Logger) Snapshot() []LogEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.snapshotLocked()
}

// Subscribe returns a channel that first receives a snapshot of the current
// ring, then live entries. The returned closure unsubscribes and must be
// called (e.g. defer). The channel is buffered to the ring cap so the snapshot
// arrives without blocking.
func (l *Logger) Subscribe() (<-chan LogEntry, func()) {
	ch := make(chan LogEntry, l.cap)
	l.mu.Lock()
	snap := l.snapshotLocked()
	for _, e := range snap {
		ch <- e
	}
	l.subs[ch] = struct{}{}
	delete(l.dropped, ch)
	l.mu.Unlock()
	unsub := func() {
		l.mu.Lock()
		delete(l.subs, ch)
		delete(l.dropped, ch)
		l.mu.Unlock()
	}
	return ch, unsub
}

// package-level helper for call sites that used log.Printf
func logf(level Level, format string, args ...any) {
	Log.logf(level, format, args...)
}

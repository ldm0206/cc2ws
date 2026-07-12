//go:build linux

package tui

import (
	"strings"
	"testing"
	"time"

	"cc2ws/internal/core"
	"cc2ws/internal/i18n"
)

func defaultCfg() core.Config {
	return core.Config{
		Listen:         "127.0.0.1:18080",
		UpstreamBase:   "https://hub.example.com",
		UpstreamWS:     "wss://hub.example.com",
		ConnectTimeout: 10 * time.Second,
		IdleTimeout:    600 * time.Second,
		LogLevel:       "info",
	}
}

func TestRenderSettingsSummary(t *testing.T) {
	m := newModel(defaultCfg(), nil)
	out := m.View()
	for _, want := range []string{"127.0.0.1:18080", "hub.example.com", "10s", "600s", "info"} {
		if !strings.Contains(out, want) {
			t.Errorf("View() missing %q\n%s", want, out)
		}
	}
}

func TestTabSwitch(t *testing.T) {
	m := newModel(defaultCfg(), nil)
	if m.tab != tabSettings {
		t.Fatal("default tab should be settings")
	}
	mm, _ := m.Update(tabMsg(tabLogs))
	m = mm.(model)
	if m.tab != tabLogs {
		t.Fatalf("tab=%v want logs", m.tab)
	}
}

func TestQuitKey(t *testing.T) {
	m := newModel(defaultCfg(), nil)
	_, cmd := m.Update(keyMsg("q"))
	if cmd == nil {
		t.Error("'q' should produce a non-nil (quit) command")
	}
}

func TestLogEntryAppended(t *testing.T) {
	m := newModel(defaultCfg(), nil)
	m.appendLog(core.LogEntry{Msg: "POST /v1/messages 200 45ms", Level: core.LevelInfo})
	if len(m.logs) != 1 {
		t.Fatalf("logs len=%d want 1", len(m.logs))
	}
	// Logs render only on the logs tab.
	mm, _ := m.Update(tabMsg(tabLogs))
	m = mm.(model)
	if !strings.Contains(m.View(), "POST /v1/messages 200") {
		t.Error("logs View() should show the appended log line")
	}
}

func TestTUIRespectsLanguage(t *testing.T) {
	defer i18n.SetLang(i18n.Default)
	i18n.SetLang(i18n.ZH)
	zhView := newModel(defaultCfg(), nil).viewSettings()
	if !strings.Contains(zhView, "上游") {
		t.Fatalf("zh view missing 上游, got:\n%s", zhView)
	}
	i18n.SetLang(i18n.EN)
	enView := newModel(defaultCfg(), nil).viewSettings()
	if !strings.Contains(enView, "Upstream base") {
		t.Fatalf("en view missing 'Upstream base', got:\n%s", enView)
	}
}

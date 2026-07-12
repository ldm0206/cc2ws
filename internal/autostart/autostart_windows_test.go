//go:build windows

package autostart

import "testing"

// Uses real HKCU\...\Run (per-user, safe in CI). Clean up on exit.
func TestWindowsRoundTrip(t *testing.T) {
	t.Cleanup(func() { _ = Disable() })
	if IsEnabled() {
		_ = Disable()
	}
	if err := Enable(); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if !IsEnabled() {
		t.Fatalf("IsEnabled false after Enable")
	}
	if err := Disable(); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if IsEnabled() {
		t.Fatalf("IsEnabled true after Disable")
	}
}

func TestWindowsEnableIfWanted(t *testing.T) {
	t.Cleanup(func() { _ = Disable() })
	_ = Disable()
	if err := EnableIfWanted(true); err != nil {
		t.Fatalf("EnableIfWanted(true): %v", err)
	}
	if !IsEnabled() {
		t.Fatal("not enabled after EnableIfWanted(true)")
	}
	if err := EnableIfWanted(false); err != nil {
		t.Fatalf("EnableIfWanted(false): %v", err)
	}
	if IsEnabled() {
		t.Fatal("still enabled after EnableIfWanted(false)")
	}
}

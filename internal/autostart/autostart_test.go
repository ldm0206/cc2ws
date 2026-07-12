//go:build darwin || linux

package autostart

import "testing"

func TestRoundTripSeam(t *testing.T) {
	setSeamForPlatform(t)
	if IsEnabled() {
		t.Fatalf("IsEnabled true before Enable")
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

func TestEnableIfWanted(t *testing.T) {
	setSeamForPlatform(t)
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

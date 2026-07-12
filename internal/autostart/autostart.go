// Package autostart registers cc2ws to launch at login on Windows, macOS,
// and Linux. The mechanism is per-platform and per-user (no elevation).
//
// EnableIfWanted(want) reconciles intent (config.AutoStart) with reality: it
// Enable()s or Disable()s to match want. Call it on startup so a user who
// deleted the OS entry out-of-band gets re-registered when their config still
// asks for autostart.
package autostart

// Enable registers cc2ws to launch at login.
func Enable() error { return enable() }

// Disable removes the autostart registration. No-op (nil error) if absent.
func Disable() error { return disable() }

// IsEnabled reports whether the registration exists and points at the
// currently-running executable.
func IsEnabled() bool { return isEnabled() }

// EnableIfWanted makes the registration match want. Returns the underlying
// Enable/Disable error, if any.
func EnableIfWanted(want bool) error {
	if want {
		return enable()
	}
	return disable()
}

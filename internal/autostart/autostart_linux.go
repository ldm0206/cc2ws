//go:build linux

package autostart

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const desktopName = "cc2ws.desktop"

func autostartDir() (string, error) {
	// CC2WS_CONFIG_DIR is the existing test seam; on Linux os.UserConfigDir()
	// resolves to ~/.config, and XDG autostart lives under ~/.config/autostart.
	cfg := os.Getenv("CC2WS_CONFIG_DIR")
	if cfg == "" {
		c, err := os.UserConfigDir()
		if err != nil {
			return "", err
		}
		cfg = c
	}
	return filepath.Join(cfg, "autostart"), nil
}

func desktopPath() (string, error) {
	d, err := autostartDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, desktopName), nil
}

func exePath() (string, error) {
	p, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		p = resolved
	}
	return p, nil
}

func enable() error {
	exe, err := exePath()
	if err != nil {
		return err
	}
	dir, err := autostartDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	desktop := fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=cc2ws
Exec=%s
Terminal=false
X-GNOME-Autostart-enabled=true
`, exe)
	p, err := desktopPath()
	if err != nil {
		return err
	}
	return os.WriteFile(p, []byte(desktop), 0o644)
}

func disable() error {
	p, err := desktopPath()
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func isEnabled() bool {
	p, err := desktopPath()
	if err != nil {
		return false
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return false
	}
	exe, err := exePath()
	if err != nil {
		return false
	}
	return strings.Contains(string(b), exe)
}

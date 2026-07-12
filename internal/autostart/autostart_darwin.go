//go:build darwin

package autostart

import (
	"os"
	"path/filepath"
	"strings"
)

const plistName = "io.cc2ws.app.plist"

func agentDir() (string, error) {
	home := os.Getenv("CC2WS_HOME_DIR") // test seam
	if home == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		home = h
	}
	return filepath.Join(home, "Library", "LaunchAgents"), nil
}

func agentPath() (string, error) {
	d, err := agentDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, plistName), nil
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
	dir, err := agentDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>io.cc2ws.app</string>
<key>ProgramArguments</key><array><string>` + exe + `</string></array>
<key>RunAtLoad</key><true/>
</dict></plist>`
	p, err := agentPath()
	if err != nil {
		return err
	}
	return os.WriteFile(p, []byte(plist), 0o644)
}

func disable() error {
	p, err := agentPath()
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func isEnabled() bool {
	p, err := agentPath()
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

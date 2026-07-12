//go:build windows

package autostart

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const runKeyName = `Software\Microsoft\Windows\CurrentVersion\Run`
const valueName = "cc2ws"

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
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKeyName, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetStringValue(valueName, `"`+exe+`"`)
}

func disable() error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyName, registry.SET_VALUE)
	if err != nil {
		if err == registry.ErrNotExist {
			return nil
		}
		return err
	}
	defer k.Close()
	if err := k.DeleteValue(valueName); err != nil && err != registry.ErrNotExist {
		return err
	}
	return nil
}

func isEnabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyName, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	val, _, err := k.GetStringValue(valueName)
	if err != nil {
		return false
	}
	exe, err := exePath()
	if err != nil {
		return false
	}
	return strings.Contains(val, exe)
}

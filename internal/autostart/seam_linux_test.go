//go:build linux

package autostart

import "testing"

func setSeamForPlatform(t *testing.T) { t.Setenv("CC2WS_CONFIG_DIR", t.TempDir()) }

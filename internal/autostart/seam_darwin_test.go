//go:build darwin

package autostart

import "testing"

func setSeamForPlatform(t *testing.T) { t.Setenv("CC2WS_HOME_DIR", t.TempDir()) }

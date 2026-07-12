//go:build windows

package autostart

import "testing"

func setSeamForPlatform(t *testing.T) {} // no seam; uses real HKCU hive

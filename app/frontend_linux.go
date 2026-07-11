//go:build linux

package app

import "cc2ws/app/tui"

func selectNativeFrontend() Frontend { return tui.New() }

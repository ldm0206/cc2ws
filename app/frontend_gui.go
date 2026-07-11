//go:build windows || darwin

package app

import "cc2ws/app/gui"

func selectNativeFrontend() Frontend { return gui.New() }

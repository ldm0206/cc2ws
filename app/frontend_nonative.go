//go:build !linux && !(windows || darwin)

package app

func selectNativeFrontend() Frontend { return nil }

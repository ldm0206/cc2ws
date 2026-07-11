//go:build !linux

package app

func selectNativeFrontend() Frontend { return nil }

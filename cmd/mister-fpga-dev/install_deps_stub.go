//go:build !linux || !arm || !fpgadev

package main

func productionInstallManager() any { return nil }

//go:build !arm

package gfx

func fillAlphaSIMD([]byte, int, int, int, int, int, Color) bool { return false }

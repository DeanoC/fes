//go:build !linux

package main

import "errors"

func activateNativeFramebuffer(string, string) error {
	return errors.New("native framebuffer activation requires Linux")
}

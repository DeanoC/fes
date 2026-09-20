//go:build linux

package main

import (
	"fmt"
	"os"
)

func activateNativeFramebuffer(path, mode string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return fmt.Errorf("open native framebuffer command FIFO: %w", err)
	}
	defer f.Close()
	if _, err := fmt.Fprintf(f, "fb_cmd_fogcast %s\n", mode); err != nil {
		return fmt.Errorf("activate native framebuffer: %w", err)
	}
	return nil
}

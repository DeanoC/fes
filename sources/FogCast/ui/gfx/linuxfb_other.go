//go:build !linux

package gfx

import "fmt"

// OpenLinuxFB is only implemented on Linux. Host tests inject a destination
// buffer through NewLinuxFB.
func OpenLinuxFB(device string) (*LinuxFB, error) {
	if device == "" {
		device = "/dev/fb0"
	}
	return nil, fmt.Errorf("linuxfb: open %s: only available on linux", device)
}

//go:build linux

package gfx

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

const defaultFBDevice = "/dev/fb0"

// OpenLinuxFB mmaps a Linux framebuffer device (default /dev/fb0) and
// returns a Device whose Present blits the software rasterizer into it.
func OpenLinuxFB(device string) (*LinuxFB, error) {
	if device == "" {
		device = defaultFBDevice
	}
	sys := filepath.Join("/sys/class/graphics", filepath.Base(device))
	virtualSize, err := os.ReadFile(filepath.Join(sys, "virtual_size"))
	if err != nil {
		return nil, fmt.Errorf("linuxfb: virtual_size: %w", err)
	}
	bpp, err := os.ReadFile(filepath.Join(sys, "bits_per_pixel"))
	if err != nil {
		return nil, fmt.Errorf("linuxfb: bits_per_pixel: %w", err)
	}
	stride, err := os.ReadFile(filepath.Join(sys, "stride"))
	if err != nil {
		return nil, fmt.Errorf("linuxfb: stride: %w", err)
	}
	cfg, err := ParseFBSysfs(string(virtualSize), string(bpp), string(stride))
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(device, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("linuxfb: open %s: %w", device, err)
	}
	size := cfg.Height * cfg.Stride
	mem, err := unix.Mmap(int(f.Fd()), 0, size, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("linuxfb: mmap: %w", err)
	}
	sw, err := NewSoftware(cfg.Width, cfg.Height)
	if err != nil {
		_ = unix.Munmap(mem)
		f.Close()
		return nil, err
	}
	return attachLinuxFB(sw, mem, cfg, f, func() error { return unix.Munmap(mem) }), nil
}

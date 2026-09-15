package gfx

import (
	"fmt"
	"strconv"
	"strings"
)

// FBConfig describes a 32bpp memory framebuffer destination. Stride is
// bytes per scanline and may be larger than Width*4.
type FBConfig struct {
	Width  int
	Height int
	Stride int
	BPP    int
}

func (c FBConfig) String() string {
	return fmt.Sprintf("%dx%d stride=%d bpp=%d", c.Width, c.Height, c.Stride, c.BPP)
}

// ValidateFBConfig requires a 32bpp destination whose stride can hold a
// packed RGBA/BGRx row.
func ValidateFBConfig(c FBConfig) error {
	if c.Width < 1 || c.Height < 1 {
		return fmt.Errorf("linuxfb: invalid size %dx%d", c.Width, c.Height)
	}
	if c.BPP != 32 {
		return fmt.Errorf("linuxfb: require 32bpp, got %d", c.BPP)
	}
	if c.Stride < c.Width*4 {
		return fmt.Errorf("linuxfb: stride %d too small for width %d", c.Stride, c.Width)
	}
	return nil
}

// ParseFBSysfs builds an FBConfig from sysfs virtual_size, bits_per_pixel,
// and stride text (the MiSTer class-graphics nodes).
func ParseFBSysfs(virtualSize, bpp, stride string) (FBConfig, error) {
	w, h, err := parseVirtualSize(virtualSize)
	if err != nil {
		return FBConfig{}, err
	}
	bits, err := strconv.Atoi(strings.TrimSpace(bpp))
	if err != nil {
		return FBConfig{}, fmt.Errorf("linuxfb: bits_per_pixel: %w", err)
	}
	row, err := strconv.Atoi(strings.TrimSpace(stride))
	if err != nil {
		return FBConfig{}, fmt.Errorf("linuxfb: stride: %w", err)
	}
	cfg := FBConfig{Width: w, Height: h, Stride: row, BPP: bits}
	if err := ValidateFBConfig(cfg); err != nil {
		return FBConfig{}, err
	}
	return cfg, nil
}

func parseVirtualSize(s string) (int, int, error) {
	parts := strings.Split(strings.TrimSpace(s), ",")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("linuxfb: invalid virtual_size %q", s)
	}
	w, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return 0, 0, fmt.Errorf("linuxfb: virtual_size width: %w", err)
	}
	h, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return 0, 0, fmt.Errorf("linuxfb: virtual_size height: %w", err)
	}
	return w, h, nil
}

// BlitRGBA copies a packed RGBA8 software framebuffer into dst as BGRX
// (little-endian 0x00RRGGBB), honoring destination stride. Extra stride
// bytes are left untouched. Source pixels beyond the FB are clipped.
//
// This matches the known-good MiSTer_fb 32bpp layout used by the native
// framebuffer bridge: byte order B, G, R, 0.
func BlitRGBA(dst []byte, cfg FBConfig, src []byte, srcW, srcH, srcStride int) error {
	if err := ValidateFBConfig(cfg); err != nil {
		return err
	}
	if srcW < 1 || srcH < 1 || srcStride < srcW*4 {
		return fmt.Errorf("linuxfb: invalid source %dx%d stride=%d", srcW, srcH, srcStride)
	}
	if len(src) < srcH*srcStride {
		return fmt.Errorf("linuxfb: short source")
	}
	need := cfg.Height * cfg.Stride
	if len(dst) < need {
		return fmt.Errorf("linuxfb: short destination")
	}
	w := srcW
	if w > cfg.Width {
		w = cfg.Width
	}
	h := srcH
	if h > cfg.Height {
		h = cfg.Height
	}
	for y := 0; y < h; y++ {
		s := src[y*srcStride:]
		d := dst[y*cfg.Stride:]
		for x := 0; x < w; x++ {
			so := x * 4
			do := x * 4
			d[do+0] = s[so+2]
			d[do+1] = s[so+1]
			d[do+2] = s[so+0]
			d[do+3] = 0
		}
	}
	return nil
}

// SampleBGRX returns the destination pixel at (x,y) in B,G,R,X order.
func SampleBGRX(dst []byte, cfg FBConfig, x, y int) (b, g, r, xx byte, err error) {
	if err := ValidateFBConfig(cfg); err != nil {
		return 0, 0, 0, 0, err
	}
	if x < 0 || y < 0 || x >= cfg.Width || y >= cfg.Height {
		return 0, 0, 0, 0, fmt.Errorf("linuxfb: sample out of range")
	}
	off := y*cfg.Stride + x*4
	if off+3 >= len(dst) {
		return 0, 0, 0, 0, fmt.Errorf("linuxfb: short destination")
	}
	return dst[off], dst[off+1], dst[off+2], dst[off+3], nil
}

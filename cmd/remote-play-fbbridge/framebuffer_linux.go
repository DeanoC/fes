//go:build linux

package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
)

type nativeFramebuffer struct {
	f                     *os.File
	mem                   []byte
	width, height, stride int
}

func readInt(path string) (int, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	v, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		return 0, err
	}
	return v, nil
}

func readVirtualSize(path string) (int, int, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, err
	}
	parts := strings.Split(strings.TrimSpace(string(b)), ",")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid virtual_size format: %q", string(b))
	}
	w, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, err
	}
	h, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, err
	}
	return w, h, nil
}

func openNativeFramebuffer(p string) (*nativeFramebuffer, error) {
	width, height, err := readVirtualSize("/sys/class/graphics/fb0/virtual_size")
	if err != nil {
		return nil, fmt.Errorf("read framebuffer size: %w", err)
	}
	bpp, err := readInt("/sys/class/graphics/fb0/bits_per_pixel")
	if err != nil {
		return nil, fmt.Errorf("read framebuffer bpp: %w", err)
	}
	stride, err := readInt("/sys/class/graphics/fb0/stride")
	if err != nil {
		return nil, fmt.Errorf("read framebuffer stride: %w", err)
	}
	if bpp != 32 {
		return nil, fmt.Errorf("framebuffer requires 32bpp, got %d", bpp)
	}
	f, err := os.OpenFile(p, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("open framebuffer: %w", err)
	}
	size := height * stride
	mem, err := syscall.Mmap(int(f.Fd()), 0, size, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("map framebuffer: %w", err)
	}
	return &nativeFramebuffer{f: f, mem: mem, width: width, height: height, stride: stride}, nil
}

func (b *nativeFramebuffer) Width() int  { return b.width }
func (b *nativeFramebuffer) Height() int { return b.height }
func (b *nativeFramebuffer) Stride() int { return b.stride }
func (b *nativeFramebuffer) Write(x []byte) error {
	r := b.width * 4
	for y := 0; y < b.height; y++ {
		for offset := 0; offset < r; offset += 4 {
			// The known-good C probe writes 0x00RRGGBB words. On the
			// little-endian target this is BGRA byte order in memory.
			src := x[y*r+offset:]
			dst := b.mem[y*b.stride+offset:]
			dst[0], dst[1], dst[2], dst[3] = src[2], src[1], src[0], 0
		}
	}
	return nil
}
func (b *nativeFramebuffer) Close() error {
	if b.mem != nil {
		syscall.Munmap(b.mem)
	}
	return b.f.Close()
}

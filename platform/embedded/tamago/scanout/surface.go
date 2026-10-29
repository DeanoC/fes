// SPDX-License-Identifier: GPL-3.0-or-later

// Package scanout copies private RGBA frames into a boot-owned XRGB8888
// scanout window. Publication completes CPU writes, never scanout or DMA drain.
// There is no release/revoke API: the entire window stays reserved until reboot.
package scanout

import (
	"errors"
	"fmt"
	"image"
	"runtime"
	"sync"
)

// Memory must be dedicated, pinned, CPU-writable Normal noncacheable memory
// outside Go's heap. Write copies synchronously and must not retain src or
// expose it to DMA. Publish orders those stores; it is not a hardware fence.
type Memory interface {
	Size() uint64
	Write(offset uint64, src []byte) error
	Publish() error
}

type Surface struct {
	mu                    sync.Mutex
	memory                Memory
	width, height, stride int
	scratch               []byte
	fault                 error
}

// New does not infer firmware identity, addresses or geometry. The board must
// admit those before constructing the memory window, and must keep it pinned
// on every failure, including a failure before this function returns.
func New(width, height, stride int, memory Memory) (*Surface, error) {
	if memory == nil || width < 1 || width > 4096 || height < 1 || height > 2160 || stride < width*4 || stride > 16384 || stride%4 != 0 || uint64(stride)*uint64(height) > memory.Size() {
		return nil, errors.New("scanout: invalid geometry or reserved window")
	}
	return &Surface{memory: memory, width: width, height: height, stride: stride, scratch: make([]byte, width*4*4)}, nil
}

// Present publishes B,G,R,0xff bytes, matching XRGB8888 in little-endian
// memory. The fixed firmware plane may read during this copy: tearing is
// permitted. Returning nil never authorizes FPGA replacement or memory reuse.
func (s *Surface) Present(frame *image.RGBA) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fault != nil {
		return s.fault
	}
	if frame == nil || frame.Rect.Dx() != s.width || frame.Rect.Dy() != s.height || frame.Stride < s.width*4 || frame.Stride > 1<<30 || uint64(len(frame.Pix)) < uint64(s.height-1)*uint64(frame.Stride)+uint64(s.width*4) {
		return errors.New("scanout: frame does not match reserved geometry")
	}
	for y := 0; y < s.height; y += 4 {
		runtime.Gosched()
		rows := min(4, s.height-y)
		for row := 0; row < rows; row++ {
			src := frame.Pix[(y+row)*frame.Stride:]
			dest := s.scratch[row*s.width*4:]
			for x := 0; x < s.width*4; x += 4 {
				dest[x] = src[x+2]
				dest[x+1] = src[x+1]
				dest[x+2] = src[x]
				dest[x+3] = 0xff
			}
		}
		// Contiguous destinations can be published in one bounded chunk; padded
		// rows preserve firmware-owned padding and use separate bounded writes.
		if s.stride == s.width*4 {
			if err := s.memory.Write(uint64(y*s.stride), s.scratch[:rows*s.width*4]); err != nil {
				s.fault = fmt.Errorf("scanout: uncertain copy; window retained until reboot: %w", err)
				return s.fault
			}
		} else {
			for row := 0; row < rows; row++ {
				if err := s.memory.Write(uint64((y+row)*s.stride), s.scratch[row*s.width*4:(row+1)*s.width*4]); err != nil {
					s.fault = fmt.Errorf("scanout: uncertain copy; window retained until reboot: %w", err)
					return s.fault
				}
			}
		}
	}
	if err := s.memory.Publish(); err != nil {
		s.fault = fmt.Errorf("scanout: uncertain publication; window retained until reboot: %w", err)
		return s.fault
	}
	return nil
}

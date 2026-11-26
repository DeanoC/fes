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

// SurfaceStats reports cumulative presentation and dirty-rectangle transfer activity.
// FramesPresented tracks full-frame Present calls; RectsPresented tracks non-empty
// PresentRect calls.
type SurfaceStats struct {
	FramesPresented    uint64
	RectsPresented     uint64
	EmptyRects         uint64
	BytesWritten       uint64
	RejectedValidation uint64
	Faulted            bool
}

type Surface struct {
	mu                    sync.Mutex
	memory                Memory
	width, height, stride int
	scratch               []byte
	fault                 error
	stats                 SurfaceStats
}

// CheckFrame validates dimensions and storage before a platform changes reader
// ownership. It performs no memory writes and does not consume the surface.
func (s *Surface) CheckFrame(frame *image.RGBA) error {
	if frame == nil || frame.Rect.Dx() != s.width || frame.Rect.Dy() != s.height || frame.Stride < s.width*4 || frame.Stride > 1<<30 || uint64(len(frame.Pix)) < uint64(s.height-1)*uint64(frame.Stride)+uint64(s.width*4) {
		return errors.New("scanout: frame does not match reserved geometry")
	}
	return nil
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

// Stats returns a snapshot of cumulative presentation counters and fault state.
func (s *Surface) Stats() SurfaceStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.stats
	st.Faulted = s.fault != nil
	return st
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
	if err := s.CheckFrame(frame); err != nil {
		s.stats.RejectedValidation++
		return err
	}
	if err := s.presentRegionLocked(frame, image.Rect(0, 0, s.width, s.height)); err != nil {
		return err
	}
	s.stats.FramesPresented++
	return nil
}

// PresentRect copies pixels inside rect (in surface coordinates [0, width) x [0, height))
// from frame into the scanout window and publishes the update. An empty rect validates the
// frame and returns nil without writing memory or calling Publish.
func (s *Surface) PresentRect(frame *image.RGBA, rect image.Rectangle) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fault != nil {
		return s.fault
	}
	if err := s.CheckFrame(frame); err != nil {
		s.stats.RejectedValidation++
		return err
	}
	if rect.Empty() {
		s.stats.EmptyRects++
		return nil
	}
	bounds := image.Rect(0, 0, s.width, s.height)
	if !rect.In(bounds) {
		s.stats.RejectedValidation++
		return errors.New("scanout: rect exceeds surface bounds")
	}
	if err := s.presentRegionLocked(frame, rect); err != nil {
		return err
	}
	s.stats.RectsPresented++
	return nil
}

func (s *Surface) presentRegionLocked(frame *image.RGBA, rect image.Rectangle) error {
	w := rect.Dx()
	rowBytes := w * 4
	contiguous := w == s.width && s.stride == s.width*4
	for y := rect.Min.Y; y < rect.Max.Y; y += 4 {
		runtime.Gosched()
		rows := min(4, rect.Max.Y-y)
		for row := 0; row < rows; row++ {
			src := frame.Pix[(y+row)*frame.Stride+rect.Min.X*4:]
			dest := s.scratch[row*rowBytes:]
			for x := 0; x < rowBytes; x += 4 {
				dest[x] = src[x+2]
				dest[x+1] = src[x+1]
				dest[x+2] = src[x]
				dest[x+3] = 0xff
			}
		}
		// Contiguous full-width writes publish in one bounded chunk; partial-width
		// rectangles or padded strides preserve untouched columns/padding via
		// per-row bounded writes.
		if contiguous {
			chunk := s.scratch[:rows*rowBytes]
			if err := s.memory.Write(uint64(y*s.stride), chunk); err != nil {
				s.fault = fmt.Errorf("scanout: uncertain copy; window retained until reboot: %w", err)
				s.stats.Faulted = true
				return s.fault
			}
			s.stats.BytesWritten += uint64(len(chunk))
		} else {
			for row := 0; row < rows; row++ {
				chunk := s.scratch[row*rowBytes : (row+1)*rowBytes]
				offset := uint64((y+row)*s.stride + rect.Min.X*4)
				if err := s.memory.Write(offset, chunk); err != nil {
					s.fault = fmt.Errorf("scanout: uncertain copy; window retained until reboot: %w", err)
					s.stats.Faulted = true
					return s.fault
				}
				s.stats.BytesWritten += uint64(len(chunk))
			}
		}
	}
	if err := s.memory.Publish(); err != nil {
		s.fault = fmt.Errorf("scanout: uncertain publication; window retained until reboot: %w", err)
		s.stats.Faulted = true
		return s.fault
	}
	return nil
}

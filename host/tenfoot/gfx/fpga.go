package gfx

import "image"

// FPGAStub is a wireable placeholder for a future MiSTer custom 2D
// accelerator. This slice does not talk to kit, runtime, or RBF, and it
// does not define an FPGA protocol.
//
// Current policy: thin wrapper over Software. Device methods raster on
// the CPU today. A later hardware path would replace the inner
// rasterizer with mailbox/register setup and DMA of the RGBA8
// framebuffer or a command list; Device and draw.go stay unchanged.
type FPGAStub struct {
	// sw is the current CPU stand-in. Future HW: drop this and submit
	// the same BeginFrame/Clear/FillRect/Draw/Present sequence through
	// a mailbox, with textures staged by DMA.
	sw *Software
}

// NewFPGAStub returns an FPGA stub Device of logicalW×logicalH.
func NewFPGAStub(logicalW, logicalH int) (*FPGAStub, error) {
	sw, err := NewSoftware(logicalW, logicalH)
	if err != nil {
		return nil, err
	}
	return &FPGAStub{sw: sw}, nil
}

// BackendName returns "fpga-stub".
func (f *FPGAStub) BackendName() string { return BackendFPGAStub }

// IsStub reports that this Device is the CPU placeholder, not programmed
// FPGA 2D hardware.
func (f *FPGAStub) IsStub() bool { return true }

// Snapshot returns a copy of the current stub framebuffer (Software today).
func (f *FPGAStub) Snapshot() *image.RGBA { return f.sw.Snapshot() }

// Framebuffer returns the live stub buffer. Callers must not mutate Pix.
func (f *FPGAStub) Framebuffer() *image.RGBA { return f.sw.Framebuffer() }

func (f *FPGAStub) BeginFrame() { f.sw.BeginFrame() }

func (f *FPGAStub) Clear(c Color) { f.sw.Clear(c) }

// Present currently no-ops through Software. Future HW would flush a
// command list over a mailbox and wait for a frame interrupt.
func (f *FPGAStub) Present() { f.sw.Present() }

func (f *FPGAStub) CreateRGBA(img *image.RGBA) (Texture, error) {
	return f.sw.CreateRGBA(img)
}

func (f *FPGAStub) UpdateRGBA(tex Texture, img *image.RGBA) error {
	return f.sw.UpdateRGBA(tex, img)
}

func (f *FPGAStub) Destroy(tex Texture) { f.sw.Destroy(tex) }

func (f *FPGAStub) FillRect(rect Rect, c Color) { f.sw.FillRect(rect, c) }

// Draw currently nearest-blits on the CPU. Future HW would enqueue a
// textured blit with source/dest rects through registers or a command
// ring, with bitmap bytes already in DMA-visible memory.
func (f *FPGAStub) Draw(tex Texture, src *Rect, dst Rect) {
	f.sw.Draw(tex, src, dst)
}

func (f *FPGAStub) SetBlend(mode BlendMode) { f.sw.SetBlend(mode) }

func (f *FPGAStub) DebugText(x, y int, text string, scale int) {
	f.sw.DebugText(x, y, text, scale)
}

func (f *FPGAStub) Close() { f.sw.Close() }

var _ Device = (*FPGAStub)(nil)

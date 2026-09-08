package gfx

import "image"

// FPGAStub is a thin Software wrapper kept for TENFOOT_GFX=fpga-stub
// compatibility. It does not record a command stream and does not talk
// to kit, runtime, or RBF. The FC2D encoder lives on FPGA (NewFPGA,
// BackendName "fpga"); see fpga_protocol.md.
type FPGAStub struct {
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

func (f *FPGAStub) Present() { f.sw.Present() }

func (f *FPGAStub) CreateRGBA(img *image.RGBA) (Texture, error) {
	return f.sw.CreateRGBA(img)
}

func (f *FPGAStub) UpdateRGBA(tex Texture, img *image.RGBA) error {
	return f.sw.UpdateRGBA(tex, img)
}

func (f *FPGAStub) Destroy(tex Texture) { f.sw.Destroy(tex) }

func (f *FPGAStub) FillRect(rect Rect, c Color) { f.sw.FillRect(rect, c) }

func (f *FPGAStub) Draw(tex Texture, src *Rect, dst Rect) {
	f.sw.Draw(tex, src, dst)
}

func (f *FPGAStub) SetBlend(mode BlendMode) { f.sw.SetBlend(mode) }

func (f *FPGAStub) DebugText(x, y int, text string, scale int) {
	f.sw.DebugText(x, y, text, scale)
}

func (f *FPGAStub) DrawText(x, y int, text string, sizePx int, c Color) {
	f.DrawTextWeight(x, y, text, sizePx, WeightRegular, c)
}

func (f *FPGAStub) DrawTextWeight(x, y int, text string, sizePx int, w Weight, c Color) {
	f.sw.DrawTextWeight(x, y, text, sizePx, w, c)
}

func (f *FPGAStub) Close() { f.sw.Close() }

var _ Device = (*FPGAStub)(nil)

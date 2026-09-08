package gfx

import (
	"fmt"
	"hash/crc32"
	"image"
	"math"
)

// RasterSoftwareReplay is the current FPGA Device raster path: encode the
// FC2D stream and draw with Software. A programmed 2D core would replace
// this with mailbox submit.
const RasterSoftwareReplay = "software-replay"

// FPGA is a Device that records the versioned FC2D command stream and
// rasters through Software. It does not talk to kit, runtime, or RBF.
//
// BackendName is "fpga", distinct from FPGAStub ("fpga-stub"). IsStub is
// true until a live hardware transport exists; this slice never sets one.
type FPGA struct {
	sw       *Software
	hdr      Header
	cmds     []Command
	frame    uint32
	hardware bool
}

// NewFPGA returns an FPGA Device of logicalW×logicalH. The Device encodes
// every call into FC2D v1 and rasters with Software so tests and the
// current kit path can sample pixels. Hardware FPGA 2D is not live.
func NewFPGA(logicalW, logicalH int) (*FPGA, error) {
	sw, err := NewSoftware(logicalW, logicalH)
	if err != nil {
		return nil, err
	}
	hdr, err := NewHeader(sw.w, sw.h)
	if err != nil {
		return nil, err
	}
	return &FPGA{sw: sw, hdr: hdr}, nil
}

// BackendName returns "fpga".
func (f *FPGA) BackendName() string { return BackendFPGA }

// IsStub reports that this Device is not talking to a programmed 2D core.
func (f *FPGA) IsStub() bool { return f == nil || !f.hardware }

// RasterMode returns "software-replay" until hardware submit exists.
func (f *FPGA) RasterMode() string {
	if f != nil && f.hardware {
		return "hardware"
	}
	return RasterSoftwareReplay
}

// Header returns the FC2D stream header.
func (f *FPGA) Header() Header { return f.hdr }

// Commands returns a copy of the recorded command list.
func (f *FPGA) Commands() []Command {
	out := make([]Command, len(f.cmds))
	copy(out, f.cmds)
	return out
}

// Bytes encodes the session stream (header plus commands).
func (f *FPGA) Bytes() ([]byte, error) {
	return EncodeStream(f.hdr, f.cmds)
}

// Snapshot returns a copy of the software framebuffer.
func (f *FPGA) Snapshot() *image.RGBA { return f.sw.Snapshot() }

// Framebuffer returns the live software buffer. Callers must not mutate Pix.
func (f *FPGA) Framebuffer() *image.RGBA { return f.sw.Framebuffer() }

func (f *FPGA) record(c Command) {
	f.cmds = append(f.cmds, c)
}

func (f *FPGA) BeginFrame() {
	f.frame++
	f.record(Command{Op: OpBeginFrame, Frame: f.frame})
	f.sw.BeginFrame()
}

func (f *FPGA) Clear(c Color) {
	f.record(Command{Op: OpClear, Color: c})
	f.sw.Clear(c)
}

func (f *FPGA) Present() {
	f.record(Command{Op: OpPresent})
	f.sw.Present()
}

func (f *FPGA) CreateRGBA(img *image.RGBA) (Texture, error) {
	tex, err := f.sw.CreateRGBA(img)
	if err != nil {
		return Texture{}, err
	}
	if tex.id > math.MaxUint32 {
		f.sw.Destroy(tex)
		return Texture{}, fmt.Errorf("fpga: texture id overflows u32")
	}
	w, h, pix, err := tightRGBA(img)
	if err != nil {
		f.sw.Destroy(tex)
		return Texture{}, err
	}
	f.record(Command{
		Op:     OpCreateTexture,
		TexID:  uint32(tex.id),
		Width:  w,
		Height: h,
		Format: FormatRGBA8,
		CRC32:  crc32.ChecksumIEEE(pix),
		Pixels: pix,
	})
	return tex, nil
}

func (f *FPGA) UpdateRGBA(tex Texture, img *image.RGBA) error {
	if err := f.sw.UpdateRGBA(tex, img); err != nil {
		return err
	}
	w, h, pix, err := tightRGBA(img)
	if err != nil {
		return err
	}
	f.record(Command{
		Op:     OpUpdateTexture,
		TexID:  uint32(tex.id),
		Width:  w,
		Height: h,
		Format: FormatRGBA8,
		CRC32:  crc32.ChecksumIEEE(pix),
		Pixels: pix,
	})
	return nil
}

func (f *FPGA) Destroy(tex Texture) {
	f.record(Command{Op: OpDestroyTexture, TexID: uint32(tex.id)})
	f.sw.Destroy(tex)
}

func (f *FPGA) FillRect(rect Rect, c Color) {
	f.record(Command{Op: OpFillRect, Dst: rect, Color: c})
	f.sw.FillRect(rect, c)
}

func (f *FPGA) Draw(tex Texture, src *Rect, dst Rect) {
	var srcCopy *Rect
	if src != nil {
		cp := *src
		srcCopy = &cp
	}
	f.record(Command{Op: OpDraw, TexID: uint32(tex.id), Src: srcCopy, Dst: dst})
	f.sw.Draw(tex, src, dst)
}

func (f *FPGA) SetBlend(mode BlendMode) {
	f.record(Command{Op: OpSetBlend, Blend: mode})
	f.sw.SetBlend(mode)
}

func (f *FPGA) DebugText(x, y int, text string, scale int) {
	f.record(Command{Op: OpDebugText, X: x, Y: y, Text: text, Scale: scale})
	f.sw.DebugText(x, y, text, scale)
}

func (f *FPGA) DrawText(x, y int, text string, sizePx int, c Color) {
	f.DrawTextWeight(x, y, text, sizePx, WeightRegular, c)
}

func (f *FPGA) DrawTextWeight(x, y int, text string, sizePx int, w Weight, c Color) {
	w = NormalizeWeight(w)
	f.record(Command{Op: OpDrawText, X: x, Y: y, SizePx: sizePx, Weight: w, Color: c, Text: text})
	f.sw.DrawTextWeight(x, y, text, sizePx, w, c)
}

func (f *FPGA) Close() {
	f.record(Command{Op: OpClose})
	f.sw.Close()
}

var _ Device = (*FPGA)(nil)

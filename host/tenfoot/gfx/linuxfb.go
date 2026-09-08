package gfx

import (
	"fmt"
	"image"
	"os"
)

// LinuxFB is a Device that rasters with Software and Present-blits the
// RGBA8 framebuffer onto a 32bpp memory framebuffer (MiSTer /dev/fb0).
// There is no cgo, SDL, DRM, or GL.
type LinuxFB struct {
	sw     *Software
	dst    []byte
	cfg    FBConfig
	unmap  func() error
	closer func() error
}

// NewLinuxFB returns a linuxfb Device of logicalW×logicalH that blits into
// dst using cfg. Dimensions below 1 use the framebuffer size. dst must
// already be large enough for cfg.Height*cfg.Stride.
func NewLinuxFB(logicalW, logicalH int, dst []byte, cfg FBConfig) (*LinuxFB, error) {
	if err := ValidateFBConfig(cfg); err != nil {
		return nil, err
	}
	if logicalW < 1 {
		logicalW = cfg.Width
	}
	if logicalH < 1 {
		logicalH = cfg.Height
	}
	if len(dst) < cfg.Height*cfg.Stride {
		return nil, fmt.Errorf("linuxfb: short destination")
	}
	sw, err := NewSoftware(logicalW, logicalH)
	if err != nil {
		return nil, err
	}
	return &LinuxFB{sw: sw, dst: dst, cfg: cfg}, nil
}

// BackendName returns "linuxfb".
func (d *LinuxFB) BackendName() string { return BackendLinuxFB }

// Config returns the destination framebuffer geometry.
func (d *LinuxFB) Config() FBConfig { return d.cfg }

// Destination returns the live 32bpp BGRX buffer Present writes into.
func (d *LinuxFB) Destination() []byte { return d.dst }

// Framebuffer returns the live software RGBA8 buffer. Callers must not
// mutate Pix.
func (d *LinuxFB) Framebuffer() *image.RGBA { return d.sw.Framebuffer() }

// Snapshot returns a copy of the current software framebuffer.
func (d *LinuxFB) Snapshot() *image.RGBA { return d.sw.Snapshot() }

func (d *LinuxFB) BeginFrame() { d.sw.BeginFrame() }

func (d *LinuxFB) Clear(c Color) { d.sw.Clear(c) }

// Present blits the software RGBA8 framebuffer into the 32bpp destination
// with BGRX byte order and destination stride.
func (d *LinuxFB) Present() {
	fb := d.sw.Framebuffer()
	_ = BlitRGBA(d.dst, d.cfg, fb.Pix, d.sw.w, d.sw.h, fb.Stride)
}

func (d *LinuxFB) CreateRGBA(img *image.RGBA) (Texture, error) {
	return d.sw.CreateRGBA(img)
}

func (d *LinuxFB) UpdateRGBA(tex Texture, img *image.RGBA) error {
	return d.sw.UpdateRGBA(tex, img)
}

func (d *LinuxFB) Destroy(tex Texture) { d.sw.Destroy(tex) }

func (d *LinuxFB) FillRect(rect Rect, c Color) { d.sw.FillRect(rect, c) }

func (d *LinuxFB) Draw(tex Texture, src *Rect, dst Rect) {
	d.sw.Draw(tex, src, dst)
}

func (d *LinuxFB) SetBlend(mode BlendMode) { d.sw.SetBlend(mode) }

func (d *LinuxFB) DebugText(x, y int, text string, scale int) {
	d.sw.DebugText(x, y, text, scale)
}

func (d *LinuxFB) DrawText(x, y int, text string, sizePx int, c Color) {
	d.sw.DrawText(x, y, text, sizePx, c)
}

func (d *LinuxFB) Close() {
	if d.sw != nil {
		d.sw.Close()
	}
	if d.unmap != nil {
		_ = d.unmap()
		d.unmap = nil
	}
	if d.closer != nil {
		_ = d.closer()
		d.closer = nil
	}
}

// PaintLinuxFBSpike draws a visible test pattern: SMPTE-style color bars,
// a bottom grid, and "FOGCAST" via debugfont. Used by the kit spike and
// host tests. Does not Present.
func PaintLinuxFBSpike(d Device, w, h int) {
	if d == nil || w < 1 || h < 1 {
		return
	}
	d.BeginFrame()
	d.Clear(RGB(16, 16, 24))
	d.SetBlend(BlendNone)
	bars := [...]Color{
		RGB(255, 255, 255),
		RGB(255, 255, 0),
		RGB(0, 255, 255),
		RGB(0, 255, 0),
		RGB(255, 0, 255),
		RGB(255, 0, 0),
		RGB(0, 0, 255),
		RGB(0, 0, 0),
	}
	barH := h * 2 / 3
	if barH < 1 {
		barH = h
	}
	barW := w / len(bars)
	if barW < 1 {
		barW = 1
	}
	for i, c := range bars {
		d.FillRect(Rect{X: float32(i * barW), Y: 0, W: float32(barW), H: float32(barH)}, c)
	}
	d.FillRect(Rect{X: 0, Y: float32(barH), W: float32(w), H: float32(h - barH)}, RGB(24, 28, 40))
	const step = 32
	grid := RGB(80, 88, 112)
	for x := 0; x < w; x += step {
		d.FillRect(Rect{X: float32(x), Y: float32(barH), W: 1, H: float32(h - barH)}, grid)
	}
	for y := barH; y < h; y += step {
		d.FillRect(Rect{X: 0, Y: float32(y), W: float32(w), H: 1}, grid)
	}
	const scale = 4
	const glyph = 8
	label := "FOGCAST"
	tw := len(label) * glyph * scale
	th := glyph * scale
	tx := (w - tw) / 2
	ty := barH + (h-barH-th)/2
	if tx < 0 {
		tx = 0
	}
	if ty < barH {
		ty = barH
	}
	d.FillRect(Rect{X: float32(tx - 8), Y: float32(ty - 8), W: float32(tw + 16), H: float32(th + 16)}, RGB(8, 8, 16))
	d.DebugText(tx, ty, label, scale)
}

// LinuxFBCursorSize is the input-spike highlight in pixels.
const LinuxFBCursorSize = 24

// LinuxFBCursorColor is opaque yellow, sampled as BGRX 0,220,255,0.
var LinuxFBCursorColor = RGB(255, 220, 0)

// PaintLinuxFBInput draws the linuxfb spike pattern plus a cursor and status.
func PaintLinuxFBInput(d Device, w, h, cx, cy int, status string) {
	PaintLinuxFBSpike(d, w, h)
	if d == nil || w < 1 || h < 1 {
		return
	}
	if cx < 0 {
		cx = 0
	}
	if cy < 0 {
		cy = 0
	}
	if cx > w-LinuxFBCursorSize {
		cx = w - LinuxFBCursorSize
		if cx < 0 {
			cx = 0
		}
	}
	if cy > h-LinuxFBCursorSize {
		cy = h - LinuxFBCursorSize
		if cy < 0 {
			cy = 0
		}
	}
	d.SetBlend(BlendNone)
	d.FillRect(Rect{
		X: float32(cx),
		Y: float32(cy),
		W: float32(LinuxFBCursorSize),
		H: float32(LinuxFBCursorSize),
	}, LinuxFBCursorColor)
	if status != "" {
		y := h - 20
		if y < 0 {
			y = 0
		}
		d.DebugText(8, y, status, 2)
	}
}

func attachLinuxFB(sw *Software, dst []byte, cfg FBConfig, f *os.File, unmap func() error) *LinuxFB {
	d := &LinuxFB{sw: sw, dst: dst, cfg: cfg, unmap: unmap}
	if f != nil {
		d.closer = f.Close
	}
	return d
}

var _ Device = (*LinuxFB)(nil)

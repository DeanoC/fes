package gfx

import (
	"context"
	"image"
	"slices"
	"weak"

	"github.com/DeanoC/FogCast/ui/menudisplay"
)

// MenuBlitPresenter is a MenuPresenter whose grants can include the menu
// blitter. The menudisplay socket client is one.
type MenuBlitPresenter interface {
	Grant(ctx context.Context, generation uint64) (menudisplay.Grant, error)
	PresentBlit(ctx context.Context, generation uint64, frame menudisplay.BlitFrame) (menudisplay.Result, error)
}

// maxBaseOps is the longest list a frame that does not start with Clear
// continues; a longer one is collapsed into one copy of its pixels.
const maxBaseOps = 1024

// frameMode is how the frame being drawn is rasterized. It is chosen at the
// frame's first call and kept until Present.
type frameMode uint8

const (
	frameUnlatched frameMode = iota
	// frameCPU draws into the Software framebuffer as linuxfb does.
	frameCPU
	// frameBlit records blitter commands; the fabric draws the frame.
	frameBlit
)

// latch picks the frame's mode: the blitter when the last grant selected it.
func (d *MenuDisplay) latch() frameMode {
	if d.mode != frameUnlatched {
		return d.mode
	}
	d.mu.Lock()
	blit := d.blitReady
	d.mu.Unlock()
	if blit {
		d.mode, d.ops, d.needBase = frameBlit, nil, true
	} else {
		d.mode = frameCPU
	}
	return d.mode
}

// blitBase starts a list frame that did not begin with Clear from the frame
// before it, as Software continues from its framebuffer.
func (d *MenuDisplay) blitBase() {
	if !d.needBase {
		return
	}
	d.needBase = false
	if d.lastList && len(d.lastOps) <= maxBaseOps {
		d.ops = slices.Clone(d.lastOps)
		return
	}
	var pix []byte
	if d.lastList {
		// Frames that never clear would grow the list without end; start
		// from the last frame as one copy instead.
		pix = make([]byte, len(d.Software.pix))
		rasterOps(pix, d.Software.stride, d.lastOps)
	} else {
		pix = slices.Clone(d.Software.Framebuffer().Pix)
	}
	fb := d.Software.fb
	source := &menudisplay.BlitSource{Pix: pix, Stride: fb.Stride, W: d.Software.w, H: d.Software.h}
	d.ops = []menudisplay.BlitOp{{Kind: menudisplay.BlitCopy, W: d.Software.w, H: d.Software.h, Source: source}}
}

// cpuBase makes the Software framebuffer hold the last frame when that frame
// was a list, before a CPU frame draws on it or anyone reads it.
func (d *MenuDisplay) cpuBase() {
	if !d.cpuStale {
		return
	}
	d.cpuStale = false
	d.Software.previous = nil
	rasterOps(d.Software.pix, d.Software.stride, d.lastOps)
}

func (d *MenuDisplay) BeginFrame() {
	d.latch()
	d.Software.BeginFrame()
}

func (d *MenuDisplay) Clear(c Color) {
	if d.latch() == frameBlit {
		d.needBase = false
		d.ops = append(d.ops[:0], menudisplay.BlitOp{Kind: menudisplay.BlitFill, W: d.Software.w, H: d.Software.h, Color: argb(premultiplied(c))})
		return
	}
	d.cpuStale = false
	d.Software.Clear(c)
}

func (d *MenuDisplay) FillRect(rect Rect, c Color) {
	if d.latch() != frameBlit {
		d.cpuBase()
		d.Software.FillRect(rect, c)
		return
	}
	d.blitBase()
	s := d.Software
	x0, y0, x1, y1, ok := clipRect(rect, s.w, s.h)
	if !ok {
		return
	}
	// A replacing fill stores the premultiplied colour, as Software does.
	kind, fill := menudisplay.BlitFill, premultiplied(c)
	if s.blend == BlendAlpha && c.A != 255 {
		if c.A == 0 {
			return
		}
		kind, fill = menudisplay.BlitFillBlend, c
	}
	d.ops = append(d.ops, menudisplay.BlitOp{Kind: kind, X: x0, Y: y0, W: x1 - x0, H: y1 - y0, Color: argb(fill)})
}

func (d *MenuDisplay) Draw(tex Texture, src *Rect, dst Rect) {
	if d.latch() != frameBlit {
		d.cpuBase()
		d.Software.Draw(tex, src, dst)
		return
	}
	d.blitBase()
	s := d.Software
	bm := s.lookup(tex)
	if bm == nil {
		return
	}
	sr := Rect{X: 0, Y: 0, W: float32(bm.w), H: float32(bm.h)}
	if src != nil {
		sr = *src
	}
	if sr.W <= 0 || sr.H <= 0 || dst.W <= 0 || dst.H <= 0 {
		return
	}
	x0, y0, x1, y1, ok := clipRect(dst, s.w, s.h)
	if !ok {
		return
	}
	if dst.W == sr.W && dst.H == sr.H && isInt(dst.X) && isInt(dst.Y) && isInt(sr.X) && isInt(sr.Y) {
		d.blitBitmap(bm, int(sr.X), int(sr.Y), x0, y0, x1, y1, int(dst.X), int(dst.Y))
		return
	}
	scaled := d.blitScaled(bm, sr, dst, x0, y0, x1, y1)
	d.blitBitmap(scaled, 0, 0, x0, y0, x1, y1, x0, y0)
}

// blitBitmap records Software.blitCopy: a 1:1 draw clipped to the source.
func (d *MenuDisplay) blitBitmap(bm *swBitmap, srcX, srcY, x0, y0, x1, y1, dstX, dstY int) {
	x0, y0 = max(x0, dstX-srcX), max(y0, dstY-srcY)
	x1, y1 = min(x1, dstX-srcX+bm.w), min(y1, dstY-srcY+bm.h)
	if x0 >= x1 || y0 >= y1 {
		return
	}
	kind := menudisplay.BlitBlend
	if bm.opaque {
		kind = menudisplay.BlitCopy
	}
	d.ops = append(d.ops, menudisplay.BlitOp{Kind: kind, X: x0, Y: y0, W: x1 - x0, H: y1 - y0,
		Source: bm.blitSource(), SourceX: srcX + x0 - dstX, SourceY: srcY + y0 - dstY})
}

// blitScaled is the visible part of a scaled draw, sampled exactly as
// Software.Draw samples it. The blitter only copies 1:1.
func (d *MenuDisplay) blitScaled(source *swBitmap, src, dst Rect, x0, y0, x1, y1 int) *swBitmap {
	key := scaledKey{weak.Make(source), src, dst}
	if bm, ok := d.blitCache.scaled.get(key); ok {
		return bm
	}
	w, h := x1-x0, y1-y0
	bitmap := &swBitmap{w: w, h: h, stride: w * 4, pix: make([]byte, w*h*4), opaque: source.opaque}
	invW, invH := src.W/dst.W, src.H/dst.H
	xs := make([]int, w)
	for x := range xs {
		xs[x] = sampleCoord(src.X+(float32(x0+x)+0.5-dst.X)*invW, source.w) * 4
	}
	for y := 0; y < h; y++ {
		sy := sampleCoord(src.Y+(float32(y0+y)+0.5-dst.Y)*invH, source.h)
		sourceRow := source.pix[sy*source.stride : sy*source.stride+source.w*4]
		row := bitmap.pix[y*bitmap.stride : (y+1)*bitmap.stride]
		for x, off := range xs {
			copy(row[x*4:x*4+4], sourceRow[off:off+4])
		}
	}
	d.blitCache.scaled.put(key, bitmap, bitmap.h*bitmap.stride)
	return bitmap
}

func (d *MenuDisplay) DebugText(x, y int, text string, scale int) {
	if d.latch() != frameBlit {
		d.cpuBase()
		d.Software.DebugText(x, y, text, scale)
		return
	}
	d.blitBase()
	if text == "" {
		return
	}
	if scale < 1 {
		scale = 1
	}
	key := debugKey{text, scale}
	bm, ok := d.blitCache.debug.get(key)
	if !ok {
		bm = debugBitmap(text, scale)
		d.blitCache.debug.put(key, bm, bm.h*bm.stride)
	}
	s := d.Software
	x0, y0, x1, y1, ok := clipRect(Rect{X: float32(x), Y: float32(y), W: float32(bm.w), H: float32(bm.h)}, s.w, s.h)
	if ok {
		d.blitBitmap(bm, 0, 0, x0, y0, x1, y1, x, y)
	}
}

// debugBitmap is DebugText's glyph blocks over transparent pixels.
func debugBitmap(text string, scale int) *swBitmap {
	const glyph = 8
	w, h := len(text)*glyph*scale, glyph*scale
	bm := &swBitmap{w: w, h: h, stride: w * 4, pix: make([]byte, w*h*4)}
	for i := 0; i < len(text); i++ {
		bits := debugGlyph(text[i])
		for row := 0; row < glyph; row++ {
			for col := 0; col < glyph; col++ {
				if bits[row]&(1<<uint(col)) == 0 {
					continue
				}
				rx, ry := (i*glyph+col)*scale, row*scale
				fillRows(bm.pix, bm.stride, rx, ry, rx+scale, ry+scale, 236, 240, 248, 255)
			}
		}
	}
	return bm
}

func (d *MenuDisplay) DrawText(x, y int, text string, sizePx int, c Color) {
	d.DrawTextWeight(x, y, text, sizePx, WeightRegular, c)
}

func (d *MenuDisplay) DrawTextWeight(x, y int, text string, sizePx int, w Weight, c Color) {
	m, src, _ := UIText(text, sizePx, w, 0)
	d.DrawMask(m, src, x, y, c)
}

// DrawMask records an expand for the selected menu blitter.
func (d *MenuDisplay) DrawMask(m *Mask, src image.Rectangle, x, y int, c Color) {
	if d.latch() != frameBlit {
		d.cpuBase()
		d.Software.DrawMask(m, src, x, y, c)
		return
	}
	d.blitBase()
	src, at, ok := clipMask(m, src, x, y, d.Software.w, d.Software.h)
	if !ok || c.A == 0 {
		return
	}
	d.ops = append(d.ops, menudisplay.BlitOp{Kind: menudisplay.BlitExpand, X: at.X, Y: at.Y, W: src.Dx(), H: src.Dy(),
		Color: argb(c), Source: m.blitSource(), SourceX: src.Min.X, SourceY: src.Min.Y})
}

// Framebuffer returns the frame drawn so far. While the blitter draws the
// frame it is a CPU raster of the commands, not a live buffer.
func (d *MenuDisplay) Framebuffer() *image.RGBA {
	if d.mode == frameBlit {
		return d.blitSnapshot()
	}
	d.cpuBase()
	return d.Software.Framebuffer()
}

// Snapshot returns a copy of the frame drawn so far.
func (d *MenuDisplay) Snapshot() *image.RGBA {
	if d.mode == frameBlit {
		return d.blitSnapshot()
	}
	d.cpuBase()
	return d.Software.Snapshot()
}

func (d *MenuDisplay) blitSnapshot() *image.RGBA {
	d.blitBase()
	out := image.NewRGBA(image.Rect(0, 0, d.Software.w, d.Software.h))
	rasterOps(out.Pix, out.Stride, d.ops)
	return out
}

// frameSurface lets FrameCache reuse a CPU backdrop. A list frame has no CPU
// surface; FrameCache then replays the backdrop's commands.
func (d *MenuDisplay) frameSurface() *image.RGBA {
	if d.latch() == frameBlit {
		return nil
	}
	d.cpuBase()
	return d.Software.frameSurface()
}

// finishFrame ends the frame being drawn and returns it as a list when the
// blitter drew it. A Present without draws repeats the last frame.
func (d *MenuDisplay) finishFrame() (ops []menudisplay.BlitOp, isList bool) {
	switch d.mode {
	case frameBlit:
		d.blitBase()
		ops = d.ops
		if ops == nil {
			ops = []menudisplay.BlitOp{}
		}
		d.ops, d.lastOps, d.lastList, d.cpuStale = nil, ops, true, true
		// The Software buffer no longer holds the newest frame.
		d.Software.previous = nil
		isList = true
	case frameCPU:
		// A CPU frame that drew nothing repeats the last frame.
		d.cpuBase()
		d.lastOps, d.lastList = nil, false
	default:
		ops, isList = d.lastOps, d.lastList
	}
	d.mode = frameUnlatched
	return ops, isList
}

// rasterOps draws a list on the CPU exactly as Software drew the calls the
// list records, alpha included.
func rasterOps(pix []byte, stride int, ops []menudisplay.BlitOp) {
	for _, op := range ops {
		x1, y1 := op.X+op.W, op.Y+op.H
		r, g, b, a := uint8(op.Color>>16), uint8(op.Color>>8), uint8(op.Color), uint8(op.Color>>24)
		switch op.Kind {
		case menudisplay.BlitFill:
			fillRows(pix, stride, op.X, op.Y, x1, y1, r, g, b, a)
		case menudisplay.BlitFillBlend:
			for y := op.Y; y < y1; y++ {
				row := pix[y*stride:]
				for x := op.X; x < x1; x++ {
					blendOver(row, x*4, r, g, b, a)
				}
			}
		case menudisplay.BlitExpand:
			s := op.Source
			src := image.Rect(op.SourceX, op.SourceY, op.SourceX+op.W, op.SourceY+op.H)
			expandRows(pix, stride, s.Pix, s.Stride, src, image.Pt(op.X, op.Y), Color{R: r, G: g, B: b, A: a})
		case menudisplay.BlitCopy, menudisplay.BlitBlend:
			s := op.Source
			for y := 0; y < op.H; y++ {
				at := (op.SourceY+y)*s.Stride + op.SourceX*4
				src := s.Pix[at : at+op.W*4]
				dst := pix[(op.Y+y)*stride+op.X*4 : (op.Y+y)*stride+x1*4]
				if op.Kind == menudisplay.BlitCopy {
					copy(dst, src)
					continue
				}
				for i := 0; i < len(src); i += 4 {
					blendOver(dst, i, src[i], src[i+1], src[i+2], src[i+3])
				}
			}
		}
	}
}

// copyFrameOps is pixels, a complete frame, as one blitter copy. The pixels
// belong to a pooled buffer, so they are uploaded every time.
func copyFrameOps(pixels []byte) []menudisplay.BlitOp {
	source := &menudisplay.BlitSource{Pix: pixels, Stride: menudisplay.Stride, W: menudisplay.Width, H: menudisplay.Height, Transient: true}
	return []menudisplay.BlitOp{{Kind: menudisplay.BlitCopy, W: menudisplay.Width, H: menudisplay.Height, Source: source}}
}

func argb(c Color) uint32 {
	return uint32(c.A)<<24 | uint32(c.R)<<16 | uint32(c.G)<<8 | uint32(c.B)
}

// blitSource is the bitmap as a blitter source, made once so the client can
// keep it resident while the bitmap lives.
func (bm *swBitmap) blitSource() *menudisplay.BlitSource {
	if bm.blit == nil {
		bm.blit = &menudisplay.BlitSource{Pix: bm.pix, Stride: bm.stride, W: bm.w, H: bm.h}
	}
	return bm.blit
}

// scaledKey does not keep its source alive: a destroyed texture's copies
// just age out.
type scaledKey struct {
	source   weak.Pointer[swBitmap]
	src, dst Rect
}

type debugKey struct {
	text  string
	scale int
}

// blitCaches keep the bitmaps a list frame draws from that Software makes
// for each call: scaled copies and debug glyphs.
type blitCaches struct {
	scaled *lru[scaledKey, *swBitmap]
	debug  *lru[debugKey, *swBitmap]
}

func newBlitCaches() blitCaches {
	return blitCaches{
		scaled: newLRU[scaledKey, *swBitmap](64, 16<<20),
		debug:  newLRU[debugKey, *swBitmap](64, 2<<20),
	}
}

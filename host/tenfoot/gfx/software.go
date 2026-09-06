package gfx

import (
	"fmt"
	"image"
	"math"
)

// Software is a pure-Go Device. It rasters into an RGBA8 framebuffer of
// logical size and stores textures as CPU bitmaps. There is no cgo and no
// SDL. Draw uses nearest-neighbour sampling (v1; bilinear is not used).
// FillRect honors BlendNone and BlendAlpha. Textured Draw always uses
// source-over alpha, matching the SDL backend's texture blend mode.
//
// Present is a no-op: the framebuffer is already current. Snapshot copies
// it for golden tests. Loops are stride-aware and do not allocate per pixel.
type Software struct {
	w, h, stride int
	pix          []byte
	fb           *image.RGBA
	blend        BlendMode
	next         uint64
	textures     map[uint64]*swBitmap
}

type swBitmap struct {
	w, h, stride int
	pix          []byte
}

// NewSoftware returns a Software Device with an RGBA8 framebuffer of
// logicalW×logicalH. Dimensions below 1 are clamped to 1.
func NewSoftware(logicalW, logicalH int) (*Software, error) {
	if logicalW < 1 {
		logicalW = 1
	}
	if logicalH < 1 {
		logicalH = 1
	}
	stride := logicalW * 4
	pix := make([]byte, stride*logicalH)
	fb := &image.RGBA{
		Pix:    pix,
		Stride: stride,
		Rect:   image.Rect(0, 0, logicalW, logicalH),
	}
	return &Software{
		w:        logicalW,
		h:        logicalH,
		stride:   stride,
		pix:      pix,
		fb:       fb,
		textures: map[uint64]*swBitmap{},
	}, nil
}

// BackendName returns "software".
func (s *Software) BackendName() string { return BackendSoftware }

// Framebuffer returns the live RGBA8 buffer. Callers must not mutate Pix.
func (s *Software) Framebuffer() *image.RGBA { return s.fb }

// Snapshot returns a copy of the current framebuffer for tests.
func (s *Software) Snapshot() *image.RGBA {
	out := image.NewRGBA(image.Rect(0, 0, s.w, s.h))
	copy(out.Pix, s.pix)
	return out
}

func (s *Software) BeginFrame() {}

func (s *Software) Clear(c Color) {
	fillRows(s.pix, s.stride, 0, 0, s.w, s.h, c.R, c.G, c.B, c.A)
}

func (s *Software) Present() {}

func (s *Software) CreateRGBA(img *image.RGBA) (Texture, error) {
	bm, err := cloneBitmap(img)
	if err != nil {
		return Texture{}, err
	}
	s.next++
	id := s.next
	s.textures[id] = bm
	return Texture{id: id, w: bm.w, h: bm.h}, nil
}

func (s *Software) UpdateRGBA(tex Texture, img *image.RGBA) error {
	bm := s.lookup(tex)
	if bm == nil {
		return fmt.Errorf("invalid texture")
	}
	next, err := cloneBitmap(img)
	if err != nil {
		return err
	}
	if next.w != tex.w || next.h != tex.h {
		return fmt.Errorf("size mismatch")
	}
	s.textures[tex.id] = next
	return nil
}

func (s *Software) Destroy(tex Texture) {
	if tex.id == 0 {
		return
	}
	delete(s.textures, tex.id)
}

func (s *Software) FillRect(rect Rect, c Color) {
	x0, y0, x1, y1, ok := clipRect(rect, s.w, s.h)
	if !ok {
		return
	}
	if s.blend == BlendAlpha && c.A != 255 {
		if c.A == 0 {
			return
		}
		for y := y0; y < y1; y++ {
			row := s.pix[y*s.stride:]
			for x := x0; x < x1; x++ {
				blendOver(row, x*4, c.R, c.G, c.B, c.A)
			}
		}
		return
	}
	fillRows(s.pix, s.stride, x0, y0, x1, y1, c.R, c.G, c.B, c.A)
}

func (s *Software) Draw(tex Texture, src *Rect, dst Rect) {
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
	// Integer 1:1 blit: no per-pixel float, stride-aware row loop.
	if dst.W == sr.W && dst.H == sr.H && isInt(dst.X) && isInt(dst.Y) && isInt(sr.X) && isInt(sr.Y) {
		s.blitCopy(bm, int(sr.X), int(sr.Y), x0, y0, x1, y1, int(dst.X), int(dst.Y))
		return
	}
	invW := sr.W / dst.W
	invH := sr.H / dst.H
	tw, th := bm.w, bm.h
	srcPix := bm.pix
	srcStride := bm.stride
	for y := y0; y < y1; y++ {
		sy := sampleCoord(sr.Y+(float32(y)+0.5-dst.Y)*invH, th)
		srcRow := srcPix[sy*srcStride:]
		dstRow := s.pix[y*s.stride:]
		for x := x0; x < x1; x++ {
			sx := sampleCoord(sr.X+(float32(x)+0.5-dst.X)*invW, tw)
			so := sx * 4
			blendOver(dstRow, x*4, srcRow[so], srcRow[so+1], srcRow[so+2], srcRow[so+3])
		}
	}
}

func (s *Software) SetBlend(mode BlendMode) { s.blend = mode }

func (s *Software) DebugText(x, y int, text string, scale int) {
	if text == "" {
		return
	}
	if scale < 1 {
		scale = 1
	}
	const glyph = 8
	px, py := x, y
	for i := 0; i < len(text); i++ {
		bits := debugGlyph(text[i])
		for row := 0; row < glyph; row++ {
			rowBits := bits[row]
			for col := 0; col < glyph; col++ {
				if rowBits&(1<<uint(col)) == 0 {
					continue
				}
				rx := px + col*scale
				ry := py + row*scale
				fillRows(s.pix, s.stride, rx, ry, rx+scale, ry+scale, 236, 240, 248, 255)
			}
		}
		px += glyph * scale
	}
}

func (s *Software) Close() {
	for id := range s.textures {
		delete(s.textures, id)
	}
}

func (s *Software) lookup(tex Texture) *swBitmap {
	if s == nil || tex.id == 0 {
		return nil
	}
	return s.textures[tex.id]
}

func (s *Software) blitCopy(bm *swBitmap, srcX, srcY, x0, y0, x1, y1, dstX, dstY int) {
	for y := y0; y < y1; y++ {
		sy := srcY + (y - dstY)
		if sy < 0 || sy >= bm.h {
			continue
		}
		srcRow := bm.pix[sy*bm.stride:]
		dstRow := s.pix[y*s.stride:]
		for x := x0; x < x1; x++ {
			sx := srcX + (x - dstX)
			if sx < 0 || sx >= bm.w {
				continue
			}
			so := sx * 4
			blendOver(dstRow, x*4, srcRow[so], srcRow[so+1], srcRow[so+2], srcRow[so+3])
		}
	}
}

func cloneBitmap(img *image.RGBA) (*swBitmap, error) {
	if img == nil {
		return nil, fmt.Errorf("empty image")
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w < 1 || h < 1 || len(img.Pix) == 0 {
		return nil, fmt.Errorf("empty image")
	}
	stride := w * 4
	pix := make([]byte, stride*h)
	for y := 0; y < h; y++ {
		off := img.PixOffset(b.Min.X, b.Min.Y+y)
		copy(pix[y*stride:(y+1)*stride], img.Pix[off:off+stride])
	}
	return &swBitmap{w: w, h: h, stride: stride, pix: pix}, nil
}

func clipRect(r Rect, maxW, maxH int) (x0, y0, x1, y1 int, ok bool) {
	if r.W <= 0 || r.H <= 0 {
		return
	}
	x0 = int(math.Floor(float64(r.X)))
	y0 = int(math.Floor(float64(r.Y)))
	x1 = int(math.Ceil(float64(r.X + r.W)))
	y1 = int(math.Ceil(float64(r.Y + r.H)))
	if x0 < 0 {
		x0 = 0
	}
	if y0 < 0 {
		y0 = 0
	}
	if x1 > maxW {
		x1 = maxW
	}
	if y1 > maxH {
		y1 = maxH
	}
	if x0 >= x1 || y0 >= y1 {
		return
	}
	ok = true
	return
}

func fillRows(pix []byte, stride, x0, y0, x1, y1 int, r, g, b, a uint8) {
	if x0 < 0 {
		x0 = 0
	}
	if y0 < 0 {
		y0 = 0
	}
	maxW := stride / 4
	maxH := len(pix) / stride
	if x1 > maxW {
		x1 = maxW
	}
	if y1 > maxH {
		y1 = maxH
	}
	if x0 >= x1 || y0 >= y1 {
		return
	}
	width := x1 - x0
	rowBytes := width * 4
	// Paint the first row, then copy it down. No per-pixel allocation.
	first := pix[y0*stride+x0*4 : y0*stride+x0*4+rowBytes]
	first[0], first[1], first[2], first[3] = r, g, b, a
	n := 4
	for n < rowBytes {
		copy(first[n:], first[:n])
		n *= 2
	}
	for y := y0 + 1; y < y1; y++ {
		copy(pix[y*stride+x0*4:y*stride+x0*4+rowBytes], first)
	}
}

func blendOver(dst []byte, i int, sr, sg, sb, sa uint8) {
	switch sa {
	case 0:
		return
	case 255:
		dst[i] = sr
		dst[i+1] = sg
		dst[i+2] = sb
		dst[i+3] = 255
		return
	}
	inv := uint32(255 - sa)
	dst[i] = uint8((uint32(sr)*uint32(sa) + uint32(dst[i])*inv) / 255)
	dst[i+1] = uint8((uint32(sg)*uint32(sa) + uint32(dst[i+1])*inv) / 255)
	dst[i+2] = uint8((uint32(sb)*uint32(sa) + uint32(dst[i+2])*inv) / 255)
	dst[i+3] = uint8(uint32(sa) + (uint32(dst[i+3])*inv)/255)
}

func sampleCoord(f float32, limit int) int {
	v := int(math.Floor(float64(f)))
	if v < 0 {
		return 0
	}
	if v >= limit {
		return limit - 1
	}
	return v
}

func isInt(f float32) bool { return f == float32(int(f)) }

var _ Device = (*Software)(nil)

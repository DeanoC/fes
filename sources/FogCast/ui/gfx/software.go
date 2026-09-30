package gfx

import (
	"fmt"
	"image"
	"math"
)

// Software is a pure-Go Device. It rasters into an RGBA8 framebuffer of
// logical size and stores textures as CPU bitmaps. There is no cgo and no
// SDL. Draw uses nearest-neighbour sampling so present-loop blits stay cheap.
// Cover, screenshot, and still downscale uses Catmull–Rom once at decode
// (ui/shared.DecodeCover and siblings), not here. FillRect honors
// BlendNone and BlendAlpha. Textured Draw always uses source-over alpha,
// matching the SDL backend's texture blend mode.
//
// Present is a no-op: the framebuffer is already current. Snapshot copies
// it for golden tests. Loops are stride-aware and do not allocate per pixel.
type Software struct {
	w, h, stride int
	pix          []byte
	fb           *image.RGBA
	blend        BlendMode
	sampleX      []int // reusable source byte offsets for scaled draws
	fillColor    Color
	fillLUT      [4][256]uint8
	fillLUTValid bool
	scaled       []scaledBitmap // most recently used first
	scaledBytes  int
	next         uint64
	textures     map[uint64]*swBitmap
}

type swBitmap struct {
	w, h, stride                 int
	pix                          []byte
	opaque                       bool
	resizeHintSrc, resizeHintDst Rect
	resizeHintValid              bool
}

const scaledBitmapBudget = 8 << 20
const scaledBitmapLimit = 64

type scaledBitmap struct {
	tex      uint64
	src, dst Rect
	bitmap   *swBitmap
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
	s.invalidateScaled(tex.id)
	s.textures[tex.id] = next
	return nil
}

func (s *Software) Destroy(tex Texture) {
	if tex.id == 0 {
		return
	}
	s.invalidateScaled(tex.id)
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
		if (x1-x0)*(y1-y0) < 256 {
			for y := y0; y < y1; y++ {
				row := s.pix[y*s.stride:]
				for x := x0; x < x1; x++ {
					blendOver(row, x*4, c.R, c.G, c.B, c.A)
				}
			}
			return
		}
		if !s.fillLUTValid || s.fillColor != c {
			alpha, inv := uint32(c.A), uint32(255-c.A)
			red, green, blue := uint32(c.R)*alpha, uint32(c.G)*alpha, uint32(c.B)*alpha
			for i := range s.fillLUT[0] {
				dest := uint32(i) * inv
				s.fillLUT[0][i] = uint8((red + dest) / 255)
				s.fillLUT[1][i] = uint8((green + dest) / 255)
				s.fillLUT[2][i] = uint8((blue + dest) / 255)
				s.fillLUT[3][i] = uint8(alpha + dest/255)
			}
			s.fillColor, s.fillLUTValid = c, true
		}
		for y := y0; y < y1; y++ {
			row := s.pix[y*s.stride+x0*4 : y*s.stride+x1*4]
			for i := 0; i < len(row); i += 4 {
				row[i] = s.fillLUT[0][row[i]]
				row[i+1] = s.fillLUT[1][row[i+1]]
				row[i+2] = s.fillLUT[2][row[i+2]]
				row[i+3] = s.fillLUT[3][row[i+3]]
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
	if resized := s.scaledBitmap(tex.id, bm, sr, dst); resized != nil {
		s.blitCopy(resized, 0, 0, x0, y0, x1, y1, x0, y0)
		return
	}
	invW := sr.W / dst.W
	invH := sr.H / dst.H
	width := x1 - x0
	if cap(s.sampleX) < width {
		s.sampleX = make([]int, width)
	}
	xs := s.sampleX[:width]
	// Calculate each column once, with the same float32 expression as the
	// original nearest-neighbour sampler. Incremental stepping can drift at
	// texel boundaries, particularly for fractional source rectangles.
	for i := range xs {
		x := x0 + i
		xs[i] = sampleCoord(sr.X+(float32(x)+0.5-dst.X)*invW, bm.w) * 4
	}
	for y := y0; y < y1; y++ {
		sy := sampleCoord(sr.Y+(float32(y)+0.5-dst.Y)*invH, bm.h)
		srcRow := bm.pix[sy*bm.stride : sy*bm.stride+bm.w*4]
		dstRow := s.pix[y*s.stride+x0*4 : y*s.stride+x1*4]
		if bm.opaque {
			for i, so := range xs {
				copy(dstRow[i*4:i*4+4], srcRow[so:so+4])
			}
		} else {
			for i, so := range xs {
				blendOver(dstRow, i*4, srcRow[so], srcRow[so+1], srcRow[so+2], srcRow[so+3])
			}
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

func (s *Software) DrawText(x, y int, text string, sizePx int, c Color) {
	s.DrawTextWeight(x, y, text, sizePx, WeightRegular, c)
}

func (s *Software) DrawTextWeight(x, y int, text string, sizePx int, w Weight, c Color) {
	blitText(s, x, y, RasterizeTextWeight(text, sizePx, w, c, 0))
}

func (s *Software) Close() {
	s.scaled, s.sampleX = nil, nil
	s.scaledBytes = 0
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
	// Clip against the source once. Unlike scaled draws, 1:1 blits skip
	// source coordinates outside the bitmap rather than clamp them.
	if x0 < dstX-srcX {
		x0 = dstX - srcX
	}
	if y0 < dstY-srcY {
		y0 = dstY - srcY
	}
	if x1 > dstX-srcX+bm.w {
		x1 = dstX - srcX + bm.w
	}
	if y1 > dstY-srcY+bm.h {
		y1 = dstY - srcY + bm.h
	}
	if x0 >= x1 || y0 >= y1 {
		return
	}
	srcOffset := (srcX + x0 - dstX) * 4
	rowBytes := (x1 - x0) * 4
	for y := y0; y < y1; y++ {
		sy := srcY + y - dstY
		srcRow := bm.pix[sy*bm.stride+srcOffset : sy*bm.stride+srcOffset+rowBytes]
		dstRow := s.pix[y*s.stride+x0*4 : y*s.stride+x0*4+rowBytes]
		if bm.opaque {
			copy(dstRow, srcRow)
		} else {
			for i := 0; i < rowBytes; i += 4 {
				blendOver(dstRow, i, srcRow[i], srcRow[i+1], srcRow[i+2], srcRow[i+3])
			}
		}
	}
}

// Cache the visible raster bounds of repeated resizes, including fractional
// fitted rectangles. The complete source and destination geometry participates
// in the key; sampling retains the original global float32 coordinates.
func (s *Software) scaledBitmap(tex uint64, source *swBitmap, src, dst Rect) *swBitmap {
	x0, y0, x1, y1, ok := clipRect(dst, s.w, s.h)
	w, h := x1-x0, y1-y0
	area := float64(w) * float64(h)
	if !ok || area < 256 || area > scaledBitmapBudget/4 {
		source.resizeHintValid = false
		return nil
	}
	for i, entry := range s.scaled {
		if entry.tex == tex && entry.src == src && entry.dst == dst {
			copy(s.scaled[1:i+1], s.scaled[:i])
			s.scaled[0] = entry
			source.resizeHintSrc, source.resizeHintDst, source.resizeHintValid = src, dst, true
			return entry.bitmap
		}
	}
	// Sample the first draw directly. Admission needs a repeated geometry
	// on these same source bytes, so streaming updates and moving sprites
	// do not allocate a resized bitmap for every frame. UpdateRGBA replaces
	// the source bitmap, which also resets this hint.
	repeated := source.resizeHintValid && source.resizeHintSrc == src && source.resizeHintDst == dst
	source.resizeHintSrc, source.resizeHintDst, source.resizeHintValid = src, dst, true
	if !repeated {
		return nil
	}
	for len(s.scaled) > 0 && (s.scaledBytes+w*h*4 > scaledBitmapBudget || len(s.scaled) >= scaledBitmapLimit) {
		last := len(s.scaled) - 1
		s.scaledBytes -= len(s.scaled[last].bitmap.pix)
		s.scaled[last] = scaledBitmap{}
		s.scaled = s.scaled[:last]
	}
	bitmap := &swBitmap{w: w, h: h, stride: w * 4, pix: make([]byte, w*h*4), opaque: source.opaque}
	invW, invH := src.W/dst.W, src.H/dst.H
	if cap(s.sampleX) < w {
		s.sampleX = make([]int, w)
	}
	xs := s.sampleX[:w]
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
	s.scaled = append(s.scaled, scaledBitmap{})
	copy(s.scaled[1:], s.scaled[:len(s.scaled)-1])
	s.scaled[0] = scaledBitmap{tex: tex, src: src, dst: dst, bitmap: bitmap}
	s.scaledBytes += len(bitmap.pix)
	return bitmap
}

func (s *Software) invalidateScaled(tex uint64) {
	for i := 0; i < len(s.scaled); {
		if s.scaled[i].tex != tex {
			i++
			continue
		}
		s.scaledBytes -= len(s.scaled[i].bitmap.pix)
		copy(s.scaled[i:], s.scaled[i+1:])
		last := len(s.scaled) - 1
		s.scaled[last] = scaledBitmap{}
		s.scaled = s.scaled[:last]
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
	opaque := true
	for i := 3; i < len(pix); i += 4 {
		if pix[i] != 255 {
			opaque = false
			break
		}
	}
	return &swBitmap{w: w, h: h, stride: stride, pix: pix, opaque: opaque}, nil
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

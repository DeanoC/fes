package gfx

import (
	"image"

	"github.com/DeanoC/FogCast/ui/menudisplay"
)

// Mask is coverage, one byte a pixel in rows of W bytes, that DrawMask draws
// in a colour. A mask must not change once drawn: the menu blitter keeps it
// resident.
type Mask struct {
	W, H int
	Pix  []byte
	blit *menudisplay.BlitSource
}

// Bounds is the whole mask.
func (m *Mask) Bounds() image.Rectangle { return image.Rect(0, 0, m.W, m.H) }

// blitSource is the mask as a blitter coverage source, made once so the
// client can keep it resident while the mask lives.
func (m *Mask) blitSource() *menudisplay.BlitSource {
	if m.blit == nil {
		m.blit = &menudisplay.BlitSource{Pix: m.Pix, Stride: m.W, W: m.W, H: m.H, Coverage: true}
	}
	return m.blit
}

// expandAlpha is alpha a scaled by coverage m, as the menu blitter's EXPAND
// forms it.
func expandAlpha(a, m uint8) uint8 { return uint8(uint32(a) * uint32(m) / 255) }

// ExpandMask returns the src part of m as straight-alpha RGBA. Each pixel is
// c with alpha A·m/255, so drawing it source-over gives exactly what DrawMask
// gives. The SDL renderer uploads this form as a texture.
func ExpandMask(m *Mask, src image.Rectangle, c Color) *image.RGBA {
	src = src.Intersect(m.Bounds())
	img := image.NewRGBA(image.Rect(0, 0, src.Dx(), src.Dy()))
	for y := 0; y < src.Dy(); y++ {
		row := m.Pix[(src.Min.Y+y)*m.W+src.Min.X : (src.Min.Y+y)*m.W+src.Max.X]
		out := img.Pix[y*img.Stride:]
		for x, v := range row {
			out[x*4], out[x*4+1], out[x*4+2], out[x*4+3] = c.R, c.G, c.B, expandAlpha(c.A, v)
		}
	}
	return img
}

// clipMask is the part of a draw of m's src with its top-left at x, y that
// lands inside a w×h target, and where that part lands.
func clipMask(m *Mask, src image.Rectangle, x, y, w, h int) (image.Rectangle, image.Point, bool) {
	if m == nil {
		return image.Rectangle{}, image.Point{}, false
	}
	off := image.Pt(x, y).Sub(src.Min)
	dst := src.Intersect(m.Bounds()).Add(off).Intersect(image.Rect(0, 0, w, h))
	if dst.Empty() {
		return image.Rectangle{}, image.Point{}, false
	}
	return dst.Sub(off), dst.Min, true
}

// expandRows draws the src part of a coverage mask with stride maskStride
// at dst in c, source-over.
func expandRows(pix []byte, stride int, mask []byte, maskStride int, src image.Rectangle, dst image.Point, c Color) {
	for y := 0; y < src.Dy(); y++ {
		row := mask[(src.Min.Y+y)*maskStride+src.Min.X : (src.Min.Y+y)*maskStride+src.Max.X]
		out := pix[(dst.Y+y)*stride+dst.X*4:]
		for x, v := range row {
			blendOver(out, x*4, c.R, c.G, c.B, expandAlpha(c.A, v))
		}
	}
}

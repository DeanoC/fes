package gfx

import "golang.org/x/sys/cpu"

// fillAlphaSIMD uses unaligned-safe NEON loads/stores on complete eight-pixel
// blocks. Narrow rectangles and row tails keep the exact scalar blend.
func fillAlphaSIMD(pix []byte, stride, x0, y0, x1, y1 int, c Color) bool {
	if !cpu.ARM.HasNEON || x1-x0 < 8 {
		return false
	}
	a := uint16(c.A)
	r, g, b, alpha := uint16(c.R)*a, uint16(c.G)*a, uint16(c.B)*a, a*255
	// Bias the numerator once for exact division by 255 in the kernel.
	source := [8]uint16{r + 1, g + 1, b + 1, alpha + 1, r + 1, g + 1, b + 1, alpha + 1}
	inv := uint32(255 - c.A)
	for y := y0; y < y1; y++ {
		row := pix[y*stride+x0*4 : y*stride+x1*4]
		n := len(row) &^ 31
		fillAlphaNEON(row[:n], &source, inv)
		for i := n; i < len(row); i += 4 {
			blendOver(row, i, c.R, c.G, c.B, c.A)
		}
	}
	return true
}

// fillAlphaNEON blends complete 32-byte blocks. The caller supplies a nonempty
// multiple of 32 bytes and eight biased source contributions for two RGBA
// pixels. The alpha contribution is A*255+1, preserving destination alpha.
//
//go:noescape
func fillAlphaNEON(row []byte, source *[8]uint16, inverse uint32)

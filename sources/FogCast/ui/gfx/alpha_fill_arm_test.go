package gfx

import (
	"bytes"
	"math/rand"
	"testing"

	"golang.org/x/sys/cpu"
)

func TestNEONAlphaFillRows(t *testing.T) {
	if !cpu.ARM.HasNEON {
		t.Skip("NEON unavailable; scalar renderer tests cover fallback")
	}
	rng := rand.New(rand.NewSource(318))
	for offset := 0; offset < 16; offset++ {
		for width := 1; width <= 129; width++ {
			for _, alpha := range []uint8{0, 1, 127, 128, 180, 254, 255} {
				x0, height := rng.Intn(4), 3
				// Odd strides exercise different alignments on consecutive rows.
				stride := (x0+width+3)*4 + rng.Intn(4)
				buf := make([]byte, offset+height*stride+32)
				rng.Read(buf)
				want := append([]byte(nil), buf...)
				pix := buf[offset : offset+height*stride]
				c := RGBA(uint8(rng.Intn(256)), uint8(rng.Intn(256)), uint8(rng.Intn(256)), alpha)
				used := fillAlphaSIMD(pix, stride, x0, 0, x0+width, height, c)
				if used != (width >= 8) {
					t.Fatalf("width=%d SIMD admission=%v", width, used)
				}
				if used {
					for y := 0; y < height; y++ {
						row := want[offset+y*stride : offset+(y+1)*stride]
						for x := x0; x < x0+width; x++ {
							blendOver(row, x*4, c.R, c.G, c.B, c.A)
						}
					}
				}
				if !bytes.Equal(buf, want) {
					t.Fatalf("offset=%d width=%d stride=%d color=%v: pixels or guards differ", offset, width, stride, c)
				}
			}
		}
	}
}

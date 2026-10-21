package menudisplay

import "golang.org/x/sys/cpu"

// bgrxTable picks B, G, R and a zero (index 8 is out of range) for each of
// the two pixels in a NEON D register.
var bgrxTable = [8]byte{2, 1, 0, 8, 6, 5, 4, 8}

// writeRow converts complete 32-byte blocks with NEON and the tail in Go.
func writeRow(dst, src []byte) {
	n := 0
	if cpu.ARM.HasNEON {
		n = len(src) &^ 31
		if n > 0 {
			writeRowNEON(dst[:n], src[:n], &bgrxTable)
		}
	}
	writeRowScalar(dst[n:], src[n:])
}

// writeRowNEON converts len(src) bytes, a nonempty multiple of 32, of RGBA
// pixels into XRGB8888 words in dst, which is at least as long.
//
//go:noescape
func writeRowNEON(dst, src []byte, table *[8]byte)

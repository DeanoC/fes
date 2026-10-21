package menudisplay

import "golang.org/x/sys/cpu"

// bgraTable picks B, G, R and A for each of the two pixels in a NEON D
// register.
var bgraTable = [8]byte{2, 1, 0, 3, 6, 5, 4, 7}

// writeRow converts complete 32-byte blocks with NEON and the tail in Go.
func writeRow(dst, src []byte) {
	n := 0
	if cpu.ARM.HasNEON {
		n = len(src) &^ 31
		if n > 0 {
			writeRowNEON(dst[:n], src[:n], &bgraTable)
		}
	}
	writeRowScalar(dst[n:], src[n:])
}

// writeRowNEON converts len(src) bytes, a nonempty multiple of 32, of RGBA
// pixels into B, G, R, A words in dst, which is at least as long.
//
//go:noescape
func writeRowNEON(dst, src []byte, table *[8]byte)

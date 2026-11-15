//go:build tamago

package gfx

import "runtime"

// TamaGo has no asynchronous preemption. Yield at least every four raster
// rows so a full software frame cannot starve socket and timer goroutines.
// This is a work bound, not a real-time or frame-rate guarantee.
func rasterRow(row int) {
	if row&3 == 0 {
		runtime.Gosched()
	}
}

func rasterCopy(dst, src []byte) {
	for len(dst) > 0 && len(src) > 0 {
		n := min(len(dst), len(src), 32<<10)
		copy(dst[:n], src[:n])
		dst, src = dst[n:], src[n:]
		runtime.Gosched()
	}
}

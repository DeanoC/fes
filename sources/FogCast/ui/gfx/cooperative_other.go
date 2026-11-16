//go:build !tamago

package gfx

// Other runtimes already preempt raster work. Keep their existing copy and
// loop behavior; these small helpers inline away.
func rasterRow(int)              {}
func rasterCopy(dst, src []byte) { copy(dst, src) }

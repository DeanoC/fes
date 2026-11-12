//go:build !arm && !arm64

package menudisplay

func writeRow(dst, src []byte) { writeRowScalar(dst, src) }

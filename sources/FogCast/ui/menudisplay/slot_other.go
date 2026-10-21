//go:build !arm

package menudisplay

func writeRow(dst, src []byte) { writeRowScalar(dst, src) }

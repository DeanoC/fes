package gfx

import (
	"strconv"
	"testing"
)

func BenchmarkPixelCopy(b *testing.B) {
	for _, n := range []int{1024, 1280 * 720 * 4} {
		b.Run(strconv.Itoa(n), func(b *testing.B) {
			dst, src := make([]byte, n), make([]byte, n)
			b.SetBytes(int64(n))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				copy(dst, src)
			}
		})
	}
}

func BenchmarkOpaqueFillPanel(b *testing.B) {
	s, _ := NewSoftware(1280, 720)
	r := Rect{X: 280, Y: 100, W: 720, H: 360}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.FillRect(r, RGB(18, 20, 28))
	}
}

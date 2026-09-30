package gfx

import (
	"fmt"
	"image"
	"testing"
)

// Warmed-up raster operations exclude asset upload and font rasterization.
func BenchmarkSoftwareSprites(b *testing.B) {
	for _, alpha := range []bool{false, true} {
		name := "Opaque"
		if alpha {
			name = "Alpha"
		}
		for _, scaled := range []bool{false, true} {
			suffix := "/Unscaled"
			dst := Rect{X: 100, Y: 100, W: 256, H: 256}
			if scaled {
				suffix = "/Scaled"
				dst.W = 384
				dst.H = 384
			}
			b.Run(name+suffix, func(b *testing.B) {
				s, _ := NewSoftware(1280, 720)
				img := opaqueRGBA(256, 256)
				if alpha {
					for i := 3; i < len(img.Pix); i += 4 {
						img.Pix[i] = uint8((i / 4) % 256)
					}
				}
				tex, err := s.CreateRGBA(img)
				if err != nil {
					b.Fatal(err)
				}
				s.Draw(tex, nil, dst)
				s.Draw(tex, nil, dst)
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					s.Draw(tex, nil, dst)
				}
			})
		}
	}
}

func BenchmarkSoftwareLabelBlit(b *testing.B) {
	s, _ := NewSoftware(1280, 720)
	img := RasterizeTextWeight("Super Mario World", 24, WeightBold, RGB(240, 240, 255), 0)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		blitText(s, 100, 100, img)
	}
}

func BenchmarkSoftwareDrawText(b *testing.B) {
	s, _ := NewSoftware(1280, 720)
	s.DrawText(100, 100, "Super Mario World", 24, RGB(240, 240, 255))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.DrawText(100, 100, "Super Mario World", 24, RGB(240, 240, 255))
	}
}

// Sub-images exercise texture uploads with nonzero bounds and parent stride.
func BenchmarkSoftwareUpload(b *testing.B) {
	s, _ := NewSoftware(1280, 720)
	img := opaqueRGBA(512, 512).SubImage(image.Rect(10, 20, 266, 276)).(*image.RGBA)
	tex, _ := s.CreateRGBA(img)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := s.UpdateRGBA(tex, img); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSoftwareAlphaFill(b *testing.B) {
	for _, width := range []int{8, 32, 1280} {
		height := width
		if width == 1280 {
			height = 720
		}
		b.Run(fmt.Sprintf("%dx%d", width, height), func(b *testing.B) {
			s, _ := NewSoftware(1280, 720)
			s.Clear(RGBA(40, 80, 120, 160))
			s.SetBlend(BlendAlpha)
			rect := Rect{X: 0, Y: 0, W: float32(width), H: float32(height)}
			color := RGBA(18, 25, 40, 176)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				s.FillRect(rect, color)
			}
		})
	}
}

func BenchmarkSoftwareScaledCold(b *testing.B) {
	s, _ := NewSoftware(1280, 720)
	tex, _ := s.CreateRGBA(opaqueRGBA(256, 256))
	dst := Rect{X: 100, Y: 100, W: 384, H: 384}
	s.Draw(tex, nil, dst)
	s.Draw(tex, nil, dst)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.invalidateScaled(tex.id)
		s.Draw(tex, nil, dst)
	}
}

// Updating the source every frame must never allocate resized cache entries.
func BenchmarkSoftwareStreamingScaled(b *testing.B) {
	s, _ := NewSoftware(1280, 720)
	img := opaqueRGBA(256, 256)
	tex, _ := s.CreateRGBA(img)
	dst := Rect{X: 100, Y: 100, W: 384, H: 384}
	s.Draw(tex, nil, dst)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		img.Pix[0] = uint8(i)
		if err := s.UpdateRGBA(tex, img); err != nil {
			b.Fatal(err)
		}
		s.Draw(tex, nil, dst)
	}
}

func BenchmarkSoftwareFractionalScaled(b *testing.B) {
	s, _ := NewSoftware(1280, 720)
	tex, _ := s.CreateRGBA(opaqueRGBA(256, 256))
	dst := Rect{X: 100.25, Y: 100.5, W: 384.25, H: 384.75}
	s.Draw(tex, nil, dst)
	s.Draw(tex, nil, dst)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Draw(tex, nil, dst)
	}
}

func BenchmarkSoftwareMovingScaled(b *testing.B) {
	s, _ := NewSoftware(1280, 720)
	tex, _ := s.CreateRGBA(opaqueRGBA(256, 256))
	dst := Rect{X: 99.25, Y: 100.5, W: 384.25, H: 384.75}
	s.Draw(tex, nil, dst)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dst.X = float32(i%100) + 100.25
		s.Draw(tex, nil, dst)
	}
}

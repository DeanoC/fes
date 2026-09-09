package tenfoot

import (
	"image"
	"image/color"
)

// FauxBox projects a 2D cover into a cheap 3/4 box: a receding front face
// plus a darkened left spine. Transparent padding fills the unused corners.
// Nil or empty src returns nil. The source image is not mutated.
func FauxBox(src *image.RGBA) *image.RGBA {
	if src == nil {
		return nil
	}
	b := src.Bounds()
	sw, sh := b.Dx(), b.Dy()
	if sw < 1 || sh < 1 {
		return nil
	}
	spine := sw / 8
	if spine < 4 {
		spine = 4
	}
	if spine > sw/3 {
		spine = sw / 3
		if spine < 1 {
			spine = 1
		}
	}
	taper := sh / 10
	if taper < 2 {
		taper = 2
	}
	outW := sw + spine
	outH := sh
	dst := image.NewRGBA(image.Rect(0, 0, outW, outH))
	denY := outH - 1
	if denY < 1 {
		denY = 1
	}
	for y := 0; y < outH; y++ {
		t := float64(y) / float64(denY)
		rightCut := int(float64(taper) * (1 - t) * float64(sw) / float64(sh))
		if rightCut < 0 {
			rightCut = 0
		}
		frontL := spine
		frontR := outW - 1 - rightCut
		if frontR < frontL {
			frontR = frontL
		}
		frontW := frontR - frontL + 1
		srcY := b.Min.Y + y*(sh-1)/denY
		if srcY >= b.Max.Y {
			srcY = b.Max.Y - 1
		}
		spineInset := int(float64(spine/2) * (1 - t))
		left := src.RGBAAt(b.Min.X, srcY)
		dark := color.RGBA{R: uint8(int(left.R) * 5 / 12), G: uint8(int(left.G) * 5 / 12), B: uint8(int(left.B) * 5 / 12), A: left.A}
		for x := spineInset; x < frontL; x++ {
			dst.SetRGBA(x, y, dark)
		}
		denX := frontW - 1
		if denX < 1 {
			denX = 1
		}
		for x := frontL; x <= frontR; x++ {
			srcX := b.Min.X
			if sw > 1 {
				srcX = b.Min.X + (x-frontL)*(sw-1)/denX
			}
			if srcX >= b.Max.X {
				srcX = b.Max.X - 1
			}
			dst.SetRGBA(x, y, src.RGBAAt(srcX, srcY))
		}
	}
	return dst
}

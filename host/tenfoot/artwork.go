package tenfoot

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	_ "image/jpeg"
	_ "image/png"
)

const (
	coverMaxW = 256
	coverMaxH = 320
)

// DecodeCover decodes JPEG/PNG artwork and scales it to the cover cell.
func DecodeCover(data []byte) (*image.RGBA, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("artwork is empty")
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	return scaleToFit(img, coverMaxW, coverMaxH), nil
}

func scaleToFit(src image.Image, maxW, maxH int) *image.RGBA {
	b := src.Bounds()
	srcW, srcH := b.Dx(), b.Dy()
	dstW, dstH := fitSize(srcW, srcH, maxW, maxH)
	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	if srcW == dstW && srcH == dstH {
		draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Src)
		return dst
	}
	for y := 0; y < dstH; y++ {
		srcY := b.Min.Y + y*srcH/dstH
		for x := 0; x < dstW; x++ {
			srcX := b.Min.X + x*srcW/dstW
			dst.Set(x, y, src.At(srcX, srcY))
		}
	}
	return dst
}

func fitSize(srcW, srcH, maxW, maxH int) (int, int) {
	if srcW <= 0 || srcH <= 0 {
		return 1, 1
	}
	if srcW <= maxW && srcH <= maxH {
		return srcW, srcH
	}
	xScale := float64(maxW) / float64(srcW)
	yScale := float64(maxH) / float64(srcH)
	scale := xScale
	if yScale < xScale {
		scale = yScale
	}
	w := int(float64(srcW)*scale + 0.5)
	h := int(float64(srcH)*scale + 0.5)
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	return w, h
}

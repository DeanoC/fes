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
	shotMaxW  = 640
	shotMaxH  = 360
	stillMaxW = 1280
	stillMaxH = 720
	// Source decode limits. Cover cells are smaller; these bound allocations
	// before image.Decode reads full pixels. Matches host artwork policy.
	coverDecodeMaxEdge   = 4096
	coverDecodeMaxPixels = 16_777_216
)

// DecodeCover decodes JPEG/PNG artwork and scales it to the cover cell.
func DecodeCover(data []byte) (*image.RGBA, error) {
	return decodeArtwork(data, coverMaxW, coverMaxH)
}

// DecodeStill decodes JPEG/PNG attract artwork and scales it to a 720p-class stage.
func DecodeStill(data []byte) (*image.RGBA, error) {
	return decodeArtwork(data, stillMaxW, stillMaxH)
}

// DecodeScreenshot decodes JPEG/PNG screenshot artwork for the detail carousel.
func DecodeScreenshot(data []byte) (*image.RGBA, error) {
	return decodeArtwork(data, shotMaxW, shotMaxH)
}

func decodeArtwork(data []byte, maxW, maxH int) (*image.RGBA, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("artwork is empty")
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if cfg.Width < 1 || cfg.Height < 1 {
		return nil, fmt.Errorf("artwork dimensions are invalid")
	}
	pixels := int64(cfg.Width) * int64(cfg.Height)
	if cfg.Width > coverDecodeMaxEdge || cfg.Height > coverDecodeMaxEdge || pixels > coverDecodeMaxPixels {
		return nil, fmt.Errorf("artwork dimensions %dx%d exceed limit", cfg.Width, cfg.Height)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	return scaleToFit(img, maxW, maxH), nil
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

// CoverDestRect returns a centered, aspect-preserving destination inside a
// cover cell. texW/texH are the stored texture pixel size.
func CoverDestRect(cellX, cellY, cellW, cellH, texW, texH int) (x, y, w, h float32) {
	cw, ch := float32(cellW), float32(cellH)
	if cw < 1 {
		cw = 1
	}
	if ch < 1 {
		ch = 1
	}
	if texW < 1 || texH < 1 {
		return float32(cellX), float32(cellY), cw, ch
	}
	tw, th := float32(texW), float32(texH)
	scale := cw / tw
	if ch/th < scale {
		scale = ch / th
	}
	w = tw * scale
	h = th * scale
	x = float32(cellX) + (cw-w)/2
	y = float32(cellY) + (ch-h)/2
	return x, y, w, h
}

func coverDestRect(cellX, cellY, cellW, cellH, texW, texH int) (x, y, w, h float32) {
	return CoverDestRect(cellX, cellY, cellW, cellH, texW, texH)
}

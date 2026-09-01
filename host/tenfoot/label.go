package tenfoot

import (
	"image"
	"image/color"
	"os"
	"strings"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

var (
	labelMu          sync.Mutex
	labelTTF         []byte
	labelTTFOverride []byte
	labelFaces       = map[int]font.Face{}
)

func loadLabelTTF() []byte {
	if len(labelTTFOverride) > 0 {
		return labelTTFOverride
	}
	for _, path := range []string{
		"/System/Library/Fonts/Supplemental/Arial Unicode.ttf",
		"/System/Library/Fonts/Supplemental/Arial.ttf",
		"/System/Library/Fonts/Geneva.ttf",
	} {
		data, err := os.ReadFile(path)
		if err == nil && len(data) > 0 {
			if _, err := opentype.Parse(data); err == nil {
				return data
			}
		}
	}
	return goregular.TTF
}

func labelFace(sizePx int) font.Face {
	if sizePx < 1 {
		sizePx = 1
	}
	if face, ok := labelFaces[sizePx]; ok {
		return face
	}
	if len(labelTTF) == 0 {
		labelTTF = loadLabelTTF()
	}
	parsed, err := opentype.Parse(labelTTF)
	if err != nil {
		parsed, err = opentype.Parse(goregular.TTF)
		if err != nil {
			return nil
		}
	}
	face, err := opentype.NewFace(parsed, &opentype.FaceOptions{
		Size:    float64(sizePx),
		DPI:     72,
		Hinting: font.HintingFull,
	})
	if err != nil {
		return nil
	}
	labelFaces[sizePx] = face
	return face
}

// rasterizeLabel draws UTF-8 text with a real font, clipped to maxWidth pixels.
func rasterizeLabel(text string, maxWidth, sizePx int) *image.RGBA {
	text = strings.TrimSpace(text)
	if text == "" || maxWidth < 1 || sizePx < 1 {
		return nil
	}
	labelMu.Lock()
	defer labelMu.Unlock()
	face := labelFace(sizePx)
	if face == nil {
		return nil
	}
	display := fitLabel(face, text, maxWidth)
	if display == "" {
		return nil
	}
	w := font.MeasureString(face, display).Ceil()
	if w < 1 {
		w = 1
	}
	if w > maxWidth {
		w = maxWidth
	}
	metrics := face.Metrics()
	h := (metrics.Ascent + metrics.Descent).Ceil()
	if h < 1 {
		h = sizePx
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	d := font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(color.RGBA{R: 236, G: 240, B: 248, A: 255}),
		Face: face,
		Dot:  fixed.Point26_6{X: 0, Y: metrics.Ascent},
	}
	d.DrawString(display)
	unpremultiplyRGBA(img)
	return img
}

// unpremultiplyRGBA converts Go image.RGBA (premultiplied) to straight alpha
// for SDL_BLENDMODE_BLEND. Anti-aliased edges otherwise composite too dark.
func unpremultiplyRGBA(img *image.RGBA) {
	if img == nil {
		return
	}
	pix := img.Pix
	for i := 0; i+3 < len(pix); i += 4 {
		a := pix[i+3]
		if a == 0 || a == 255 {
			continue
		}
		pix[i+0] = uint8((int(pix[i+0])*255 + int(a)/2) / int(a))
		pix[i+1] = uint8((int(pix[i+1])*255 + int(a)/2) / int(a))
		pix[i+2] = uint8((int(pix[i+2])*255 + int(a)/2) / int(a))
	}
}

func fitLabel(face font.Face, text string, maxWidth int) string {
	if font.MeasureString(face, text).Ceil() <= maxWidth {
		return text
	}
	runes := []rune(text)
	ellipsis := "..."
	for len(runes) > 0 {
		if len(runes) == 1 {
			return string(runes)
		}
		runes = runes[:len(runes)-1]
		candidate := string(runes) + ellipsis
		if font.MeasureString(face, candidate).Ceil() <= maxWidth {
			return candidate
		}
	}
	return ""
}

func labelHasInk(img *image.RGBA) bool {
	if img == nil {
		return false
	}
	for i := 3; i < len(img.Pix); i += 4 {
		if img.Pix[i] != 0 {
			return true
		}
	}
	return false
}

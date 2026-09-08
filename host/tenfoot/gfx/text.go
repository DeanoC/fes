package gfx

import (
	"image"
	"image/color"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// GlyphPx is the DebugText glyph size. Theme HeaderScale / LabelScale /
// StatusScale map to pixel size GlyphPx*scale so existing theme JSON keeps
// the same hierarchy without a font-family picker.
const GlyphPx = 8

// ScalePx maps a theme *Scale token to a UI-face pixel size.
func ScalePx(scale int) int {
	if scale < 1 {
		scale = 1
	}
	return GlyphPx * scale
}

var (
	uiMu    sync.Mutex
	uiTTF   []byte
	uiFont  *opentype.Font
	uiFaces = map[int]font.Face{}
	uiReady bool
)

func uiTTFBytes() []byte {
	if len(uiTTF) > 0 {
		return uiTTF
	}
	return goregular.TTF
}

func ensureUIFont() *opentype.Font {
	if uiReady && uiFont != nil {
		return uiFont
	}
	uiReady = true
	parsed, err := opentype.Parse(uiTTFBytes())
	if err != nil || parsed == nil {
		return nil
	}
	uiFont = parsed
	return uiFont
}

func uiFaceLocked(sizePx int) font.Face {
	if sizePx < 1 {
		sizePx = 1
	}
	if face, ok := uiFaces[sizePx]; ok {
		return face
	}
	parsed := ensureUIFont()
	if parsed == nil {
		return nil
	}
	face, err := opentype.NewFace(parsed, &opentype.FaceOptions{
		Size:    float64(sizePx),
		DPI:     72,
		Hinting: font.HintingFull,
	})
	if err != nil {
		return nil
	}
	uiFaces[sizePx] = face
	return face
}

// MeasureText returns the pixel width of text at sizePx in the embedded UI
// face. Empty text is 0.
func MeasureText(text string, sizePx int) int {
	if text == "" {
		return 0
	}
	uiMu.Lock()
	defer uiMu.Unlock()
	face := uiFaceLocked(sizePx)
	if face == nil {
		return 0
	}
	return font.MeasureString(face, text).Ceil()
}

// TextHeight is the pixel line height (ascent+descent) of the UI face at
// sizePx. It is used to vertically center chrome labels.
func TextHeight(sizePx int) int {
	uiMu.Lock()
	defer uiMu.Unlock()
	face := uiFaceLocked(sizePx)
	if face == nil {
		if sizePx < 1 {
			return 1
		}
		return sizePx
	}
	m := face.Metrics()
	h := (m.Ascent + m.Descent).Ceil()
	if h < 1 {
		if sizePx < 1 {
			return 1
		}
		return sizePx
	}
	return h
}

// FitText truncates text with an ASCII ellipsis so its UI-face width is at
// most maxWidth pixels. maxWidth < 1 leaves the string unchanged.
func FitText(text string, sizePx, maxWidth int) string {
	if text == "" || maxWidth < 1 {
		return text
	}
	uiMu.Lock()
	defer uiMu.Unlock()
	face := uiFaceLocked(sizePx)
	if face == nil {
		return text
	}
	return fitText(face, text, maxWidth)
}

func fitText(face font.Face, text string, maxWidth int) string {
	if font.MeasureString(face, text).Ceil() <= maxWidth {
		return text
	}
	runes := []rune(text)
	const ellipsis = "..."
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

// RasterizeText draws text in c with the embedded UI face. maxWidth > 0
// truncates with an ellipsis. The result is straight-alpha RGBA so Software
// blendOver and SDL blend match. Nil means nothing to draw.
func RasterizeText(text string, sizePx int, c Color, maxWidth int) *image.RGBA {
	if text == "" {
		return nil
	}
	if sizePx < 1 {
		sizePx = 1
	}
	uiMu.Lock()
	defer uiMu.Unlock()
	face := uiFaceLocked(sizePx)
	if face == nil {
		return nil
	}
	display := text
	if maxWidth > 0 {
		display = fitText(face, text, maxWidth)
	}
	if display == "" {
		return nil
	}
	bounds, advance := font.BoundString(face, display)
	left := bounds.Min.X.Floor()
	top := bounds.Min.Y.Floor()
	w := bounds.Max.X.Ceil() - left
	h := bounds.Max.Y.Ceil() - top
	if adv := advance.Ceil() - left; adv > w {
		w = adv
	}
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = sizePx
	}
	// Glyph masks can extend one pixel past the integer bounds.
	w++
	h++
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	src := straightToPremult(c)
	d := font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(src),
		Face: face,
		Dot:  fixed.Point26_6{X: -bounds.Min.X, Y: -bounds.Min.Y},
	}
	d.DrawString(display)
	unpremultiplyRGBA(img)
	if maxWidth > 0 && img.Bounds().Dx() > maxWidth {
		cropped := image.NewRGBA(image.Rect(0, 0, maxWidth, img.Bounds().Dy()))
		for y := 0; y < cropped.Bounds().Dy(); y++ {
			copy(cropped.Pix[y*cropped.Stride:(y+1)*cropped.Stride], img.Pix[y*img.Stride:y*img.Stride+cropped.Stride])
		}
		img = cropped
	}
	return img
}

func straightToPremult(c Color) color.RGBA {
	a := c.A
	if a == 0 {
		return color.RGBA{}
	}
	if a == 255 {
		return color.RGBA{R: c.R, G: c.G, B: c.B, A: 255}
	}
	return color.RGBA{
		R: uint8(uint32(c.R) * uint32(a) / 255),
		G: uint8(uint32(c.G) * uint32(a) / 255),
		B: uint8(uint32(c.B) * uint32(a) / 255),
		A: a,
	}
}

// unpremultiplyRGBA converts Go image.RGBA (premultiplied) to straight alpha
// for blendOver / SDL_BLENDMODE_BLEND. Anti-aliased edges otherwise composite
// too dark.
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

func textHasInk(img *image.RGBA) bool {
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

func blitText(s *Software, x, y int, img *image.RGBA) {
	if s == nil || img == nil {
		return
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w < 1 || h < 1 {
		return
	}
	bm, err := cloneBitmap(img)
	if err != nil {
		return
	}
	dst := Rect{X: float32(x), Y: float32(y), W: float32(w), H: float32(h)}
	x0, y0, x1, y1, ok := clipRect(dst, s.w, s.h)
	if !ok {
		return
	}
	s.blitCopy(bm, 0, 0, x0, y0, x1, y1, x, y)
}

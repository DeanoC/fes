package gfx

import (
	"image"
	"image/color"
	"strings"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// GlyphPx is the DebugText glyph size. Theme HeaderScale / LabelScale /
// StatusScale map to pixel size GlyphPx*scale when the matching title_px /
// body_px / caption_px / status_px role is unset, so existing theme JSON
// keeps the same hierarchy without a font-family picker.
const GlyphPx = 8

// ScalePx maps a theme *Scale token to a UI-face pixel size. Prefer
// theme.TitlePx and the other role helpers at paint sites.
func ScalePx(scale int) int {
	if scale < 1 {
		scale = 1
	}
	return GlyphPx * scale
}

type faceKey struct {
	size   int
	weight Weight
}

var (
	uiMu      sync.Mutex
	uiTTF     []byte
	uiBoldTTF []byte
	uiFonts   = map[Weight]*opentype.Font{}
	uiFaces   = map[faceKey]font.Face{}
)

func uiTTFBytes(w Weight) []byte {
	if NormalizeWeight(w) == WeightBold {
		if len(uiBoldTTF) > 0 {
			return uiBoldTTF
		}
		return gobold.TTF
	}
	if len(uiTTF) > 0 {
		return uiTTF
	}
	return goregular.TTF
}

func ensureUIFont(w Weight) *opentype.Font {
	w = NormalizeWeight(w)
	if parsed, ok := uiFonts[w]; ok && parsed != nil {
		return parsed
	}
	parsed, err := opentype.Parse(uiTTFBytes(w))
	if err != nil || parsed == nil {
		return nil
	}
	uiFonts[w] = parsed
	return parsed
}

func uiFaceLocked(sizePx int, w Weight) font.Face {
	if sizePx < 1 {
		sizePx = 1
	}
	w = NormalizeWeight(w)
	key := faceKey{size: sizePx, weight: w}
	if face, ok := uiFaces[key]; ok {
		return face
	}
	parsed := ensureUIFont(w)
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
	uiFaces[key] = face
	return face
}

// MeasureText returns the pixel width of text at sizePx in the Regular UI
// face. Empty text is 0.
func MeasureText(text string, sizePx int) int {
	return MeasureTextWeight(text, sizePx, WeightRegular)
}

// MeasureTextWeight is MeasureText with an explicit face weight.
func MeasureTextWeight(text string, sizePx int, w Weight) int {
	if text == "" {
		return 0
	}
	uiMu.Lock()
	defer uiMu.Unlock()
	face := uiFaceLocked(sizePx, w)
	if face == nil {
		return 0
	}
	return font.MeasureString(face, text).Ceil()
}

// TextHeight is the pixel line height (ascent+descent) of the Regular UI
// face at sizePx. It is used to vertically center chrome labels.
func TextHeight(sizePx int) int {
	return TextHeightWeight(sizePx, WeightRegular)
}

// TextHeightWeight is TextHeight with an explicit face weight.
func TextHeightWeight(sizePx int, w Weight) int {
	uiMu.Lock()
	defer uiMu.Unlock()
	face := uiFaceLocked(sizePx, w)
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

// FitText truncates text with an ASCII ellipsis so its Regular UI-face
// width is at most maxWidth pixels. maxWidth < 1 leaves the string unchanged.
func FitText(text string, sizePx, maxWidth int) string {
	return FitTextWeight(text, sizePx, maxWidth, WeightRegular)
}

// FitTextWeight is FitText with an explicit face weight.
func FitTextWeight(text string, sizePx, maxWidth int, w Weight) string {
	if text == "" || maxWidth < 1 {
		return text
	}
	uiMu.Lock()
	defer uiMu.Unlock()
	face := uiFaceLocked(sizePx, w)
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

// WrapText word-wraps text to maxWidth pixels in the Regular UI face.
// maxLines < 1 keeps every line; otherwise the last kept line is truncated
// with an ellipsis. Empty text returns nil.
func WrapText(text string, sizePx, maxWidth, maxLines int) []string {
	return WrapTextWeight(text, sizePx, maxWidth, maxLines, WeightRegular)
}

// WrapTextWeight is WrapText with an explicit face weight.
func WrapTextWeight(text string, sizePx, maxWidth, maxLines int, w Weight) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if maxWidth < 1 {
		return []string{text}
	}
	uiMu.Lock()
	defer uiMu.Unlock()
	face := uiFaceLocked(sizePx, w)
	if face == nil {
		return []string{text}
	}
	return wrapText(face, text, maxWidth, maxLines)
}

func wrapText(face font.Face, text string, maxWidth, maxLines int) []string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return nil
	}
	lines := make([]string, 0, 4)
	var cur string
	flush := func() {
		if cur == "" {
			return
		}
		lines = append(lines, cur)
		cur = ""
	}
	for _, word := range words {
		if font.MeasureString(face, word).Ceil() > maxWidth {
			flush()
			word = fitText(face, word, maxWidth)
			if word == "" {
				continue
			}
			lines = append(lines, word)
			continue
		}
		next := word
		if cur != "" {
			next = cur + " " + word
		}
		if font.MeasureString(face, next).Ceil() <= maxWidth {
			cur = next
			continue
		}
		flush()
		cur = word
	}
	flush()
	if maxLines < 1 || len(lines) <= maxLines {
		return lines
	}
	kept := append([]string{}, lines[:maxLines-1]...)
	rest := strings.Join(lines[maxLines-1:], " ")
	if last := fitText(face, rest, maxWidth); last != "" {
		kept = append(kept, last)
	}
	if len(kept) == 0 {
		return nil
	}
	return kept
}

// RasterizeText draws text in c with the Regular UI face. maxWidth > 0
// truncates with an ellipsis. The result is straight-alpha RGBA so Software
// blendOver and SDL blend match. Nil means nothing to draw.
func RasterizeText(text string, sizePx int, c Color, maxWidth int) *image.RGBA {
	return RasterizeTextWeight(text, sizePx, WeightRegular, c, maxWidth)
}

// RasterizeTextWeight is RasterizeText with an explicit face weight.
func RasterizeTextWeight(text string, sizePx int, w Weight, c Color, maxWidth int) *image.RGBA {
	if text == "" {
		return nil
	}
	if sizePx < 1 {
		sizePx = 1
	}
	uiMu.Lock()
	defer uiMu.Unlock()
	face := uiFaceLocked(sizePx, w)
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
	width := bounds.Max.X.Ceil() - left
	h := bounds.Max.Y.Ceil() - top
	if adv := advance.Ceil() - left; adv > width {
		width = adv
	}
	if width < 1 {
		width = 1
	}
	if h < 1 {
		h = sizePx
	}
	// Glyph masks can extend one pixel past the integer bounds.
	width++
	h++
	img := image.NewRGBA(image.Rect(0, 0, width, h))
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

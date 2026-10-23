package gfx

import (
	"image"
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

// UIText lays text out in the UI face of weight w at sizePx, fitted with an
// ellipsis to maxWidth when maxWidth > 0. It returns the ink as a mask, nil
// when there is none, and the part of the mask to draw: at most maxWidth
// wide. DrawText draws that part with its top-left at the draw point. width
// is the text's box from the ink's left edge to the pen's advance or the
// ink's right edge, whichever is further, at most maxWidth: what alignment
// measures, trailing spaces included.
func UIText(text string, sizePx int, w Weight, maxWidth int) (m *Mask, src image.Rectangle, width int) {
	if text == "" {
		return nil, image.Rectangle{}, 0
	}
	uiMu.Lock()
	defer uiMu.Unlock()
	face := uiFaceLocked(sizePx, w)
	if face == nil {
		return nil, image.Rectangle{}, 0
	}
	display := text
	if maxWidth > 0 {
		display = fitText(face, text, maxWidth)
	}
	line := LayoutText(face, display)
	if line.Mask == nil {
		return nil, image.Rectangle{}, 0
	}
	src = line.Mask.Bounds()
	width = max(line.Mask.W, line.Advance-line.Origin.X)
	if maxWidth > 0 {
		src.Max.X = min(src.Max.X, maxWidth)
		width = min(width, maxWidth)
	}
	return line.Mask, src, width
}

// TextLine is text laid out along a baseline in one face.
type TextLine struct {
	// Mask is the ink, nil when the text has none.
	Mask *Mask
	// Origin is where the mask's top-left sits from the pen's start on
	// the baseline.
	Origin image.Point
	// Advance is how far the pen moved.
	Advance int
}

// LayoutText lays text out in face from cached glyph masks, as font.Drawer
// would draw it from an integer pen. Under full hinting every advance and
// kern is whole pixels, so each glyph is rasterized once per face. Where
// glyphs overlap their coverage combines as m1 + m2·(255−m1)/255. The
// caller holds whatever lock guards face; faces are cached by their makers,
// so a face's identity keys its glyphs and lines.
func LayoutText(face font.Face, text string) TextLine {
	key := lineKey{face, text}
	textMu.Lock()
	defer textMu.Unlock()
	if line, ok := textLines.get(key); ok {
		return line
	}
	type placed struct {
		g  *glyph
		at image.Point
	}
	glyphs := make([]placed, 0, len(text))
	var ink image.Rectangle
	dot := fixed.Int26_6(0)
	prev := rune(-1)
	for _, r := range text {
		if prev >= 0 {
			dot += face.Kern(prev, r)
		}
		prev = r
		g := glyphLocked(face, r)
		at := image.Pt(dot.Round(), 0)
		dot += g.advance
		if g.rect.Empty() {
			continue
		}
		glyphs = append(glyphs, placed{g, at})
		ink = ink.Union(g.rect.Add(at))
	}
	line := TextLine{Advance: dot.Round()}
	if !ink.Empty() {
		m := &Mask{W: ink.Dx(), H: ink.Dy(), Pix: make([]byte, ink.Dx()*ink.Dy())}
		for _, p := range glyphs {
			r := p.g.rect.Add(p.at).Sub(ink.Min)
			for y := 0; y < r.Dy(); y++ {
				row := m.Pix[(r.Min.Y+y)*m.W+r.Min.X:]
				for x, m2 := range p.g.pix[y*r.Dx() : (y+1)*r.Dx()] {
					m1 := uint32(row[x])
					row[x] = uint8(m1 + uint32(m2)*(255-m1)/255)
				}
			}
		}
		line.Mask, line.Origin = m, ink.Min
	}
	textLines.put(key, line, len(text)+line.bytes())
	return line
}

// glyph is one rune's coverage at a pen on the origin, and its advance.
type glyph struct {
	rect    image.Rectangle
	pix     []byte
	advance fixed.Int26_6
}

type glyphKey struct {
	face font.Face
	r    rune
}

type lineKey struct {
	face font.Face
	text string
}

func (l TextLine) bytes() int {
	if l.Mask == nil {
		return 0
	}
	return len(l.Mask.Pix)
}

// maxGlyphs bounds the glyph cache; past it the cache starts over.
const maxGlyphs = 8192

var (
	// textMu guards the glyph and line caches.
	textMu    sync.Mutex
	glyphs    = map[glyphKey]*glyph{}
	textLines = newLRU[lineKey, TextLine](2048, 4<<20)
)

func glyphLocked(face font.Face, r rune) *glyph {
	key := glyphKey{face, r}
	if g, ok := glyphs[key]; ok {
		return g
	}
	// Like font.Drawer, draw what the face returns for a rune it lacks:
	// its .notdef box.
	dr, mask, maskp, advance, _ := face.Glyph(fixed.Point26_6{}, r)
	if mask == nil {
		dr = image.Rectangle{}
	}
	g := &glyph{rect: dr, pix: make([]byte, dr.Dx()*dr.Dy()), advance: advance}
	for y := 0; y < dr.Dy(); y++ {
		for x := 0; x < dr.Dx(); x++ {
			_, _, _, a := mask.At(maskp.X+x, maskp.Y+y).RGBA()
			g.pix[y*dr.Dx()+x] = uint8(a >> 8)
		}
	}
	if len(glyphs) >= maxGlyphs {
		clear(glyphs)
	}
	glyphs[key] = g
	return g
}

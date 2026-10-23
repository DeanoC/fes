package tenfoot

import (
	"image"
	"os"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"

	"github.com/DeanoC/FogCast/ui/gfx"
)

var (
	labelMu          sync.Mutex
	labelTTFOverride []byte
	labelFonts       []*opentype.Font
	labelPathsLeft   []string
	labelReady       bool
	labelFaces       = map[int]font.Face{}
	labelSizeFaces   = map[int][]font.Face{}
)

// Mac system fonts first, then common Linux Noto/DejaVu paths. TTC collections
// contribute their first parseable face; later faces are not loaded.
var labelFontPaths = []string{
	"/System/Library/Fonts/Supplemental/Arial Unicode.ttf",
	"/Library/Fonts/Arial Unicode.ttf",
	"/System/Library/Fonts/Supplemental/Arial.ttf",
	"/System/Library/Fonts/Geneva.ttf",
	"/System/Library/Fonts/Hiragino Sans GB.ttc",
	"/System/Library/Fonts/AppleSDGothicNeo.ttc",
	"/System/Library/Fonts/PingFang.ttc",
	"/System/Library/Fonts/Supplemental/Songti.ttc",
	"/System/Library/Fonts/GeezaPro.ttc",
	"/System/Library/Fonts/Kohinoor.ttc",
	"/System/Library/Fonts/ArialHB.ttc",
	"/System/Library/Fonts/ThonburiUI.ttc",
	"/usr/share/fonts/truetype/noto/NotoSansCJK-Regular.ttc",
	"/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc",
	"/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
}

func parseFirstFont(data []byte) *opentype.Font {
	if len(data) == 0 {
		return nil
	}
	if col, err := opentype.ParseCollection(data); err == nil {
		for i := 0; i < col.NumFonts(); i++ {
			parsed, err := col.Font(i)
			if err == nil && parsed != nil {
				return parsed
			}
		}
	}
	parsed, err := opentype.Parse(data)
	if err != nil {
		return nil
	}
	return parsed
}

func appendLabelFont(parsed *opentype.Font) {
	if parsed == nil {
		return
	}
	labelFonts = append(labelFonts, parsed)
}

func loadNextLabelFont() bool {
	for len(labelPathsLeft) > 0 {
		path := labelPathsLeft[0]
		labelPathsLeft = labelPathsLeft[1:]
		data, err := os.ReadFile(path)
		if err != nil || len(data) == 0 {
			continue
		}
		parsed := parseFirstFont(data)
		if parsed == nil {
			continue
		}
		appendLabelFont(parsed)
		return true
	}
	return false
}

func ensureLabelFonts() {
	if labelReady {
		return
	}
	labelReady = true
	labelPathsLeft = append([]string(nil), labelFontPaths...)
	if len(labelTTFOverride) > 0 {
		appendLabelFont(parseFirstFont(labelTTFOverride))
	}
	if len(labelFonts) == 0 {
		for loadNextLabelFont() {
			break
		}
	}
	if len(labelFonts) == 0 {
		// Same embedded Go Regular face the kit gfx.DrawText path uses when
		// the target has no system fonts.
		appendLabelFont(parseFirstFont(goregular.TTF))
	}
}

func facesAtSize(sizePx int) []font.Face {
	faces := labelSizeFaces[sizePx]
	if len(faces) == len(labelFonts) {
		return faces
	}
	opts := &opentype.FaceOptions{
		Size:    float64(sizePx),
		DPI:     72,
		Hinting: font.HintingFull,
	}
	for i := len(faces); i < len(labelFonts); i++ {
		face, err := opentype.NewFace(labelFonts[i], opts)
		if err != nil {
			continue
		}
		faces = append(faces, face)
	}
	labelSizeFaces[sizePx] = faces
	return faces
}

type fallbackFace struct {
	sizePx int
}

func (f *fallbackFace) Close() error { return nil }

func (f *fallbackFace) faceFor(r rune) font.Face {
	for {
		faces := facesAtSize(f.sizePx)
		for _, face := range faces {
			if _, ok := face.GlyphAdvance(r); ok {
				return face
			}
		}
		if !loadNextLabelFont() {
			if len(faces) > 0 {
				return faces[0]
			}
			return nil
		}
	}
}

func (f *fallbackFace) Glyph(dot fixed.Point26_6, r rune) (dr image.Rectangle, mask image.Image, maskp image.Point, advance fixed.Int26_6, ok bool) {
	face := f.faceFor(r)
	if face == nil {
		return image.Rectangle{}, nil, image.Point{}, 0, false
	}
	return face.Glyph(dot, r)
}

func (f *fallbackFace) GlyphBounds(r rune) (bounds fixed.Rectangle26_6, advance fixed.Int26_6, ok bool) {
	face := f.faceFor(r)
	if face == nil {
		return fixed.Rectangle26_6{}, 0, false
	}
	return face.GlyphBounds(r)
}

func (f *fallbackFace) GlyphAdvance(r rune) (advance fixed.Int26_6, ok bool) {
	face := f.faceFor(r)
	if face == nil {
		return 0, false
	}
	return face.GlyphAdvance(r)
}

func (f *fallbackFace) Kern(r0, r1 rune) fixed.Int26_6 {
	a := f.faceFor(r0)
	b := f.faceFor(r1)
	if a == nil || a != b {
		return 0
	}
	return a.Kern(r0, r1)
}

func (f *fallbackFace) Metrics() font.Metrics {
	faces := facesAtSize(f.sizePx)
	if len(faces) == 0 {
		return font.Metrics{}
	}
	m := faces[0].Metrics()
	for _, face := range faces[1:] {
		om := face.Metrics()
		if om.Ascent > m.Ascent {
			m.Ascent = om.Ascent
		}
		if om.Descent > m.Descent {
			m.Descent = om.Descent
		}
		if om.Height > m.Height {
			m.Height = om.Height
		}
		if om.XHeight > m.XHeight {
			m.XHeight = om.XHeight
		}
		if om.CapHeight > m.CapHeight {
			m.CapHeight = om.CapHeight
		}
	}
	return m
}

func labelFace(sizePx int) font.Face {
	if sizePx < 1 {
		sizePx = 1
	}
	if face, ok := labelFaces[sizePx]; ok {
		return face
	}
	ensureLabelFonts()
	if len(labelFonts) == 0 {
		return nil
	}
	face := &fallbackFace{sizePx: sizePx}
	labelFaces[sizePx] = face
	return face
}

// labelCacheKey binds a texture slot to the exact rasterized string and size so
// chrome and detail labels cannot reuse a stale texture after text changes.
func labelCacheKey(slot, text string, maxW, sizePx int) string {
	return strings.Join([]string{slot, strconv.Itoa(maxW), strconv.Itoa(sizePx), strings.TrimSpace(text)}, "\x1f")
}

func measureLabel(text string, sizePx int) int {
	text = strings.TrimSpace(text)
	if text == "" || sizePx < 1 {
		return 0
	}
	labelMu.Lock()
	defer labelMu.Unlock()
	face := labelFace(sizePx)
	if face == nil {
		return 0
	}
	return font.MeasureString(face, text).Ceil()
}

// labelColor is the colour labels draw in.
var labelColor = gfx.RGB(236, 240, 248)

// layoutLabel lays display out in face as a label: a box at most maxWidth
// wide and one line tall, with the baseline at the ascent. Ink outside the
// box is clipped. It reports false for an empty string.
func layoutLabel(face font.Face, display string, maxWidth, sizePx int) (gpuTexture, bool) {
	if face == nil || display == "" {
		return gpuTexture{}, false
	}
	w := min(max(font.MeasureString(face, display).Ceil(), 1), maxWidth)
	metrics := face.Metrics()
	h := (metrics.Ascent + metrics.Descent).Ceil()
	if h < 1 {
		h = sizePx
	}
	label := gpuTexture{w: w, h: h}
	line := gfx.LayoutText(face, display)
	if line.Mask == nil {
		return label, true
	}
	ink := line.Mask.Bounds().Add(line.Origin).Add(image.Pt(0, metrics.Ascent.Round()))
	visible := ink.Intersect(image.Rect(0, 0, w, h))
	if visible.Empty() {
		return label, true
	}
	label.mask, label.maskAt = line.Mask, visible.Min
	label.maskSrc = visible.Sub(ink.Min)
	return label, true
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

func labelHasRealGlyph(r rune) bool {
	labelMu.Lock()
	defer labelMu.Unlock()
	face := labelFace(16)
	if face == nil {
		return false
	}
	_, ok := face.GlyphAdvance(r)
	return ok
}

package gfx

import (
	"image"
	"math"
	"strings"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
	"golang.org/x/image/vector"
)

// PromptFamily is whose button markings a prompt glyph copies.
type PromptFamily uint8

const (
	// PromptGeneric marks a face button by where it sits, for a pad whose
	// printed labels are unknown.
	PromptGeneric PromptFamily = iota
	PromptXbox
	PromptPlayStation
	PromptNintendo
	promptFamilies
)

// PromptControl is a physical control on a pad. Face buttons are named by
// position, as SDL names them.
type PromptControl uint8

const (
	PromptSouth PromptControl = iota
	PromptEast
	PromptWest
	PromptNorth
	PromptL
	PromptR
	PromptSelect
	PromptStart
	PromptGuide
	PromptDPad
	promptControls
)

// promptBase starts the private-use runes the UI faces draw as button
// glyphs: one rune per family and control.
const promptBase rune = 0xF4E0

// PromptRune is the rune a face from PromptFace draws as control c in
// family f's markings. In a string it measures, wraps, fits and caches like
// any other character.
func PromptRune(f PromptFamily, c PromptControl) rune {
	if f >= promptFamilies {
		f = PromptGeneric
	}
	return promptBase + rune(f)*rune(promptControls) + rune(c%promptControls)
}

func promptOf(r rune) (PromptFamily, PromptControl, bool) {
	i := r - promptBase
	if i < 0 || i >= rune(promptFamilies)*rune(promptControls) {
		return 0, 0, false
	}
	return PromptFamily(i / rune(promptControls)), PromptControl(i % rune(promptControls)), true
}

type promptShape uint8

const (
	shapeLabel promptShape = iota
	shapePill
	shapeCross
	shapeCircle
	shapeSquare
	shapeTriangle
	shapeDotSouth
	shapeDotEast
	shapeDotWest
	shapeDotNorth
	shapeDPad
	shapeHome
)

// promptSpec is one glyph: the word that names it in plain text, and what
// is cut out of its disc or pill.
type promptSpec struct {
	name  string
	shape promptShape
}

var promptSpecs = [promptFamilies][promptControls]promptSpec{
	PromptGeneric: {
		{"bottom button", shapeDotSouth}, {"right button", shapeDotEast}, {"left button", shapeDotWest}, {"top button", shapeDotNorth},
		{"L", shapePill}, {"R", shapePill}, {"SELECT", shapePill}, {"START", shapePill}, {"GUIDE", shapePill}, {"D-pad", shapeDPad},
	},
	PromptXbox: {
		{"A", shapeLabel}, {"B", shapeLabel}, {"X", shapeLabel}, {"Y", shapeLabel},
		{"LB", shapePill}, {"RB", shapePill}, {"VIEW", shapePill}, {"MENU", shapePill}, {"GUIDE", shapePill}, {"D-pad", shapeDPad},
	},
	PromptPlayStation: {
		{"Cross", shapeCross}, {"Circle", shapeCircle}, {"Square", shapeSquare}, {"Triangle", shapeTriangle},
		{"L1", shapePill}, {"R1", shapePill}, {"CREATE", shapePill}, {"OPTIONS", shapePill}, {"PS", shapePill}, {"D-pad", shapeDPad},
	},
	PromptNintendo: {
		{"B", shapeLabel}, {"A", shapeLabel}, {"Y", shapeLabel}, {"X", shapeLabel},
		{"L", shapePill}, {"R", shapePill}, {"-", shapeLabel}, {"+", shapeLabel}, {"HOME", shapeHome}, {"D-pad", shapeDPad},
	},
}

// PromptWords replaces every prompt rune in s with the word for its button,
// for logs, tests and anything that cannot draw glyphs.
func PromptWords(s string) string {
	if !strings.ContainsFunc(s, func(r rune) bool { _, _, ok := promptOf(r); return ok }) {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		if f, c, ok := promptOf(r); ok {
			b.WriteString(promptSpecs[f][c].name)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

var (
	promptFontOnce sync.Once
	promptFont     *opentype.Font
)

// promptFace draws prompt runes as button glyphs and everything else as
// inner does.
type promptFace struct {
	inner font.Face
	// label is the bold face for the letters cut out of a glyph. Like
	// inner, it is guarded by whatever lock guards this face.
	label  font.Face
	size   int
	glyphs map[rune]*promptGlyph
}

type promptGlyph struct {
	mask *image.Alpha
	// top is the mask's top edge from the baseline.
	top int
}

// PromptFace returns inner with the prompt runes added, drawn to sit beside
// text of sizePx. The caller serializes use of the result as it does inner.
func PromptFace(inner font.Face, sizePx int) font.Face {
	if inner == nil {
		return nil
	}
	promptFontOnce.Do(func() { promptFont, _ = opentype.Parse(gobold.TTF) })
	f := &promptFace{inner: inner, size: max(sizePx, 1), glyphs: map[rune]*promptGlyph{}}
	if promptFont != nil {
		f.label, _ = opentype.NewFace(promptFont, &opentype.FaceOptions{
			Size: math.Round(float64(f.diameter()) * 0.66), DPI: 72, Hinting: font.HintingFull,
		})
	}
	return f
}

// diameter is a round glyph's size: a little taller than the text's
// capitals, so it reads as a button at hint sizes.
func (f *promptFace) diameter() int { return f.size + (f.size+4)/8 }

func (f *promptFace) Close() error { return f.inner.Close() }

func (f *promptFace) Metrics() font.Metrics { return f.inner.Metrics() }

func (f *promptFace) Kern(r0, r1 rune) fixed.Int26_6 {
	if _, _, ok := promptOf(r0); ok {
		return 0
	}
	if _, _, ok := promptOf(r1); ok {
		return 0
	}
	return f.inner.Kern(r0, r1)
}

func (f *promptFace) Glyph(dot fixed.Point26_6, r rune) (image.Rectangle, image.Image, image.Point, fixed.Int26_6, bool) {
	g := f.glyph(r)
	if g == nil {
		return f.inner.Glyph(dot, r)
	}
	b := g.mask.Bounds()
	at := image.Pt(dot.X.Round(), dot.Y.Round()+g.top)
	return b.Add(at), g.mask, image.Point{}, fixed.I(b.Dx() + 1), true
}

func (f *promptFace) GlyphBounds(r rune) (fixed.Rectangle26_6, fixed.Int26_6, bool) {
	g := f.glyph(r)
	if g == nil {
		return f.inner.GlyphBounds(r)
	}
	b := g.mask.Bounds()
	return fixed.R(0, g.top, b.Dx(), g.top+b.Dy()), fixed.I(b.Dx() + 1), true
}

func (f *promptFace) GlyphAdvance(r rune) (fixed.Int26_6, bool) {
	g := f.glyph(r)
	if g == nil {
		return f.inner.GlyphAdvance(r)
	}
	return fixed.I(g.mask.Bounds().Dx() + 1), true
}

func (f *promptFace) glyph(r rune) *promptGlyph {
	fam, c, ok := promptOf(r)
	if !ok {
		return nil
	}
	if g, ok := f.glyphs[r]; ok {
		return g
	}
	g := f.draw(promptSpecs[fam][c])
	f.glyphs[r] = g
	return g
}

// draw makes a glyph: a filled disc or pill with its marking cut out, so
// one colour draws it on any surface.
func (f *promptFace) draw(spec promptSpec) *promptGlyph {
	d := f.diameter()
	w, h := d, d
	var cut []byte
	if spec.shape == shapePill {
		h = d - d/7
		textW := f.labelWidth(spec.name)
		w = textW + 2*max(d*3/10, 2)
		cut = make([]byte, w*h)
		f.drawLabel(cut, w, h, spec.name, (w-textW)/2)
	} else {
		cut = f.marking(spec, d)
	}
	body := rasterize(w, h, func(z *vector.Rasterizer) {
		roundRect(z, 0, 0, float32(w), float32(h), float32(h)/2*pillRadius(spec.shape))
	})
	mask := image.NewAlpha(image.Rect(0, 0, w, h))
	for i, v := range body {
		mask.Pix[i] = uint8(uint32(v) * uint32(255-cut[i]) / 255)
	}
	// Sit on the text's capitals: most of the glyph above the baseline,
	// the rest in the descent.
	return &promptGlyph{mask: mask, top: -(d - d/7) + (d-h)/2}
}

// pillRadius is the corner radius as a share of half the height: a disc is
// fully round, a pill is squared off.
func pillRadius(s promptShape) float32 {
	if s == shapePill {
		return 0.5
	}
	return 1
}

func (f *promptFace) labelWidth(text string) int {
	if f.label == nil {
		return f.size * len(text) / 2
	}
	return font.MeasureString(f.label, text).Ceil()
}

// drawLabel adds text to the coverage in pix, w wide and h tall, from pen
// x, centred on its capitals.
func (f *promptFace) drawLabel(pix []byte, w, h int, text string, x int) {
	if f.label == nil {
		return
	}
	m := f.label.Metrics()
	capH := m.CapHeight.Round()
	if capH <= 0 {
		capH = m.Ascent.Round() * 3 / 4
	}
	dot := fixed.P(x, (h+capH)/2)
	if text == "-" || text == "+" {
		// Signs sit at the middle of the lowercase, not the capitals.
		dot.Y = fixed.I(h/2 + m.XHeight.Round()/2)
	}
	for _, r := range text {
		dr, mask, maskp, advance, ok := f.label.Glyph(dot, r)
		dot.X += advance
		if !ok || mask == nil {
			continue
		}
		for y := max(dr.Min.Y, 0); y < min(dr.Max.Y, h); y++ {
			for px := max(dr.Min.X, 0); px < min(dr.Max.X, w); px++ {
				_, _, _, a := mask.At(maskp.X+px-dr.Min.X, maskp.Y+y-dr.Min.Y).RGBA()
				pix[y*w+px] = max(pix[y*w+px], uint8(a>>8))
			}
		}
	}
}

// marking is what is cut out of a d-pixel disc.
func (f *promptFace) marking(spec promptSpec, d int) []byte {
	fd := float32(d)
	c := fd / 2
	stroke := max(fd*0.11, 1.6)
	ring := func(outer, inner func(z *vector.Rasterizer)) []byte {
		pix := rasterize(d, d, outer)
		for i, v := range rasterize(d, d, inner) {
			pix[i] -= min(pix[i], v)
		}
		return pix
	}
	switch spec.shape {
	case shapeCross:
		arm, t := fd*0.25, stroke/2
		return rasterize(d, d, func(z *vector.Rasterizer) {
			for _, s := range [2]float32{1, -1} {
				// A bar along the diagonal (1, s), stroke wide.
				ux, uy := arm*float32(math.Sqrt2)/2, arm*float32(math.Sqrt2)/2*s
				nx, ny := -t*float32(math.Sqrt2)/2*s, t*float32(math.Sqrt2)/2
				polygon(z, c-ux+nx, c-uy+ny, c+ux+nx, c+uy+ny, c+ux-nx, c+uy-ny, c-ux-nx, c-uy-ny)
			}
		})
	case shapeCircle:
		r := fd * 0.29
		return ring(func(z *vector.Rasterizer) { circle(z, c, c, r) }, func(z *vector.Rasterizer) { circle(z, c, c, r-stroke) })
	case shapeSquare:
		r := fd * 0.25
		return ring(
			func(z *vector.Rasterizer) { polygon(z, c-r, c-r, c+r, c-r, c+r, c+r, c-r, c+r) },
			func(z *vector.Rasterizer) {
				r := r - stroke
				polygon(z, c-r, c-r, c+r, c-r, c+r, c+r, c-r, c+r)
			})
	case shapeTriangle:
		tri := func(r float32) func(z *vector.Rasterizer) {
			return func(z *vector.Rasterizer) {
				cy := c + fd*0.05
				polygon(z, c, cy-r, c+r*0.866, cy+r/2, c-r*0.866, cy+r/2)
			}
		}
		r := fd * 0.37
		return ring(tri(r), tri(r-2*stroke))
	case shapeDotSouth, shapeDotEast, shapeDotWest, shapeDotNorth:
		at := [4][2]float32{{0, 1}, {1, 0}, {-1, 0}, {0, -1}}
		lit := int(spec.shape - shapeDotSouth)
		return rasterize(d, d, func(z *vector.Rasterizer) {
			for i, p := range at {
				r := fd * 0.075
				if i == lit {
					r = fd * 0.19
				} else if d < 24 {
					// Too small to tell a dot from noise: show only the
					// button meant.
					continue
				}
				circle(z, c+p[0]*fd*0.24, c+p[1]*fd*0.24, r)
			}
		})
	case shapeDPad:
		a, l := fd*0.12, fd*0.33
		return rasterize(d, d, func(z *vector.Rasterizer) {
			polygon(z, c-a, c-l, c+a, c-l, c+a, c-a, c+l, c-a, c+l, c+a, c+a, c+a, c+a, c+l, c-a, c+l, c-a, c+a, c-l, c+a, c-l, c-a, c-a, c-a)
		})
	case shapeHome:
		r := fd * 0.3
		return rasterize(d, d, func(z *vector.Rasterizer) {
			polygon(z, c, c-r, c+r, c, c+r*0.72, c, c+r*0.72, c+r*0.85, c-r*0.72, c+r*0.85, c-r*0.72, c, c-r, c)
		})
	}
	pix := make([]byte, d*d)
	f.drawLabel(pix, d, d, spec.name, (d-f.labelWidth(spec.name))/2)
	return pix
}

// rasterize is the coverage of the paths draw adds, w bytes a row.
func rasterize(w, h int, draw func(z *vector.Rasterizer)) []byte {
	z := vector.NewRasterizer(w, h)
	draw(z)
	dst := image.NewAlpha(image.Rect(0, 0, w, h))
	z.Draw(dst, dst.Bounds(), image.Opaque, image.Point{})
	return dst.Pix
}

func polygon(z *vector.Rasterizer, xy ...float32) {
	z.MoveTo(xy[0], xy[1])
	for i := 2; i < len(xy); i += 2 {
		z.LineTo(xy[i], xy[i+1])
	}
	z.ClosePath()
}

func circle(z *vector.Rasterizer, cx, cy, r float32) {
	roundRect(z, cx-r, cy-r, 2*r, 2*r, r)
}

// roundRect adds a rectangle with corners of radius r.
func roundRect(z *vector.Rasterizer, x, y, w, h, r float32) {
	// k places a cubic's control points for a quarter circle.
	k := r * 0.5523
	z.MoveTo(x+r, y)
	z.LineTo(x+w-r, y)
	z.CubeTo(x+w-r+k, y, x+w, y+r-k, x+w, y+r)
	z.LineTo(x+w, y+h-r)
	z.CubeTo(x+w, y+h-r+k, x+w-r+k, y+h, x+w-r, y+h)
	z.LineTo(x+r, y+h)
	z.CubeTo(x+r-k, y+h, x, y+h-r+k, x, y+h-r)
	z.LineTo(x, y+r)
	z.CubeTo(x, y+r-k, x+r-k, y, x+r, y)
	z.ClosePath()
}

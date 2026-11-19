package gfx

import (
	"image"
	"image/png"
	"os"
	"testing"
)

func TestPromptRunesRoundTripToWords(t *testing.T) {
	t.Parallel()
	seen := map[rune]bool{}
	for f := PromptFamily(0); f < promptFamilies; f++ {
		for c := PromptControl(0); c < promptControls; c++ {
			r := PromptRune(f, c)
			if seen[r] {
				t.Fatalf("family %d control %d reuses rune %U", f, c, r)
			}
			seen[r] = true
			if got := PromptWords(string(r)); got != promptSpecs[f][c].name || got == "" {
				t.Errorf("family %d control %d reads %q", f, c, got)
			}
		}
	}
	text := string(PromptRune(PromptPlayStation, PromptSouth)) + " play  " + string(PromptRune(PromptXbox, PromptL)) + " page"
	if got := PromptWords(text); got != "Cross play  LB page" {
		t.Fatalf("PromptWords = %q", got)
	}
	if got := PromptWords("A play"); got != "A play" {
		t.Fatalf("plain text changed to %q", got)
	}
}

// Every glyph draws ink, sits inside the line and advances the pen, so a
// hint with glyphs measures and fits like text.
func TestPromptGlyphsDrawAndMeasure(t *testing.T) {
	t.Parallel()
	for _, size := range []int{16, 18, 24, 36} {
		line := TextHeight(size)
		for f := PromptFamily(0); f < promptFamilies; f++ {
			for c := PromptControl(0); c < promptControls; c++ {
				s := string(PromptRune(f, c))
				m, src, width := UIText(s, size, WeightRegular, 0)
				if m == nil || src.Empty() {
					t.Fatalf("size %d family %d control %d drew nothing", size, f, c)
				}
				lit, cut := 0, 0
				for _, v := range m.Pix {
					if v > 200 {
						lit++
					} else if v < 50 {
						cut++
					}
				}
				if lit < size || cut < size/4 {
					t.Errorf("size %d %q: %d solid and %d clear pixels; want a body with a marking", size, PromptWords(s), lit, cut)
				}
				if m.H > line+2 {
					t.Errorf("size %d %q is %d tall on a %d line", size, PromptWords(s), m.H, line)
				}
				if got := MeasureText(s, size); got < m.W || got != width+0 && got > width+2 {
					t.Errorf("size %d %q measures %d for %d of ink (box %d)", size, PromptWords(s), got, m.W, width)
				}
			}
		}
	}
	a := string(PromptRune(PromptXbox, PromptSouth))
	if both, one := MeasureText(a+" play", 18), MeasureText(a, 18)+MeasureText(" play", 18); both != one {
		t.Fatalf("glyph then text measures %d, parts %d", both, one)
	}
	if fit := FitText(a+" select  "+a+" a very long action name", 18, 120); fit == "" || MeasureText(fit, 18) > 120 {
		t.Fatalf("fit %q is %d wide", PromptWords(fit), MeasureText(fit, 18))
	}
}

// TestWritePromptSheet writes every glyph to FES_PROMPT_SHEET for review.
func TestWritePromptSheet(t *testing.T) {
	path := os.Getenv("FES_PROMPT_SHEET")
	if path == "" {
		t.Skip("FES_PROMPT_SHEET is not set")
	}
	sizes := []int{18, 24, 36}
	img := image.NewRGBA(image.Rect(0, 0, 900, 60*len(sizes)*int(promptFamilies)+20))
	for i := range img.Pix {
		img.Pix[i] = 0xF4
	}
	y := 10
	for f := PromptFamily(0); f < promptFamilies; f++ {
		for _, size := range sizes {
			text := ""
			for c := PromptControl(0); c < promptControls; c++ {
				text += string(PromptRune(f, c)) + " "
			}
			text += string(PromptRune(f, PromptSouth)) + " select  " + string(PromptRune(f, PromptEast)) + " back"
			m, src, _ := UIText(text, size, WeightRegular, 0)
			ink := ExpandMask(m, src, RGB(0x24, 0x36, 0x31))
			for py := 0; py < ink.Rect.Dy(); py++ {
				for px := 0; px < ink.Rect.Dx() && px+10 < 900; px++ {
					o, d := ink.PixOffset(px, py), img.PixOffset(px+10, y+py)
					a := uint32(ink.Pix[o+3])
					for k := 0; k < 3; k++ {
						img.Pix[d+k] = uint8((uint32(ink.Pix[o+k])*a + uint32(img.Pix[d+k])*(255-a)) / 255)
					}
					img.Pix[d+3] = 255
				}
			}
			y += 60
		}
	}
	out, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	if err := png.Encode(out, img); err != nil {
		t.Fatal(err)
	}
}

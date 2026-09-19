package fbgrid

import (
	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/theme"
	"strings"
	"testing"
)

func TestReadingFooterPreservesRecoveryAndTitle(t *testing.T) {
	message := "Target retains an earlier error; use Stop to clear it, then retry."
	title := "Verified FES Pong package 20260919"
	th := theme.Default()
	for _, size := range [][2]int{{320, 240}, {640, 480}, {1280, 720}} {
		w, h := size[0], size[1]
		lines := ReadingFooter(message, title, w, h, th)
		if got := strings.Join(lines, " "); got != message+" "+title {
			t.Fatalf("%dx%d truncated: %q", w, h, got)
		}
		if readingFooterHeight(lines, th) > h/2 {
			t.Fatal("unbounded footer")
		}
		g := NewWithTiles(w, h, []Tile{{Name: "Verified FES Po..."}})
		ApplyTheme(&g, th)
		g.SetFooterLines(lines)
		for _, detail := range []bool{false, true} {
			rec := gfx.NewRecorder()
			if detail {
				PaintDetail(rec, DetailFrame{Width: w, Height: h, Title: title, FooterLines: lines, Theme: th})
			} else {
				Paint(rec, g)
			}
			var painted []string
			for _, c := range rec.Calls {
				if c.Op == "DrawText" && c.Y >= h-readingFooterHeight(lines, th) {
					for _, line := range lines {
						if c.Text == line {
							painted = append(painted, c.Text)
							break
						}
					}
				}
			}
			if strings.Join(painted, " ") != message+" "+title {
				t.Fatalf("detail=%v missing rendered text: %v", detail, painted)
			}
		}
	}
}

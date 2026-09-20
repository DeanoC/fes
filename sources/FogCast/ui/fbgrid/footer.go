package fbgrid

import (
	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/theme"
)

// ReadingFooter is opt-in multiline chrome. It reserves at most one half
// of the viewport, keeping the browse stage and its controls visible.
func ReadingFooter(message, title string, width, height int, th theme.Theme) []string {
	th = th.Complete()
	lineH := gfx.TextHeightWeight(th.StatusPx(), th.StatusWeight()) + 2
	limit := (height/2 - 12) / lineH
	if limit > 8 {
		limit = 8
	}
	if limit < 2 || width <= 16 {
		return nil
	}
	messageLimit := limit
	if title != "" {
		messageLimit -= 2
	}
	if messageLimit < 1 {
		messageLimit = 1
	}
	lines := gfx.WrapTextWeight(message, th.StatusPx(), width-16, messageLimit, th.StatusWeight())
	if title != "" && len(lines) < limit {
		lines = append(lines, gfx.WrapTextWeight(title, th.StatusPx(), width-16, limit-len(lines), th.StatusWeight())...)
	}
	return lines
}

// SetFooterLines reserves wrapped chrome before callers inspect grid geometry.
func (g *Grid) SetFooterLines(lines []string) {
	g.FooterLines = append([]string(nil), lines...)
	if len(lines) > 0 {
		g.FooterH = readingFooterHeight(lines, g.Theme.Complete())
	} else {
		g.FooterH = g.Theme.Complete().FooterH
	}
	g.layout()
}

func readingFooterHeight(lines []string, th theme.Theme) int {
	return 12 + len(lines)*(gfx.TextHeightWeight(th.StatusPx(), th.StatusWeight())+2)
}

func paintReadingFooter(d gfx.Device, lines []string, width, height int, th theme.Theme) {
	y := height - readingFooterHeight(lines, th) + 6
	for _, line := range lines {
		d.DrawTextWeight(8, y, gfx.FitTextWeight(line, th.StatusPx(), width-16, th.StatusWeight()), th.StatusPx(), th.StatusWeight(), th.Status)
		y += gfx.TextHeightWeight(th.StatusPx(), th.StatusWeight()) + 2
	}
}

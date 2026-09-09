package fbgrid

import (
	"strings"

	"github.com/DeanoC/FogCast/host/tenfoot"
	"github.com/DeanoC/FogCast/host/tenfoot/gfx"
	"github.com/DeanoC/FogCast/host/tenfoot/theme"
)

// OSKFrame is the kit search keyboard overlay. PaintOSK draws it after Paint
// so the filtered shelf stays visible above the keys.
type OSKFrame struct {
	Width, Height    int
	HeaderH, FooterH int
	OSK              tenfoot.OSKSnapshot
	Theme            theme.Theme
}

// PaintOSK draws a gamepad keyboard over the catalog stage. It does not
// BeginFrame or Present.
func PaintOSK(d gfx.Device, f OSKFrame) {
	if d == nil || !f.OSK.Open || len(f.OSK.Rows) == 0 || f.Width < 1 || f.Height < 1 {
		return
	}
	th := f.Theme.Complete()
	headerH := f.HeaderH
	if headerH < 0 {
		headerH = 0
	}
	footerH := f.FooterH
	if footerH < 0 {
		footerH = 0
	}
	contentY := headerH
	contentH := f.Height - headerH - footerH
	if contentH < 80 {
		contentY = 0
		contentH = f.Height
	}
	d.SetBlend(gfx.BlendAlpha)
	d.FillRect(gfx.Rect{X: 0, Y: float32(contentY), W: float32(f.Width), H: float32(contentH)}, gfx.RGBA(8, 8, 12, 180))
	d.SetBlend(gfx.BlendNone)

	rows := f.OSK.Rows
	gap := 6
	header := 36
	hintH := 22
	pad := 12
	maxH := contentH - 16
	if maxH < 120 {
		maxH = contentH
	}
	keyH := 32
	panelH := header + len(rows)*keyH + (len(rows)-1)*gap + hintH + pad
	if panelH > maxH {
		remain := maxH - header - hintH - pad - (len(rows)-1)*gap
		if remain < len(rows)*18 {
			remain = len(rows) * 18
		}
		keyH = remain / len(rows)
		if keyH < 18 {
			keyH = 18
		}
		panelH = header + len(rows)*keyH + (len(rows)-1)*gap + hintH + pad
		if panelH > maxH {
			panelH = maxH
		}
	}
	panelW := f.Width - 24
	if panelW > 600 {
		panelW = 600
	}
	if panelW < 280 {
		panelW = f.Width - 8
	}
	if panelW < 1 {
		panelW = f.Width
	}
	x := (f.Width - panelW) / 2
	y := contentY + contentH - panelH - 8
	if y < contentY+8 {
		y = contentY + 8
	}
	d.FillRect(gfx.Rect{X: float32(x - 3), Y: float32(y - 3), W: float32(panelW + 6), H: float32(panelH + 6)}, th.Highlight)
	d.FillRect(gfx.Rect{X: float32(x), Y: float32(y), W: float32(panelW), H: float32(panelH)}, th.HeaderBar)

	prompt := strings.TrimSpace(f.OSK.Prompt)
	if prompt == "" {
		prompt = "Search"
	}
	query := f.OSK.Buffer + "_"
	line := prompt + ": " + query
	titleSize := th.TitlePx()
	titleW := th.TitleWeight()
	line = gfx.FitTextWeight(line, titleSize, panelW-24, titleW)
	d.DrawTextWeight(x+12, chromeTextY(y, header, gfx.TextHeightWeight(titleSize, titleW), true), line, titleSize, titleW, th.Header)

	innerX := x + 10
	innerW := panelW - 20
	if innerW < 1 {
		innerW = 1
	}
	refCols := 10
	unitW := (innerW - (refCols-1)*gap) / refCols
	if unitW < 8 {
		unitW = 8
	}
	rowY := y + header
	labelSize := th.BodyPx()
	if keyH < 26 {
		labelSize = th.CaptionPx()
	}
	labelW := th.BodyWeight()
	for r, row := range rows {
		colX := innerX
		for c, key := range row {
			span := key.Span
			if span < 1 {
				span = 1
			}
			kw := span*unitW + (span-1)*gap
			if r < 3 {
				kw = unitW
			}
			cell := gfx.Rect{X: float32(colX), Y: float32(rowY), W: float32(kw), H: float32(keyH)}
			fill := th.FooterBar
			if key.Focus {
				d.FillRect(gfx.Rect{X: cell.X - 2, Y: cell.Y - 2, W: cell.W + 4, H: cell.H + 4}, th.Highlight)
				fill = th.LabelBar
			}
			d.FillRect(cell, fill)
			text := gfx.FitTextWeight(key.Label, labelSize, kw-4, labelW)
			textH := gfx.TextHeightWeight(labelSize, labelW)
			tx := colX + (kw-gfx.MeasureTextWeight(text, labelSize, labelW))/2
			ty := rowY + (keyH-textH)/2
			if tx < colX+1 {
				tx = colX + 1
			}
			ink := th.Status
			if key.Focus {
				ink = th.Highlight
			}
			d.DrawTextWeight(tx, ty, text, labelSize, labelW, ink)
			_ = c
			colX += kw + gap
		}
		rowY += keyH + gap
	}
	hint := strings.TrimSpace(f.OSK.Hint)
	if hint == "" {
		hint = tenfoot.OSKKitHint(f.OSK.Page)
	}
	statusSize := th.StatusPx()
	statusW := th.StatusWeight()
	hint = gfx.FitTextWeight(hint, statusSize, panelW-24, statusW)
	d.DrawTextWeight(x+12, y+panelH-hintH, hint, statusSize, statusW, th.Status)
}

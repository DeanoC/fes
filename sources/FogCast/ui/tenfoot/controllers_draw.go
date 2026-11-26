package tenfoot

import (
	"fmt"
	"image"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/DeanoC/FogCast/ui/gfx"
	"golang.org/x/image/vector"
)

// The Controllers page is laid out on a 1280×720 sheet and scaled to the
// safe area.
const (
	controllersSheetW = 1280
	controllersSheetH = 720
)

var (
	controllersChanged = gfx.RGBA(0xEF, 0xD4, 0x8D, 0xFF)
	controllersLit     = gfx.RGBA(0xC8, 0xE3, 0xAC, 0xFF)
	controllersLine    = gfx.RGBA(0x7C, 0x8A, 0x80, 0xFF)
)

// controllersSheet maps sheet coordinates to the screen.
type controllersSheet struct {
	ox, oy int
	scale  float64
}

func newControllersSheet(grid Grid) controllersSheet {
	w, h := grid.contentWidth(), grid.contentHeight()
	scale := math.Min(float64(w)/controllersSheetW, float64(h)/controllersSheetH)
	return controllersSheet{
		ox:    grid.contentLeft() + (w-int(controllersSheetW*scale))/2,
		oy:    grid.contentTop() + (h-int(controllersSheetH*scale))/2,
		scale: scale,
	}
}

func (s controllersSheet) x(v float64) int { return s.ox + int(math.Round(v*s.scale)) }
func (s controllersSheet) y(v float64) int { return s.oy + int(math.Round(v*s.scale)) }

// n scales a length, keeping anything drawn at least a pixel.
func (s controllersSheet) n(v float64) int { return max(1, int(math.Round(v*s.scale))) }

func (s controllersSheet) rect(x, y, w, h float64) image.Rectangle {
	return image.Rect(s.x(x), s.y(y), s.x(x+w), s.y(y+h))
}

// controllerRowAt is each row's top-left corner on the sheet, in padSources
// order. Rows are 340 wide and 44 high.
var controllerRowAt = [8][2]float64{
	{40, 174}, {40, 414},
	{900, 174}, {900, 222}, {900, 270}, {900, 318}, {900, 366}, {900, 414},
}

// controllerAnchor is where each row's button sits on the pad picture.
var controllerAnchor = [8][2]float64{
	{520, 201}, {608, 276},
	{760, 201}, {774, 250}, {804, 280}, {774, 310}, {744, 280}, {672, 276},
}

var shapeMasks struct {
	sync.Mutex
	m map[string]*gfx.Mask
}

// shapeMask rasterizes a filled outline once per key. A mask must not
// change after it is drawn, so each size keeps its own.
func shapeMask(key string, w, h int, outline func(r *vector.Rasterizer)) *gfx.Mask {
	shapeMasks.Lock()
	defer shapeMasks.Unlock()
	if m, ok := shapeMasks.m[key]; ok {
		return m
	}
	r := vector.NewRasterizer(w, h)
	outline(r)
	dst := image.NewAlpha(image.Rect(0, 0, w, h))
	r.Draw(dst, dst.Bounds(), image.Opaque, image.Point{})
	m := &gfx.Mask{W: w, H: h, Pix: dst.Pix}
	if shapeMasks.m == nil {
		shapeMasks.m = map[string]*gfx.Mask{}
	}
	shapeMasks.m[key] = m
	return m
}

// addRoundRect outlines a rectangle with corner radius rad. reverse winds
// it the other way, which cuts it out of a shape already outlined.
func addRoundRect(r *vector.Rasterizer, x, y, w, h, rad float32, reverse bool) {
	rad = min(rad, w/2, h/2)
	// Corner points in order, each with the control point of its arc.
	type pt struct{ x, y float32 }
	pts := []pt{
		{x + rad, y}, {x + w - rad, y}, {x + w, y}, {x + w, y + rad},
		{x + w, y + h - rad}, {x + w, y + h}, {x + w - rad, y + h},
		{x + rad, y + h}, {x, y + h}, {x, y + h - rad},
		{x, y + rad}, {x, y}, {x + rad, y},
	}
	if reverse {
		for i, j := 0, len(pts)-1; i < j; i, j = i+1, j-1 {
			pts[i], pts[j] = pts[j], pts[i]
		}
	}
	r.MoveTo(pts[0].x, pts[0].y)
	// After the start point the outline alternates a straight edge with a
	// corner arc; reversed, it starts with an arc.
	i := 1
	if reverse {
		r.QuadTo(pts[1].x, pts[1].y, pts[2].x, pts[2].y)
		i = 3
	}
	for i < len(pts) {
		r.LineTo(pts[i].x, pts[i].y)
		if i+2 < len(pts) {
			r.QuadTo(pts[i+1].x, pts[i+1].y, pts[i+2].x, pts[i+2].y)
		}
		i += 3
	}
	r.ClosePath()
}

func drawShape(dev gfx.Device, m *gfx.Mask, x, y int, c gfx.Color) {
	dev.DrawMask(m, m.Bounds(), x, y, c)
}

// drawRoundRect fills a rounded rectangle given in screen pixels.
func drawRoundRect(dev gfx.Device, r image.Rectangle, rad int, c gfx.Color) {
	w, h := r.Dx(), r.Dy()
	if w < 1 || h < 1 {
		return
	}
	m := shapeMask(fmt.Sprintf("rr:%d:%d:%d", w, h, rad), w, h, func(v *vector.Rasterizer) {
		addRoundRect(v, 0, 0, float32(w), float32(h), float32(rad), false)
	})
	drawShape(dev, m, r.Min.X, r.Min.Y, c)
}

// drawRing strokes a circle of the given outer size and line width.
func drawRing(dev gfx.Device, r image.Rectangle, line int, c gfx.Color) {
	d := r.Dx()
	if d < 2*line+1 {
		return
	}
	m := shapeMask(fmt.Sprintf("ring:%d:%d", d, line), d, d, func(v *vector.Rasterizer) {
		addRoundRect(v, 0, 0, float32(d), float32(d), float32(d)/2, false)
		addRoundRect(v, float32(line), float32(line), float32(d-2*line), float32(d-2*line), float32(d)/2, true)
	})
	drawShape(dev, m, r.Min.X, r.Min.Y, c)
}

func sheetText(dev gfx.Device, labels map[string]gpuTexture, used map[string]struct{}, s controllersSheet, x, y, maxW, size float64, bold bool, c gfx.Color, text string) {
	heroLine(dev, labels, used, s.x(x), s.y(y), s.n(maxW), s.n(size), bold, c, text)
}

func sheetTextWidth(s controllersSheet, size float64, bold bool, text string) float64 {
	weight := gfx.WeightRegular
	if bold {
		weight = gfx.WeightBold
	}
	return float64(gfx.MeasureTextWeight(text, s.n(size), weight)) / s.scale
}

// sheetBox fills a box and frames it with a border of the given width.
func sheetBox(dev gfx.Device, s controllersSheet, x, y, w, h, border float64, fill, frame gfx.Color) {
	r := s.rect(x, y, w, h)
	fillRectI(dev, r, fill)
	heroOutline(dev, r.Min.X, r.Min.Y, r.Dx(), r.Dy(), s.n(border), frame)
}

func drawControllers(dev gfx.Device, snap Snapshot, labels map[string]gpuTexture, used map[string]struct{}) {
	page := snap.Controllers
	if !page.Open {
		return
	}
	s := newControllersSheet(snap.Grid)
	fillRectI(dev, image.Rect(0, 0, snap.Grid.Width, snap.Grid.Height), heroBackground)
	drawControllersHeader(dev, snap, labels, used, s)
	hint := ""
	if page.Learn.Open {
		drawControllerLearn(dev, snap, labels, used, s)
		if page.Learn.Review {
			hint = fmt.Sprintf("%s  ·  %s choose  ·  %s cancel", moveWord(snap.Affinity), selectWord(snap.Affinity), backWord(snap.Affinity))
		} else {
			hint = "Esc or another pad's B cancels"
			if page.Learn.Optional {
				hint += "  ·  Keyboard X or another pad's X skips"
			}
		}
	} else if page.Picker.Open {
		drawControllerPicker(dev, snap, labels, used, s)
		hint = fmt.Sprintf("%s  ·  %s choose  ·  %s button default  ·  %s cancel",
			moveWord(snap.Affinity), selectWord(snap.Affinity), westWord(snap.Affinity), backWord(snap.Affinity))
	} else {
		drawControllerLayout(dev, snap, labels, used, s)
		hint = fmt.Sprintf("%s  ·  %s change  ·  %s button default", moveWord(snap.Affinity), selectWord(snap.Affinity), westWord(snap.Affinity))
		if len(page.Tabs) > 1 {
			other := "Tab"
			if snap.Affinity == InputGamepad {
				other = "LB/RB"
			}
			hint += "  ·  " + other + " other pad"
		}
		hint += "  ·  " + backWord(snap.Affinity) + " back"
	}
	fillRectI(dev, s.rect(0, 650, controllersSheetW, 2), heroDivider)
	hint = snap.padHint(hint)
	sheetText(dev, labels, used, s, 1248-sheetTextWidth(s, 20, false, hint), 674, 1216, 20, false, heroSecondary, hint)
	if page.Status != "" {
		sheetText(dev, labels, used, s, 40, 616, 1200, 19, false, heroText, page.Status)
	}
}

// moveWord is the footer's name for the directions.
func moveWord(kind InputKind) string {
	if kind == InputKeyboard {
		return "Arrows move"
	}
	return "D-pad move"
}

func drawControllersHeader(dev gfx.Device, snap Snapshot, labels map[string]gpuTexture, used map[string]struct{}, s controllersSheet) {
	page := snap.Controllers
	sheetText(dev, labels, used, s, 32, 18, 80, 28, true, heroText, "FES")
	fillRectI(dev, s.rect(92, 20, 2, 30), heroDivider)
	sheetText(dev, labels, used, s, 108, 21, 420, 23, false, heroText, "Settings › Controllers")
	fillRectI(dev, s.rect(0, 70, controllersSheetW, 2), heroDivider)
	// Pad tabs, laid from the right edge.
	right := 1248.0
	shoulders := len(page.Tabs) > 1 && snap.Affinity == InputGamepad
	if shoulders {
		g := string(gfx.PromptRune(page.Family, gfx.PromptR))
		right -= sheetTextWidth(s, 20, false, g)
		sheetText(dev, labels, used, s, right, 23, 80, 20, false, heroText, g)
		right -= 10
	}
	for i := len(page.Tabs) - 1; i >= 0; i-- {
		tab := page.Tabs[i]
		name := gfx.FitTextWeight(tab.Name, s.n(19), s.n(250), gfx.WeightRegular)
		labelW := sheetTextWidth(s, 19, true, tab.Label)
		w := 14 + labelW + 10 + sheetTextWidth(s, 19, false, name) + 14
		x := right - w
		fill, ink, frame := heroSurface, heroSecondary, heroDivider
		if i == page.Tab {
			fill, ink, frame = heroAccent, heroOnAccent, heroAccent
		}
		if tab.Pressed {
			frame = controllersChanged
		}
		sheetBox(dev, s, x, 14, w, 42, 3, fill, frame)
		sheetText(dev, labels, used, s, x+14, 24, labelW+4, 19, true, ink, tab.Label)
		sheetText(dev, labels, used, s, x+14+labelW+10, 24, 260, 19, false, ink, name)
		right = x - 10
	}
	if shoulders {
		g := string(gfx.PromptRune(page.Family, gfx.PromptL))
		sheetText(dev, labels, used, s, right-sheetTextWidth(s, 20, false, g), 23, 80, 20, false, heroText, g)
	}
}

func drawControllerLayout(dev gfx.Device, snap Snapshot, labels map[string]gpuTexture, used map[string]struct{}, s controllersSheet) {
	page := snap.Controllers
	sheetText(dev, labels, used, s, 40, 92, 700, 34, true, heroText, "Button layout")
	if page.Tested {
		bx := 40 + sheetTextWidth(s, 34, true, "Button layout") + 16
		bw := sheetTextWidth(s, 17, true, "Tested") + 20
		fillRectI(dev, s.rect(bx, 99, bw, 28), controllersChanged)
		sheetText(dev, labels, used, s, bx+10, 102, bw, 17, true, heroText, "Tested")
	}
	sheetText(dev, labels, used, s, 40, 136, 900, 19, false, heroSecondary, "What each button sends to FES and to the computer you are playing.")

	// Callout lines run from each row into the picture and end under it.
	for i := range page.Rows {
		at, anchor := controllerRowAt[i], controllerAnchor[i]
		c, line := controllersLine, 2.0
		if i == page.Focus {
			c, line = heroAccent, 3
		}
		cy := at[1] + 22
		from, to := at[0]+340, anchor[0]
		if at[0] > 640 {
			from, to = anchor[0], at[0]
		}
		fillRectI(dev, s.rect(from, cy-line/2, to-from, line), c)
		top, bottom := math.Min(cy, anchor[1]), math.Max(cy, anchor[1])
		fillRectI(dev, s.rect(anchor[0]-line/2, top, line, bottom-top+line/2), c)
	}
	drawControllerPicture(dev, snap, labels, used, s)

	for i, row := range page.Rows {
		at := controllerRowAt[i]
		drawControllerRow(dev, labels, used, s, at[0], at[1], row, i == page.Focus)
	}
	// What a profile does not rebind.
	sheetBox(dev, s, 40, 254, 340, 44, 1, heroBackground, heroDivider)
	sheetText(dev, labels, used, s, 52, 265, 120, 20, false, heroSecondary, "Left stick")
	note := "moves like the D-pad"
	sheetText(dev, labels, used, s, 368-sheetTextWidth(s, 15, false, note), 269, 200, 15, false, heroSecondary, note)
	sheetBox(dev, s, 40, 334, 340, 44, 1, heroBackground, heroDivider)
	sheetText(dev, labels, used, s, 52, 343, 40, 22, false, heroText, string(gfx.PromptRune(page.Family, gfx.PromptDPad)))
	sheetText(dev, labels, used, s, 92, 345, 120, 20, false, heroSecondary, "D-pad")
	note = "sends directions"
	sheetText(dev, labels, used, s, 368-sheetTextWidth(s, 15, false, note), 349, 200, 15, false, heroSecondary, note)

	for i, line := range gfx.WrapText(page.Identity, s.n(17), s.n(470), 2) {
		w := float64(gfx.MeasureTextWeight(line, s.n(17), gfx.WeightRegular)) / s.scale
		sheetText(dev, labels, used, s, 640-w/2, 478+float64(i)*22, 480, 17, false, heroSecondary, line)
	}
	tip := "Press a button on the pad to see it light up here."
	sheetText(dev, labels, used, s, 640-sheetTextWidth(s, 17, false, tip)/2, 526, 480, 17, false, heroSecondary, tip)

	sheetBox(dev, s, 900, 474, 14, 14, 1, controllersChanged, heroText)
	sheetText(dev, labels, used, s, 922, 470, 320, 17, false, heroSecondary, "Changed from this pad's own layout")

	swap := "Off"
	if page.SwapAB {
		swap = "On"
	}
	drawControllerButton(dev, labels, used, s, 40, 556, 232, "Swap A / B", swap, page.Focus == controllersFocusSwap)
	drawControllerButton(dev, labels, used, s, 286, 556, 250, "Map step by step", "", page.Focus == controllersFocusLearn)
	drawControllerButton(dev, labels, used, s, 550, 556, 200, "Reset this pad", "", page.Focus == controllersFocusReset)
}

func drawControllerRow(dev gfx.Device, labels map[string]gpuTexture, used map[string]struct{}, s controllersSheet, x, y float64, row ControllerRow, focused bool) {
	fill, frame, border, chip := heroSurface, heroDivider, 1.0, heroRaised
	if focused {
		fill, frame, border, chip = heroRaised, heroAccent, 3, heroSurface
	}
	if row.Pressed {
		frame, border = controllersChanged, 3
	}
	if row.Changed {
		chip = controllersChanged
	}
	sheetBox(dev, s, x, y, 340, 44, border, fill, frame)
	sheetText(dev, labels, used, s, x+12, y+9, 80, 22, false, heroText, row.Glyph)
	glyphW := sheetTextWidth(s, 22, false, row.Glyph)
	chipW := sheetTextWidth(s, 18, true, row.Target) + 20
	chipX := x + 340 - 12 - chipW
	fillRectI(dev, s.rect(chipX, y+9, chipW, 26), chip)
	sheetText(dev, labels, used, s, chipX+10, y+12, chipW, 18, true, heroText, row.Target)
	sendsW := sheetTextWidth(s, 15, false, "sends")
	sheetText(dev, labels, used, s, chipX-8-sendsW, y+15, sendsW+4, 15, false, heroSecondary, "sends")
	labelX := x + 12 + glyphW + 10
	sheetText(dev, labels, used, s, labelX, y+11, chipX-8-sendsW-8-labelX, 20, focused, heroText, row.Label)
}

func drawControllerButton(dev gfx.Device, labels map[string]gpuTexture, used map[string]struct{}, s controllersSheet, x, y, w float64, label, value string, focused bool) {
	fill, frame, border := heroSurface, heroDivider, 2.0
	if focused {
		fill, frame, border = heroRaised, heroAccent, 3
	}
	sheetBox(dev, s, x, y, w, 48, border, fill, frame)
	sheetText(dev, labels, used, s, x+18, y+12, w-36, 21, true, heroText, label)
	if value != "" {
		sheetText(dev, labels, used, s, x+w-18-sheetTextWidth(s, 21, false, value), y+12, 80, 21, false, heroSecondary, value)
	}
}

// drawControllerPicture draws the pad with each button as its family's
// glyph. A PlayStation pad has its sticks side by side below the D-pad.
func drawControllerPicture(dev gfx.Device, snap Snapshot, labels map[string]gpuTexture, used map[string]struct{}, s controllersSheet) {
	page := snap.Controllers
	tint := func(i int, base gfx.Color) gfx.Color {
		switch {
		case page.Rows[i].Pressed:
			return controllersChanged
		case i == page.Focus:
			return controllersLit
		}
		return base
	}
	drawRoundRect(dev, s.rect(470, 186, 100, 30), s.n(9), tint(0, heroDivider))
	drawRoundRect(dev, s.rect(710, 186, 100, 30), s.n(9), tint(2, heroDivider))
	drawRoundRect(dev, s.rect(414, 336, 128, 128), s.n(64), heroText)
	drawRoundRect(dev, s.rect(738, 336, 128, 128), s.n(64), heroText)
	drawRoundRect(dev, s.rect(430, 204, 420, 200), s.n(44), heroText)

	stick, dpad := [2]float64{482, 248}, [2]float64{538, 324}
	if page.Family == gfx.PromptPlayStation {
		stick, dpad = [2]float64{562, 328}, [2]float64{466, 246}
	}
	drawRing(dev, s.rect(stick[0], stick[1], 48, 48), s.n(3), heroSecondary)
	drawRing(dev, s.rect(690, 328, 48, 48), s.n(3), heroSecondary)
	fillRectI(dev, s.rect(dpad[0], dpad[1]+18, 56, 20), heroRaised)
	fillRectI(dev, s.rect(dpad[0]+18, dpad[1], 20, 56), heroRaised)

	drawRoundRect(dev, s.rect(594, 270, 28, 12), s.n(6), tint(1, heroDivider))
	drawRoundRect(dev, s.rect(658, 270, 28, 12), s.n(6), tint(7, heroDivider))
	drawRoundRect(dev, s.rect(627, 223, 26, 26), s.n(13), heroSecondary)

	// Face buttons are the pad's own glyphs, drawn light on the body.
	for _, i := range []int{3, 4, 5, 6} {
		anchor := controllerAnchor[i]
		sheetText(dev, labels, used, s, anchor[0]-15, anchor[1]-16, 60, 27, false, tint(i, heroRaised), page.Rows[i].Glyph)
	}
}

func drawControllerPicker(dev gfx.Device, snap Snapshot, labels map[string]gpuTexture, used map[string]struct{}, s controllersSheet) {
	page := snap.Controllers
	row := page.Rows[page.Focus]
	sheetText(dev, labels, used, s, 40, 96, 60, 36, false, heroText, row.Glyph)
	sheetText(dev, labels, used, s, 40+sheetTextWidth(s, 36, false, row.Glyph)+12, 98, 400, 32, true, heroText, row.Label+" sends…")
	sheetText(dev, labels, used, s, 40, 152, 440, 19, false, heroSecondary, "Now: "+row.Target+". Pick what this button should send instead.")
	about := "In FES menus A confirms and B goes back, so moving them changes which button you confirm with. " +
		"FES keeps a button on each: taking one from its only button hands it to the button you took from."
	for i, line := range gfx.WrapText(about, s.n(18), s.n(430), 8) {
		sheetText(dev, labels, used, s, 40, 220+float64(i)*25, 440, 18, false, heroSecondary, line)
	}

	sheetBox(dev, s, 510, 96, 730, 530, 2, heroSurface, heroDivider)
	titles := [3]string{"Pad buttons", "Directions", "Keypad, for ColecoVision and other keypad machines"}
	tops := [3]float64{118, 276, 374}
	for g, title := range titles {
		sheetText(dev, labels, used, s, 536, tops[g], 680, 17, false, heroSecondary, title)
	}
	for i, target := range page.Picker.Targets {
		group, r, c := controllerPickerCell(i)
		cols := float64(controllerPickerColumns[group])
		w := (678 - 8*(cols-1)) / cols
		x, y := 536+float64(c)*(w+8), tops[group]+28+float64(r)*60
		fill, frame, border := heroSurface, heroDivider, 1.0
		if target.Now {
			fill = heroRaised
		}
		if i == page.Picker.Index {
			frame, border = heroAccent, 3
		}
		sheetBox(dev, s, x, y, w, 52, border, fill, frame)
		note := target.Note
		if target.Now {
			if note != "" {
				note += " · "
			}
			note += "now"
		}
		labelY := y + 14
		if note != "" {
			labelY = y + 6
			sheetText(dev, labels, used, s, x+(w-sheetTextWidth(s, 14, false, note))/2, y+31, w, 14, false, heroSecondary, note)
		}
		sheetText(dev, labels, used, s, x+(w-sheetTextWidth(s, 21, true, target.Label))/2, labelY, w, 21, true, heroText, target.Label)
	}
}

// drawControllerLearn draws step-by-step mapping: the control asked for on
// the left, and every step with what was pressed for it on the right.
func drawControllerLearn(dev gfx.Device, snap Snapshot, labels map[string]gpuTexture, used map[string]struct{}, s controllersSheet) {
	learn := snap.Controllers.Learn
	sheetText(dev, labels, used, s, 40, 92, 560, 34, true, heroText, "Map step by step")
	if learn.Review {
		sheetText(dev, labels, used, s, 40, 136, 560, 19, false, heroSecondary, "Check what you pressed")
		about := "Save to read this pad through the buttons you pressed. Swap A / B and changing a button still work on top of it, and Reset this pad takes it off again."
		for i, line := range gfx.WrapText(about, s.n(20), s.n(540), 6) {
			sheetText(dev, labels, used, s, 40, 200+float64(i)*28, 560, 20, false, heroText, line)
		}
		choices := [learnChoices]struct {
			label string
			x, w  float64
		}{{"Save", 40, 160}, {"Start again", 214, 200}, {"Cancel", 428, 160}}
		for i, c := range choices {
			drawControllerButton(dev, labels, used, s, c.x, 556, c.w, c.label, "", learn.Choice == i)
		}
	} else {
		sheetText(dev, labels, used, s, 40, 136, 560, 19, false, heroSecondary,
			fmt.Sprintf("Step %d of %d", learn.Step+1, len(learn.Rows)))
		ask, label := "Press the control for", learn.Label
		if learn.Stick {
			ask, label = "Push the left stick all the way", strings.ToUpper(learn.Label[6:7])+learn.Label[7:]
		}
		sheetText(dev, labels, used, s, 40, 196, 560, 24, false, heroSecondary, ask)
		sheetText(dev, labels, used, s, 40, 232, 560, 64, true, heroText, label)
		if learn.Note != "" {
			sheetText(dev, labels, used, s, 40, 316, 560, 22, false, heroSecondary, learn.Note+" in FES menus")
		}
		help := fmt.Sprintf("Press it on the pad you are mapping. A stick works for the directions. To stop without changing anything, press nothing for %d seconds.", int(learnIdle/time.Second))
		if learn.Optional {
			help = "No such button? Press " + learn.Skip + ", the control you gave B, to skip it."
		}
		if learn.Stick {
			help = "Then let it go. This lets games read the stick as a stick. No stick, or it already moves the directions? Press " + learn.Skip + " to skip."
		}
		for i, line := range gfx.WrapText(help, s.n(20), s.n(540), 3) {
			sheetText(dev, labels, used, s, 40, 368+float64(i)*28, 560, 20, false, heroText, line)
		}
	}
	// A refusal can run to two lines.
	for i, line := range gfx.WrapTextWeight(learn.Message, s.n(20), s.n(560), 2, gfx.WeightBold) {
		sheetText(dev, labels, used, s, 40, 462+float64(i)*28, 560, 20, true, heroText, line)
	}
	if learn.Seconds > 0 && learn.Seconds <= 10 {
		sheetText(dev, labels, used, s, 40, 524, 560, 17, false, heroSecondary,
			fmt.Sprintf("Stops in %d s unless something is pressed. Nothing changes then.", learn.Seconds))
	}

	sheetBox(dev, s, 660, 96, 580, 530, 2, heroSurface, heroDivider)
	// The rows share the box's height: 42 apart while they fit.
	pitch := min(42, 504/float64(max(1, len(learn.Rows))))
	for i, row := range learn.Rows {
		y := 112 + float64(i)*pitch
		if !learn.Review && i == learn.Step {
			sheetBox(dev, s, 672, y, 556, pitch-4, 3, heroRaised, heroAccent)
		}
		ty := y + (pitch-4-20)/2 - 1
		sheetText(dev, labels, used, s, 688, ty, 140, 20, true, heroText, row.Label)
		if row.Note != "" {
			sheetText(dev, labels, used, s, 688+sheetTextWidth(s, 20, true, row.Label)+10, ty+3, 200, 16, false, heroSecondary, row.Note)
		}
		source, ink := row.Source, heroText
		if source == "Skipped" {
			ink = heroSecondary
		}
		if source != "" {
			sheetText(dev, labels, used, s, 1212-sheetTextWidth(s, 19, false, source), ty+1, 240, 19, false, ink, source)
		}
	}
}

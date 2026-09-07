// Package fbgrid contains the small cover-grid primitive used by CGO-free
// linuxfb applications. New uses a deterministic fixture for the standalone
// spike; NewWithTiles lets an application provide its own catalog rows.
package fbgrid

import (
	"image"

	"github.com/DeanoC/FogCast/host/tenfoot/gfx"
	"github.com/DeanoC/FogCast/host/tenfoot/linuxinput"
)

// Tile is one catalog cell: a short label, a solid colour fallback, and
// optional decoded cover pixels.
type Tile struct {
	Name  string
	Color gfx.Color
	Cover *image.RGBA
}

const (
	defaultColumns = 4
	headerH        = 36
	footerH        = 28
	pad            = 16
	gap            = 8
	border         = 4
	// ConfirmFrames is how long the selected tile stays flashed.
	ConfirmFrames = 45
	repeatDelay   = 18
	repeatRate    = 8
)

// Highlight is the focus border, sampled as BGRX 0,220,255,0.
var Highlight = gfx.RGB(255, 220, 0)

// Flash is the confirm fill, sampled as BGRX 255,255,255,0.
var Flash = gfx.RGB(255, 255, 255)

// FakeTiles is the static 4×3 kit catalog.
func FakeTiles() []Tile {
	return []Tile{
		{Name: "SONIC 2", Color: gfx.RGB(40, 90, 200)},
		{Name: "STREETS", Color: gfx.RGB(200, 40, 40)},
		{Name: "GUNSTAR", Color: gfx.RGB(40, 180, 80)},
		{Name: "CASTLE", Color: gfx.RGB(140, 40, 180)},
		{Name: "PONG", Color: gfx.RGB(220, 180, 40)},
		{Name: "CONTRA", Color: gfx.RGB(40, 140, 160)},
		{Name: "METROID", Color: gfx.RGB(180, 80, 40)},
		{Name: "OUTRUN", Color: gfx.RGB(80, 80, 200)},
		{Name: "R-TYPE", Color: gfx.RGB(200, 40, 120)},
		{Name: "SIMCITY", Color: gfx.RGB(40, 160, 120)},
		{Name: "TETRIS", Color: gfx.RGB(60, 60, 60)},
		{Name: "DOOM", Color: gfx.RGB(160, 20, 20)},
	}
}

// Grid is a 2D focus over FakeTiles. Apply consumes mapped linuxinput
// events; Tick repeats a held direction after a short delay and decays
// the confirm flash.
type Grid struct {
	Tiles        []Tile
	Header       string
	Footer       string
	Columns      int
	Width        int
	Height       int
	HeaderH      int
	FooterH      int
	Pad          int
	Gap          int
	Border       int
	CellW        int
	CellH        int
	Focus        int
	Selected     string
	ConfirmIndex int
	ConfirmLeft  int
	Quit         bool
	Last         string
	holdX        int
	holdY        int
	analogX      bool
	analogY      bool
	waitX        int
	waitY        int
	confirmHeld  bool
}

// New lays out FakeTiles for a w×h framebuffer.
func New(w, h int) Grid {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	g := Grid{
		Tiles:        FakeTiles(),
		Columns:      defaultColumns,
		Width:        w,
		Height:       h,
		HeaderH:      headerH,
		FooterH:      footerH,
		Pad:          pad,
		Gap:          gap,
		Border:       border,
		ConfirmIndex: -1,
	}
	g.layout()
	return g
}

// NewWithTiles lays out caller-supplied tiles for a w×h framebuffer. The
// slice is copied so a catalog refresh cannot mutate a grid while it is being
// painted.
func NewWithTiles(w, h int, tiles []Tile) Grid {
	g := New(w, h)
	g.Tiles = append([]Tile(nil), tiles...)
	g.layout()
	return g
}

func (g *Grid) layout() {
	if g.Columns < 1 {
		g.Columns = 1
	}
	cols := g.Columns
	innerW := g.Width - 2*g.Pad - (cols-1)*g.Gap
	g.CellW = innerW / cols
	if g.CellW < 1 {
		g.CellW = 1
	}
	rows := g.rows()
	if rows < 1 {
		rows = 1
	}
	innerH := g.Height - g.HeaderH - g.FooterH - 2*g.Pad - (rows-1)*g.Gap
	g.CellH = innerH / rows
	if g.CellH < 1 {
		g.CellH = 1
	}
}

func (g Grid) rows() int {
	n := len(g.Tiles)
	if n == 0 || g.Columns < 1 {
		return 0
	}
	return (n + g.Columns - 1) / g.Columns
}

func (g Grid) count() int { return len(g.Tiles) }

// Move shifts focus by cells. Left/right stay on the current row.
func (g *Grid) Move(dx, dy int) {
	if g.count() == 0 {
		return
	}
	if g.Columns < 1 {
		g.Columns = 1
	}
	col := g.Focus % g.Columns
	row := g.Focus / g.Columns
	col += dx
	row += dy
	if col < 0 {
		col = 0
	}
	if col >= g.Columns {
		col = g.Columns - 1
	}
	if row < 0 {
		row = 0
	}
	maxRow := (g.count() - 1) / g.Columns
	if row > maxRow {
		row = maxRow
	}
	focus := row*g.Columns + col
	if focus >= g.count() {
		focus = g.count() - 1
	}
	if focus < 0 {
		focus = 0
	}
	g.Focus = focus
}

// CellOrigin is the top-left pixel of tile i.
func (g Grid) CellOrigin(i int) (x, y int, ok bool) {
	if i < 0 || i >= g.count() || g.Columns < 1 {
		return 0, 0, false
	}
	col := i % g.Columns
	row := i / g.Columns
	x = g.Pad + col*(g.CellW+g.Gap)
	y = g.HeaderH + g.Pad + row*(g.CellH+g.Gap)
	return x, y, true
}

// HighlightSample is a pixel on the focused tile's border.
func (g Grid) HighlightSample() (x, y int, ok bool) {
	ox, oy, ok := g.CellOrigin(g.Focus)
	if !ok {
		return 0, 0, false
	}
	return ox + 1, oy + 1, true
}

// InteriorSample is a pixel in the focused tile's fill, away from the label.
func (g Grid) InteriorSample() (x, y int, ok bool) {
	ox, oy, ok := g.CellOrigin(g.Focus)
	if !ok {
		return 0, 0, false
	}
	return ox + g.CellW/2, oy + g.CellH/2, true
}

// Apply updates hold state, steps on a rising edge, and confirms or quits.
func (g *Grid) Apply(m linuxinput.Mapped) {
	if m.Action == linuxinput.ActionNone {
		return
	}
	g.Last = m.String()
	switch m.Action {
	case linuxinput.ActionQuit:
		if m.Active {
			g.Quit = true
		}
	case linuxinput.ActionConfirm:
		g.applyConfirm(m)
	case linuxinput.ActionLeft, linuxinput.ActionRight:
		dir := 1
		if m.Action == linuxinput.ActionLeft {
			dir = -1
		}
		g.applyAxis(&g.holdX, &g.analogX, &g.waitX, dir, m, true)
	case linuxinput.ActionUp, linuxinput.ActionDown:
		dir := 1
		if m.Action == linuxinput.ActionUp {
			dir = -1
		}
		g.applyAxis(&g.holdY, &g.analogY, &g.waitY, dir, m, false)
	}
}

func (g *Grid) applyConfirm(m linuxinput.Mapped) {
	if m.Active && !m.Repeat && !g.confirmHeld {
		g.confirmHeld = true
		if g.Focus >= 0 && g.Focus < g.count() {
			g.Selected = g.Tiles[g.Focus].Name
			g.ConfirmIndex = g.Focus
			g.ConfirmLeft = ConfirmFrames
		}
		return
	}
	if !m.Active {
		g.confirmHeld = false
	}
}

func (g *Grid) applyAxis(hold *int, analog *bool, wait *int, dir int, m linuxinput.Mapped, horizontal bool) {
	if m.Active {
		rising := *hold != dir
		*hold = dir
		*analog = m.Analog
		if rising && !m.Repeat {
			g.step(dir, horizontal)
			*wait = repeatDelay
		}
		return
	}
	if m.Analog {
		if *analog {
			*hold = 0
			*analog = false
			*wait = 0
		}
		return
	}
	if *hold == dir {
		*hold = 0
		*analog = false
		*wait = 0
	}
}

func (g *Grid) step(dir int, horizontal bool) {
	if horizontal {
		g.Move(dir, 0)
		return
	}
	g.Move(0, dir)
}

// Tick repeats motion while a direction is held and decays confirm flash.
func (g *Grid) Tick() {
	if g.ConfirmLeft > 0 {
		g.ConfirmLeft--
	}
	g.tickAxis(g.holdX, &g.waitX, true)
	g.tickAxis(g.holdY, &g.waitY, false)
}

func (g *Grid) tickAxis(hold int, wait *int, horizontal bool) {
	if hold == 0 {
		return
	}
	if *wait > 0 {
		*wait--
		if *wait > 0 {
			return
		}
	}
	g.step(hold, horizontal)
	*wait = repeatRate
}

// Status is the footer line.
func (g Grid) Status() string {
	name := ""
	if g.Focus >= 0 && g.Focus < g.count() {
		name = g.Tiles[g.Focus].Name
	}
	if g.Selected != "" {
		if g.ConfirmLeft > 0 {
			return "SEL " + g.Selected
		}
		return "WAS " + g.Selected + "  " + name
	}
	if name == "" {
		return "idle"
	}
	return name
}

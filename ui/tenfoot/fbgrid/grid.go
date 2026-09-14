// Package fbgrid contains the small cover-grid primitive used by CGO-free
// linuxfb applications. New uses a deterministic fixture for the standalone
// spike; NewWithTiles lets an application provide its own catalog rows.
package fbgrid

import (
	"image"
	"time"

	"github.com/DeanoC/FogCast/ui/tenfoot/audioreact"
	"github.com/DeanoC/FogCast/ui/tenfoot/gfx"
	"github.com/DeanoC/FogCast/ui/tenfoot/linuxinput"
	"github.com/DeanoC/FogCast/ui/tenfoot/theme"
)

// CoverKind is the cheap cover-fetch state painted when Cover is nil.
type CoverKind int

const (
	// CoverMissing is no handle, a failed fetch, or art that will not arrive.
	CoverMissing CoverKind = iota
	// CoverLoading is an in-flight GET/decode.
	CoverLoading
	// CoverPresent is a decoded image in Tile.Cover.
	CoverPresent
)

// Tile is one catalog cell: a short label, a solid colour fallback, optional
// decoded cover pixels, the cover-fetch kind used for placeholders, an
// optional clear logo for the label bar, optional metadata chips, and
// optional short catalog facts for split hero copy.
type Tile struct {
	Name      string
	Color     gfx.Color
	Cover     *image.RGBA
	CoverKind CoverKind
	// Box is optional 3D box/cart art. Focused cells prefer it over Cover;
	// unfocused cells keep Cover and only use Box when Cover is missing.
	Box    *image.RGBA
	Logo   *image.RGBA
	Badges []Badge
	// Meta is optional short catalog/presentation facts for split hero copy.
	Meta string
}

const (
	// DefaultColumns is the kit catalog row width (4×3 pages of 12).
	DefaultColumns = 4
	headerH        = 36
	footerH        = 28
	pad            = 16
	gap            = 8
	border         = 4
	// ConfirmFrames is how long the selected tile's confirm pulse runs.
	ConfirmFrames = 45
	repeatDelay   = 18
	repeatRate    = 8
)

// Highlight is the default-theme focus border, sampled as BGRX 0,220,255,0.
var Highlight = theme.Default().Highlight

// Flash is the default-theme confirm fill, sampled as BGRX 255,255,255,0.
var Flash = theme.Default().Flash

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
// events; Tick repeats a held direction after a short delay and advances
// the focus pop and confirm pulse. Now, when set, drives those tweens from
// wall-clock instead of TickPeriod.
type Grid struct {
	Tiles          []Tile
	Header         string
	Footer         string
	Columns        int
	Width          int
	Height         int
	HeaderH        int
	FooterH        int
	Pad            int
	Gap            int
	Border         int
	CellW          int
	CellH          int
	Focus          int
	Selected       string
	ConfirmIndex   int
	ConfirmLeft    int
	Quit           bool
	Last           string
	Theme          theme.Theme
	Now            time.Time
	holdX          int
	holdY          int
	analogX        bool
	analogY        bool
	waitX          int
	waitY          int
	confirmHeld    bool
	popAt          time.Time
	confirmAt      time.Time
	popElapsed     time.Duration
	confirmElapsed time.Duration
	popIndex       int
	popLive        bool
	Strip          []Tile
	StripLabel     string
	StripFocus     int
	StripActive    bool
	StripCellW     int
	StripCellH     int
	Series         []Tile
	SeriesLabel    string
	SeriesFocus    int
	SeriesActive   bool
	// Atmosphere is optional fanart/backdrop painted cover-fill behind chrome.
	// When nil, Paint uses a dimmed cover-wall of decoded tile/strip covers.
	// When those are also empty, the stage stays the solid theme background.
	Atmosphere *image.RGBA
	// Kind selects the catalog presentation. Zero is the 4×3 box grid.
	Kind BrowseKind
	// EmptyLabel is optional stage copy when Tiles is empty (search misses).
	// Ordinary empty catalogs leave this blank and keep chrome only.
	EmptyLabel string
	// Session is optional pause overlay chrome when the host reports a live
	// session. Empty State leaves browse undimmed.
	Session SessionChrome
	// Audio is optional edge chrome. Zero skips paint (default).
	Audio audioreact.Sample
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
		Columns:      DefaultColumns,
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

// ApplyTheme copies colour, spacing, and chrome tokens onto g and relayouts.
// Zero Theme selects Default, which matches New's built-in spacing.
func ApplyTheme(g *Grid, th theme.Theme) {
	if g == nil {
		return
	}
	th = th.Complete()
	g.Theme = th
	g.Pad = th.Pad
	g.Gap = th.Gap
	g.Border = th.Border
	g.HeaderH = th.HeaderH
	g.FooterH = th.FooterH
	g.layout()
}

func (g *Grid) layout() {
	switch g.Kind {
	case BrowseWall:
		g.Columns = WallColumns
	case BrowseCoverflow:
		n := g.count()
		if n < 1 {
			n = 1
		}
		g.Columns = n
		g.layoutStrip()
		if r, ok := g.coverflowRect(g.Focus); ok {
			g.CellW = int(r.W)
			g.CellH = int(r.H)
		} else {
			g.CellW = coverflowFocusW
			g.CellH = coverflowFocusH
		}
		if g.CellW < 1 {
			g.CellW = 1
		}
		if g.CellH < 1 {
			g.CellH = 1
		}
		return
	case BrowseSplit:
		g.Columns = 1
		g.layoutStrip()
		if r, ok := g.splitListRect(g.Focus); ok {
			g.CellW = int(r.W)
			g.CellH = int(r.H)
		} else if r, ok := g.splitListRect(0); ok {
			g.CellW = int(r.W)
			g.CellH = int(r.H)
		} else {
			g.CellW = splitListMinW
			g.CellH = splitRowMaxH
		}
		if g.CellW < 1 {
			g.CellW = 1
		}
		if g.CellH < 1 {
			g.CellH = 1
		}
		return
	default:
		if g.Columns < 1 {
			g.Columns = DefaultColumns
		}
	}
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
	innerH := g.Height - g.HeaderH - g.FooterH - 2*g.Pad - (rows-1)*g.Gap - g.stripReserve()
	g.CellH = innerH / rows
	if g.CellH < 1 {
		g.CellH = 1
	}
	g.layoutStrip()
}

func (g Grid) rows() int {
	n := len(g.Tiles)
	if n == 0 || g.Columns < 1 {
		return 0
	}
	return (n + g.Columns - 1) / g.Columns
}

func (g Grid) count() int { return len(g.Tiles) }

// MoveFocus shifts a flat catalog index by cells. Left/right stay on the
// current row and clamp at the row ends. Up/down move by columns and clamp
// at the first and last rows. An incomplete last row clamps onto the last
// item instead of wrapping.
func MoveFocus(focus, count, columns, dx, dy int) int {
	if count <= 0 {
		return 0
	}
	if columns < 1 {
		columns = 1
	}
	if focus < 0 {
		focus = 0
	}
	if focus >= count {
		focus = count - 1
	}
	col := focus % columns
	row := focus / columns
	col += dx
	row += dy
	if col < 0 {
		col = 0
	}
	if col >= columns {
		col = columns - 1
	}
	if row < 0 {
		row = 0
	}
	maxRow := (count - 1) / columns
	if row > maxRow {
		row = maxRow
	}
	focus = row*columns + col
	if focus >= count {
		focus = count - 1
	}
	if focus < 0 {
		focus = 0
	}
	return focus
}

// Move shifts focus by cells. Left/right stay on the current row. A change
// starts the focus pop; the catalog layout (CellOrigin) is unchanged.
func (g *Grid) Move(dx, dy int) {
	next := MoveFocus(g.Focus, g.count(), g.Columns, dx, dy)
	if next == g.Focus {
		return
	}
	g.Focus = next
	g.startFocusPop()
}

// CellOrigin is the top-left pixel of tile i.
func (g Grid) CellOrigin(i int) (x, y int, ok bool) {
	r, ok := g.tileBaseRect(i)
	if !ok {
		return 0, 0, false
	}
	return int(r.X), int(r.Y), true
}

func (g Grid) gridCellOrigin(i int) (x, y int, ok bool) {
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

// LabelBarSample is a pixel in tile i's label bar, centered horizontally.
func (g Grid) LabelBarSample(i int) (x, y int, ok bool) {
	r, ok := g.tileInner(i)
	if !ok {
		r, ok = g.tileRect(i)
		if !ok {
			return 0, 0, false
		}
	}
	th := g.Theme.Complete()
	textH := gfx.TextHeightWeight(th.BodyPx(), th.BodyWeight())
	barH := float32(textH + 4)
	if barH < 16 {
		barH = 16
	}
	if i >= 0 && i < len(g.Tiles) && g.Tiles[i].Logo != nil && barH < 24 {
		barH = 24
	}
	if barH > r.H {
		barH = r.H
	}
	if barH < 2 || r.W < 2 {
		return 0, 0, false
	}
	x = int(r.X + r.W/2)
	y = int(r.Y + r.H - barH/2)
	return x, y, true
}

// PanelSample is a pixel on tile i's placeholder or letterbox panel: inside
// the focus border and 1px outline, above the label bar.
func (g Grid) PanelSample(i int) (x, y int, ok bool) {
	ox, oy, ok := g.CellOrigin(i)
	if !ok {
		return 0, 0, false
	}
	inset := 4
	if i == g.Focus {
		inset = g.Border + 4
		if inset < 5 {
			inset = 5
		}
	}
	return ox + inset, oy + inset, true
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
			g.startConfirmPulse()
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

// Tick repeats motion while a direction is held and advances focus pop and
// confirm pulse by TickPeriod.
func (g *Grid) Tick() {
	if g.ConfirmLeft > 0 {
		g.ConfirmLeft--
	}
	g.advanceMotion()
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

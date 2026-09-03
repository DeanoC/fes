package tenfoot

// Grid is a 2D focus graph over a linear catalog.
type Grid struct {
	Count        int
	Columns      int
	VisibleRows  int
	Focus        int
	ScrollRow    int
	HeaderHeight int
	FooterHeight int
	Pad          int
	Gap          int
	CellW        int
	CellH        int
	Width        int
	Height       int
	Safe         SafeArea
}

const (
	defaultHeaderHeight = 88
	defaultFooterHeight = 120
	defaultPad          = 36
	defaultGap          = 18
	defaultCellW        = 210
	defaultCellH        = 320
)

func (g *Grid) defaults() {
	if g.HeaderHeight <= 0 {
		g.HeaderHeight = defaultHeaderHeight
	}
	if g.FooterHeight <= 0 {
		g.FooterHeight = defaultFooterHeight
	}
	if g.Pad <= 0 {
		g.Pad = defaultPad
	}
	if g.Gap <= 0 {
		g.Gap = defaultGap
	}
	if g.CellW <= 0 {
		g.CellW = defaultCellW
	}
	if g.CellH <= 0 {
		g.CellH = defaultCellH
	}
	if g.Columns < 1 {
		g.Columns = 1
	}
	if g.VisibleRows < 1 {
		g.VisibleRows = 1
	}
}

// Layout recomputes columns and visible rows from the window size.
func (g *Grid) Layout(width, height int) {
	g.defaults()
	g.CellW = defaultCellW
	g.CellH = defaultCellH
	g.Width = width
	g.Height = height
	innerW := width - g.Safe.Left - g.Safe.Right - 2*g.Pad
	if innerW < g.CellW {
		innerW = g.CellW
	}
	g.Columns = (innerW + g.Gap) / (g.CellW + g.Gap)
	if g.Columns < 1 {
		g.Columns = 1
	}
	innerH := height - g.Safe.Top - g.Safe.Bottom - g.HeaderHeight - g.FooterHeight - 2*g.Pad
	if innerH < 1 {
		innerH = 1
	}
	if g.CellH > innerH {
		g.CellH = innerH
	}
	g.VisibleRows = (innerH + g.Gap) / (g.CellH + g.Gap)
	if g.VisibleRows < 1 {
		g.VisibleRows = 1
	}
	g.clamp()
}

// SetCount updates the catalog length and keeps focus in range.
func (g *Grid) SetCount(n int) {
	if n < 0 {
		n = 0
	}
	g.Count = n
	g.clamp()
}

// Move shifts focus by cells. Left/right stay on the current row; up/down change rows.
func (g *Grid) Move(dx, dy int) {
	g.defaults()
	if g.Count == 0 {
		return
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
	maxRow := (g.Count - 1) / g.Columns
	if row > maxRow {
		row = maxRow
	}
	focus := row*g.Columns + col
	if focus >= g.Count {
		focus = g.Count - 1
	}
	if focus < 0 {
		focus = 0
	}
	g.Focus = focus
	g.ensureVisible()
}

// VisibleRange is the half-open [start, end) catalog span currently on screen.
func (g *Grid) VisibleRange() (int, int) {
	g.defaults()
	if g.Count == 0 {
		return 0, 0
	}
	start := g.ScrollRow * g.Columns
	if start > g.Count {
		start = g.Count
	}
	end := (g.ScrollRow + g.VisibleRows) * g.Columns
	if end > g.Count {
		end = g.Count
	}
	if start < 0 {
		start = 0
	}
	return start, end
}

// PrefetchRange extends the visible span by extra rows for async cover loads.
func (g *Grid) PrefetchRange(extraRows int) (int, int) {
	g.defaults()
	if extraRows < 0 {
		extraRows = 0
	}
	start, end := g.VisibleRange()
	start -= extraRows * g.Columns
	if start < 0 {
		start = 0
	}
	end += extraRows * g.Columns
	if end > g.Count {
		end = g.Count
	}
	return start, end
}

// CellOrigin returns the top-left pixel of catalog index i, or false if offscreen.
func (g *Grid) CellOrigin(i int) (x, y int, ok bool) {
	g.defaults()
	start, end := g.VisibleRange()
	if i < start || i >= end {
		return 0, 0, false
	}
	local := i - start
	col := local % g.Columns
	row := local / g.Columns
	x = g.Safe.Left + g.Pad + col*(g.CellW+g.Gap)
	y = g.Safe.Top + g.HeaderHeight + g.Pad + row*(g.CellH+g.Gap)
	return x, y, true
}

func (g *Grid) clamp() {
	g.defaults()
	if g.Count == 0 {
		g.Focus = 0
		g.ScrollRow = 0
		return
	}
	if g.Focus >= g.Count {
		g.Focus = g.Count - 1
	}
	if g.Focus < 0 {
		g.Focus = 0
	}
	g.ensureVisible()
}

func (g *Grid) ensureVisible() {
	if g.Count == 0 || g.Columns < 1 {
		g.ScrollRow = 0
		return
	}
	row := g.Focus / g.Columns
	if row < g.ScrollRow {
		g.ScrollRow = row
	}
	if row >= g.ScrollRow+g.VisibleRows {
		g.ScrollRow = row - g.VisibleRows + 1
	}
	if g.ScrollRow < 0 {
		g.ScrollRow = 0
	}
}

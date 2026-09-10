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
	Mode         LayoutKind
}

const (
	defaultHeaderHeight = 88
	defaultFooterHeight = 120
	defaultPad          = 36
	defaultGap          = 18
	defaultCellW        = 210
	defaultCellH        = 320
	defaultShelfCellW   = 280
	defaultShelfCellH   = 400
	defaultListRowH     = 88
	listThumbW          = 56
	listThumbPad        = 8
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
	g.Width = width
	g.Height = height
	switch g.Mode {
	case LayoutShelf:
		g.layoutShelf()
	case LayoutList:
		g.layoutList()
	default:
		g.layoutGrid()
	}
	g.clamp()
}

func (g *Grid) layoutGrid() {
	g.CellW = defaultCellW
	g.CellH = defaultCellH
	innerW := g.innerWidth()
	if innerW < g.CellW {
		innerW = g.CellW
	}
	g.Columns = (innerW + g.Gap) / (g.CellW + g.Gap)
	if g.Columns < 1 {
		g.Columns = 1
	}
	innerH := g.innerHeight()
	if g.CellH > innerH {
		g.CellH = innerH
	}
	g.VisibleRows = (innerH + g.Gap) / (g.CellH + g.Gap)
	if g.VisibleRows < 1 {
		g.VisibleRows = 1
	}
}

func (g *Grid) layoutShelf() {
	g.VisibleRows = 1
	g.CellW = defaultShelfCellW
	g.CellH = defaultShelfCellH
	innerH := g.innerHeight()
	if g.CellH > innerH {
		g.CellH = innerH
	}
	if defaultShelfCellH > 0 {
		g.CellW = defaultShelfCellW * g.CellH / defaultShelfCellH
	}
	if g.CellW < 1 {
		g.CellW = 1
	}
	innerW := g.innerWidth()
	if innerW < g.CellW {
		innerW = g.CellW
	}
	g.Columns = (innerW + g.Gap) / (g.CellW + g.Gap)
	if g.Columns < 1 {
		g.Columns = 1
	}
}

func (g *Grid) layoutList() {
	g.Columns = 1
	g.CellH = defaultListRowH
	innerW := g.innerWidth()
	if innerW < 1 {
		innerW = 1
	}
	g.CellW = innerW
	innerH := g.innerHeight()
	if g.CellH > innerH {
		g.CellH = innerH
	}
	g.VisibleRows = (innerH + g.Gap) / (g.CellH + g.Gap)
	if g.VisibleRows < 1 {
		g.VisibleRows = 1
	}
}

func (g *Grid) innerWidth() int {
	innerW := g.Width - g.Safe.Left - g.Safe.Right - 2*g.Pad
	if innerW < 1 {
		return 1
	}
	return innerW
}

func (g *Grid) innerHeight() int {
	innerH := g.Height - g.Safe.Top - g.Safe.Bottom - g.HeaderHeight - g.FooterHeight - 2*g.Pad
	if innerH < 1 {
		return 1
	}
	return innerH
}

func (g *Grid) pageSize() int {
	switch g.Mode {
	case LayoutList:
		if g.VisibleRows < 1 {
			return 1
		}
		return g.VisibleRows
	default:
		if g.Columns < 1 {
			return 1
		}
		return g.Columns
	}
}

// SetCount updates the catalog length and keeps focus in range.
func (g *Grid) SetCount(n int) {
	if n < 0 {
		n = 0
	}
	g.Count = n
	g.clamp()
}

// Move shifts focus by cells. Grid left/right stay on the current row.
// Shelf left/right move one title; up/down jump by a visible page of covers.
// List up/down move one row; left/right jump by a visible page of rows.
func (g *Grid) Move(dx, dy int) {
	g.defaults()
	if g.Count == 0 {
		return
	}
	switch g.Mode {
	case LayoutShelf:
		g.moveLinear(dx + dy*g.pageSize())
	case LayoutList:
		g.moveLinear(dy + dx*g.pageSize())
	default:
		g.moveGrid(dx, dy)
	}
}

func (g *Grid) moveGrid(dx, dy int) {
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

func (g *Grid) moveLinear(delta int) {
	focus := g.Focus + delta
	if focus < 0 {
		focus = 0
	}
	if focus >= g.Count {
		focus = g.Count - 1
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
	if g.Mode == LayoutShelf || g.Mode == LayoutList {
		start := g.ScrollRow
		if start < 0 {
			start = 0
		}
		if start > g.Count {
			start = g.Count
		}
		end := start + g.visibleCount()
		if end > g.Count {
			end = g.Count
		}
		return start, end
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

func (g *Grid) visibleCount() int {
	n := g.pageSize()
	if g.Mode == LayoutShelf {
		n = g.Columns
	}
	if n < 1 {
		return 1
	}
	return n
}

// PrefetchRange extends the visible span by extra pages for async cover loads.
func (g *Grid) PrefetchRange(extraRows int) (int, int) {
	g.defaults()
	if extraRows < 0 {
		extraRows = 0
	}
	start, end := g.VisibleRange()
	page := g.pageSize()
	start -= extraRows * page
	if start < 0 {
		start = 0
	}
	end += extraRows * page
	if end > g.Count {
		end = g.Count
	}
	return start, end
}

// CellRect is the on-screen pixel box of catalog index i, or false if offscreen.
func (g Grid) CellRect(i int) (x, y, w, h int, ok bool) {
	x, y, ok = g.CellOrigin(i)
	if !ok {
		return 0, 0, 0, 0, false
	}
	return x, y, g.CellW, g.CellH, true
}

// HitIndex returns the visible catalog index whose cell contains (px, py).
func (g Grid) HitIndex(px, py int) (int, bool) {
	start, end := g.VisibleRange()
	for i := start; i < end; i++ {
		x, y, w, h, ok := g.CellRect(i)
		if ok && px >= x && py >= y && px < x+w && py < y+h {
			return i, true
		}
	}
	return -1, false
}

// CellOrigin returns the top-left pixel of catalog index i, or false if offscreen.
func (g *Grid) CellOrigin(i int) (x, y int, ok bool) {
	g.defaults()
	start, end := g.VisibleRange()
	if i < start || i >= end {
		return 0, 0, false
	}
	local := i - start
	switch g.Mode {
	case LayoutShelf:
		x = g.Safe.Left + g.Pad + local*(g.CellW+g.Gap)
		y = g.shelfOriginY()
		return x, y, true
	case LayoutList:
		x = g.Safe.Left + g.Pad
		y = g.Safe.Top + g.HeaderHeight + g.Pad + local*(g.CellH+g.Gap)
		return x, y, true
	default:
		col := local % g.Columns
		row := local / g.Columns
		x = g.Safe.Left + g.Pad + col*(g.CellW+g.Gap)
		y = g.Safe.Top + g.HeaderHeight + g.Pad + row*(g.CellH+g.Gap)
		return x, y, true
	}
}

func (g *Grid) shelfOriginY() int {
	availTop := g.Safe.Top + g.HeaderHeight + g.Pad
	availBot := g.footerY() - g.Pad
	if availBot-availTop > g.CellH {
		return availTop + (availBot-availTop-g.CellH)/2
	}
	return availTop
}

func (g *Grid) listThumbRect(x, y int) (tx, ty, tw, th int) {
	th = g.CellH - 2*listThumbPad
	if th < 1 {
		th = 1
	}
	tw = listThumbW
	tx = x + listThumbPad
	ty = y + listThumbPad
	return tx, ty, tw, th
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
	if g.Mode == LayoutShelf || g.Mode == LayoutList {
		page := g.visibleCount()
		if g.Focus < g.ScrollRow {
			g.ScrollRow = g.Focus
		}
		if g.Focus >= g.ScrollRow+page {
			g.ScrollRow = g.Focus - page + 1
		}
		if g.ScrollRow < 0 {
			g.ScrollRow = 0
		}
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

package tenfoot

// Shared overlay geometry for draw and pointer hit-testing.
// Numbers match the current sofa chrome in draw.go.

type rectI struct {
	X, Y, W, H int
}

func (r rectI) contains(x, y int) bool {
	return r.W > 0 && r.H > 0 && x >= r.X && y >= r.Y && x < r.X+r.W && y < r.Y+r.H
}

type listPanel struct {
	X, Y, W, H     int
	HeaderH        int
	FooterH        int
	RowH           int
	Start, Visible int
}

func (p listPanel) contains(x, y int) bool {
	return rectI{p.X, p.Y, p.W, p.H}.contains(x, y)
}

func (p listPanel) rowAt(px, py int) (int, bool) {
	if p.RowH < 1 || p.Visible < 1 {
		return 0, false
	}
	if px < p.X || px >= p.X+p.W {
		return 0, false
	}
	bodyY := p.Y + p.HeaderH
	if py < bodyY {
		return 0, false
	}
	vis := (py - bodyY) / p.RowH
	if vis < 0 || vis >= p.Visible {
		return 0, false
	}
	if py >= bodyY+p.Visible*p.RowH {
		return 0, false
	}
	return p.Start + vis, true
}

func (p listPanel) rowY(visibleIndex int) int {
	return p.Y + p.HeaderH + visibleIndex*p.RowH
}

type detailGeom struct {
	Pane     rectI
	Carousel rectI
}

func detailGeomOf(snap Snapshot) (detailGeom, bool) {
	h := snap.Grid.FooterHeight
	if snap.Detail.Open {
		h = detailPaneHeight(snap.Grid)
	}
	if h < 1 {
		return detailGeom{}, false
	}
	x := snap.Grid.contentLeft()
	w := snap.Grid.contentWidth()
	y := snap.Grid.Height - snap.Grid.Safe.Bottom - h
	minY := snap.Grid.headerY() + snap.Grid.HeaderHeight
	if y < minY {
		y = minY
		h = snap.Grid.Height - snap.Grid.Safe.Bottom - y
	}
	if h < 1 {
		return detailGeom{}, false
	}
	g := detailGeom{Pane: rectI{X: x, Y: y, W: w, H: h}}
	if !snap.Detail.Open || snap.Detail.Count < 1 {
		return g, true
	}
	pad := 24
	shotW := w * 38 / 100
	if shotW < 160 {
		shotW = 160
	}
	if shotW > w/2 {
		shotW = w / 2
	}
	textW := w - shotW - 3*pad
	if textW < 160 {
		return g, true
	}
	g.Carousel = rectI{X: x + w - pad - shotW, Y: y + 12, W: shotW, H: h - 24}
	return g, true
}

func roomDestGeom(snap Snapshot) (rectI, bool) {
	if !snap.Room.Open || snap.Room.Err != "" || snap.Detail.Open || snap.Room.Choice.Open {
		return rectI{}, false
	}
	if !snap.Room.Destination.Set() {
		return rectI{}, false
	}
	pad := 16
	h := 118
	w := snap.Room.Width - 2*pad
	if w < 120 {
		return rectI{}, false
	}
	x := snap.Room.OffsetX + pad
	y := snap.Room.OffsetY + snap.Room.Height - h - 8
	if y < snap.Room.OffsetY+48 {
		y = snap.Room.OffsetY + 48
		h = snap.Room.OffsetY + snap.Room.Height - 8 - y
	}
	if h < 72 {
		return rectI{}, false
	}
	return rectI{X: x, Y: y, W: w, H: h}, true
}

func roomChoicePanel(snap Snapshot) (listPanel, bool) {
	if !snap.Room.Choice.Open {
		return listPanel{}, false
	}
	rows := len(snap.Room.Choice.Rows)
	if rows < 1 {
		rows = 1
	}
	g := snap.Grid
	panelW := 640
	if max := g.contentWidth() - 48; panelW > max {
		panelW = max
	}
	if panelW < 280 {
		panelW = g.contentWidth()
	}
	headerH, footerH, rowH := 48, 36, 44
	visible := rows
	if visible > 6 {
		visible = 6
	}
	panelH := headerH + footerH + visible*rowH + 8
	x := g.contentLeft() + (g.contentWidth()-panelW)/2
	y := g.contentTop() + (g.contentHeight()-panelH)/2
	if y < g.contentTop()+8 {
		y = g.contentTop() + 8
	}
	start := snap.Room.Choice.Index - visible/2
	if start < 0 {
		start = 0
	}
	if start > rows-visible {
		start = rows - visible
	}
	if start < 0 {
		start = 0
	}
	return listPanel{X: x, Y: y, W: panelW, H: panelH, HeaderH: headerH, FooterH: footerH, RowH: rowH, Start: start, Visible: visible}, true
}

func viewPickerRows(snap Snapshot) []LibraryView {
	rows := snap.PickerRows
	if len(rows) == 0 {
		rows = snap.Views
	}
	return rows
}

func viewPickerPanel(snap Snapshot) (listPanel, bool) {
	rows := viewPickerRows(snap)
	if !snap.ViewPicker || len(rows) == 0 {
		return listPanel{}, false
	}
	g := snap.Grid
	contentW := g.contentWidth()
	panelW := 480
	if panelW > contentW-48 {
		panelW = contentW - 48
	}
	if panelW < 200 {
		panelW = contentW - 24
	}
	rowH := 28
	headerH := 40
	footerH := 24
	maxRows := 10
	if maxRows > len(rows) {
		maxRows = len(rows)
	}
	panelH := headerH + maxRows*rowH + footerH
	x := g.contentLeft() + (contentW-panelW)/2
	y := g.headerY() + g.HeaderHeight + 12
	if y+panelH > g.footerY()-8 {
		y = g.contentTop() + (g.contentHeight()-panelH)/2
	}
	start := snap.ViewPickerIndex - maxRows/2
	if start < 0 {
		start = 0
	}
	if start+maxRows > len(rows) {
		start = len(rows) - maxRows
	}
	if start < 0 {
		start = 0
	}
	return listPanel{
		X: x, Y: y, W: panelW, H: panelH,
		HeaderH: headerH, FooterH: footerH, RowH: rowH,
		Start: start, Visible: maxRows,
	}, true
}

func collectionMenuPanel(snap Snapshot) (listPanel, bool) {
	menu := snap.CollectionMenu
	if !menu.Open {
		return listPanel{}, false
	}
	g := snap.Grid
	contentW := g.contentWidth()
	panelW := 420
	if panelW > contentW-48 {
		panelW = contentW - 48
	}
	if panelW < 200 {
		panelW = contentW - 24
	}
	rowH := 28
	headerH := 44
	footerH := 28
	rows := menu.Rows
	panelH := headerH + len(rows)*rowH + footerH
	if panelH < headerH+footerH+rowH {
		panelH = headerH + footerH + rowH
	}
	x := g.contentLeft() + (contentW-panelW)/2
	y := g.headerY() + g.HeaderHeight + 48
	if y+panelH > g.footerY()-8 {
		y = g.contentTop() + (g.contentHeight()-panelH)/2
	}
	visible := len(rows)
	if visible < 1 {
		visible = 1
	}
	return listPanel{
		X: x, Y: y, W: panelW, H: panelH,
		HeaderH: headerH, FooterH: footerH, RowH: rowH,
		Start: 0, Visible: visible,
	}, true
}

func settingsPanel(snap Snapshot) (listPanel, bool) {
	if !snap.Settings.Open || len(snap.Settings.Rows) == 0 {
		return listPanel{}, false
	}
	g := snap.Grid
	contentW := g.contentWidth()
	contentH := g.contentHeight()
	panelW := 720
	if panelW > contentW-48 {
		panelW = contentW - 48
	}
	if panelW < 280 {
		panelW = contentW - 24
	}
	rowH := 32
	headerH := 44
	footerH := 28
	rows := snap.Settings.Rows
	panelH := headerH + len(rows)*rowH + footerH
	maxH := contentH - 24
	if maxH < 120 {
		maxH = contentH
	}
	if panelH > maxH {
		panelH = maxH
	}
	x := g.contentLeft() + (contentW-panelW)/2
	y := g.headerY() + g.HeaderHeight + 12
	if y+panelH > g.footerY()-8 {
		y = g.contentTop() + (contentH-panelH)/2
	}
	visible := (panelH - headerH - footerH) / rowH
	if visible < 1 {
		visible = 1
	}
	if visible > len(rows) {
		visible = len(rows)
	}
	start := snap.Settings.Index - visible/2
	if start < 0 {
		start = 0
	}
	if start+visible > len(rows) {
		start = len(rows) - visible
	}
	if start < 0 {
		start = 0
	}
	return listPanel{
		X: x, Y: y, W: panelW, H: panelH,
		HeaderH: headerH, FooterH: footerH, RowH: rowH,
		Start: start, Visible: visible,
	}, true
}

func filtersPanel(snap Snapshot) (listPanel, bool) {
	if !snap.Filters.Open || len(snap.Filters.Rows) == 0 {
		return listPanel{}, false
	}
	g := snap.Grid
	contentW := g.contentWidth()
	contentH := g.contentHeight()
	panelW := 560
	if panelW > contentW-48 {
		panelW = contentW - 48
	}
	if panelW < 280 {
		panelW = contentW - 24
	}
	rowH := 28
	headerH := 44
	footerH := 28
	rows := snap.Filters.Rows
	maxH := contentH - 24
	if maxH < 120 {
		maxH = contentH
	}
	visible := (maxH - headerH - footerH) / rowH
	if visible < 1 {
		visible = 1
	}
	if visible > len(rows) {
		visible = len(rows)
	}
	if visible > 12 {
		visible = 12
	}
	panelH := headerH + visible*rowH + footerH
	if panelH > maxH {
		panelH = maxH
	}
	x := g.contentLeft() + (contentW-panelW)/2
	y := g.headerY() + g.HeaderHeight + 12
	if y+panelH > g.footerY()-8 {
		y = g.contentTop() + (contentH-panelH)/2
	}
	start := snap.Filters.Index - visible/2
	if start < 0 {
		start = 0
	}
	if start+visible > len(rows) {
		start = len(rows) - visible
	}
	if start < 0 {
		start = 0
	}
	return listPanel{
		X: x, Y: y, W: panelW, H: panelH,
		HeaderH: headerH, FooterH: footerH, RowH: rowH,
		Start: start, Visible: visible,
	}, true
}

type oskGeom struct {
	X, Y, W, H int
	HeaderH    int
	FooterH    int
	KeyH, Gap  int
}

func (g oskGeom) contains(x, y int) bool {
	return rectI{g.X, g.Y, g.W, g.H}.contains(x, y)
}

type oskKeyRect struct {
	Row, Col   int
	ID         string
	Label      string
	Focus      bool
	X, Y, W, H int
}

func oskLayout(snap Snapshot) (oskGeom, []oskKeyRect, bool) {
	if !snap.OSK.Open || len(snap.OSK.Rows) == 0 {
		return oskGeom{}, nil, false
	}
	g := snap.Grid
	contentW := g.contentWidth()
	contentH := g.contentHeight()
	panelW := contentW - 48
	if panelW < 280 {
		panelW = contentW - 24
	}
	if panelW < 1 {
		panelW = contentW
	}
	headerH := 48
	footerH := 28
	gap := 6
	rows := snap.OSK.Rows
	maxH := contentH - 24
	if maxH < 120 {
		maxH = contentH
	}
	keyH := 40
	panelH := headerH + len(rows)*keyH + (len(rows)-1)*gap + footerH + 16
	if panelH > maxH {
		remain := maxH - headerH - footerH - 16 - (len(rows)-1)*gap
		if remain < len(rows)*24 {
			remain = len(rows) * 24
		}
		keyH = remain / len(rows)
		if keyH < 22 {
			keyH = 22
		}
		panelH = headerH + len(rows)*keyH + (len(rows)-1)*gap + footerH + 16
		if panelH > maxH {
			panelH = maxH
		}
	}
	x := g.contentLeft() + (contentW-panelW)/2
	y := g.headerY() + g.HeaderHeight + 12
	if y+panelH > g.footerY()-8 {
		y = g.contentTop() + (contentH-panelH)/2
	}
	if y < g.contentTop()+8 {
		y = g.contentTop() + 8
	}
	geom := oskGeom{X: x, Y: y, W: panelW, H: panelH, HeaderH: headerH, FooterH: footerH, KeyH: keyH, Gap: gap}
	innerX := x + 12
	innerW := panelW - 24
	if innerW < 1 {
		innerW = 1
	}
	refCols := 10
	unitW := (innerW - (refCols-1)*gap) / refCols
	if unitW < 8 {
		unitW = 8
	}
	keys := make([]oskKeyRect, 0, 48)
	rowY := y + headerH
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
			keys = append(keys, oskKeyRect{
				Row: r, Col: c, ID: key.ID, Label: key.Label, Focus: key.Focus,
				X: colX, Y: rowY, W: kw, H: keyH,
			})
			colX += kw + gap
		}
		rowY += keyH + gap
	}
	return geom, keys, true
}

func roomPickerPanel(snap Snapshot) (listPanel, bool) {
	rows := snap.RoomPicker.Rows
	if !snap.RoomPicker.Open || len(rows) == 0 {
		return listPanel{}, false
	}
	g := snap.Grid
	contentW := g.contentWidth()
	contentH := g.contentHeight()
	panelW := 640
	if panelW > contentW-48 {
		panelW = contentW - 48
	}
	if panelW < 240 {
		panelW = contentW - 24
	}
	rowH := 48
	headerH := 52
	footerH := 28
	maxRows := (contentH - 24 - headerH - footerH) / rowH
	if maxRows < 1 {
		maxRows = 1
	}
	if maxRows > 8 {
		maxRows = 8
	}
	if maxRows > len(rows) {
		maxRows = len(rows)
	}
	panelH := headerH + maxRows*rowH + footerH
	x := g.contentLeft() + (contentW-panelW)/2
	y := g.contentTop() + (contentH-panelH)/2
	if y < g.contentTop() {
		y = g.contentTop()
	}
	start := snap.RoomPicker.Index - maxRows/2
	if start < 0 {
		start = 0
	}
	if start+maxRows > len(rows) {
		start = len(rows) - maxRows
	}
	if start < 0 {
		start = 0
	}
	return listPanel{
		X: x, Y: y, W: panelW, H: panelH,
		HeaderH: headerH, FooterH: footerH, RowH: rowH,
		Start: start, Visible: maxRows,
	}, true
}

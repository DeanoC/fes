package tenfoot

import "testing"

func TestGridLayoutAndMoveStayOnRow(t *testing.T) {
	t.Parallel()
	grid := Grid{}
	grid.Layout(1280, 720)
	grid.SetCount(20)
	if grid.Columns < 2 || grid.VisibleRows < 1 {
		t.Fatalf("layout columns=%d rows=%d", grid.Columns, grid.VisibleRows)
	}
	grid.Move(1, 0)
	if grid.Focus != 1 {
		t.Fatalf("right focus = %d", grid.Focus)
	}
	grid.Move(0, 1)
	want := grid.Columns + 1
	if grid.Focus != want {
		t.Fatalf("down focus = %d want %d", grid.Focus, want)
	}
	grid.Move(-100, 0)
	if grid.Focus != grid.Columns {
		t.Fatalf("left clamp focus = %d want row start %d", grid.Focus, grid.Columns)
	}
}

func TestGridVisibleRangeFollowsFocus(t *testing.T) {
	t.Parallel()
	grid := Grid{Columns: 4, VisibleRows: 2, CellW: 210, CellH: 320}
	grid.SetCount(40)
	grid.Focus = 0
	grid.ensureVisible()
	start, end := grid.VisibleRange()
	if start != 0 || end != 8 {
		t.Fatalf("visible = [%d,%d)", start, end)
	}
	grid.Move(0, 5)
	if grid.Focus != 20 {
		t.Fatalf("focus = %d", grid.Focus)
	}
	start, end = grid.VisibleRange()
	if grid.Focus < start || grid.Focus >= end {
		t.Fatalf("focus %d outside visible [%d,%d) scroll=%d", grid.Focus, start, end, grid.ScrollRow)
	}
}

func TestGridEmptyMoveIsNoop(t *testing.T) {
	t.Parallel()
	grid := Grid{}
	grid.Layout(640, 480)
	grid.Move(1, 1)
	if grid.Focus != 0 {
		t.Fatalf("focus = %d", grid.Focus)
	}
}

func TestGridShortWindowKeepsCellsAboveFooter(t *testing.T) {
	t.Parallel()
	grid := Grid{}
	grid.Layout(1280, 480)
	grid.SetCount(8)
	_, y, ok := grid.CellOrigin(0)
	if !ok {
		t.Fatal("cell 0 offscreen")
	}
	bottom := y + grid.CellH
	footerTop := grid.Height - grid.FooterHeight
	if bottom > footerTop {
		t.Fatalf("cell bottom %d overlaps footer at %d (cellH=%d y=%d)", bottom, footerTop, grid.CellH, y)
	}
	if grid.CellH >= defaultCellH {
		t.Fatalf("short window kept full cell height %d", grid.CellH)
	}
	grid.Layout(1280, 720)
	if grid.CellH != defaultCellH {
		t.Fatalf("taller window cellH=%d want %d", grid.CellH, defaultCellH)
	}
}

func TestGridSafeAreaKeepsChromeInsideInsets(t *testing.T) {
	t.Parallel()
	grid := Grid{}
	grid.Safe = insetsFromPct(1280, 720, 0.05)
	grid.Layout(1280, 720)
	grid.SetCount(8)
	if grid.Safe.Left < 60 || grid.Safe.Top < 30 {
		t.Fatalf("insets = %+v", grid.Safe)
	}
	x, y, ok := grid.CellOrigin(0)
	if !ok {
		t.Fatal("cell 0 offscreen")
	}
	if x < grid.Safe.Left || y < grid.Safe.Top+grid.HeaderHeight {
		t.Fatalf("cell origin %d,%d outside safe header", x, y)
	}
	bottom := y + grid.CellH
	footerTop := grid.footerY()
	if bottom > footerTop {
		t.Fatalf("cell bottom %d overlaps footer at %d", bottom, footerTop)
	}
	if footerTop+grid.FooterHeight > grid.Height-grid.Safe.Bottom {
		t.Fatalf("footer %d+%d exceeds bottom inset %d", footerTop, grid.FooterHeight, grid.Safe.Bottom)
	}
	right := x + grid.CellW
	if right > grid.Width-grid.Safe.Right {
		t.Fatalf("cell right %d exceeds inset", right)
	}
}

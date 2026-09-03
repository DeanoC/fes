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

func TestShelfMovePagesVertically(t *testing.T) {
	t.Parallel()
	grid := Grid{Mode: LayoutShelf}
	grid.Layout(1280, 720)
	grid.SetCount(20)
	if grid.VisibleRows != 1 || grid.Columns < 2 {
		t.Fatalf("shelf columns=%d rows=%d", grid.Columns, grid.VisibleRows)
	}
	grid.Move(1, 0)
	if grid.Focus != 1 {
		t.Fatalf("right focus = %d", grid.Focus)
	}
	grid.Move(0, 1)
	want := 1 + grid.Columns
	if grid.Focus != want {
		t.Fatalf("down page focus = %d want %d", grid.Focus, want)
	}
	grid.Move(-100, 0)
	if grid.Focus != 0 {
		t.Fatalf("left clamp focus = %d", grid.Focus)
	}
	start, end := grid.VisibleRange()
	if grid.Focus < start || grid.Focus >= end {
		t.Fatalf("focus %d outside visible [%d,%d)", grid.Focus, start, end)
	}
}

func TestListMovePagesHorizontally(t *testing.T) {
	t.Parallel()
	grid := Grid{Mode: LayoutList}
	grid.Layout(1280, 720)
	grid.SetCount(40)
	if grid.Columns != 1 || grid.VisibleRows < 2 {
		t.Fatalf("list columns=%d rows=%d", grid.Columns, grid.VisibleRows)
	}
	grid.Move(0, 1)
	if grid.Focus != 1 {
		t.Fatalf("down focus = %d", grid.Focus)
	}
	grid.Move(1, 0)
	want := 1 + grid.VisibleRows
	if grid.Focus != want {
		t.Fatalf("right page focus = %d want %d", grid.Focus, want)
	}
	grid.Move(0, -100)
	if grid.Focus != 0 {
		t.Fatalf("up clamp focus = %d", grid.Focus)
	}
	start, end := grid.VisibleRange()
	if start != 0 || end != grid.VisibleRows {
		t.Fatalf("visible = [%d,%d) rows=%d", start, end, grid.VisibleRows)
	}
}

func TestShelfAndListStayInsideSafeArea(t *testing.T) {
	t.Parallel()
	for _, mode := range []LayoutKind{LayoutShelf, LayoutList} {
		grid := Grid{Mode: mode}
		grid.Safe = insetsFromPct(1280, 720, 0.05)
		grid.Layout(1280, 720)
		grid.SetCount(12)
		x, y, ok := grid.CellOrigin(0)
		if !ok {
			t.Fatalf("%s cell 0 offscreen", mode)
		}
		if x < grid.Safe.Left || y < grid.Safe.Top+grid.HeaderHeight {
			t.Fatalf("%s origin %d,%d outside safe header", mode, x, y)
		}
		if y+grid.CellH > grid.footerY() {
			t.Fatalf("%s cell bottom %d overlaps footer at %d", mode, y+grid.CellH, grid.footerY())
		}
		if x+grid.CellW > grid.Width-grid.Safe.Right {
			t.Fatalf("%s cell right %d exceeds inset", mode, x+grid.CellW)
		}
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

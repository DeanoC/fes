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

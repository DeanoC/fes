package fbgrid

import "testing"

func TestBrowseKindCyclesGridCoverflowWallSplit(t *testing.T) {
	t.Parallel()
	if BrowseGrid.Next() != BrowseCoverflow || BrowseCoverflow.Next() != BrowseWall || BrowseWall.Next() != BrowseSplit || BrowseSplit.Next() != BrowseGrid {
		t.Fatalf("cycle %s %s %s %s", BrowseGrid.Next(), BrowseCoverflow.Next(), BrowseWall.Next(), BrowseSplit.Next())
	}
	if BrowseGrid.String() != "grid" || BrowseCoverflow.HeaderTag() != "FLOW" || BrowseWall.ShortLabel() != "wall" || BrowseSplit.HeaderTag() != "SPLIT" {
		t.Fatalf("labels %s %s %s %s", BrowseGrid, BrowseCoverflow.HeaderTag(), BrowseWall.ShortLabel(), BrowseSplit.HeaderTag())
	}
	if BrowsePageSize(BrowseGrid) != 12 || BrowsePageSize(BrowseCoverflow) != 5 || BrowsePageSize(BrowseWall) != 18 || BrowsePageSize(BrowseSplit) != 8 {
		t.Fatalf("pages %d %d %d %d", BrowsePageSize(BrowseGrid), BrowsePageSize(BrowseCoverflow), BrowsePageSize(BrowseWall), BrowsePageSize(BrowseSplit))
	}
	if BrowseColumns(BrowseCoverflow, 25) != 25 || BrowseColumns(BrowseWall, 25) != 6 || BrowseColumns(BrowseSplit, 25) != 1 {
		t.Fatalf("cols flow=%d wall=%d split=%d", BrowseColumns(BrowseCoverflow, 25), BrowseColumns(BrowseWall, 25), BrowseColumns(BrowseSplit, 25))
	}
}

func TestCatalogPageCoverflowCentersAndClamps(t *testing.T) {
	t.Parallel()
	start, end := CatalogPage(0, 25, BrowseCoverflow)
	if start != 0 || end != 5 {
		t.Fatalf("start window %d:%d", start, end)
	}
	start, end = CatalogPage(10, 25, BrowseCoverflow)
	if start != 8 || end != 13 {
		t.Fatalf("mid window %d:%d", start, end)
	}
	start, end = CatalogPage(24, 25, BrowseCoverflow)
	if start != 20 || end != 25 {
		t.Fatalf("end window %d:%d", start, end)
	}
	start, end = CatalogPage(1, 3, BrowseCoverflow)
	if start != 0 || end != 3 {
		t.Fatalf("short catalog %d:%d", start, end)
	}
	start, end = CatalogPage(0, 0, BrowseCoverflow)
	if start != 0 || end != 0 {
		t.Fatalf("empty %d:%d", start, end)
	}
	start, end = CatalogPage(19, 40, BrowseWall)
	if start != 18 || end != 36 {
		t.Fatalf("wall page %d:%d", start, end)
	}
	start, end = CatalogPage(0, 25, BrowseSplit)
	if start != 0 || end != 8 {
		t.Fatalf("split start window %d:%d", start, end)
	}
	start, end = CatalogPage(10, 25, BrowseSplit)
	if start != 6 || end != 14 {
		t.Fatalf("split mid window %d:%d", start, end)
	}
	start, end = CatalogPage(24, 25, BrowseSplit)
	if start != 17 || end != 25 {
		t.Fatalf("split end window %d:%d", start, end)
	}
}

func TestCoverflowMoveFocusIsOneRow(t *testing.T) {
	t.Parallel()
	const n = 12
	cols := BrowseColumns(BrowseCoverflow, n)
	if got := MoveFocus(0, n, cols, 1, 0); got != 1 {
		t.Fatalf("right %d", got)
	}
	if got := MoveFocus(0, n, cols, -1, 0); got != 0 {
		t.Fatalf("left clamp %d", got)
	}
	if got := MoveFocus(5, n, cols, 0, 1); got != 5 {
		t.Fatalf("down stays %d", got)
	}
	if got := MoveFocus(11, n, cols, 1, 0); got != 11 {
		t.Fatalf("right end %d", got)
	}
}

func TestWallMoveFocusUsesSixColumns(t *testing.T) {
	t.Parallel()
	const n, cols = 20, WallColumns
	if got := MoveFocus(0, n, cols, 1, 0); got != 1 {
		t.Fatalf("right %d", got)
	}
	if got := MoveFocus(0, n, cols, 0, 1); got != 6 {
		t.Fatalf("down %d", got)
	}
	if got := MoveFocus(5, n, cols, 1, 0); got != 5 {
		t.Fatalf("row clamp %d", got)
	}
}

func TestSplitMoveFocusIsOneColumn(t *testing.T) {
	t.Parallel()
	const n = 12
	cols := BrowseColumns(BrowseSplit, n)
	if got := MoveFocus(0, n, cols, 0, 1); got != 1 {
		t.Fatalf("down %d", got)
	}
	if got := MoveFocus(0, n, cols, 1, 0); got != 0 {
		t.Fatalf("right clamp %d", got)
	}
	if got := MoveFocus(0, n, cols, -1, 0); got != 0 {
		t.Fatalf("left clamp %d", got)
	}
	if got := MoveFocus(11, n, cols, 0, 1); got != 11 {
		t.Fatalf("last down stays %d", got)
	}
	if got := MoveFocus(5, n, cols, 0, -1); got != 4 {
		t.Fatalf("up %d", got)
	}
}

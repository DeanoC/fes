package fbgrid

import "testing"

func TestBrowseKindCyclesGridCoverflowWall(t *testing.T) {
	t.Parallel()
	if BrowseGrid.Next() != BrowseCoverflow || BrowseCoverflow.Next() != BrowseWall || BrowseWall.Next() != BrowseGrid {
		t.Fatalf("cycle %s %s %s", BrowseGrid.Next(), BrowseCoverflow.Next(), BrowseWall.Next())
	}
	if BrowseGrid.String() != "grid" || BrowseCoverflow.HeaderTag() != "FLOW" || BrowseWall.ShortLabel() != "wall" {
		t.Fatalf("labels %s %s %s", BrowseGrid, BrowseCoverflow.HeaderTag(), BrowseWall.ShortLabel())
	}
	if BrowsePageSize(BrowseGrid) != 12 || BrowsePageSize(BrowseCoverflow) != 5 || BrowsePageSize(BrowseWall) != 18 {
		t.Fatalf("pages %d %d %d", BrowsePageSize(BrowseGrid), BrowsePageSize(BrowseCoverflow), BrowsePageSize(BrowseWall))
	}
	if BrowseColumns(BrowseCoverflow, 25) != 25 || BrowseColumns(BrowseWall, 25) != 6 {
		t.Fatalf("cols flow=%d wall=%d", BrowseColumns(BrowseCoverflow, 25), BrowseColumns(BrowseWall, 25))
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

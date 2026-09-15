package tenfoot

import "testing"

func TestParseLayoutAndCycle(t *testing.T) {
	t.Parallel()
	if parseLayout("") != LayoutGrid || parseLayout("nope") != LayoutGrid {
		t.Fatal("invalid layout")
	}
	if parseLayout("SHELF") != LayoutShelf || parseLayout("list") != LayoutList {
		t.Fatal("named layout")
	}
	if LayoutGrid.Next() != LayoutShelf || LayoutShelf.Next() != LayoutList || LayoutList.Next() != LayoutGrid {
		t.Fatal("cycle")
	}
	if LayoutShelf.String() != "shelf" || LayoutList.Label() != "List" {
		t.Fatalf("names %s %s", LayoutShelf, LayoutList.Label())
	}
}

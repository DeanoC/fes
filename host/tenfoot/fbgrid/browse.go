package fbgrid

// BrowseKind is the kit catalog presentation after the platform wheel.
// Grid is the default 4×3 box page. Coverflow is a scaled focus row.
// Wall is a denser 6-column cover mosaic. Split is a vertical clear-logo
// list with a focused hero and short meta.
type BrowseKind int

const (
	BrowseGrid BrowseKind = iota
	BrowseCoverflow
	BrowseWall
	BrowseSplit
)

const (
	// DefaultPageSize is the visible 4×3 grid page.
	DefaultPageSize = 12
	// CoverflowWindow is how many titles the focus row keeps on screen.
	CoverflowWindow = 5
	// WallColumns is the denser mosaic row width.
	WallColumns = 6
	// WallPageSize is a 6×3 mosaic page.
	WallPageSize = 18
	// SplitWindow is how many titles the split list keeps on screen.
	SplitWindow = 8
)

// Next cycles grid → coverflow → wall → split → grid.
func (k BrowseKind) Next() BrowseKind {
	switch k {
	case BrowseGrid:
		return BrowseCoverflow
	case BrowseCoverflow:
		return BrowseWall
	case BrowseWall:
		return BrowseSplit
	default:
		return BrowseGrid
	}
}

func (k BrowseKind) String() string {
	switch k {
	case BrowseCoverflow:
		return "coverflow"
	case BrowseWall:
		return "wall"
	case BrowseSplit:
		return "split"
	default:
		return "grid"
	}
}

// Label is the living-room name for chrome.
func (k BrowseKind) Label() string {
	switch k {
	case BrowseCoverflow:
		return "Coverflow"
	case BrowseWall:
		return "Wall"
	case BrowseSplit:
		return "Split"
	default:
		return "Grid"
	}
}

// HeaderTag is the short header suffix. Grid stays untagged so existing
// 4×3 chrome is unchanged.
func (k BrowseKind) HeaderTag() string {
	switch k {
	case BrowseCoverflow:
		return "FLOW"
	case BrowseWall:
		return "WALL"
	case BrowseSplit:
		return "SPLIT"
	default:
		return ""
	}
}

// ShortLabel is the footer Y-hint for this layout.
func (k BrowseKind) ShortLabel() string {
	switch k {
	case BrowseCoverflow:
		return "flow"
	case BrowseWall:
		return "wall"
	case BrowseSplit:
		return "split"
	default:
		return "grid"
	}
}

// BrowsePageSize is the visible catalog window for kind.
func BrowsePageSize(kind BrowseKind) int {
	switch kind {
	case BrowseCoverflow:
		return CoverflowWindow
	case BrowseWall:
		return WallPageSize
	case BrowseSplit:
		return SplitWindow
	default:
		return DefaultPageSize
	}
}

// BrowseColumns is the MoveFocus column count. Coverflow is one row of
// the whole catalog so left/right walk titles and down cannot change rows.
// Split is one column so up/down walk the list and left/right clamp.
func BrowseColumns(kind BrowseKind, count int) int {
	switch kind {
	case BrowseCoverflow:
		if count < 1 {
			return 1
		}
		return count
	case BrowseWall:
		return WallColumns
	case BrowseSplit:
		return 1
	default:
		return DefaultColumns
	}
}

// CatalogPage is the visible tile window for a catalog focus index.
// Grid and wall stay page-aligned. Coverflow and split keep a window
// centred on focus and slide at the ends.
func CatalogPage(focus, count int, kind BrowseKind) (start, end int) {
	if count <= 0 {
		return 0, 0
	}
	if focus < 0 {
		focus = 0
	}
	if focus >= count {
		focus = count - 1
	}
	page := BrowsePageSize(kind)
	if page < 1 {
		page = 1
	}
	if kind == BrowseCoverflow || kind == BrowseSplit {
		return catalogWindow(focus, count, page)
	}
	start = (focus / page) * page
	if start >= count {
		start = 0
	}
	end = start + page
	if end > count {
		end = count
	}
	return start, end
}

func catalogWindow(focus, count, page int) (start, end int) {
	if count <= page {
		return 0, count
	}
	start = focus - page/2
	if start < 0 {
		start = 0
	}
	if start+page > count {
		start = count - page
	}
	return start, start + page
}

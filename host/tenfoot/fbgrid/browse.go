package fbgrid

// BrowseKind is the kit catalog presentation after the platform wheel.
// Grid is the default 4×3 box page. Coverflow is a scaled focus row.
// Wall is a denser 6-column cover mosaic.
type BrowseKind int

const (
	BrowseGrid BrowseKind = iota
	BrowseCoverflow
	BrowseWall
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
)

// Next cycles grid → coverflow → wall → grid.
func (k BrowseKind) Next() BrowseKind {
	switch k {
	case BrowseGrid:
		return BrowseCoverflow
	case BrowseCoverflow:
		return BrowseWall
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
	default:
		return DefaultPageSize
	}
}

// BrowseColumns is the MoveFocus column count. Coverflow is one row of
// the whole catalog so left/right walk titles and down cannot change rows.
func BrowseColumns(kind BrowseKind, count int) int {
	switch kind {
	case BrowseCoverflow:
		if count < 1 {
			return 1
		}
		return count
	case BrowseWall:
		return WallColumns
	default:
		return DefaultColumns
	}
}

// CatalogPage is the visible tile window for a catalog focus index.
// Grid and wall stay page-aligned. Coverflow keeps a window centred on
// focus and slides at the ends.
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
	if kind == BrowseCoverflow {
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

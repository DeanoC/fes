package tenfoot

import "strings"

// LayoutKind is the sofa browse presentation.
type LayoutKind int

const (
	LayoutGrid LayoutKind = iota
	LayoutShelf
	LayoutList
)

func parseLayout(s string) LayoutKind {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "shelf":
		return LayoutShelf
	case "list":
		return LayoutList
	default:
		return LayoutGrid
	}
}

func (m LayoutKind) String() string {
	switch m {
	case LayoutShelf:
		return "shelf"
	case LayoutList:
		return "list"
	default:
		return "grid"
	}
}

func (m LayoutKind) Label() string {
	switch m {
	case LayoutShelf:
		return "Shelf"
	case LayoutList:
		return "List"
	default:
		return "Grid"
	}
}

func (m LayoutKind) Next() LayoutKind {
	switch m {
	case LayoutGrid:
		return LayoutShelf
	case LayoutShelf:
		return LayoutList
	default:
		return LayoutGrid
	}
}

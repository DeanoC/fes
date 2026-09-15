package kitlauncher

import "github.com/DeanoC/FogCast/ui/tenfoot/fbgrid"

// CycleBrowse advances the catalog presentation (Grid → Coverflow → Wall →
// Split → Grid). Y is ignored on the platform wheel, title pane, attract,
// and search OSK so it cannot steal those views.
func (m *Model) CycleBrowse() {
	if m == nil || m.WheelOpen || m.DetailOpen || m.AttractActive || m.SearchOpen {
		return
	}
	m.leaveSeries()
	m.Browse = m.Browse.Next()
}

// BrowseColumns is the catalog MoveFocus width for the active layout.
func (m Model) BrowseColumns() int {
	return fbgrid.BrowseColumns(m.Browse, len(m.Games))
}

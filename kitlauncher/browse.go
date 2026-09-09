package kitlauncher

import "github.com/DeanoC/FogCast/host/tenfoot/fbgrid"

// CycleBrowse advances the catalog presentation. Y is ignored on the
// platform wheel, title pane, and attract so it cannot steal those views.
func (m *Model) CycleBrowse() {
	if m == nil || m.WheelOpen || m.DetailOpen || m.AttractActive {
		return
	}
	m.Browse = m.Browse.Next()
}

// BrowseColumns is the catalog MoveFocus width for the active layout.
func (m Model) BrowseColumns() int {
	return fbgrid.BrowseColumns(m.Browse, len(m.Games))
}

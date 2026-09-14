package kitlauncher

import "github.com/DeanoC/FogCast/ui/tenfoot/theme"

// CyclePack advances the living-room look. X is ignored during attract and
// the search OSK so it cannot steal dismiss or search; wheel, browse, strip,
// and the title pane cycle.
func (m *Model) CyclePack() {
	if m == nil || m.AttractActive || m.SearchOpen {
		return
	}
	m.Pack = theme.NextPack(m.Pack)
}

// PackTag is the short header suffix for the active pack. Classic is empty.
func (m Model) PackTag() string {
	return theme.PackTag(m.Pack)
}

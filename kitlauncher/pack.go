package kitlauncher

import "github.com/DeanoC/FogCast/host/tenfoot/theme"

// CyclePack advances the living-room look. X is ignored during attract so
// it cannot steal dismiss; wheel, browse, strip, and the title pane cycle.
func (m *Model) CyclePack() {
	if m == nil || m.AttractActive {
		return
	}
	m.Pack = theme.NextPack(m.Pack)
}

// PackTag is the short header suffix for the active pack. Classic is empty.
func (m Model) PackTag() string {
	return theme.PackTag(m.Pack)
}

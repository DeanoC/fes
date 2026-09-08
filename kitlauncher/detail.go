package kitlauncher

import (
	"strings"
	"time"

	"github.com/DeanoC/FogCast/host/tenfoot"
	"github.com/DeanoC/FogCast/remoteinput"
)

func (m *Model) focusedGame() (tenfoot.Game, bool) {
	if m == nil {
		return tenfoot.Game{}, false
	}
	if (m.StripActive || m.detailFromStrip) && m.StripFocus >= 0 && m.StripFocus < len(m.Strip) {
		return m.Strip[m.StripFocus], true
	}
	if m.Focus < 0 || m.Focus >= len(m.Games) {
		return tenfoot.Game{}, false
	}
	return m.Games[m.Focus], true
}

// FocusedGame is the strip title when that row is active, otherwise the grid cell.
func (m Model) FocusedGame() (tenfoot.Game, bool) {
	return m.focusedGame()
}

func (m *Model) openDetail(now time.Time) {
	if _, ok := m.focusedGame(); !ok {
		return
	}
	if m.AttractActive {
		m.hideAttract()
	}
	m.detailFromStrip = m.StripActive
	m.DetailOpen = true
	m.shotIndex = 0
	m.previewAt = time.Time{}
	m.noteActivity(now)
}

func (m *Model) closeDetail() {
	if !m.DetailOpen {
		return
	}
	m.DetailOpen = false
	m.shotIndex = 0
	m.previewAt = time.Time{}
}

func (m *Model) inputDetail(e remoteinput.Event, dx, dy int, now time.Time) string {
	if !significantPad(e, dx, dy) {
		return ""
	}
	m.noteActivity(now)
	if e.Kind == remoteinput.KindButton && e.Action == remoteinput.ActionPress {
		switch e.Code {
		case remoteinput.ButtonA:
			if game, ok := m.focusedGame(); ok && game.Launchable {
				return "launch"
			}
			return ""
		case remoteinput.ButtonB:
			m.closeDetail()
			m.noteActivity(now)
			return ""
		case remoteinput.ButtonL:
			m.stepShot(-1)
			return ""
		case remoteinput.ButtonR:
			m.stepShot(1)
			return ""
		}
	}
	if dx != 0 {
		m.stepShot(dx)
		return ""
	}
	if dy < 0 {
		m.closeDetail()
		m.noteActivity(now)
	}
	return ""
}

// FocusDetail is catalog metadata plus any presentation fetched for the
// focused title. Platform is the catalog system in living-room case.
func (m Model) FocusDetail() tenfoot.FocusDetail {
	game, ok := m.focusedGame()
	if !ok {
		return tenfoot.FocusDetail{}
	}
	p := tenfoot.Presentation{}
	if m.presentationID == game.ID {
		p = m.presentation
	}
	d := tenfoot.GameDetail(game, p)
	if d.Platform != "" {
		d.Platform = strings.ToUpper(d.Platform)
	}
	return d
}

// ApplyPresentation stores host presentation for the currently focused title.
func (m *Model) ApplyPresentation(id string, p tenfoot.Presentation) {
	id = strings.TrimSpace(id)
	game, ok := m.focusedGame()
	if id == "" || !ok || game.ID != id {
		return
	}
	m.presentationID = id
	m.presentation = p
	m.clampShot()
}

func (m *Model) clampShot() {
	n := len(m.previewHandles())
	if n < 1 {
		m.shotIndex = 0
		return
	}
	if m.shotIndex < 0 {
		m.shotIndex = 0
	}
	if m.shotIndex >= n {
		m.shotIndex = n - 1
	}
}

func (m *Model) stepShot(delta int) {
	ids := m.previewHandles()
	n := len(ids)
	if n < 2 || delta == 0 {
		return
	}
	m.shotIndex = (m.shotIndex + delta) % n
	if m.shotIndex < 0 {
		m.shotIndex += n
	}
	m.previewAt = time.Time{}
}

const defaultPreviewCycle = 2 * time.Second

func (m *Model) tickPreview(now time.Time) {
	if m == nil || !m.DetailOpen || m.FocusVideoHandle() == "" {
		return
	}
	ids := m.previewHandles()
	if len(ids) < 2 {
		return
	}
	if now.IsZero() {
		now = time.Now()
	}
	if m.previewAt.IsZero() {
		m.previewAt = now
		return
	}
	if now.Sub(m.previewAt) < defaultPreviewCycle {
		return
	}
	n := len(ids)
	m.shotIndex = (m.shotIndex + 1) % n
	m.previewAt = now
}

func (m Model) previewHandles() []string {
	game, ok := m.focusedGame()
	if !ok {
		return nil
	}
	p := m.presentationFor(game.ID)
	return tenfoot.DetailPreviewHandles(p, tenfoot.CoverHandle(game, p))
}

// PreviewHandles is the current title's screenshot/poster stills.
func (m Model) PreviewHandles() []string {
	return m.previewHandles()
}

// ShotIndex is the current screenshot / preview carousel index.
func (m Model) ShotIndex() int {
	n := len(m.previewHandles())
	if n < 1 {
		return 0
	}
	if m.shotIndex < 0 {
		return 0
	}
	if m.shotIndex >= n {
		return n - 1
	}
	return m.shotIndex
}

// ShotHandle is the current screenshot or preview-poster artwork handle.
func (m Model) ShotHandle() string {
	ids := m.previewHandles()
	if len(ids) == 0 {
		return ""
	}
	return ids[m.ShotIndex()]
}

// FocusVideoHandle is the presentation video handle for the focused title.
func (m Model) FocusVideoHandle() string {
	game, ok := m.focusedGame()
	if !ok {
		return ""
	}
	return tenfoot.VideoHandle(m.presentationFor(game.ID))
}

// HasVideoPreview reports a video handle that the pane previews with stills.
func (m Model) HasVideoPreview() bool {
	return m.FocusVideoHandle() != ""
}

// DetailHint is the footer for the title pane.
func (m Model) DetailHint() string {
	if m.HasVideoPreview() && len(m.previewHandles()) > 1 {
		return "A play | B back | L/R preview"
	}
	if len(m.FocusDetail().ScreenshotIDs) > 1 {
		return "A play | B back | L/R shots"
	}
	return "A play | B back"
}

// FocusCoverHandle is the catalog or presentation cover for the focused title.
func (m Model) FocusCoverHandle() string {
	game, ok := m.focusedGame()
	if !ok {
		return ""
	}
	return tenfoot.CoverHandle(game, m.presentationFor(game.ID))
}

// FocusLogoHandle is the presentation clear-logo for the focused title.
func (m Model) FocusLogoHandle() string {
	game, ok := m.focusedGame()
	if !ok {
		return ""
	}
	return tenfoot.LogoHandle(m.presentationFor(game.ID))
}

// DetailPrefetchHandles is the focused cover, logo, and current screenshot.
func (m Model) DetailPrefetchHandles() []string {
	out := make([]string, 0, 3)
	seen := map[string]struct{}{}
	add := func(handle string) {
		handle = strings.TrimSpace(handle)
		if handle == "" {
			return
		}
		if _, ok := seen[handle]; ok {
			return
		}
		seen[handle] = struct{}{}
		out = append(out, handle)
	}
	add(m.FocusCoverHandle())
	add(m.FocusLogoHandle())
	for _, handle := range m.previewHandles() {
		add(handle)
	}
	return out
}

func (m Model) presentationFor(id string) tenfoot.Presentation {
	if m.presentationID == id {
		return m.presentation
	}
	return tenfoot.Presentation{}
}

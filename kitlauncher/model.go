package kitlauncher

import (
	"strings"
	"time"

	"github.com/DeanoC/FogCast/host/tenfoot"
	"github.com/DeanoC/FogCast/host/tenfoot/fbgrid"
	"github.com/DeanoC/FogCast/kitlauncher/controller"
	"github.com/DeanoC/FogCast/remoteinput"
)

const axisDeadzone int32 = 8000

// Model is the kit/UI boundary. A renderer consumes it without owning network,
// framebuffer enablement, controller capture, or the target session lease.
type Model struct {
	Catalog                                           []tenfoot.Game
	Games                                             []tenfoot.Game
	Shelves                                           []string
	Shelf                                             string
	Focus                                             int
	Session                                           Session
	Connected, TargetReady, Busy, ControllerConnected bool
	Message                                           string
	AttractActive                                     bool
	DetailOpen                                        bool
	WheelOpen                                         bool
	fromWheel                                         bool
	chord                                             controller.Chord
	presentationID                                    string
	presentation                                      tenfoot.Presentation
	shotIndex                                         int
	previewAt                                         time.Time
	axisX, axisY                                      int
	lastInput                                         time.Time
	attractIdle                                       time.Duration
	attractCycle                                      time.Duration
	attractIdleReady                                  bool
	attractItems                                      []tenfoot.AttractItem
	attractIndex                                      int
	attractShownAt                                    time.Time
	attractCycleAt                                    time.Time
	attractPresentationID                             string
	attractPresentation                               tenfoot.Presentation
	attractShotIndex                                  int
	attractPreviewAt                                  time.Time
	launchID                                          string
	Strip                                             []tenfoot.Game
	StripLabel                                        string
	StripFocus                                        int
	StripActive                                       bool
	Recents                                           []tenfoot.Game
	detailFromStrip                                   bool
	Series                                            []tenfoot.Game
	SeriesLabel                                       string
	SeriesFocus                                       int
	SeriesActive                                      bool
	Browse                                            fbgrid.BrowseKind
	Pack                                              string
	SearchOpen                                        bool
	SearchQuery                                       string
	searchField                                       tenfoot.TextField
	searchRestoreID                                   string
	searchPool                                        []tenfoot.Game
}

func (m *Model) ResetControls() { m.chord = controller.Chord{}; m.axisX = 0; m.axisY = 0 }
func (m *Model) Input(e remoteinput.Event, now time.Time) string {
	if now.IsZero() {
		now = time.Now()
	}
	if sessionCanStop(m.Session.State) {
		if e.Kind == remoteinput.KindButton {
			m.chord.Update(e.Code, e.Action == remoteinput.ActionPress, now)
		}
		return ""
	}
	if m.Busy || !m.Connected || !m.TargetReady {
		return ""
	}
	dx, dy := m.padDelta(e)
	if m.AttractActive {
		return m.inputAttract(e, dx, dy, now)
	}
	if m.SearchOpen {
		return m.inputSearch(e, dx, dy, now)
	}
	if m.DetailOpen {
		if e.Kind == remoteinput.KindButton && e.Action == remoteinput.ActionPress && e.Code == remoteinput.ButtonStart {
			m.openSearch(now)
			return ""
		}
		return m.inputDetail(e, dx, dy, now)
	}
	if m.WheelOpen {
		if e.Kind == remoteinput.KindButton && e.Action == remoteinput.ActionPress && e.Code == remoteinput.ButtonStart {
			m.openSearch(now)
			return ""
		}
		return m.inputWheel(e, dx, dy, now)
	}
	if m.SeriesActive {
		return m.inputSeriesBrowse(e, dx, dy, now)
	}
	if significantPad(e, dx, dy) {
		m.noteActivity(now)
	}
	if e.Kind == remoteinput.KindButton && e.Action == remoteinput.ActionPress {
		switch e.Code {
		case remoteinput.ButtonStart:
			m.openSearch(now)
			return ""
		case remoteinput.ButtonL:
			m.CycleShelf(-1)
		case remoteinput.ButtonR, remoteinput.ButtonSelect:
			m.CycleShelf(1)
		case remoteinput.ButtonY:
			m.CycleBrowse()
			return ""
		case remoteinput.ButtonX:
			m.CyclePack()
			return ""
		case remoteinput.ButtonA:
			if m.StripActive {
				m.openDetail(now)
				return ""
			}
			if len(m.Games) > 0 && m.Focus >= 0 && m.Focus < len(m.Games) && m.Games[m.Focus].Launchable {
				return "launch"
			}
		case remoteinput.ButtonB:
			if m.StripActive {
				m.leaveStrip()
				return ""
			}
			if m.fromWheel {
				m.leavePlatform(now)
				return ""
			}
			m.openDetail(now)
			return ""
		}
	}
	if m.StripActive {
		if dx != 0 || dy != 0 {
			m.inputStrip(dx, dy)
		}
		return ""
	}
	if len(m.Games) > 0 && (dx != 0 || dy != 0) {
		next := fbgrid.MoveFocus(m.Focus, len(m.Games), m.BrowseColumns(), dx, dy)
		if dy > 0 && next == m.Focus {
			if len(m.Strip) > 0 && m.SearchTag() == "" {
				m.enterStrip()
				return ""
			}
			m.openDetail(now)
			return ""
		}
		if dx > 0 && next == m.Focus && m.Browse == fbgrid.BrowseSplit && len(m.Series) > 0 {
			m.enterSeries()
			return ""
		}
		m.Focus = next
		m.refreshSeries()
	}
	return ""
}

func (m *Model) axisStep(hold *int, value int32) int {
	dir := 0
	if value > axisDeadzone {
		dir = 1
	}
	if value < -axisDeadzone {
		dir = -1
	}
	move := 0
	if dir != *hold {
		move = dir
	}
	*hold = dir
	return move
}
func (m *Model) Tick(now time.Time) string {
	if now.IsZero() {
		now = time.Now()
	}
	if sessionCanStop(m.Session.State) && !m.Busy && m.chord.Ready(now) {
		return "stop"
	}
	m.tickAttract(now)
	m.tickPreview(now)
	return ""
}

// Failed sessions retain host-side cleanup state, so they use the same
// Select+Start recovery path as active sessions.
func sessionCanStop(state string) bool { return state == "active" || state == "failed" }

// SessionChrome is pause overlay state for fbgrid paint. East/B, Start, and
// Guide stay on the existing input map; only Select+Start requests Stop.
func (m Model) SessionChrome() fbgrid.SessionChrome {
	state := m.Session.State
	if m.Busy {
		if strings.Contains(strings.ToLower(m.Message), "stop") {
			state = "stopping"
		} else if !fbgrid.SessionLive(fbgrid.SessionChrome{State: state}) {
			state = "launching"
		}
	}
	hint := fbgrid.SessionKitHint
	if state == "failed" {
		hint = fbgrid.SessionKitRetryHint
	}
	return fbgrid.SessionChrome{
		State: state,
		Title: m.SessionTitle(),
		Hint:  hint,
	}
}

// SessionTitle is the catalog title for the host session game id.
func (m Model) SessionTitle() string {
	id := strings.TrimSpace(m.Session.GameID)
	if id == "" {
		return ""
	}
	for _, pool := range [][]tenfoot.Game{m.Catalog, m.Games, m.Strip} {
		for _, game := range pool {
			if game.ID != id {
				continue
			}
			if title := strings.TrimSpace(game.Title); title != "" {
				return title
			}
			return id
		}
	}
	return id
}

package kitlauncher

import (
	"fmt"
	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/remoteinput"
	"github.com/DeanoC/FogCast/ui/fbgrid"
	"github.com/DeanoC/FogCast/ui/kitlauncher/controller"
	"github.com/DeanoC/FogCast/ui/shared"
	"strings"
	"time"
)

const axisDeadzone int32 = 8000

// Model is the kit/UI boundary. A renderer consumes it without owning network,
// framebuffer enablement, controller capture, or the target session lease.
type Model struct {
	Catalog                                                         []hostclient.Game
	Games                                                           []hostclient.Game
	Shelves                                                         []string
	Shelf                                                           string
	Focus                                                           int
	Session                                                         Session
	Connected, TargetReady, Busy, ControllerConnected, ForeignLease bool
	Message                                                         string
	AttractActive                                                   bool
	DetailOpen                                                      bool
	WheelOpen                                                       bool
	fromWheel                                                       bool
	chord                                                           [2]controller.Chord
	presentationID                                                  string
	presentation                                                    hostclient.Presentation
	shotIndex                                                       int
	previewAt                                                       time.Time
	axisX, axisY                                                    int
	lastInput                                                       time.Time
	attractIdle                                                     time.Duration
	attractCycle                                                    time.Duration
	attractIdleReady                                                bool
	attractItems                                                    []hostclient.AttractItem
	attractIndex                                                    int
	attractShownAt                                                  time.Time
	attractCycleAt                                                  time.Time
	attractPresentationID                                           string
	attractPresentation                                             hostclient.Presentation
	attractShotIndex                                                int
	attractPreviewAt                                                time.Time
	launchID                                                        string
	Strip                                                           []hostclient.Game
	StripLabel                                                      string
	StripFocus                                                      int
	StripActive                                                     bool
	Recents                                                         []hostclient.Game
	detailFromStrip                                                 bool
	Series                                                          []hostclient.Game
	SeriesLabel                                                     string
	SeriesFocus                                                     int
	SeriesActive                                                    bool
	Browse                                                          fbgrid.BrowseKind
	Pack                                                            string
	SearchOpen                                                      bool
	SearchQuery                                                     string
	searchField                                                     shared.TextField
	searchRestoreID                                                 string
	searchPool                                                      []hostclient.Game
	Cache                                                           CacheStatus
	CoreStatuses                                                    []hostclient.CoreAvailability
	CoreStatusUnavailable                                           bool
	CoreStatusRevision                                              uint64
}

// CacheStatus is visible ROM/cover used-free plus last catalog sync.
type CacheStatus struct {
	ROMUsedBytes   int64
	ROMMaxBytes    int64
	ROMFreeBytes   int64
	ROMReachable   bool
	CoverUsedBytes int64
	CoverMaxBytes  int64
	CoverFreeBytes int64
	LastSyncUnix   int64
}

// CacheChrome is compact living-room used/max, omitted when unknown.
func (s CacheStatus) Chrome() string {
	parts := make([]string, 0, 2)
	if s.ROMReachable && s.ROMMaxBytes > 0 {
		parts = append(parts, "ROM "+formatCacheBytes(s.ROMUsedBytes)+"/"+formatCacheBytes(s.ROMMaxBytes))
	}
	if s.CoverMaxBytes > 0 {
		parts = append(parts, "ART "+formatCacheBytes(s.CoverUsedBytes)+"/"+formatCacheBytes(s.CoverMaxBytes))
	}
	return strings.Join(parts, "  ")
}

func formatCacheBytes(n int64) string {
	if n < 0 {
		n = 0
	}
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1fG", float64(n)/float64(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.0fM", float64(n)/float64(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0fK", float64(n)/float64(1<<10))
	default:
		return fmt.Sprintf("%dB", n)
	}
}

func (m *Model) ResetControls() { m.chord = [2]controller.Chord{}; m.axisX = 0; m.axisY = 0 }

// armStopChord records Select+Start for a session that can stop.
// It does not move the platform wheel, change browse selection, or request a launch.
func (m *Model) armStopChord(e remoteinput.Event, now time.Time) {
	if m == nil || !sessionCanStop(m.Session.State) {
		return
	}
	if now.IsZero() {
		now = time.Now()
	}
	if e.Kind == remoteinput.KindButton && e.Player < 2 {
		m.chord[e.Player].Update(e.Code, e.Action == remoteinput.ActionPress, now)
	}
}

func (m *Model) Input(e remoteinput.Event, now time.Time) string {
	if now.IsZero() {
		now = time.Now()
	}
	if sessionCanStop(m.Session.State) {
		m.armStopChord(e, now)
		return ""
	}
	if m.Busy {
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
			if len(m.Games) > 0 && m.Focus >= 0 && m.Focus < len(m.Games) && m.Games[m.Focus].LaunchEligible() && m.canLaunch() {
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

// canLaunch admits a launch only when the configured host API and target are
// currently ready. Cached catalog rows remain browseable while disconnected.
func (m Model) canLaunch() bool {
	return m.Connected && m.TargetReady
}

func (m *Model) Tick(now time.Time) string {
	if now.IsZero() {
		now = time.Now()
	}
	if sessionCanStop(m.Session.State) && !m.Busy {
		for i := range m.chord {
			if m.chord[i].Ready(now) {
				return "stop"
			}
		}
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
	for _, pool := range [][]hostclient.Game{m.Catalog, m.Games, m.Strip} {
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

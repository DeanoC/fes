package tenfoot

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/rooms"
)

const staleBrowseActionStatus = "The selection changed. Choose its action again."

// BrowseActionsSnapshot describes focus on the selected item's visible actions.
// The item stays selected while focus moves to this row.
type BrowseActionsSnapshot struct {
	Visible, Focused bool
	Index            int
	Primary          string
}

type browseActionState struct {
	open       bool
	index      int
	room       *rooms.Instance
	client     *Client
	sessionGen int
	identity   string
}

func browseGameIdentity(g hostclient.Game) string {
	parts := []string{g.ID, g.System, g.Execution, g.CoreID, g.PackageID, g.MediaID, g.MediaRole, g.ROMID, g.ROMMediaID, g.ROMRole, g.ExpansionID}
	for _, slot := range g.SlotExpansions {
		parts = append(parts, strconv.Itoa(slot.Slot), slot.ExpansionID)
	}
	return strings.Join(parts, "\x00")
}

func browseDestinationIdentity(d rooms.Destination) string {
	parts := []string{string(d.Kind), d.PackageID, d.CoreID, d.GameID, d.Query, d.Platform, strconv.Itoa(int(d.Confirm()))}
	for _, g := range d.Matches {
		parts = append(parts, browseGameIdentity(g))
	}
	return strings.Join(parts, "\x01")
}

func (a *App) browseActionsAllowedLocked() bool {
	if a.settingsOpen || a.settingsOSKOpenLocked() || a.filtersOpen || a.searchOpen || a.viewPickerOpen || a.nameEntryOpenLocked() || a.roomPickerOpen || a.driveSettings.Open || a.coreLibrary.Open || a.firmwarePickerOpen || a.tapePickerOpen {
		return false
	}
	if a.sessionStopOfferedLocked() || a.localPhase != "" || a.launch.Phase == "launching" || a.detailOpen || a.roomCoreDetail != "" {
		return false
	}
	if a.room != nil {
		return a.room.ID() == installedCoresHomeID && a.room.Hero() && a.room.Err() == nil && !a.roomChoiceOpen
	}
	return !a.loading && !a.searchPending && a.grid.Focus >= 0 && a.grid.Focus < len(a.games)
}

func (a *App) browseActionsIdentityLocked(d rooms.Destination) string {
	if a.room != nil {
		return browseDestinationIdentity(d)
	}
	return browseGameIdentity(a.games[a.grid.Focus]) + "\x01" + strconv.Itoa(int(d.Confirm()))
}

func (a *App) browseActionsDestinationLocked() (rooms.Destination, string, bool) {
	if !a.browseActionsAllowedLocked() {
		return rooms.Destination{}, "", false
	}
	d := a.detailPlayLocked()
	if a.room != nil {
		d = a.roomDestinationLocked()
		if d.Kind != rooms.KindCore && d.Kind != rooms.KindGame {
			return rooms.Destination{}, "", false
		}
	}
	return d, a.browseActionsIdentityLocked(d), true
}

func browseActionLabels(d rooms.Destination) (primary string, secondary bool) {
	switch d.Confirm() {
	case rooms.ConfirmLaunch, rooms.ConfirmLaunchCore, rooms.ConfirmLaunchKit:
		return "Play", true
	case rooms.ConfirmChoose:
		return "Choose", false
	case rooms.ConfirmImportFirmware:
		return "Import", false
	case rooms.ConfirmOpenLibrary:
		return "Open library", false
	case rooms.ConfirmExplain:
		return "Details", false
	default:
		return "", false
	}
}

// Snapshot already classified this destination for its readiness and title.
// Ordinary browse frames need neither a second classification nor an identity
// string; only an open action row checks its captured subject.
func (a *App) browseActionsSnapshotLocked(d rooms.Destination) BrowseActionsSnapshot {
	if !a.browseActionsAllowedLocked() || (a.room != nil && d.Kind != rooms.KindCore && d.Kind != rooms.KindGame) {
		return BrowseActionsSnapshot{}
	}
	primary, _ := browseActionLabels(d)
	focused := a.browseActions.open && a.browseActionsMatchesLocked(a.browseActionsIdentityLocked(d))
	return BrowseActionsSnapshot{Visible: primary != "", Focused: focused, Index: a.browseActions.index, Primary: primary}
}

func (a *App) browseActionsMatchesLocked(identity string) bool {
	s := a.browseActions
	return s.room == a.room && s.client == a.client && s.sessionGen == a.sessionGen && s.identity == identity
}

func (a *App) enterBrowseActionsLocked(index int) bool {
	d, identity, ok := a.browseActionsDestinationLocked()
	if !ok {
		return false
	}
	primary, secondary := browseActionLabels(d)
	if primary == "" {
		return false
	}
	if !secondary || index < 0 {
		index = 0
	}
	a.browseActions = browseActionState{open: true, index: index, room: a.room, client: a.client, sessionGen: a.sessionGen, identity: identity}
	if a.room != nil {
		a.room.Hover("native:actions")
	}
	return true
}

func (a *App) leaveBrowseActionsLocked() {
	if a.browseActions.open && a.browseActions.room == a.room && a.room != nil {
		a.room.Hover("native:shelf")
	}
	a.browseActions = browseActionState{}
}

// handleBrowseActionsLocked owns only the action row. Normal Confirm on a
// selected title retains the direct play path.
func (a *App) handleBrowseActionsLocked(cmd Command) bool {
	if !a.browseActions.open {
		return false
	}
	d, identity, ok := a.browseActionsDestinationLocked()
	if !ok || !a.browseActionsMatchesLocked(identity) {
		a.leaveBrowseActionsLocked()
		// Never let a delayed refresh or context change turn this Confirm
		// into an activation of the item now occupying the old position.
		if cmd == CmdSelect {
			a.status = staleBrowseActionStatus
			return true
		}
		return false
	}
	_, secondary := browseActionLabels(d)
	switch cmd {
	case CmdLeft:
		a.browseActions.index = 0
	case CmdRight:
		if secondary {
			a.browseActions.index = 1
		}
	case CmdBack, CmdDown, CmdUp:
		isHome := a.room != nil
		a.leaveBrowseActionsLocked()
		if isHome && cmd == CmdUp {
			// The hero sits above the shelf; another Up reaches its header.
			a.room.Input("up")
			a.applyRoomActionsLocked()
		}
	case CmdSelect:
		index := a.browseActions.index
		a.leaveBrowseActionsLocked()
		if a.room != nil {
			if index == 1 {
				a.openRoomDetailsLocked()
			} else {
				a.applyRoomDestinationConfirmLocked()
			}
		} else if index == 1 {
			a.openDetailLocked()
		} else {
			a.startLaunchLocked()
		}
	case CmdDetails, CmdSearch:
		a.leaveBrowseActionsLocked()
		return false
	default:
		a.leaveBrowseActionsLocked()
		return false
	}
	return true
}

// libraryActionRects is shared by production painting and pointer routing.
func libraryActionRects(snap Snapshot) (primary, secondary rectI) {
	if snap.Room.Open || snap.Detail.Open || !snap.BrowseActions.Visible {
		return primary, secondary
	}
	geom, ok := detailGeomOf(snap)
	if !ok {
		return primary, secondary
	}
	_, hasSecondary := browseActionLabels(snap.DetailPlay)
	pw := heroButtonWidth(selectWord(snap.Affinity), snap.BrowseActions.Primary)
	sw := 0
	if hasSecondary {
		sw = heroButtonWidth(libraryActionPrompt(snap, 1), "Details") + heroButtonGap
	}
	x := geom.Pane.X + geom.Pane.W - 24 - pw - sw
	y := geom.Pane.Y + (geom.Pane.H-heroButtonH)/2
	primary = rectI{X: x, Y: y, W: pw, H: heroButtonH}
	if hasSecondary {
		secondary = rectI{X: x + pw + heroButtonGap, Y: y, W: sw - heroButtonGap, H: heroButtonH}
	}
	return primary, secondary
}

func libraryActionPrompt(snap Snapshot, index int) string {
	if index == 1 && !snap.BrowseActions.Focused && snap.Affinity != InputMouse {
		return detailsWord(snap.Affinity)
	}
	return selectWord(snap.Affinity)
}

func drawLibraryActions(dev gfx.Device, snap Snapshot, labels map[string]gpuTexture, used map[string]struct{}) {
	primary, secondary := libraryActionRects(snap)
	for index, r := range []rectI{primary, secondary} {
		if r.W <= 0 {
			continue
		}
		label := snap.BrowseActions.Primary
		if index == 1 {
			label = "Details"
		}
		heroButton(dev, labels, used, r.X, r.Y, r.H, libraryActionPrompt(snap, index), label, index == 0 && label == "Play")
		if snap.BrowseActions.Focused && snap.BrowseActions.Index == index {
			heroOutline(dev, r.X-4, r.Y-4, r.W+8, r.H+8, 3, heroAccent)
		}
	}
}

func browseActionsClosedHint(snap Snapshot) string {
	kind := snap.Affinity
	switch kind {
	case InputKeyboard:
		return strings.Replace(browseHint(kind), "Enter launch", "Enter play  i details", 1)
	case InputMouse:
		return "move focus  click play  click details"
	default:
		hint := strings.Replace(browseHint(kind), "Y search", detailsWord(kind)+" details", 1)
		return selectWord(kind) + " " + strings.ToLower(snap.BrowseActions.Primary) + "  " + hint
	}
}

func browseActionsHint(snap Snapshot) string {
	label, back := snap.BrowseActions.Primary, "return to title"
	if snap.BrowseActions.Index == 1 {
		label = "Details"
	}
	if snap.Room.Open {
		back = "return to shelf"
	}
	return fmt.Sprintf("Left / Right choose  ·  %s %s  ·  %s %s", selectWord(snap.Affinity), strings.ToLower(label), backWord(snap.Affinity), back)
}

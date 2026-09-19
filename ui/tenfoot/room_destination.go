package tenfoot

import (
	"context"
	"strings"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/ui/rooms"
)

// RoomChoiceSnapshot is the edition-choice overlay shown for Needs a choice.
type RoomChoiceSnapshot struct {
	Open  bool
	Index int
	Rows  []hostclient.Game
	Hint  string
}

func roomPickKey(d rooms.Destination) string {
	return rooms.DestinationPreferenceKey(d)
}

func (a *App) roomDestinationLocked() rooms.Destination {
	if a.room == nil {
		return rooms.Destination{}
	}
	d := a.room.Destination()
	if key := roomPickKey(d); key != "" && a.roomPicks != nil {
		if id := strings.TrimSpace(a.roomPicks[key]); id != "" {
			return rooms.ApplyEditionPreference(d, id)
		}
	}
	return d
}

func (a *App) rememberRoomPickLocked(d rooms.Destination, game hostclient.Game) {
	if strings.TrimSpace(game.ID) == "" {
		return
	}
	if a.roomPicks == nil {
		a.roomPicks = map[string]string{}
	}
	key := roomPickKey(d)
	if key == "" {
		return
	}
	a.roomPicks[key] = game.ID
	query := strings.TrimSpace(d.Query)
	if query == "" {
		query = strings.TrimSpace(d.Label)
	}
	platform := strings.TrimSpace(d.Platform)
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	go a.persistEditionPreference(ctx, query, platform, game.ID)
}

func (a *App) persistEditionPreference(ctx context.Context, query, platform, gameID string) {
	if a == nil || a.client == nil {
		return
	}
	_, _ = a.client.SetEditionPreference(ctx, query, platform, gameID)
}

func (a *App) mergeEditionPreferencesLocked(prefs []hostclient.EditionPreference) {
	if a.roomPicks == nil {
		a.roomPicks = map[string]string{}
	}
	for _, pref := range prefs {
		id := strings.TrimSpace(pref.GameID)
		key := rooms.DestinationPreferenceKey(rooms.Destination{Query: pref.Query, Platform: pref.Platform})
		if key == "" || id == "" {
			continue
		}
		if _, exists := a.roomPicks[key]; exists {
			continue
		}
		a.roomPicks[key] = id
	}
}

func (a *App) loadEditionPreferences(ctx context.Context) {
	if a == nil || a.client == nil {
		return
	}
	prefs, err := a.client.EditionPreferences(ctx)
	if err != nil || ctx.Err() != nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.mergeEditionPreferencesLocked(prefs)
}

func (a *App) closeRoomOverlaysLocked() {
	a.closeDetailLocked()
	a.roomChoiceOpen = false
	a.roomChoice = nil
	a.roomChoiceIndex = 0
	a.roomDetail = hostclient.Game{}
}

func (a *App) handleRoomLocked(cmd Command) {
	if a.room == nil {
		return
	}
	if cmd == CmdHome {
		a.closeRoomOverlaysLocked()
		a.openRoomPickerLocked()
		return
	}
	if a.room.Err() != nil {
		if cmd == CmdBack || cmd == CmdSelect {
			a.leaveRoomLocked()
		}
		return
	}
	if a.launchOverlayActiveLocked() {
		a.handleLaunchOverlayLocked(cmd)
		return
	}
	if a.roomChoiceOpen {
		a.handleRoomChoiceLocked(cmd)
		return
	}
	if a.detailOpen && a.room != nil {
		if a.handleRoomDetailLocked(cmd) {
			return
		}
	}
	if cmd == CmdSearch || cmd == CmdDetails {
		a.openRoomDetailsLocked()
		return
	}
	if cmd == CmdSelect {
		if a.applyRoomDestinationConfirmLocked() {
			return
		}
	}
	handled := a.room.Input(roomCommandName(cmd))
	a.applyRoomActionsLocked()
	if a.room == nil {
		return
	}
	if !handled && cmd == CmdBack {
		if a.launch.Phase == "launching" {
			a.status = "launch in progress"
			return
		}
		a.leaveRoomLocked()
	}
}

func (a *App) handleRoomDetailLocked(cmd Command) bool {
	switch cmd {
	case CmdBack, CmdUp:
		a.closeDetailLocked()
		a.roomDetail = hostclient.Game{}
		return true
	case CmdSelect:
		a.applyRoomDestinationConfirmLocked()
		return true
	case CmdLeft:
		a.stepCarouselLocked(-1)
		return true
	case CmdRight:
		a.stepCarouselLocked(1)
		return true
	case CmdStop:
		a.startStopLocked()
		return true
	case CmdSearch, CmdDetails:
		return true
	default:
		return true
	}
}

func (a *App) handleRoomChoiceLocked(cmd Command) {
	n := len(a.roomChoice)
	if n == 0 {
		a.roomChoiceOpen = false
		return
	}
	if a.roomChoiceIndex < 0 {
		a.roomChoiceIndex = 0
	}
	if a.roomChoiceIndex >= n {
		a.roomChoiceIndex = n - 1
	}
	switch cmd {
	case CmdUp, CmdLeft, CmdTabPrev:
		a.roomChoiceIndex = (a.roomChoiceIndex - 1 + n) % n
	case CmdDown, CmdRight, CmdTab:
		a.roomChoiceIndex = (a.roomChoiceIndex + 1) % n
	case CmdBack:
		a.roomChoiceOpen = false
	case CmdSelect, CmdDetails, CmdSearch:
		game := a.roomChoice[a.roomChoiceIndex]
		dest := a.roomDestinationLocked()
		a.rememberRoomPickLocked(dest, game)
		a.roomChoiceOpen = false
		a.roomDetail = game
		if cmd == CmdSelect && game.LaunchEligible() {
			a.startLaunchGameLocked(game)
			return
		}
		a.openRoomDetailsForLocked(game)
	}
}

func (a *App) applyRoomDestinationConfirmLocked() bool {
	dest := a.roomDestinationLocked()
	if !dest.Set() {
		return false
	}
	switch dest.Confirm() {
	case rooms.ConfirmWait:
		a.status = dest.Status
		if a.status == "" {
			a.status = "Matching this title in your library…"
		}
		return true
	case rooms.ConfirmOpenLibrary:
		a.status = dest.Status
		if q := strings.TrimSpace(dest.Query); q != "" {
			a.searchField.Buffer = q
		}
		a.openLibraryFromRoomLocked(rooms.Action{
			Kind:     rooms.ActionOpenLibrary,
			Platform: dest.Platform,
		})
		return true
	case rooms.ConfirmChoose:
		a.openRoomChoiceLocked(dest)
		return true
	case rooms.ConfirmExplain:
		a.status = dest.Status
		a.openRoomDetailsLocked()
		return true
	case rooms.ConfirmLaunch:
		if a.launch.Phase == "launching" {
			return true
		}
		if game, ok := dest.Game(); ok {
			a.startLaunchGameLocked(game)
			return true
		}
		if dest.GameID != "" {
			a.launchFromRoomLocked(dest.GameID)
			return true
		}
		a.status = dest.Status
		return true
	case rooms.ConfirmEnterRoom:
		if dest.RoomID != "" {
			a.openNestedRoomLocked(dest.RoomID)
			return true
		}
		return false
	case rooms.ConfirmOpenLibraryBrowse:
		a.openLibraryFromRoomLocked(rooms.Action{Kind: rooms.ActionOpenLibrary})
		return true
	default:
		return false
	}
}

func (a *App) openRoomChoiceLocked(dest rooms.Destination) {
	if len(dest.Matches) == 0 {
		a.status = dest.Status
		return
	}
	a.closeDetailLocked()
	a.roomChoice = append([]hostclient.Game(nil), dest.Matches...)
	a.roomChoiceIndex = 0
	a.roomChoiceOpen = true
	a.status = dest.Status
}

func (a *App) openRoomDetailsLocked() {
	dest := a.roomDestinationLocked()
	switch dest.Confirm() {
	case rooms.ConfirmWait, rooms.ConfirmOpenLibrary:
		a.status = dest.Status
		return
	case rooms.ConfirmChoose:
		a.openRoomChoiceLocked(dest)
		return
	case rooms.ConfirmEnterRoom:
		a.status = dest.Status
		return
	case rooms.ConfirmOpenLibraryBrowse:
		a.status = dest.Status
		return
	}
	if game, ok := dest.Game(); ok {
		a.openRoomDetailsForLocked(game)
		return
	}
	a.status = dest.Status
}

func (a *App) openRoomDetailsForLocked(game hostclient.Game) {
	if strings.TrimSpace(game.ID) == "" {
		return
	}
	a.roomDetail = game
	a.detailOpen = true
	a.carouselIndex = skipFailedCarousel(a.focusDetailLocked().ScreenshotIDs, a.shotFailedLocked, a.carouselIndex)
}

func (a *App) openNestedRoomLocked(id string) {
	if a.room == nil {
		return
	}
	parent := a.room
	pack, ok := a.roomsIndex.Find(id)
	if !ok || !pack.Valid() {
		a.status = "room " + id + " is not installed"
		return
	}
	a.closeRoomOverlaysLocked()
	a.room = nil
	a.roomStack = append(a.roomStack, parent)
	a.openRoomLocked(id)
	if a.room == nil {
		a.roomStack = a.roomStack[:len(a.roomStack)-1]
		a.room = parent
	}
}

func (a *App) roomChoiceSnapshotLocked() RoomChoiceSnapshot {
	if !a.roomChoiceOpen {
		return RoomChoiceSnapshot{}
	}
	return RoomChoiceSnapshot{
		Open:  true,
		Index: a.roomChoiceIndex,
		Rows:  append([]hostclient.Game(nil), a.roomChoice...),
		Hint:  roomChoiceHint(a.affinity.current.Kind),
	}
}

func roomChoiceHint(kind InputKind) string {
	return selectWord(kind) + " choose  " + detailsWord(kind) + " details  " + backWord(kind) + " close"
}

func (a *App) queueRoomDetailWorkLocked(now time.Time, queue []pendingWork) []pendingWork {
	if a.room == nil || !a.detailOpen || a.roomDetail.ID == "" || a.gpuParked {
		return queue
	}
	if len(a.inflight) >= maxInflight {
		return queue
	}
	game := a.roomDetail
	slot := a.covers[game.ID]
	if slot == nil {
		slot = &coverSlot{phase: coverIdle}
		if handle := hostclient.NormalizeHandle(game.Cover); handle != "" {
			slot.handle = handle
			slot.phase = coverArtwork
		}
		a.covers[game.ID] = slot
	}
	if _, have := a.details[game.ID]; !have && slot.phase != coverIdle && slot.phase != coverArtwork {
		if _, busy := a.inflight[game.ID]; !busy && !now.Before(slot.detailNext) {
			a.inflight[game.ID] = workPresentation
			return append(queue, pendingWork{item: workItem{kind: workPresentation, gameID: game.ID, handle: slot.handle, gen: a.loadGen}, key: game.ID})
		}
	}
	if _, busy := a.inflight[game.ID]; busy {
		return queue
	}
	switch slot.phase {
	case coverIdle:
		a.inflight[game.ID] = workPresentation
		return append(queue, pendingWork{item: workItem{kind: workPresentation, gameID: game.ID, handle: slot.handle, gen: a.loadGen}, key: game.ID})
	case coverArtwork:
		if slot.handle == "" {
			slot.phase = coverMissing
			return queue
		}
		a.inflight[game.ID] = workArtwork
		return append(queue, pendingWork{item: workItem{kind: workArtwork, gameID: game.ID, handle: slot.handle, gen: a.loadGen}, key: game.ID})
	}
	return queue
}

func detailsWord(kind InputKind) string {
	switch kind {
	case InputKeyboard, InputMouse:
		return "i"
	default:
		return "Y"
	}
}

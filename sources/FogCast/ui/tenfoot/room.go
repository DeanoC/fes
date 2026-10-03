package tenfoot

import (
	"context"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/ui/rooms"
	"github.com/DeanoC/FogCast/ui/shared"
)

const roomCoverWorkers = 4

// RoomSnapshot is the frame-loop readable copy of the open room.
type RoomSnapshot struct {
	Open   bool
	ID     string
	Title  string
	Frame  rooms.Frame
	Images map[string]*image.RGBA
	Err    string
	// OffsetX/OffsetY translate room coordinates (0,0 = top-left of the
	// safe content area) into window pixels.
	OffsetX, OffsetY int
	Width, Height    int
	Destination      rooms.Destination
	Choice           RoomChoiceSnapshot
	// Parents is the nested-room stack under the current room (root first).
	// Return-from-play must keep this stack; Back still pops one parent.
	Parents []string
	// ReducedMotion is the launcher preference currently exposed to the script.
	ReducedMotion bool
	// DuringPlay means the room owns local navigation while the core runs.
	DuringPlay bool
	Notice     string
}

// RoomPickerRow is one entry of the Home overlay.
type RoomPickerRow struct {
	ID      string
	Label   string
	Detail  string
	Kind    string
	Library bool
	Invalid bool
	Pinned  bool
}

// RoomPickerSnapshot is the Home overlay (pinned / recent / rooms / library).
type RoomPickerSnapshot struct {
	Open  bool
	Index int
	Rows  []RoomPickerRow
}

// roomServices adapts the host client for room scripts.
type roomServices struct {
	client *Client
}

func (s roomServices) QueryGames(ctx context.Context, q hostclient.GameListQuery, max int) ([]hostclient.Game, error) {
	if q.Limit <= 0 || q.Limit > defaultPageLimit {
		q.Limit = defaultPageLimit
	}
	// Rooms classify offline and missing-media rows. Other ListGames callers
	// keep the ready-only default by leaving Availability empty.
	if strings.TrimSpace(q.Availability) == "" {
		q.Availability = "all"
	}
	var out []hostclient.Game
	for {
		page, next, err := s.client.ListGames(ctx, q)
		if err != nil {
			return out, err
		}
		out = append(out, page...)
		if next == "" || len(page) == 0 || (max > 0 && len(out) >= max) {
			break
		}
		q.Cursor = next
	}
	if max > 0 && len(out) > max {
		out = out[:max]
	}
	return out, nil
}

func (s roomServices) Platforms(ctx context.Context) ([]hostclient.Platform, error) {
	return s.client.Platforms(ctx)
}

func (s roomServices) Collections(ctx context.Context) ([]hostclient.Collection, error) {
	return s.client.Collections(ctx)
}

func (s roomServices) Game(ctx context.Context, id string) (hostclient.Game, error) {
	return s.client.Game(ctx, id)
}

// SetRooms installs the discovered room packs. dir is shown in settings.
func (a *App) SetRooms(index *rooms.Index, dir string) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.roomsIndex = index
	a.roomsDir = dir
}

// SetHomeRooms selects the Home overlay (true) or the library (false) as the
// screen shown at start. Home always lists pinned rooms, recently played
// games, installed rooms, and the library once opened.
func (a *App) SetHomeRooms(on bool) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.homeRooms = on
}

// SetHomeRoom names the room opened as the root at start and for Home when
// Home is rooms. Empty, or an id that cannot be opened, uses the picker.
// The id is kept even when it fails the pack id rule so the fallback can
// name it in host diagnostics.
func (a *App) SetHomeRoom(id string) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.homeRoom = strings.TrimSpace(id)
}

// RoomOpen reports whether a scripted room owns the screen.
func (a *App) RoomOpen() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.room != nil
}

// RoomPickerOpen reports whether the home picker overlay is on screen.
func (a *App) RoomPickerOpen() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.roomPickerOpen
}

func (a *App) roomsAvailableLocked() bool {
	return a.roomsIndex != nil && len(a.roomsIndex.Packs) > 0
}

func (a *App) showHomeLocked() {
	if !a.homeRooms {
		return
	}
	if a.openHomeRoomAsRootLocked() {
		return
	}
	if a.roomsIndex != nil && a.roomsIndex.ValidCount() > 0 {
		a.openRoomPickerLocked()
	}
}

func (a *App) roomPickerSnapshotLocked() RoomPickerSnapshot {
	if !a.roomPickerOpen {
		return RoomPickerSnapshot{}
	}
	return RoomPickerSnapshot{Open: true, Index: a.roomPickerIndex, Rows: a.roomPickerRowsLocked()}
}

func (a *App) closeRoomPickerLocked() {
	a.roomPickerOpen = false
}

func (a *App) toggleRoomPickerLocked() {
	if a.roomPickerOpen {
		a.closeRoomPickerLocked()
		return
	}
	a.openRoomPickerLocked()
}

func (a *App) roomStorePathLocked(id string) string {
	base := a.prefsPath
	if base == "" {
		base = defaultPrefsPath()
	}
	if base == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(base), "rooms", id+".state.json")
}

// openRoomLocked replaces the current room (if any) with id. Nested rooms
// are opened by pushing the live parent instance onto roomStack first
// (see applyRoomActionsLocked) so its state survives until Back.
func (a *App) openRoomLocked(id string) {
	pack, ok := a.roomsIndex.Find(id)
	if !ok || !pack.Valid() {
		a.roomErr = fmt.Sprintf("room %s is not installed", id)
		a.status = a.roomErr
		return
	}
	a.openValidRoomLocked(pack)
}

// openValidRoomLocked replaces the current room with pack. It reports
// false only when rooms.New fails; a script error still leaves the
// instance up so the existing error panel can show it.
func (a *App) openValidRoomLocked(pack rooms.Pack) bool {
	a.closeRoomLocked()
	a.closeRoomOverlaysLocked()
	a.closeFiltersLocked()
	a.closeCollectionOverlaysLocked()
	a.searchOpen = false
	w, h := a.roomContentSizeLocked(pack.ID)
	inst, err := rooms.New(pack, rooms.Options{
		Width:                  w,
		Height:                 h,
		Services:               roomServices{client: a.client},
		Index:                  a.roomsIndex,
		Theme:                  a.theme,
		ReducedMotion:          a.reducedMotion,
		SessionDisplayRequired: a.needsSessionDisplayLocked(),
		StorePath:              a.roomStorePathLocked(pack.ID),
		Local:                  a.localCores,
	})
	if err != nil {
		a.roomErr = err.Error()
		a.status = a.roomErr
		return false
	}
	a.room = inst
	a.roomErr = ""
	a.roomWasParked = a.gpuParked
	inst.SetSessionState(a.session.State)
	inst.SetLiveControls(a.sessionDisplayOfferedLocked())
	a.postUIEventLocked("ui.nav", map[string]string{"reason": "room", "view": "room:" + pack.ID})
	if err := inst.Load(); err != nil {
		a.roomErr = err.Error()
		a.status = "room failed: " + err.Error()
		return true
	}
	a.applyRoomActionsLocked()
	a.status = pack.Title
	return true
}

// openHomeRoomAsRootLocked opens home_room as the only room when Home is
// rooms and the pack can be created. It reports false when Home is the
// library, no id is set, play already owns the screen, or the pack cannot
// be opened. A false result leaves the caller on today's picker path.
func (a *App) openHomeRoomAsRootLocked() bool {
	if a == nil || !a.homeRooms || a.roomDuringPlay || a.sessionStopOfferedLocked() {
		return false
	}
	id := strings.TrimSpace(a.homeRoom)
	if id == "" {
		return false
	}
	pack, reason, ok := a.resolveHomeRoomLocked(id)
	if !ok {
		a.noteHomeRoomFallbackLocked(id, reason)
		return false
	}
	if a.reclaimHomeRoomLocked(id) {
		return true
	}
	prevStatus := a.status
	a.closeAllRoomsLocked()
	a.roomPickerOpen = false
	if a.openValidRoomLocked(pack) {
		return true
	}
	a.closeRoomLocked()
	a.roomErr = ""
	a.status = prevStatus
	a.noteHomeRoomFallbackLocked(id, "open failed")
	return false
}

func (a *App) resolveHomeRoomLocked(id string) (rooms.Pack, string, bool) {
	if !rooms.ValidRoomID(id) {
		return rooms.Pack{}, "invalid id", false
	}
	if a.roomsIndex == nil {
		return rooms.Pack{}, "missing", false
	}
	pack, ok := a.roomsIndex.Find(id)
	if !ok {
		return rooms.Pack{}, "missing", false
	}
	if !pack.Valid() {
		return rooms.Pack{}, "invalid", false
	}
	return pack, "", true
}

// reclaimHomeRoomLocked makes an already-open home room the only room.
// The root-most live instance is kept so Home does not reload it.
func (a *App) reclaimHomeRoomLocked(id string) bool {
	var home *rooms.Instance
	for _, parent := range a.roomStack {
		if parent != nil && parent.ID() == id && parent.Err() == nil {
			home = parent
			break
		}
	}
	if home == nil && a.room != nil && a.room.ID() == id && a.room.Err() == nil {
		home = a.room
	}
	if home == nil {
		return false
	}
	switched := a.room != home
	if switched {
		a.closeRoomLocked()
	}
	for _, parent := range a.roomStack {
		if parent != nil && parent != home {
			parent.Close()
		}
	}
	a.roomStack = nil
	a.room = home
	a.roomPickerOpen = false
	a.roomWasParked = a.gpuParked
	a.resizeRoomsLocked()
	if switched {
		a.postUIEventLocked("ui.nav", map[string]string{"reason": "home-room", "view": "room:" + home.ID()})
		home.Resume()
		a.applyRoomActionsLocked()
	}
	return a.room != nil && a.room.ID() == id
}

func (a *App) noteHomeRoomFallbackLocked(id, reason string) {
	// TODO(slice 5): fallback notice copy per brief §4
	fmt.Fprintf(os.Stderr, "tenfoot: home_room %q fallback: %s\n", id, reason)
	a.postUIEventLocked("ui.home_room_fallback", map[string]string{
		"room":   id,
		"reason": reason,
	})
}

// closeRoomLocked closes the current room only; parents on roomStack stay alive.
func (a *App) closeRoomLocked() {
	if a.room == nil {
		return
	}
	id := a.room.ID()
	a.room.Close()
	a.room = nil
	a.roomErr = ""
	a.roomFrame = rooms.Frame{}
	a.closeRoomOverlaysLocked()
	a.postUIEventLocked("ui.nav", map[string]string{"reason": "room-close", "view": "room:" + id})
}

// closeAllRoomsLocked closes the current room and every suspended parent.
func (a *App) closeAllRoomsLocked() {
	a.closeRoomLocked()
	for _, parent := range a.roomStack {
		parent.Close()
	}
	a.roomStack = nil
}

// leaveRoomLocked resumes the suspended parent room or, at the root
// (including a home_room root), returns to the picker.
func (a *App) leaveRoomLocked() {
	a.closeRoomOverlaysLocked()
	if n := len(a.roomStack); n > 0 {
		parent := a.roomStack[n-1]
		a.roomStack = a.roomStack[:n-1]
		a.closeRoomLocked()
		a.room = parent
		a.roomWasParked = a.gpuParked
		a.resizeRoomsLocked()
		a.postUIEventLocked("ui.nav", map[string]string{"reason": "room-resume", "view": "room:" + parent.ID()})
		parent.Resume()
		a.applyRoomActionsLocked()
		return
	}
	a.closeRoomLocked()
	a.openRoomPickerLocked()
}

// resizeRoomsLocked pushes the current safe content box into the open room
// and any suspended parents.
func (a *App) resizeRoomsLocked() {
	if a.room != nil {
		w, h := a.roomContentSizeLocked(a.room.ID())
		a.room.Resize(w, h)
	}
	for _, parent := range a.roomStack {
		w, h := a.roomContentSizeLocked(parent.ID())
		parent.Resize(w, h)
	}
}

func (a *App) roomContentSizeLocked(id string) (int, int) {
	w, h := a.grid.contentWidth(), a.grid.contentHeight()
	if id == hardwareRoomID {
		h = max(1, h-hardwareRoomFooterHeight)
	}
	return w, h
}

func roomCommandName(cmd Command) string {
	return strings.ReplaceAll(cmd.String(), "-", "_")
}

func (a *App) applyRoomActionsLocked() {
	if a.room == nil {
		return
	}
	for _, act := range a.room.TakeActions() {
		switch act.Kind {
		case rooms.ActionHardwareSetup:
			if a.session.State != "active" {
				a.openZX81SetupLocked()
			}
		case rooms.ActionHardwareTapes, rooms.ActionHardwareImportExpansion:
			a.openHardwarePickerLocked(act)
		case rooms.ActionLaunch:
			a.launchFromRoomLocked(act.GameID)
		case rooms.ActionStop:
			if a.roomSessionMatchesLocked(act) {
				a.startStopLocked()
			}
		case rooms.ActionOpenTape:
			if a.roomSessionMatchesLocked(act) {
				a.openTapePickerLocked()
			}
		case rooms.ActionResumeSession:
			a.resumeRoomSessionLocked()
		case rooms.ActionHome:
			if !a.openHomeRoomAsRootLocked() {
				a.openRoomPickerLocked()
			}
			return
		case rooms.ActionOpenRoom:
			a.openNestedRoomLocked(act.RoomID)
			return
		case rooms.ActionBack:
			if a.roomDuringPlay {
				a.resumeRoomSessionLocked()
				return
			}
			a.leaveRoomLocked()
			return
		case rooms.ActionOpenLibrary:
			a.openLibraryFromRoomLocked(act)
			return
		}
		if a.room == nil {
			return
		}
	}
}

// dropRoomNavActionsLocked discards launcher requests queued by on_resume
// after a play session. Nested Back still uses leaveRoomLocked; that path
// applies parent actions normally.
func (a *App) dropRoomNavActionsLocked() {
	if a.room == nil {
		return
	}
	_ = a.room.TakeActions()
}

func (a *App) openLibraryFromRoomLocked(act rooms.Action) {
	if a.sessionStopOfferedLocked() {
		a.status = "Stop the running machine before opening the library."
		return
	}
	a.closeAllRoomsLocked()
	a.roomDuringPlay = false
	a.syncGPUParkLocked()
	a.roomPickerOpen = false
	a.platformID = strings.TrimSpace(act.Platform)
	a.collectionID = strings.TrimSpace(act.Collection)
	a.normalizeSortForCollectionLocked()
	if layout := strings.TrimSpace(act.Layout); layout != "" {
		a.setLayoutLocked(parseLayout(layout), false)
	}
	a.stampNavLocked("room-library")
	a.reloadLocked()
}

func (a *App) launchFromRoomLocked(gameID string) {
	gameID = strings.TrimSpace(gameID)
	if gameID == "" {
		return
	}
	if a.sessionStopOfferedLocked() {
		a.status = "Stop the running machine before starting a new hardware setup."
		return
	}
	if game, ok := a.room.CachedGame(gameID); ok {
		a.startLaunchGameLocked(game)
		return
	}
	for _, game := range a.games {
		if game.ID == gameID {
			a.startLaunchGameLocked(game)
			return
		}
	}
	if a.launch.Phase == "launching" || a.sessionStopOfferedLocked() {
		return
	}
	a.launchLeaseRefusal = false
	a.launch = LaunchSnapshot{GameID: gameID, Phase: "launching", Message: "resolving " + gameID}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	go a.resolveRoomLaunch(ctx, gameID)
}

func (a *App) resolveRoomLaunch(ctx context.Context, gameID string) {
	game, err := a.client.Game(ctx, gameID)
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.launch.GameID != gameID || a.launch.Phase != "launching" {
		return
	}
	if err != nil {
		a.launchLeaseRefusal = false
		a.launch = LaunchSnapshot{GameID: gameID, Phase: "error", Message: "launch failed: " + err.Error(), ErrorMessage: err.Error()}
		return
	}
	a.launchLeaseRefusal = false
	a.launch = LaunchSnapshot{Phase: "idle"}
	a.startLaunchGameLocked(game)
}

// tickRoomLocked advances the open room: session state, resume after play,
// async results, update/draw, and any launcher requests the script made.
func (a *App) tickRoomLocked(now time.Time) {
	if a.room == nil {
		return
	}
	a.room.SetSessionState(a.session.State)
	a.room.SetLiveControls(a.sessionDisplayOfferedLocked())
	if a.gpuParked || a.localPresentsPaused {
		a.roomWasParked = true
		return
	}
	if a.roomWasParked {
		a.roomWasParked = false
		a.room.Resume()
		// on_resume may refresh Played chrome. It must not navigate: a game
		// launched inside a nested room returns there (§5, §6), not to the
		// parent or picker.
		a.dropRoomNavActionsLocked()
		if a.room == nil {
			return
		}
	}
	if a.room.Err() != nil {
		a.roomErr = a.room.Err().Error()
		return
	}
	a.roomFrame = a.room.Step(now)
	a.applyRoomActionsLocked()
	if a.room == nil {
		return
	}
	if err := a.room.Err(); err != nil {
		a.roomErr = err.Error()
		a.roomFrame = rooms.Frame{}
		return
	}
	for _, id := range a.room.TakeCoverRequests() {
		a.requestRoomCoverLocked(a.room, id)
	}
}

func (a *App) requestRoomCoverLocked(room *rooms.Instance, gameID string) {
	if slot := a.covers[gameID]; slot != nil && slot.image != nil {
		room.DeliverCover(gameID, slot.image, nil)
		return
	}
	if a.roomCoverSem == nil {
		a.roomCoverSem = make(chan struct{}, roomCoverWorkers)
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	sem := a.roomCoverSem
	go func() {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			return
		}
		defer func() { <-sem }()
		img, err := a.fetchRoomCover(ctx, gameID)
		room.DeliverCover(gameID, img, err)
	}()
}

func (a *App) fetchRoomCover(ctx context.Context, gameID string) (*image.RGBA, error) {
	pres, err := a.client.GamePresentation(ctx, gameID)
	if err != nil {
		return nil, err
	}
	handle := shared.CoverHandle(hostclient.Game{ID: gameID}, pres)
	if handle == "" {
		return nil, nil
	}
	data, _, err := a.client.Artwork(ctx, handle)
	if err != nil {
		return nil, err
	}
	return shared.DecodeCover(data)
}

func (a *App) roomSnapshotLocked(withImages bool) RoomSnapshot {
	if a.room == nil {
		return RoomSnapshot{}
	}
	w, h := a.roomContentSizeLocked(a.room.ID())
	snap := RoomSnapshot{
		Open:          true,
		ID:            a.room.ID(),
		Title:         a.room.Title(),
		Frame:         a.roomFrame,
		Err:           a.roomErr,
		OffsetX:       a.grid.contentLeft(),
		OffsetY:       a.grid.contentTop(),
		Width:         w,
		Height:        h,
		Destination:   a.roomDestinationLocked(),
		Choice:        a.roomChoiceSnapshotLocked(),
		Parents:       a.roomParentIDsLocked(),
		ReducedMotion: a.room.ReducedMotion(),
		DuringPlay:    a.roomDuringPlay,
		Notice:        a.roomSessionNotice,
	}
	if withImages {
		snap.Images = a.room.Images()
	}
	return snap
}

func (a *App) roomParentIDsLocked() []string {
	if len(a.roomStack) == 0 {
		return nil
	}
	ids := make([]string, 0, len(a.roomStack))
	for _, parent := range a.roomStack {
		if parent == nil {
			continue
		}
		ids = append(ids, parent.ID())
	}
	if len(ids) == 0 {
		return nil
	}
	return ids
}

func (a *App) roomHintLocked() string {
	kind := a.affinity.current.Kind
	if a.room != nil && a.room.Err() != nil {
		return backWord(kind) + " home"
	}
	if a.roomChoiceOpen {
		return roomChoiceHint(kind)
	}
	if a.firmwarePickerOpen {
		return firmwarePickerHint(kind)
	}
	if a.detailOpen {
		dest := a.roomDestinationLocked()
		if dest.Confirm() == rooms.ConfirmImportFirmware {
			return selectWord(kind) + " import  " + backWord(kind) + " close  " + settingsWord(kind) + " settings"
		}
		return selectWord(kind) + " play  " + backWord(kind) + " close  " + settingsWord(kind) + " settings"
	}
	dest := a.roomDestinationLocked()
	action := strings.TrimSpace(dest.Action)
	if action == "" {
		action = "confirm"
	}
	return selectWord(kind) + " " + strings.ToLower(action) + "  " + detailsWord(kind) + " details  " + backWord(kind) + " back  " + homeWord(kind) + " home  " + settingsWord(kind) + " settings"
}

func homeWord(kind InputKind) string {
	switch kind {
	case InputKeyboard, InputMouse:
		return "h"
	default:
		return "hold B"
	}
}

func settingsWord(kind InputKind) string {
	switch kind {
	case InputKeyboard, InputMouse:
		return "o"
	default:
		return "GUIDE"
	}
}

func roomPickerHint(kind InputKind) string {
	return selectWord(kind) + " open  " + detailsWord(kind) + " pin  " + backWord(kind) + " close  " + settingsWord(kind) + " settings"
}

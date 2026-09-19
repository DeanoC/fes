package tenfoot

import (
	"context"
	"fmt"
	"image"
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
}

// RoomPickerRow is one entry of the home picker.
type RoomPickerRow struct {
	ID      string
	Label   string
	Detail  string
	Library bool
	Invalid bool
}

// RoomPickerSnapshot is the home picker overlay.
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

// SetHomeRooms selects the room picker (true) or the library (false) as the
// screen shown at start.
func (a *App) SetHomeRooms(on bool) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.homeRooms = on
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
	if a.homeRooms && a.roomsIndex != nil && a.roomsIndex.ValidCount() > 0 {
		a.openRoomPickerLocked()
	}
}

func (a *App) roomPickerRowsLocked() []RoomPickerRow {
	rows := []RoomPickerRow{{ID: "", Label: "Library", Detail: "browse every title", Library: true}}
	if a.roomsIndex == nil {
		return rows
	}
	for _, p := range a.roomsIndex.Packs {
		row := RoomPickerRow{ID: p.ID, Label: p.Title, Detail: strings.TrimSpace(p.Description)}
		if p.Err != nil {
			row.Invalid = true
			row.Detail = "unavailable: " + p.Err.Error()
		} else if row.Detail == "" && strings.TrimSpace(p.Author) != "" {
			row.Detail = "by " + strings.TrimSpace(p.Author)
		}
		rows = append(rows, row)
	}
	return rows
}

func (a *App) roomPickerSnapshotLocked() RoomPickerSnapshot {
	if !a.roomPickerOpen {
		return RoomPickerSnapshot{}
	}
	return RoomPickerSnapshot{Open: true, Index: a.roomPickerIndex, Rows: a.roomPickerRowsLocked()}
}

func (a *App) openRoomPickerLocked() {
	if !a.roomsAvailableLocked() {
		a.status = "no rooms installed"
		return
	}
	a.closeRoomOverlaysLocked()
	a.closeFiltersLocked()
	a.closeCollectionOverlaysLocked()
	a.searchOpen = false
	a.roomPickerOpen = true
	rows := a.roomPickerRowsLocked()
	a.roomPickerIndex = 0
	if a.room != nil {
		for i, row := range rows {
			if row.ID == a.room.ID() {
				a.roomPickerIndex = i
				break
			}
		}
	}
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

func (a *App) handleRoomPickerLocked(cmd Command) {
	rows := a.roomPickerRowsLocked()
	n := len(rows)
	if n == 0 {
		a.roomPickerOpen = false
		return
	}
	if a.roomPickerIndex < 0 {
		a.roomPickerIndex = 0
	}
	if a.roomPickerIndex >= n {
		a.roomPickerIndex = n - 1
	}
	switch cmd {
	case CmdUp, CmdLeft, CmdTabPrev:
		a.roomPickerIndex = (a.roomPickerIndex - 1 + n) % n
	case CmdDown, CmdRight, CmdTab:
		a.roomPickerIndex = (a.roomPickerIndex + 1) % n
	case CmdSelect:
		row := rows[a.roomPickerIndex]
		if row.Invalid {
			a.status = row.Detail
			return
		}
		a.roomPickerOpen = false
		if row.Library {
			a.closeAllRoomsLocked()
			a.stampNavLocked("library")
			return
		}
		a.closeAllRoomsLocked()
		a.openRoomLocked(row.ID)
	case CmdBack, CmdHome:
		a.roomPickerOpen = false
	}
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
	a.closeRoomLocked()
	a.closeRoomOverlaysLocked()
	a.closeFiltersLocked()
	a.closeCollectionOverlaysLocked()
	a.searchOpen = false
	inst, err := rooms.New(pack, rooms.Options{
		Width:     a.grid.contentWidth(),
		Height:    a.grid.contentHeight(),
		Services:  roomServices{client: a.client},
		Index:     a.roomsIndex,
		Theme:     a.theme,
		StorePath: a.roomStorePathLocked(pack.ID),
	})
	if err != nil {
		a.roomErr = err.Error()
		a.status = a.roomErr
		return
	}
	a.room = inst
	a.roomErr = ""
	a.roomWasParked = a.gpuParked
	inst.SetSessionState(a.session.State)
	a.postUIEventLocked("ui.nav", map[string]string{"reason": "room", "view": "room:" + pack.ID})
	if err := inst.Load(); err != nil {
		a.roomErr = err.Error()
		a.status = "room failed: " + err.Error()
		return
	}
	a.applyRoomActionsLocked()
	a.status = pack.Title
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

// leaveRoomLocked resumes the suspended parent room or, at the root,
// returns to the picker.
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
	w, h := a.grid.contentWidth(), a.grid.contentHeight()
	if a.room != nil {
		a.room.Resize(w, h)
	}
	for _, parent := range a.roomStack {
		parent.Resize(w, h)
	}
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
		case rooms.ActionLaunch:
			a.launchFromRoomLocked(act.GameID)
		case rooms.ActionOpenRoom:
			a.openNestedRoomLocked(act.RoomID)
			return
		case rooms.ActionBack:
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

func (a *App) openLibraryFromRoomLocked(act rooms.Action) {
	a.closeAllRoomsLocked()
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
		a.launch = LaunchSnapshot{Phase: "error", Message: "launch failed: " + err.Error()}
		return
	}
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
	if a.gpuParked {
		a.roomWasParked = true
		return
	}
	if a.roomWasParked {
		a.roomWasParked = false
		a.room.Resume()
		a.applyRoomActionsLocked()
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
	snap := RoomSnapshot{
		Open:        true,
		ID:          a.room.ID(),
		Title:       a.room.Title(),
		Frame:       a.roomFrame,
		Err:         a.roomErr,
		OffsetX:     a.grid.contentLeft(),
		OffsetY:     a.grid.contentTop(),
		Width:       a.grid.contentWidth(),
		Height:      a.grid.contentHeight(),
		Destination: a.roomDestinationLocked(),
		Choice:      a.roomChoiceSnapshotLocked(),
	}
	if withImages {
		snap.Images = a.room.Images()
	}
	return snap
}

func (a *App) roomHintLocked() string {
	kind := a.affinity.current.Kind
	if a.room != nil && a.room.Err() != nil {
		return backWord(kind) + " home"
	}
	if a.roomChoiceOpen {
		return roomChoiceHint(kind)
	}
	if a.detailOpen {
		return selectWord(kind) + " play  " + backWord(kind) + " close  " + settingsWord(kind) + " settings"
	}
	dest := a.roomDestinationLocked()
	action := strings.TrimSpace(dest.Action)
	if action == "" {
		action = "confirm"
	}
	return selectWord(kind) + " " + strings.ToLower(action) + "  " + detailsWord(kind) + " details  " + backWord(kind) + " back  " + homeWord(kind) + " rooms  " + settingsWord(kind) + " settings"
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
	return selectWord(kind) + " open  " + backWord(kind) + " close"
}

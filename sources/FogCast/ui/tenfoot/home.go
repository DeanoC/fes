package tenfoot

import (
	"context"
	"strings"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/internal/release"
	"github.com/DeanoC/FogCast/ui/rooms"
)

// Home row kinds. The overlay lists pinned rooms, recently played games,
// every installed room, and the full library.
const (
	HomeKindPinned  = "pinned"
	HomeKindRecent  = "recent"
	HomeKindRecents = "recents"
	HomeKindRoom    = "room"
	HomeKindLibrary = "library"
)

const (
	homeRecentLimit     = 5
	homeRecentPageLimit = 50
)

func (r RoomPickerRow) key() string {
	switch r.Kind {
	case HomeKindLibrary:
		return HomeKindLibrary
	case HomeKindRecents:
		return HomeKindRecents
	case HomeKindRecent:
		return HomeKindRecent + ":" + r.ID
	case HomeKindPinned:
		return HomeKindPinned + ":" + r.ID
	default:
		return HomeKindRoom + ":" + r.ID
	}
}

func (r RoomPickerRow) isRoom() bool {
	return r.Kind == HomeKindRoom || r.Kind == HomeKindPinned
}

// Internal screens are opened from their owning navigation flow, rather than
// offered as independent destinations or default Home rooms.
func userFacingRoom(id string) bool {
	return id != systemRoomID && id != hardwareRoomID && id != colecoArcadeRoomID
}

func (a *App) hasColecoRoomOpenLocked() bool {
	if a.room != nil && a.room.ID() == colecoArcadeRoomID {
		return true
	}
	for _, parent := range a.roomStack {
		if parent != nil && parent.ID() == colecoArcadeRoomID {
			return true
		}
	}
	if a.libraryReturn != nil {
		if a.libraryReturn.room != nil && a.libraryReturn.room.ID() == colecoArcadeRoomID {
			return true
		}
		for _, parent := range a.libraryReturn.stack {
			if parent != nil && parent.ID() == colecoArcadeRoomID {
				return true
			}
		}
	}
	return false
}

func (a *App) publicRoomCountLocked() int {
	count := 0
	if a.roomsIndex != nil {
		for _, p := range a.roomsIndex.Packs {
			if userFacingRoom(p.ID) && p.Valid() {
				count++
			}
		}
	}
	return count
}

func normalizePinnedRooms(ids []string) []string {
	out := make([]string, 0, len(ids))
	seen := map[string]struct{}{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func (a *App) loadPinnedRoomsLocked() {
	path := strings.TrimSpace(a.prefsPath)
	if path == "" {
		return
	}
	prefs, err := loadTenfootPrefs(path)
	if err != nil {
		return
	}
	a.pinnedRooms = normalizePinnedRooms(prefs.PinnedRooms)
}

func (a *App) roomPinnedLocked(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}
	for _, existing := range a.pinnedRooms {
		if existing == id {
			return true
		}
	}
	return false
}

func (a *App) homeRoomRowLocked(id string) RoomPickerRow {
	row := RoomPickerRow{ID: id, Label: id, Kind: HomeKindRoom}
	if a.roomsIndex == nil {
		row.Invalid = true
		row.Detail = "room is not installed"
		return row
	}
	pack, ok := a.roomsIndex.Find(id)
	if !ok {
		row.Invalid = true
		row.Detail = "room is not installed"
		return row
	}
	return a.packToHomeRow(pack)
}

func (a *App) packToHomeRow(p rooms.Pack) RoomPickerRow {
	row := RoomPickerRow{ID: p.ID, Label: p.Title, Detail: strings.TrimSpace(p.Description), Kind: HomeKindRoom}
	if p.Err != nil {
		row.Invalid = true
		row.Detail = "unavailable: " + p.Err.Error()
	} else if row.Detail == "" && strings.TrimSpace(p.Author) != "" {
		row.Detail = "by " + strings.TrimSpace(p.Author)
	}
	return row
}

func (a *App) roomPickerRowsLocked() []RoomPickerRow {
	rows := make([]RoomPickerRow, 0, 8)
	pinnedSeen := map[string]struct{}{}
	for _, id := range a.pinnedRooms {
		id = strings.TrimSpace(id)
		if id == "" || !userFacingRoom(id) {
			continue
		}
		if strings.HasPrefix(id, rooms.ExamplePrefix) {
			installed := false
			if a.roomsIndex != nil {
				_, installed = a.roomsIndex.Find(id)
			}
			if !installed {
				continue
			}
		}
		if _, dup := pinnedSeen[id]; dup {
			continue
		}
		pinnedSeen[id] = struct{}{}
		row := a.homeRoomRowLocked(id)
		row.Kind = HomeKindPinned
		row.Pinned = true
		if strings.TrimSpace(row.Detail) == "" {
			row.Detail = "Pinned"
		} else if !strings.HasPrefix(row.Detail, "Pinned") {
			row.Detail = "Pinned  ·  " + row.Detail
		}
		rows = append(rows, row)
	}
	shown := a.shownRecentsLocked()
	for _, game := range shown {
		id := strings.TrimSpace(game.ID)
		if id == "" {
			continue
		}
		label := strings.TrimSpace(displayGameTitle(game))
		if label == "" {
			label = id
		}
		detail := strings.ToUpper(strings.TrimSpace(game.System))
		if detail == "" {
			detail = "Recently played"
		} else {
			detail = "Recent  ·  " + detail
		}
		rows = append(rows, RoomPickerRow{ID: id, Label: label, Detail: detail, Kind: HomeKindRecent})
	}
	recents := RoomPickerRow{ID: "recents", Label: "Recent", Kind: HomeKindRecents, Detail: "recently played games"}
	switch {
	case !a.homeRecentsLoaded:
		recents.Detail = "loading recently played…"
	case a.homeRecentsErr != "":
		recents.Detail = a.homeRecentsErr
	case len(shown) == 0:
		recents.Detail = "no recently played games"
	}
	rows = append(rows, recents)
	if a.roomsIndex != nil {
		for _, p := range a.roomsIndex.Packs {
			colecoOrigin := p.ID == colecoArcadeRoomID && a.hasColecoRoomOpenLocked()
			if !userFacingRoom(p.ID) && !colecoOrigin {
				continue
			}
			row := a.packToHomeRow(p)
			if a.room != nil && a.room.ID() == p.ID && a.room.Revision() != p.Revision {
				row.Detail = "Update ready. Select to reload."
			}
			row.Kind = HomeKindRoom
			if _, pinned := pinnedSeen[p.ID]; pinned {
				row.Pinned = true
			}
			rows = append(rows, row)
		}
	}
	rows = append(rows, RoomPickerRow{
		ID: "", Label: "Library", Detail: "browse every title",
		Library: true, Kind: HomeKindLibrary,
	})
	if a.sessionStopOfferedLocked() {
		for i := range rows {
			if homeRowNeedsIdle(rows[i]) {
				rows[i].Invalid = true
				rows[i].Detail = "Stop the running machine before opening the library or another title."
			}
		}
	}
	return rows
}

func homeRowNeedsIdle(row RoomPickerRow) bool {
	return row.Kind == HomeKindLibrary || row.Kind == HomeKindRecents || row.Kind == HomeKindRecent
}

func (a *App) homeFocusKeyLocked() string {
	rows := a.roomPickerRowsLocked()
	if a.roomPickerIndex >= 0 && a.roomPickerIndex < len(rows) {
		return rows[a.roomPickerIndex].key()
	}
	return ""
}

func (a *App) restoreHomeFocusLocked(key string) {
	rows := a.roomPickerRowsLocked()
	if len(rows) == 0 {
		a.roomPickerIndex = 0
		return
	}
	if key != "" {
		for i, row := range rows {
			if row.key() == key {
				a.roomPickerIndex = i
				return
			}
		}
	}
	a.roomPickerIndex = 0
}

func (a *App) openRoomPickerLocked() {
	a.refreshInstalledRoomsLocked()
	a.closeRoomOverlaysLocked()
	a.closeFiltersLocked()
	a.closeCollectionOverlaysLocked()
	a.searchOpen = false
	wasOpen := a.roomPickerOpen
	prevKey := ""
	if wasOpen {
		prevKey = a.homeFocusKeyLocked()
	}
	a.roomPickerOpen = true
	a.ensureHomeRecentsLocked()
	key := ""
	if a.room != nil {
		id := a.room.ID()
		if a.roomPinnedLocked(id) {
			key = HomeKindPinned + ":" + id
		} else {
			key = HomeKindRoom + ":" + id
		}
	} else if wasOpen {
		key = prevKey
	}
	a.restoreHomeFocusLocked(key)
}

func (a *App) goHomeNowLocked() {
	if a.sessionStopOfferedLocked() || a.launch.Phase == "launching" {
		a.settingsStatus = "stop the session to go home"
		a.status = a.settingsStatus
		return
	}
	a.closeSettingsLocked()
	if a.openHomeRoomAsRootLocked() {
		return
	}
	a.openRoomPickerLocked()
	a.status = "home"
}

func (a *App) activateHomeRowLocked(row RoomPickerRow) {
	if a.sessionStopOfferedLocked() && homeRowNeedsIdle(row) {
		a.status = "Stop the running machine before opening the library or another title."
		return
	}
	if row.Invalid {
		a.status = row.Detail
		return
	}
	switch row.Kind {
	case HomeKindLibrary:
		a.roomPickerOpen = false
		a.suspendRoomsForLibraryLocked()
		a.libraryCoreIDs = nil
		a.stampNavLocked("library")
		a.reloadLocked()
	case HomeKindRecents:
		a.openRecentsFromHomeLocked()
	case HomeKindRecent:
		a.launchRecentFromHomeLocked(row.ID)
	case HomeKindPinned, HomeKindRoom:
		pack, ok := rooms.Pack{}, false
		if a.roomsIndex != nil {
			pack, ok = a.roomsIndex.Find(row.ID)
		}
		if a.room != nil && a.room.ID() == row.ID {
			if ok && pack.Revision == a.room.Revision() {
				if a.room.ReleaseOnly() == !a.allCores {
					return
				}
			}
		}
		if a.sessionStopOfferedLocked() || a.launch.Phase == "launching" {
			a.status = "Stop the running machine before opening another room."
			return
		}
		a.roomPickerOpen = false
		if ok && pack.Revision != "" {
			a.openInstalledRoomLocked(pack, true, "")
			return
		}
		a.closeAllRoomsLocked()
		a.openRoomLocked(row.ID)
	default:
		a.status = "unknown home row"
	}
}

func (a *App) openRecentsFromHomeLocked() {
	a.suspendRoomsForLibraryLocked()
	a.roomPickerOpen = false
	a.collectionID = "recents"
	a.libraryCoreIDs = nil
	a.normalizeSortForCollectionLocked()
	a.stampNavLocked("home-recents")
	a.reloadLocked()
}

func (a *App) launchRecentFromHomeLocked(id string) {
	game, ok := a.homeRecentByIDLocked(id)
	if !ok {
		a.status = "recent title is gone"
		return
	}
	a.roomPickerOpen = false
	a.closeAllRoomsLocked()
	a.startLaunchGameLocked(game)
}

// shownRecentsLocked is Home's recents without games for cores outside the v1
// release, unless all cores are shown. A game with no core is kept.
func (a *App) shownRecentsLocked() []hostclient.Game {
	shown := make([]hostclient.Game, 0, len(a.homeRecents))
	for _, game := range a.homeRecents {
		if game.CoreID == "" || release.Shown(game.CoreID, a.allCores) {
			shown = append(shown, game)
		}
	}
	return shown
}

func (a *App) homeRecentByIDLocked(id string) (hostclient.Game, bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		return hostclient.Game{}, false
	}
	for _, game := range a.shownRecentsLocked() {
		if game.ID == id {
			return game, true
		}
	}
	return hostclient.Game{}, false
}

func (a *App) togglePinnedRoomLocked(id string) {
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	key := ""
	for i, existing := range a.pinnedRooms {
		if existing != id {
			continue
		}
		a.pinnedRooms = append(a.pinnedRooms[:i], a.pinnedRooms[i+1:]...)
		a.persistPrefsLocked("pinned_rooms")
		a.status = "unpinned"
		key = HomeKindRoom + ":" + id
		a.restoreHomeFocusLocked(key)
		return
	}
	a.pinnedRooms = append([]string{id}, a.pinnedRooms...)
	a.persistPrefsLocked("pinned_rooms")
	a.status = "pinned"
	a.restoreHomeFocusLocked(HomeKindPinned + ":" + id)
}

func (a *App) ensureHomeRecentsLocked() {
	a.homeRecentsGen++
	gen := a.homeRecentsGen
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	go a.loadHomeRecents(ctx, gen)
}

func (a *App) loadHomeRecents(ctx context.Context, gen int) {
	if a == nil {
		return
	}
	a.mu.Lock()
	client := a.client
	allCores := a.allCores
	current := gen == a.homeRecentsGen
	a.mu.Unlock()
	if !current {
		return
	}
	if client == nil {
		a.mu.Lock()
		if gen == a.homeRecentsGen {
			a.homeRecents = nil
			a.homeRecentsErr = ""
			a.homeRecentsLoaded = true
		}
		a.mu.Unlock()
		return
	}
	var games []hostclient.Game
	var err error
	fails := 0
	cursor := ""
	seenCursors := map[string]struct{}{}
	for {
		if ctx.Err() != nil {
			return
		}
		var page []hostclient.Game
		var next string
		page, next, err = client.ListGames(ctx, hostclient.GameListQuery{
			Collection: "recents",
			Limit:      homeRecentPageLimit,
			Cursor:     cursor,
		})
		if err != nil {
			if !libraryNetRetry(err) {
				break
			}
			a.mu.Lock()
			stale := gen != a.homeRecentsGen
			a.mu.Unlock()
			if stale {
				return
			}
			fails++
			a.noteRecentsUnreachable(gen, games)
			if !a.libraryRetryWait(ctx, fails, gen, true) {
				return
			}
			continue
		}
		for _, game := range page {
			if !allCores && game.CoreID != "" && !release.Shown(game.CoreID, false) {
				continue
			}
			games = append(games, game)
			if len(games) == homeRecentLimit {
				break
			}
		}
		if len(games) == homeRecentLimit || next == "" {
			break
		}
		if _, seen := seenCursors[next]; seen {
			break
		}
		seenCursors[next] = struct{}{}
		cursor = next
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if gen != a.homeRecentsGen {
		return
	}
	key := ""
	if a.roomPickerOpen {
		key = a.homeFocusKeyLocked()
	}
	a.homeRecentsLoaded = true
	if err != nil && len(games) == 0 {
		a.homeRecents = nil
		a.homeRecentsErr = "couldn't load recently played"
		if a.roomPickerOpen {
			a.restoreHomeFocusLocked(key)
		}
		return
	}
	a.homeRecents = append([]hostclient.Game(nil), games...)
	a.homeRecentsErr = ""
	if a.roomPickerOpen {
		a.restoreHomeFocusLocked(key)
	}
}

func (a *App) noteRecentsUnreachable(gen int, games []hostclient.Game) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if gen != a.homeRecentsGen {
		return
	}
	key := ""
	if a.roomPickerOpen {
		key = a.homeFocusKeyLocked()
	}
	if len(games) > 0 {
		a.homeRecents = append([]hostclient.Game(nil), games...)
		a.homeRecentsErr = ""
	} else {
		a.homeRecents = nil
		a.homeRecentsErr = "couldn't load recently played"
	}
	a.homeRecentsLoaded = true
	if a.roomPickerOpen {
		a.restoreHomeFocusLocked(key)
	}
}

func homeRowLabel(row RoomPickerRow, currentRoom string) string {
	label := row.Label
	switch row.Kind {
	case HomeKindPinned:
		label = "Pinned  ·  " + label
	case HomeKindRecent:
		label = "Recent  ·  " + label
	}
	if row.Invalid {
		label += "  (unavailable)"
	}
	if currentRoom != "" && row.isRoom() && row.ID == currentRoom {
		label += "  *"
	}
	return label
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
		a.activateHomeRowLocked(rows[a.roomPickerIndex])
	case CmdDetails, CmdSearch:
		row := rows[a.roomPickerIndex]
		if !row.isRoom() {
			a.status = "pin a room"
			return
		}
		if row.Invalid {
			a.status = row.Detail
			return
		}
		a.togglePinnedRoomLocked(row.ID)
	case CmdBack, CmdHome:
		a.roomPickerOpen = false
	}
}

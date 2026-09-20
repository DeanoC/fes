package tenfoot

import (
	"context"
	"strings"

	"github.com/DeanoC/FogCast/hostclient"
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

const homeRecentLimit = 5

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
		if id == "" {
			continue
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
	for _, game := range a.homeRecents {
		id := strings.TrimSpace(game.ID)
		if id == "" {
			continue
		}
		label := strings.TrimSpace(game.Title)
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
	case len(a.homeRecents) == 0:
		recents.Detail = "no recently played games"
	}
	rows = append(rows, recents)
	if a.roomsIndex != nil {
		for _, p := range a.roomsIndex.Packs {
			row := a.packToHomeRow(p)
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
	return rows
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
	a.openRoomPickerLocked()
	a.status = "home"
}

func (a *App) activateHomeRowLocked(row RoomPickerRow) {
	if row.Invalid {
		a.status = row.Detail
		return
	}
	switch row.Kind {
	case HomeKindLibrary:
		a.roomPickerOpen = false
		a.closeAllRoomsLocked()
		a.stampNavLocked("library")
	case HomeKindRecents:
		a.openRecentsFromHomeLocked()
	case HomeKindRecent:
		a.launchRecentFromHomeLocked(row.ID)
	case HomeKindPinned, HomeKindRoom:
		a.roomPickerOpen = false
		a.closeAllRoomsLocked()
		a.openRoomLocked(row.ID)
	default:
		a.status = "unknown home row"
	}
}

func (a *App) openRecentsFromHomeLocked() {
	a.closeAllRoomsLocked()
	a.roomPickerOpen = false
	a.collectionID = "recents"
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

func (a *App) homeRecentByIDLocked(id string) (hostclient.Game, bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		return hostclient.Game{}, false
	}
	for _, game := range a.homeRecents {
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
	if a == nil || a.client == nil {
		a.mu.Lock()
		if gen == a.homeRecentsGen {
			a.homeRecents = nil
			a.homeRecentsErr = ""
			a.homeRecentsLoaded = true
		}
		a.mu.Unlock()
		return
	}
	games, _, err := a.client.ListGames(ctx, hostclient.GameListQuery{
		Collection: "recents",
		Limit:      homeRecentLimit,
	})
	if err == nil && len(games) > homeRecentLimit {
		games = games[:homeRecentLimit]
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
	if err != nil {
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

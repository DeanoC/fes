package tenfoot

import (
	"context"
	"strings"
	"unicode/utf8"
)

type nameEntryKind int

const (
	nameEntryNone nameEntryKind = iota
	nameEntryCreate
	nameEntryRename
)

// CollectionMenuSnapshot is the manage/confirm overlay on the view picker.
type CollectionMenuSnapshot struct {
	Open    bool
	Title   string
	Index   int
	Rows    []string
	Hint    string
	Confirm bool
}

func (a *App) pickerRowsLocked() []LibraryView {
	views := a.viewChoicesLocked()
	var focused Game
	hasFocus := a.grid.Focus >= 0 && a.grid.Focus < len(a.games)
	if hasFocus {
		focused = a.games[a.grid.Focus]
	}
	out := make([]LibraryView, 0, len(views)+1)
	for _, view := range views {
		view.Custom = isCustomCollectionID(view.ID)
		if view.Custom && hasFocus {
			view.Member = gameHasCollection(focused, view.ID)
		}
		out = append(out, view)
	}
	if a.collectionsLoaded {
		out = append(out, LibraryView{Label: "New collection...", Create: true})
	}
	return out
}

func (a *App) pickerRowLocked() (LibraryView, bool) {
	rows := a.pickerRowsLocked()
	if len(rows) == 0 {
		return LibraryView{}, false
	}
	if a.viewPickerIndex < 0 {
		a.viewPickerIndex = 0
	}
	if a.viewPickerIndex >= len(rows) {
		a.viewPickerIndex = len(rows) - 1
	}
	return rows[a.viewPickerIndex], true
}

func (a *App) handleViewPickerLocked(cmd Command) {
	if a.collectionConfirmOpen {
		a.handleCollectionConfirmLocked(cmd)
		return
	}
	if a.collectionManageOpen {
		a.handleCollectionManageLocked(cmd)
		return
	}
	rows := a.pickerRowsLocked()
	n := len(rows)
	if n == 0 {
		a.viewPickerOpen = false
		return
	}
	if a.viewPickerIndex < 0 {
		a.viewPickerIndex = 0
	}
	if a.viewPickerIndex >= n {
		a.viewPickerIndex = n - 1
	}
	switch cmd {
	case CmdUp, CmdLeft, CmdViewPrev:
		a.viewPickerIndex = (a.viewPickerIndex - 1 + n) % n
	case CmdDown, CmdRight, CmdViewNext:
		a.viewPickerIndex = (a.viewPickerIndex + 1) % n
	case CmdSelect:
		row := rows[a.viewPickerIndex]
		if row.Create {
			if !a.collectionsLoaded {
				a.status = "collection list not ready"
				return
			}
			a.openNameEntryLocked(nameEntryCreate, "", "")
			return
		}
		a.viewPickerOpen = false
		a.setCollectionLocked(row.ID)
	case CmdSortCycle:
		a.togglePickerMembershipLocked()
	case CmdSearch:
		a.openCollectionManageLocked()
	case CmdBack, CmdViewPicker:
		a.viewPickerOpen = false
	}
}

func (a *App) togglePickerMembershipLocked() {
	row, ok := a.pickerRowLocked()
	if !ok || row.Create {
		return
	}
	if !row.Custom {
		a.status = "hold Y to favorite; custom shelves only"
		return
	}
	a.toggleCollectionMemberLocked(row.ID)
}

func (a *App) toggleCollectionMemberLocked(collectionID string) {
	if a.membershipBusy {
		return
	}
	if !isCustomCollectionID(collectionID) {
		a.status = "that shelf cannot be curated"
		return
	}
	if a.grid.Focus < 0 || a.grid.Focus >= len(a.games) {
		a.status = "no title selected"
		return
	}
	game := a.games[a.grid.Focus]
	want := !gameHasCollection(game, collectionID)
	a.setGameCollectionLocked(game.ID, collectionID, want)
	a.membershipBusy = true
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	go a.doCollectionMember(ctx, collectionID, game.ID, want)
}

func (a *App) setGameCollectionLocked(gameID, collectionID string, member bool) {
	idx := -1
	for i, game := range a.games {
		if game.ID == gameID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return
	}
	games := append([]Game{}, a.games...)
	games[idx] = setGameCollections(games[idx], collectionID, member)
	a.games = games
}

func (a *App) doCollectionMember(ctx context.Context, collectionID, gameID string, want bool) {
	err := a.client.SetCollectionMember(ctx, collectionID, gameID, want)
	a.mu.Lock()
	defer a.mu.Unlock()
	a.membershipBusy = false
	if err != nil {
		a.setGameCollectionLocked(gameID, collectionID, !want)
		a.status = "collection failed: " + err.Error()
		return
	}
	a.setGameCollectionLocked(gameID, collectionID, want)
	if a.collectionID == collectionID {
		a.reloadLocked()
		return
	}
	a.status = a.libraryStatusLocked()
}

func (a *App) openCollectionManageLocked() {
	row, ok := a.pickerRowLocked()
	if !ok || row.Create {
		return
	}
	if !row.Custom {
		a.status = "Favorites and smart rails cannot be renamed"
		return
	}
	a.collectionManageOpen = true
	a.collectionManageIndex = 0
	a.collectionManageID = row.ID
	a.collectionManageName = row.Label
	if a.collectionManageName == "" {
		a.collectionManageName = row.ID
	}
}

func (a *App) closeCollectionManageLocked() {
	a.collectionManageOpen = false
	a.collectionConfirmOpen = false
}

func (a *App) handleCollectionManageLocked(cmd Command) {
	const n = 2
	if a.collectionManageIndex < 0 {
		a.collectionManageIndex = 0
	}
	if a.collectionManageIndex >= n {
		a.collectionManageIndex = n - 1
	}
	switch cmd {
	case CmdUp, CmdLeft, CmdViewPrev:
		a.collectionManageIndex = (a.collectionManageIndex - 1 + n) % n
	case CmdDown, CmdRight, CmdViewNext:
		a.collectionManageIndex = (a.collectionManageIndex + 1) % n
	case CmdSelect:
		if a.collectionManageIndex == 0 {
			a.openNameEntryLocked(nameEntryRename, a.collectionManageID, a.collectionManageName)
			return
		}
		a.collectionConfirmOpen = true
	case CmdBack, CmdViewPicker, CmdSearch:
		a.closeCollectionManageLocked()
	}
}

func (a *App) handleCollectionConfirmLocked(cmd Command) {
	switch cmd {
	case CmdSelect:
		if a.collectionBusy {
			a.status = "collection busy"
			return
		}
		id := a.collectionManageID
		a.closeCollectionManageLocked()
		a.viewPickerOpen = false
		a.deleteCollectionLocked(id)
	case CmdBack, CmdViewPicker, CmdSearch:
		a.collectionConfirmOpen = false
	}
}

func (a *App) existingCollectionIDsLocked() []string {
	ids := make([]string, 0, len(a.collections)+len(smartLibraryViews))
	for _, view := range smartLibraryViews {
		if view.ID != "" {
			ids = append(ids, view.ID)
		}
	}
	for _, collection := range a.collections {
		id := strings.TrimSpace(collection.ID)
		if id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

func (a *App) openNameEntryLocked(kind nameEntryKind, id, name string) {
	a.nameEntry = kind
	a.nameEntryID = id
	a.nameField = TextField{Buffer: name}
	a.nameField.OSK.Reset()
	a.closeCollectionManageLocked()
}

func (a *App) closeNameEntryLocked() {
	a.nameEntry = nameEntryNone
	a.nameEntryID = ""
	a.nameField = TextField{}
}

func (a *App) nameEntryOpenLocked() bool {
	return a.nameEntry != nameEntryNone
}

func (a *App) handleNameEntryLocked(cmd Command) {
	switch cmd {
	case CmdUp:
		a.nameField.Move(0, -1)
	case CmdDown:
		a.nameField.Move(0, 1)
	case CmdLeft:
		a.nameField.Move(-1, 0)
	case CmdRight:
		a.nameField.Move(1, 0)
	case CmdSelect:
		result := a.nameField.Activate()
		if result.Done {
			a.submitNameEntryLocked()
		}
	case CmdBack:
		if strings.TrimSpace(a.nameField.Buffer) != "" {
			a.nameField.Clear()
			return
		}
		a.closeNameEntryLocked()
	case CmdSearch:
		a.closeNameEntryLocked()
	case CmdFilterPrev:
		a.nameField.CyclePage(-1)
	case CmdFilterNext:
		a.nameField.CyclePage(1)
	}
}

func (a *App) submitNameEntryLocked() {
	if a.collectionBusy {
		return
	}
	name := strings.TrimSpace(a.nameField.Buffer)
	if name == "" {
		a.status = "collection name is required"
		return
	}
	if utf8.RuneCountInString(name) > maxCollectionNameLen {
		a.status = "collection name is too long"
		return
	}
	kind := a.nameEntry
	id := a.nameEntryID
	a.closeNameEntryLocked()
	switch kind {
	case nameEntryCreate:
		if !a.collectionsLoaded {
			a.status = "collection list not ready"
			return
		}
		id = uniqueCollectionID(name, a.existingCollectionIDsLocked())
		if IsReservedCollectionID(id) {
			a.status = "collection id is reserved"
			return
		}
		a.viewPickerOpen = false
		a.startUpsertCollectionLocked(id, name, true)
	case nameEntryRename:
		if !isCustomCollectionID(id) {
			a.status = "that shelf cannot be renamed"
			return
		}
		a.viewPickerOpen = false
		a.startUpsertCollectionLocked(id, name, false)
	}
}

func (a *App) startUpsertCollectionLocked(id, name string, create bool) {
	a.collectionBusy = true
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	go a.doUpsertCollection(ctx, id, name, create)
}

func (a *App) doUpsertCollection(ctx context.Context, id, name string, create bool) {
	collection, err := a.client.UpsertCollection(ctx, id, name)
	a.mu.Lock()
	defer a.mu.Unlock()
	a.collectionBusy = false
	if err != nil {
		a.status = "collection failed: " + err.Error()
		return
	}
	a.applyCollectionLocked(collection)
	a.requestCollectionsReloadLocked()
	if create {
		a.setCollectionLocked(collection.ID)
		return
	}
	a.status = a.libraryStatusLocked()
}

func (a *App) deleteCollectionLocked(id string) {
	if a.collectionBusy {
		a.status = "collection busy"
		return
	}
	if !isCustomCollectionID(id) {
		a.status = "that shelf cannot be deleted"
		return
	}
	a.collectionBusy = true
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	go a.doDeleteCollection(ctx, id)
}

func (a *App) doDeleteCollection(ctx context.Context, id string) {
	err := a.client.DeleteCollection(ctx, id)
	a.mu.Lock()
	defer a.mu.Unlock()
	a.collectionBusy = false
	if err != nil {
		a.status = "collection failed: " + err.Error()
		return
	}
	a.dropCollectionLocked(id)
	a.requestCollectionsReloadLocked()
	if a.collectionID == id {
		a.setCollectionLocked("")
		return
	}
	a.status = a.libraryStatusLocked()
}

func (a *App) applyCollectionLocked(collection Collection) {
	id := strings.TrimSpace(collection.ID)
	if id == "" {
		return
	}
	next := append([]Collection{}, a.collections...)
	for i, existing := range next {
		if existing.ID == id {
			next[i] = collection
			a.collections = next
			return
		}
	}
	a.collections = append(next, collection)
}

func (a *App) dropCollectionLocked(id string) {
	next := make([]Collection, 0, len(a.collections))
	for _, collection := range a.collections {
		if collection.ID == id {
			continue
		}
		next = append(next, collection)
	}
	a.collections = next
}

func (a *App) collectionMenuSnapshotLocked() CollectionMenuSnapshot {
	if a.collectionConfirmOpen {
		name := strings.TrimSpace(a.collectionManageName)
		if name == "" {
			name = a.collectionManageID
		}
		return CollectionMenuSnapshot{
			Open:    true,
			Title:   "Delete " + name + "?",
			Hint:    "A delete  B cancel",
			Confirm: true,
		}
	}
	if a.collectionManageOpen {
		name := strings.TrimSpace(a.collectionManageName)
		if name == "" {
			name = a.collectionManageID
		}
		return CollectionMenuSnapshot{
			Open:  true,
			Title: name,
			Hint:  "A select  B back",
			Rows:  []string{"Rename", "Delete"},
			Index: a.collectionManageIndex,
		}
	}
	return CollectionMenuSnapshot{}
}

func (a *App) closeCollectionOverlaysLocked() {
	a.closeNameEntryLocked()
	a.closeCollectionManageLocked()
	a.viewPickerOpen = false
}

// OSKOpen reports search, collection-name, or library-path on-screen keyboard.
func (a *App) OSKOpen() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.searchOpen || a.nameEntryOpenLocked() || a.settingsPathOpen
}

func (a *App) CollectionMenuOpen() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.collectionManageOpen || a.collectionConfirmOpen
}

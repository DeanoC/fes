package tenfoot

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/ui/shared"
)

func (a *App) openSearchLocked() {
	a.nativePadFenceMappingLocked()
	a.closeDetailLocked()
	a.closeFiltersLocked()
	a.searchOpen = true
	a.searchField.OSK.Reset()
}

func (a *App) closeSearchApplyLocked() {
	a.searchOpen = false
	a.applyPendingSearchLocked()
}

func (a *App) handleSearchLocked(cmd Command, now time.Time) {
	switch cmd {
	case CmdUp:
		a.searchField.Move(0, -1)
	case CmdDown:
		a.searchField.Move(0, 1)
	case CmdLeft:
		a.searchField.Move(-1, 0)
	case CmdRight:
		a.searchField.Move(1, 0)
	case CmdSelect:
		result := a.searchField.Activate()
		if result.Changed {
			a.markSearchLocked(now)
		}
		if result.Done {
			a.closeSearchApplyLocked()
		}
	case CmdBack:
		if strings.TrimSpace(a.searchField.Buffer) != "" {
			a.searchField.Clear()
			a.searchPending = false
			a.reloadLocked()
			return
		}
		a.searchOpen = false
	case CmdSearch:
		a.searchOpen = false
	case CmdTab:
		a.closeSearchApplyLocked()
	case CmdTabPrev, CmdFilterPrev:
		a.searchField.CyclePage(-1)
	case CmdFilterNext:
		a.searchField.CyclePage(1)
	case CmdSortCycle:
		a.cycleSortLocked()
	case CmdStop:
		a.startStopLocked()
	}
}

// TypeText appends into the on-screen search field from a physical keyboard.
func (a *App) TypeText(text string, now time.Time) {
	text = shared.SanitizeFieldText(text)
	if text == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.typeTextLocked(text, now)
}

func (a *App) typeTextLocked(text string, now time.Time) {
	a.noteActivityLocked(now)
	if a.consumeAttractLocked(CmdNone, now) {
		return
	}
	if a.settingsOSKOpenLocked() {
		a.settingsOSKField.Insert(text)
		return
	}
	if a.nameEntryOpenLocked() {
		a.nameField.Insert(text)
		return
	}
	if !a.searchOpen {
		return
	}
	a.searchField.Insert(text)
	a.markSearchLocked(now)
}

// SearchBackspace deletes the last search rune.
func (a *App) SearchBackspace(now time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.searchBackspaceLocked(now)
}

func (a *App) searchBackspaceLocked(now time.Time) {
	a.noteActivityLocked(now)
	if a.consumeAttractLocked(CmdBack, now) {
		return
	}
	if a.settingsOSKOpenLocked() {
		if a.settingsOSKField.Buffer == "" {
			return
		}
		a.settingsOSKField.Backspace()
		return
	}
	if a.nameEntryOpenLocked() {
		if a.nameField.Buffer == "" {
			return
		}
		a.nameField.Backspace()
		return
	}
	if !a.searchOpen || a.searchField.Buffer == "" {
		return
	}
	a.searchField.Backspace()
	a.markSearchLocked(now)
}

// ConfirmSearch closes the OSK and applies a pending query (physical Enter).
func (a *App) ConfirmSearch(now time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.confirmSearchLocked(now)
}

func (a *App) confirmSearchLocked(now time.Time) {
	a.noteActivityLocked(now)
	if a.consumeAttractLocked(CmdSelect, now) {
		return
	}
	if a.settingsOSKOpenLocked() {
		a.submitSettingsOSKLocked()
		return
	}
	if a.nameEntryOpenLocked() {
		a.submitNameEntryLocked()
		return
	}
	if !a.searchOpen {
		return
	}
	a.closeSearchApplyLocked()
}

func (a *App) markSearchLocked(now time.Time) {
	a.searchPending = true
	a.searchDue = now.Add(searchDebounce)
}

func (a *App) flushSearchLocked(now time.Time) {
	if !a.searchPending {
		return
	}
	if !now.IsZero() && now.Before(a.searchDue) {
		return
	}
	a.applyPendingSearchLocked()
}

func (a *App) applyPendingSearchLocked() {
	if !a.searchPending {
		return
	}
	a.searchPending = false
	a.reloadLocked()
}

func (a *App) cyclePlatformLocked(delta int) {
	choices := a.platformChoicesLocked()
	if len(choices) < 2 {
		if a.platformErr != "" {
			a.requestPlatformReloadLocked()
		}
		return
	}
	idx := 0
	for i, id := range choices {
		if id == a.platformID {
			idx = i
			break
		}
	}
	idx = (idx + delta) % len(choices)
	if idx < 0 {
		idx += len(choices)
	}
	next := choices[idx]
	if next == a.platformID {
		return
	}
	a.platformID = next
	a.libraryCoreIDs = nil
	a.stampNavLocked("platform")
	a.reloadLocked()
}

func (a *App) cycleSortLocked() {
	sorts := a.sortChoicesLocked()
	idx := 0
	for i, sort := range sorts {
		if sort == a.sort {
			idx = i
			break
		}
	}
	a.sort = sorts[(idx+1)%len(sorts)]
	a.reloadLocked()
}

func (a *App) sortChoicesLocked() []string {
	switch a.collectionID {
	case "recents":
		return recentsSorts
	case "recently_added":
		return recentlyAddedSorts
	default:
		return catalogSorts
	}
}

func (a *App) cycleViewLocked(delta int) {
	views := a.viewChoicesLocked()
	if len(views) == 0 {
		return
	}
	idx := a.currentViewIndexLocked()
	idx = (idx + delta) % len(views)
	if idx < 0 {
		idx += len(views)
	}
	a.setCollectionLocked(views[idx].ID)
}

func (a *App) setCollectionLocked(id string) {
	if id == a.collectionID {
		return
	}
	a.collectionID = id
	a.normalizeSortForCollectionLocked()
	a.stampNavLocked("collection")
	a.reloadLocked()
}

func (a *App) normalizeSortForCollectionLocked() {
	switch a.collectionID {
	case "recents":
		if a.sort == "platform" || a.sort == "system" {
			a.sort = "platform"
			return
		}
		a.sort = ""
	case "recently_added":
		if a.sort == "platform" || a.sort == "system" {
			a.sort = "platform"
			return
		}
		a.sort = "recently_added"
	default:
		if a.sort == "" {
			a.sort = "title"
		}
	}
}

func (a *App) openViewPickerLocked() {
	a.closeDetailLocked()
	a.closeFiltersLocked()
	a.searchOpen = false
	a.closeNameEntryLocked()
	a.closeCollectionManageLocked()
	a.viewPickerOpen = true
	a.viewPickerIndex = a.currentViewIndexLocked()
	if a.collectionsErr != "" {
		a.requestCollectionsReloadLocked()
	}
}

func (a *App) viewChoicesLocked() []LibraryView {
	views := make([]LibraryView, 0, len(smartLibraryViews)+len(a.collections))
	views = append(views, smartLibraryViews...)
	seen := map[string]struct{}{}
	for _, smart := range smartLibraryViews {
		seen[smart.ID] = struct{}{}
	}
	for _, collection := range a.collections {
		id := strings.TrimSpace(collection.ID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		label := strings.TrimSpace(collection.Name)
		if label == "" {
			label = id
		}
		views = append(views, LibraryView{ID: id, Label: label})
	}
	return views
}

func (a *App) currentViewIndexLocked() int {
	views := a.viewChoicesLocked()
	for i, view := range views {
		if view.ID == a.collectionID {
			return i
		}
	}
	return 0
}

func (a *App) viewLabelLocked() string {
	views := a.viewChoicesLocked()
	for _, view := range views {
		if view.ID == a.collectionID {
			if view.Label != "" {
				return view.Label
			}
			return view.ID
		}
	}
	if a.collectionID == "" {
		return "All"
	}
	return a.collectionID
}

func (a *App) toggleFavoriteLocked() {
	if a.favoriteBusy {
		return
	}
	game, ok := a.favoriteTargetLocked()
	if !ok {
		a.status = "no title selected"
		return
	}
	want := !game.Favorite
	a.setGameFavoriteLocked(game.ID, want)
	a.favoriteBusy = true
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	go a.doFavorite(ctx, game.ID, want)
}

// favoriteTargetLocked is the title Favorite changes. Open Details for a
// title that is not the focused row, such as a Home recent, owns the action.
func (a *App) favoriteTargetLocked() (hostclient.Game, bool) {
	if a.room == nil && strings.TrimSpace(a.libraryDetail.ID) != "" {
		return a.libraryDetail, true
	}
	if a.grid.Focus < 0 || a.grid.Focus >= len(a.games) {
		return hostclient.Game{}, false
	}
	return a.games[a.grid.Focus], true
}

func (a *App) setGameFavoriteLocked(gameID string, favorite bool) {
	if a.libraryDetail.ID == gameID {
		a.libraryDetail.Favorite = favorite
	}
	for i := range a.homeRecents {
		if a.homeRecents[i].ID == gameID {
			a.homeRecents[i].Favorite = favorite
		}
	}
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
	games := append([]hostclient.Game{}, a.games...)
	games[idx].Favorite = favorite
	a.games = games
}

func (a *App) doFavorite(ctx context.Context, gameID string, want bool) {
	err := a.client.SetFavorite(ctx, gameID, want)
	a.mu.Lock()
	defer a.mu.Unlock()
	a.favoriteBusy = false
	if err != nil {
		a.setGameFavoriteLocked(gameID, !want)
		a.status = "favorite failed: " + err.Error()
		return
	}
	a.setGameFavoriteLocked(gameID, want)
	if a.collectionID == "favorites" {
		a.reloadLocked()
		return
	}
	a.setLibraryStatusLocked(a.libraryStatusLocked())
}

func (a *App) platformChoicesLocked() []string {
	ids := make([]string, 0, len(a.platforms)+1)
	ids = append(ids, "")
	seen := map[string]struct{}{"": {}}
	for _, platform := range a.platforms {
		id := strings.TrimSpace(platform.ID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids
}

func (a *App) reloadLocked() {
	a.closeDetailLocked()
	a.reloadCatalogLocked()
}

// reloadCatalogLocked preserves an unrelated open Details pane during an
// asynchronous catalog mutation.
func (a *App) reloadCatalogLocked() {
	a.reloadCatalogWithNoticeLocked("")
}

// reloadCatalogWithNoticeLocked retains the outcome of a user action after a
// successful refresh, unless the user has navigated since the action finished.
func (a *App) reloadCatalogWithNoticeLocked(notice string) {
	a.captureCatalogFocusLocked()
	a.searchPending = false
	a.loadGen++
	gen := a.loadGen
	a.loading = true
	a.loadErr = ""
	a.setLibraryStatusLocked("loading library")
	ctx := a.replaceLoadContextLocked()
	lastInput := a.lastInput
	if notice != "" {
		a.setLibraryNoticeLocked(notice)
	}
	go func() {
		a.loadLibrary(ctx, gen)
		if notice == "" {
			return
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		if ctx.Err() != nil || a.loadGen != gen || a.loading || !a.lastInput.Equal(lastInput) {
			return
		}
		a.setLibraryRefreshNoticeLocked(notice)
	}()
}

// setLibraryRefreshNoticeLocked keeps a failed refresh visible alongside the
// outcome that requested it, including while setup defers library status.
func (a *App) setLibraryRefreshNoticeLocked(notice string) {
	if a.loadErr != "" || a.libraryStatus == savedListOfflineCopy || a.libraryStatus == "Can't reach the library right now." {
		a.setLibraryNoticeLocked(notice + " Couldn't refresh the library.")
		return
	}
	a.setLibraryNoticeLocked(notice)
}

func (a *App) replaceLoadContextLocked() context.Context {
	parent := a.ctx
	if parent == nil {
		parent = context.Background()
	}
	if a.loadCancel != nil {
		a.loadCancel()
	}
	a.cancelScreenshotContextLocked()
	ctx, cancel := context.WithCancel(parent)
	a.loadCancel = cancel
	a.jobCtx = ctx
	a.inflight = map[string]workKind{}
	for {
		select {
		case <-a.jobs:
		default:
			return ctx
		}
	}
}

func (a *App) requestPlatformReloadLocked() {
	if a.platformKick == nil {
		return
	}
	select {
	case a.platformKick <- struct{}{}:
	default:
	}
}

func (a *App) loadPlatforms(ctx context.Context) {
	fails := 0
	for {
		if err := ctx.Err(); err != nil {
			return
		}
		platforms, err := a.client.Platforms(ctx)
		if ctx.Err() != nil {
			return
		}
		a.mu.Lock()
		if err == nil {
			a.platforms = platforms
			a.platformErr = ""
			a.mu.Unlock()
			return
		}
		fails++
		a.platformErr = err.Error()
		delay := presentationRetryDelay(fails)
		kick := a.platformKick
		a.mu.Unlock()
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		case <-kick:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		}
	}
}

func (a *App) requestCollectionsReloadLocked() {
	if a.collectionsKick == nil {
		return
	}
	select {
	case a.collectionsKick <- struct{}{}:
	default:
	}
}

func (a *App) loadCollections(ctx context.Context) {
	fails := 0
	for {
		if err := ctx.Err(); err != nil {
			return
		}
		collections, err := a.client.Collections(ctx)
		if ctx.Err() != nil {
			return
		}
		a.mu.Lock()
		if err == nil {
			a.collections = collections
			a.collectionsErr = ""
			a.collectionsLoaded = true
			fails = 0
			if a.viewPickerOpen {
				n := len(a.pickerRowsLocked())
				if a.viewPickerIndex >= n {
					a.viewPickerIndex = a.currentViewIndexLocked()
				}
			}
			kick := a.collectionsKick
			a.mu.Unlock()
			select {
			case <-ctx.Done():
				return
			case <-kick:
			}
			continue
		}
		fails++
		a.collectionsErr = err.Error()
		delay := presentationRetryDelay(fails)
		kick := a.collectionsKick
		a.mu.Unlock()
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		case <-kick:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		}
	}
}

func (a *App) currentQueryLocked() hostclient.GameListQuery {
	return hostclient.GameListQuery{
		Limit:          a.pageLimit,
		Platform:       a.platformID,
		CoreIDs:        append([]string(nil), a.libraryCoreIDs...),
		Sort:           catalogQuerySort(a.collectionID, a.sort),
		Q:              strings.TrimSpace(a.searchField.Buffer),
		Collection:     a.collectionID,
		Genre:          a.filterGenre,
		Year:           a.filterYear,
		Region:         a.filterRegion,
		HidePrerelease: a.hidePrerelease,
		HideHacks:      a.hideHacks,
	}
}

func catalogQuerySort(collection, sort string) string {
	switch collection {
	case "recents":
		switch sort {
		case "title":
			return "title"
		case "platform", "system":
			return "platform"
		default:
			return ""
		}
	case "recently_added":
		if sort == "platform" || sort == "system" {
			return "platform"
		}
		return "recently_added"
	default:
		return sort
	}
}

func (a *App) libraryStatusLocked() string {
	n := len(a.games)
	view := a.viewLabelLocked()
	platform := "All"
	if a.platformID != "" {
		platform = a.platformLabelLocked(a.platformID)
	}
	sort := sortLabel(a.collectionID, a.sort)
	filters := a.filterSummaryLocked()
	if q := strings.TrimSpace(a.searchField.Buffer); q != "" {
		if filters != "" {
			return fmt.Sprintf("%d titles · %s · %s · %s · %s · %q", n, view, platform, sort, filters, q)
		}
		return fmt.Sprintf("%d titles · %s · %s · %s · %q", n, view, platform, sort, q)
	}
	if filters != "" {
		return fmt.Sprintf("%d titles · %s · %s · %s · %s", n, view, platform, sort, filters)
	}
	return fmt.Sprintf("%d titles · %s · %s · %s", n, view, platform, sort)
}

func sortLabel(collection, sort string) string {
	if collection == "recents" && (sort == "" || sort == "recently_added") {
		return "Last played"
	}
	switch sort {
	case "recently_added":
		return "Recently added"
	case "platform", "system":
		return "System"
	default:
		return "Title"
	}
}

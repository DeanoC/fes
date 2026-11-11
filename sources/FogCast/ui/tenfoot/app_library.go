package tenfoot

import (
	"context"
	"errors"
	"net"
	"strings"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
)

var errLibrarySuperseded = errors.New("library load superseded")

// restoreCatalogFocus keeps the pre-reload game when it is still in the catalog.
// If that id is gone, the original index is clamped to the new length instead of
// resetting to 0.
func restoreCatalogFocus(games []hostclient.Game, keepID string, keepIndex int) int {
	if keepID != "" {
		for i, game := range games {
			if game.ID == keepID {
				return i
			}
		}
	}
	if len(games) == 0 {
		return 0
	}
	if keepIndex >= len(games) {
		return len(games) - 1
	}
	if keepIndex < 0 {
		return 0
	}
	return keepIndex
}

func (a *App) applyCatalogPageLocked(games []hostclient.Game, keepID string, keepIndex int, pinned *bool, lastFocus *int, navID *string) {
	if navID == nil {
		empty := ""
		navID = &empty
	}
	// A move on the shelf wins over the id captured at reload. Remember that
	// title before SetCount clamps focus onto a shorter partial page.
	if *pinned && *lastFocus >= 0 && (a.grid.Focus != *lastFocus || a.navDirty) {
		*pinned = false
		if a.grid.Focus >= 0 && a.grid.Focus < len(a.games) {
			*navID = a.games[a.grid.Focus].ID
		}
	} else if !*pinned && *lastFocus >= 0 && a.grid.Focus != *lastFocus && a.grid.Focus >= 0 && a.grid.Focus < len(a.games) {
		*navID = a.games[a.grid.Focus].ID
	}
	a.games = games
	a.grid.SetCount(len(a.games))
	if !*pinned {
		if id := *navID; id != "" {
			for i, game := range a.games {
				if game.ID == id {
					a.grid.Focus = i
					a.grid.ensureVisible()
					*lastFocus = a.grid.Focus
					return
				}
			}
		}
		*lastFocus = a.grid.Focus
		return
	}
	a.grid.Focus = restoreCatalogFocus(a.games, keepID, keepIndex)
	a.grid.ensureVisible()
	*lastFocus = a.grid.Focus
}

// captureCatalogFocusLocked records the current title so a superseded reload
// can restore it. An already-cleared catalog or a still-loading partial page
// must not overwrite that capture unless the user moved focus.
func (a *App) captureCatalogFocusLocked() {
	if len(a.games) == 0 {
		return
	}
	if a.loading && !a.navDirty {
		return
	}
	a.keepFocusIndex = a.grid.Focus
	a.keepFocusID = ""
	if a.keepFocusIndex >= 0 && a.keepFocusIndex < len(a.games) {
		a.keepFocusID = a.games[a.keepFocusIndex].ID
	}
	a.navDirty = false
}

func (a *App) loadLibrary(ctx context.Context, gen int) {
	a.loadLibraryResume(ctx, gen, false)
}

// loadLibraryResume loads one generation. resumeHeld continues a retry whose
// cached shelf is already on screen: the shelf stays up, and partial pages
// are not published until that attempt completes.
func (a *App) loadLibraryResume(ctx context.Context, gen int, resumeHeld bool) {
	a.mu.Lock()
	if gen != a.loadGen {
		a.mu.Unlock()
		return
	}
	if resumeHeld {
		a.libraryRetrying = true
	}
	a.captureCatalogFocusLocked()
	keepID := a.keepFocusID
	keepIndex := a.keepFocusIndex
	query := a.currentQueryLocked() // fixed for this generation; TypeText does not bump loadGen until debounce
	previous := append([]hostclient.Game(nil), a.games...)
	if !resumeHeld {
		a.games = nil
		a.grid.SetCount(0)
	}
	pinned := true
	lastFocus := -1
	navID := ""
	a.mu.Unlock()
	defer a.endLibraryLoad(gen)

	fails := 0
	held := resumeHeld
	for {
		if ctx.Err() != nil {
			return
		}
		all, notice, err := a.collectLibraryPages(ctx, gen, query, keepID, keepIndex, &pinned, &lastFocus, &navID, !held)
		if err == nil && len(all) == 0 && shelfQueryUnfiltered(query) && strings.TrimSpace(query.Availability) == "" {
			fallback := query
			fallback.Availability = "all"
			fallback.Cursor = ""
			saved, savedNotice, savedErr := a.collectLibraryPages(ctx, gen, fallback, keepID, keepIndex, &pinned, &lastFocus, &navID, !held)
			if savedNotice != "" {
				notice = savedNotice
			}
			all = saved
			err = savedErr
		}
		if err == nil && held {
			a.publishLibraryAttempt(gen, all, keepID, keepIndex, &pinned, &lastFocus, &navID)
		}
		if err == nil || !libraryNetRetry(err) {
			a.finishLibraryLoad(ctx, gen, query, previous, all, keepID, keepIndex, notice, err)
			return
		}
		fails++
		if a.holdLibraryForRetry(gen, query, previous, keepID, keepIndex, &pinned, &lastFocus, &navID) {
			held = true
		}
		if !a.libraryRetryWait(ctx, fails, gen, false) {
			return
		}
	}
}

func (a *App) endLibraryLoad(gen int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if gen != a.loadGen {
		return
	}
	a.libraryRetrying = false
}

// libraryNetRetry is a connect, a short read, or a truncated body. A host
// API status, an oversize body, a JSON decode error, and a superseded or
// cancelled load are not retried.
func libraryNetRetry(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, errLibrarySuperseded) {
		return false
	}
	// ReadResponseBody turns a mid-body disconnect into ErrResponseTruncated
	// without wrapping the original net error, so errors.As would miss it.
	if errors.Is(err, hostclient.ErrResponseTruncated) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr)
}

// holdLibraryForRetry puts the last same-filter shelf back while this
// generation keeps retrying. loading is only the launch gate, so Select can
// start a title that is already on screen. libraryRetrying stays set so GPU
// park restarts this generation. Another filter's rows stay off this shelf,
// matching finishLibraryLoad, and a later success replaces them there.
// The bool reports that the cached shelf is on screen; later attempts then
// skip partial pages until one completes.
func (a *App) holdLibraryForRetry(gen int, query hostclient.GameListQuery, previous []hostclient.Game, keepID string, keepIndex int, pinned *bool, lastFocus *int, navID *string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if gen != a.loadGen {
		return false
	}
	a.libraryRetrying = true
	saved, ok := sameFilterSavedShelf(a.shelfQuerySet, a.shelfQuery, a.shelfGames, previous, query)
	if !ok {
		a.loadErr = "Can't reach the library right now."
		a.setLibraryStatusLocked(a.loadErr)
		return false
	}
	shown := append([]hostclient.Game(nil), saved...)
	a.applyCatalogPageLocked(shown, keepID, keepIndex, pinned, lastFocus, navID)
	a.loading = false
	a.loadErr = ""
	a.setLibraryStatusLocked(savedListOfflineCopy)
	return true
}

// sameFilterSavedShelf is the last shelf for query. Rows saved for another
// filter are refused. previous is the shelf this generation cleared, used
// when a successful shelf has not been stored yet.
func sameFilterSavedShelf(shelfSet bool, shelfQuery hostclient.GameListQuery, shelfGames, previous []hostclient.Game, query hostclient.GameListQuery) ([]hostclient.Game, bool) {
	if shelfSet && !shelfFilterEqual(shelfQuery, query) {
		return nil, false
	}
	saved := previous
	if shelfSet && len(shelfGames) > 0 {
		saved = shelfGames
	}
	if len(saved) == 0 {
		return nil, false
	}
	return saved, true
}

func (a *App) libraryRetryWait(ctx context.Context, fails, gen int, recents bool) bool {
	timer := time.NewTimer(presentationRetryDelay(fails))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if ctx.Err() != nil {
		return false
	}
	if recents {
		return gen == a.homeRecentsGen
	}
	return gen == a.loadGen
}

func (a *App) collectLibraryPages(ctx context.Context, gen int, query hostclient.GameListQuery, keepID string, keepIndex int, pinned *bool, lastFocus *int, navID *string, publish bool) ([]hostclient.Game, string, error) {
	var (
		all    []hostclient.Game
		notice string
	)
	for {
		if err := ctx.Err(); err != nil {
			return all, notice, err
		}
		a.mu.Lock()
		if gen != a.loadGen {
			a.mu.Unlock()
			return all, notice, errLibrarySuperseded
		}
		a.mu.Unlock()
		page, err := a.client.ListGamePage(ctx, query)
		if err != nil {
			return all, notice, err
		}
		if notice == "" {
			notice = page.Notice
		}
		remain := a.maxGames - len(all)
		if remain <= 0 {
			break
		}
		chunk := page.Games
		if len(chunk) > remain {
			chunk = chunk[:remain]
		}
		all = append(all, chunk...)
		a.mu.Lock()
		if gen != a.loadGen {
			a.mu.Unlock()
			return all, notice, errLibrarySuperseded
		}
		// A held retry keeps the cached shelf up. Publishing this partial
		// page would clamp focus, and the next failure could not put it back
		// once navigation had unpinned the reload id.
		if publish {
			a.applyCatalogPageLocked(append([]hostclient.Game(nil), all...), keepID, keepIndex, pinned, lastFocus, navID)
			a.setLibraryStatusLocked(a.libraryStatusLocked())
		}
		a.mu.Unlock()
		if page.NextCursor == "" || len(all) >= a.maxGames || len(chunk) == 0 {
			break
		}
		query.Cursor = page.NextCursor
	}
	return all, notice, nil
}

func (a *App) publishLibraryAttempt(gen int, games []hostclient.Game, keepID string, keepIndex int, pinned *bool, lastFocus *int, navID *string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if gen != a.loadGen {
		return
	}
	a.applyCatalogPageLocked(append([]hostclient.Game(nil), games...), keepID, keepIndex, pinned, lastFocus, navID)
	a.setLibraryStatusLocked(a.libraryStatusLocked())
}

func (a *App) finishLibraryLoad(ctx context.Context, gen int, query hostclient.GameListQuery, previous, games []hostclient.Game, keepID string, keepIndex int, notice string, err error) {
	if ctx.Err() != nil || errors.Is(err, errLibrarySuperseded) {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if gen != a.loadGen {
		return
	}
	a.loading = false
	a.libraryRetrying = false
	if err != nil {
		// A failed refresh may keep the last successful shelf only when that
		// shelf was produced by this same filter. Restoring SNES rows onto
		// an SMS shelf is the bug.
		if a.shelfQuerySet && !shelfFilterEqual(a.shelfQuery, query) {
			a.loadErr = ""
			if len(a.games) == 0 {
				a.grid.SetCount(0)
				a.setLibraryStatusLocked("Can't reach the library right now.")
			} else {
				a.setLibraryStatusLocked(a.libraryShelfStatusLocked(query, a.games, notice))
			}
			return
		}
		saved, ok := sameFilterSavedShelf(a.shelfQuerySet, a.shelfQuery, a.shelfGames, previous, query)
		if ok {
			pinned := true
			lastFocus := -1
			navID := ""
			a.applyCatalogPageLocked(saved, keepID, keepIndex, &pinned, &lastFocus, &navID)
			a.loadErr = ""
			a.setLibraryStatusLocked(savedListOfflineCopy)
			return
		}
		a.loadErr = "Can't reach the library right now."
		a.setLibraryStatusLocked(a.loadErr)
		return
	}
	a.shelfQuery = query
	a.shelfQuerySet = true
	a.shelfGames = append([]hostclient.Game(nil), games...)
	a.loadErr = ""
	a.setLibraryStatusLocked(a.libraryShelfStatusLocked(query, games, notice))
}

func (a *App) libraryShelfStatusLocked(query hostclient.GameListQuery, games []hostclient.Game, notice string) string {
	if !shelfQueryUnfiltered(query) {
		return a.libraryStatusLocked()
	}
	if len(games) == 0 {
		switch notice {
		case savedListOfflineCopy, shelfEmptyCopy, shelfMissingCopy:
			return notice
		}
		if notice != "" {
			return notice
		}
		return shelfEmptyCopy
	}
	if librarySavedOffline(games) {
		return savedListOfflineCopy
	}
	return a.libraryStatusLocked()
}

package fogcast

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/internal/discovery"
	"github.com/DeanoC/FogCast/internal/metadata"
	"github.com/DeanoC/FogCast/librarymedia"
	"github.com/DeanoC/FogCast/libraryuser"
	"github.com/DeanoC/FogCast/protocol"
)

type AttractItem struct {
	GameID     string          `json:"game_id"`
	Title      string          `json:"title"`
	Platform   protocol.System `json:"platform"`
	Video      string          `json:"video,omitempty"`
	Cover      string          `json:"cover,omitempty"`
	Backdrop   string          `json:"backdrop,omitempty"`
	Marquee    string          `json:"marquee,omitempty"`
	Launchable bool            `json:"launchable"`
}

func (s *Service) SetFavorite(ctx context.Context, gameID string, favorite bool) error {
	if s.users == nil {
		return canonicalError(protocol.CodeInternal, nil)
	}
	if _, err := s.Game(ctx, gameID); err != nil {
		return err
	}
	if err := s.users.SetFavorite(ctx, gameID, favorite); err != nil {
		return canonicalError(protocol.CodeInternal, safeContextError(err))
	}
	return nil
}

func (s *Service) LibraryState(ctx context.Context, gameID string) (libraryuser.State, error) {
	if s.users == nil {
		return libraryuser.State{GameID: gameID}, nil
	}
	state, err := s.users.State(ctx, gameID)
	if err != nil {
		return libraryuser.State{}, canonicalError(protocol.CodeInternal, safeContextError(err))
	}
	return state, nil
}

func (s *Service) LibraryStates(ctx context.Context, ids []string) (map[string]libraryuser.State, error) {
	if s.users == nil {
		return map[string]libraryuser.State{}, nil
	}
	states, err := s.users.States(ctx, ids)
	if err != nil {
		return nil, canonicalError(protocol.CodeInternal, safeContextError(err))
	}
	return states, nil
}

func (s *Service) RecordPlay(ctx context.Context, gameID string) error {
	if s.users == nil {
		return nil
	}
	if err := protocol.ValidateGameID(gameID); err != nil {
		return canonicalError(protocol.CodeBadRequest, nil)
	}
	if err := s.users.RecordPlay(ctx, gameID); err != nil {
		return canonicalError(protocol.CodeInternal, safeContextError(err))
	}
	return nil
}

func (s *Service) Collections(ctx context.Context) ([]libraryuser.Collection, error) {
	if s.users == nil {
		return []libraryuser.Collection{}, nil
	}
	collections, err := s.users.Collections(ctx)
	if err != nil {
		return nil, canonicalError(protocol.CodeInternal, safeContextError(err))
	}
	return collections, nil
}

func (s *Service) UpsertCollection(ctx context.Context, id, name string) (libraryuser.Collection, error) {
	if s.users == nil {
		return libraryuser.Collection{}, canonicalError(protocol.CodeInternal, nil)
	}
	collection, err := s.users.UpsertCollection(ctx, id, name)
	if err != nil {
		if errors.Is(err, libraryuser.ErrReservedID) || errors.Is(err, libraryuser.ErrInvalid) {
			return libraryuser.Collection{}, canonicalError(protocol.CodeBadRequest, nil)
		}
		return libraryuser.Collection{}, canonicalError(protocol.CodeInternal, safeContextError(err))
	}
	return collection, nil
}

func (s *Service) DeleteCollection(ctx context.Context, id string) error {
	if s.users == nil {
		return canonicalError(protocol.CodeInternal, nil)
	}
	if err := s.users.DeleteCollection(ctx, id); err != nil {
		if errors.Is(err, libraryuser.ErrNotFound) {
			return libraryuser.ErrNotFound
		}
		if errors.Is(err, libraryuser.ErrReservedID) || errors.Is(err, libraryuser.ErrInvalid) {
			return canonicalError(protocol.CodeBadRequest, nil)
		}
		return canonicalError(protocol.CodeInternal, safeContextError(err))
	}
	return nil
}

func (s *Service) SetCollectionMember(ctx context.Context, collectionID, gameID string, member bool) error {
	if s.users == nil {
		return canonicalError(protocol.CodeInternal, nil)
	}
	if _, err := s.users.Collection(ctx, collectionID); err != nil {
		if errors.Is(err, libraryuser.ErrNotFound) {
			return libraryuser.ErrNotFound
		}
		if errors.Is(err, libraryuser.ErrReservedID) || errors.Is(err, libraryuser.ErrInvalid) {
			return canonicalError(protocol.CodeBadRequest, nil)
		}
		return canonicalError(protocol.CodeInternal, safeContextError(err))
	}
	if _, err := s.Game(ctx, gameID); err != nil {
		return err
	}
	if err := s.users.SetCollectionMember(ctx, collectionID, gameID, member); err != nil {
		if errors.Is(err, libraryuser.ErrNotFound) {
			return libraryuser.ErrNotFound
		}
		if errors.Is(err, libraryuser.ErrReservedID) || errors.Is(err, libraryuser.ErrInvalid) {
			return canonicalError(protocol.CodeBadRequest, nil)
		}
		return canonicalError(protocol.CodeInternal, safeContextError(err))
	}
	return nil
}

func (s *Service) GameCollectionIDs(ctx context.Context, gameID string) ([]string, error) {
	if s.users == nil {
		return []string{}, nil
	}
	ids, err := s.users.GameCollectionIDs(ctx, gameID)
	if err != nil {
		if errors.Is(err, libraryuser.ErrInvalid) {
			return nil, canonicalError(protocol.CodeBadRequest, nil)
		}
		return nil, canonicalError(protocol.CodeInternal, safeContextError(err))
	}
	return ids, nil
}

func (s *Service) CollectionIDsByGame(ctx context.Context, ids []string) (map[string][]string, error) {
	if s.users == nil {
		return map[string][]string{}, nil
	}
	result, err := s.users.CollectionIDsByGame(ctx, ids)
	if err != nil {
		return nil, canonicalError(protocol.CodeInternal, safeContextError(err))
	}
	return result, nil
}

func (s *Service) favoriteIDs(ctx context.Context) ([]string, error) {
	if s.users == nil {
		return []string{}, nil
	}
	ids, err := s.users.FavoriteIDs(ctx)
	if err != nil {
		return nil, canonicalError(protocol.CodeInternal, safeContextError(err))
	}
	return ids, nil
}

func (s *Service) playedIDs(ctx context.Context) ([]string, error) {
	if s.users == nil {
		return []string{}, nil
	}
	ids, err := s.users.PlayedIDs(ctx)
	if err != nil {
		return nil, canonicalError(protocol.CodeInternal, safeContextError(err))
	}
	return ids, nil
}

func (s *Service) queryCustomCollection(ctx context.Context, query catalog.Query) (catalog.Page, error) {
	if s.users == nil {
		return catalog.Page{}, canonicalError(protocol.CodeBadRequest, nil)
	}
	if err := libraryuser.ValidateCollectionID(query.Collection); err != nil {
		return catalog.Page{}, canonicalError(protocol.CodeBadRequest, nil)
	}
	if _, err := s.users.Collection(ctx, query.Collection); err != nil {
		if errors.Is(err, libraryuser.ErrNotFound) || errors.Is(err, libraryuser.ErrInvalid) || errors.Is(err, libraryuser.ErrReservedID) {
			return catalog.Page{}, canonicalError(protocol.CodeBadRequest, nil)
		}
		return catalog.Page{}, canonicalError(protocol.CodeInternal, safeContextError(err))
	}
	ids, err := s.users.CollectionGameIDs(ctx, query.Collection)
	if err != nil {
		return catalog.Page{}, canonicalError(protocol.CodeInternal, safeContextError(err))
	}
	query.Restrict = true
	query.RestrictIDs = ids
	query.Collection = ""
	page, err := s.catalog.QueryGames(ctx, query)
	if err != nil {
		if ctx.Err() != nil {
			return catalog.Page{}, ctx.Err()
		}
		if errors.Is(err, catalog.ErrInvalidQuery) {
			return catalog.Page{}, canonicalError(protocol.CodeBadRequest, nil)
		}
		return catalog.Page{}, canonicalError(protocol.CodeInternal, safeContextError(err))
	}
	return page, nil
}

func (s *Service) queryContinue(ctx context.Context, query catalog.Query) (catalog.Page, error) {
	if s.users == nil {
		return catalog.Page{}, nil
	}
	normalized, err := catalog.NormalizeQuery(query)
	if err != nil {
		return catalog.Page{}, canonicalError(protocol.CodeBadRequest, nil)
	}
	query = normalized
	ids, err := s.users.RecentIDs(ctx, 0)
	if err != nil {
		return catalog.Page{}, canonicalError(protocol.CodeInternal, safeContextError(err))
	}
	games, err := s.catalog.GamesByIDs(ctx, ids)
	if err != nil {
		return catalog.Page{}, canonicalError(protocol.CodeInternal, safeContextError(err))
	}
	grouped, hasGroup := s.catalog.(interface {
		GamesInGroup(context.Context, string, int) ([]catalog.Game, error)
	})
	for _, game := range games {
		if hasGroup && query.Grouped && game.GroupKey != "" {
			variants, err := grouped.GamesInGroup(ctx, game.GroupKey, catalog.UnboundedVariantLimit)
			if err != nil {
				return catalog.Page{}, canonicalError(protocol.CodeInternal, safeContextError(err))
			}
			if picked, ok := preferredSurvivingDump(variants, query); ok {
				return catalog.Page{Games: []catalog.Game{picked}}, nil
			}
			continue
		}
		if !catalog.MatchesQueryFilters(game, query) {
			continue
		}
		if game.VariantCount <= 0 {
			game.VariantCount = 1
		}
		return catalog.Page{Games: []catalog.Game{game}}, nil
	}
	return catalog.Page{}, nil
}

func preferredSurvivingDump(games []catalog.Game, query catalog.Query) (catalog.Game, bool) {
	surviving := make([]catalog.Game, 0, len(games))
	for _, game := range games {
		if catalog.MatchesQueryFilters(game, query) {
			surviving = append(surviving, game)
		}
	}
	if len(surviving) == 0 {
		return catalog.Game{}, false
	}
	return catalog.PreferredDump(surviving, query.PreferredRegions), true
}

func (s *Service) GamesInGroup(ctx context.Context, groupKey string) ([]catalog.Game, error) {
	grouped, ok := s.catalog.(interface {
		GamesInGroup(context.Context, string, int) ([]catalog.Game, error)
	})
	if !ok {
		return []catalog.Game{}, nil
	}
	games, err := grouped.GamesInGroup(ctx, groupKey, catalog.UnboundedVariantLimit)
	if err != nil {
		return nil, canonicalError(protocol.CodeInternal, safeContextError(err))
	}
	return games, nil
}

func (s *Service) Facets(ctx context.Context) (catalog.FacetValues, error) {
	faceted, ok := s.catalog.(interface {
		Facets(context.Context) (catalog.FacetValues, error)
	})
	if !ok {
		return catalog.FacetValues{}, nil
	}
	values, err := faceted.Facets(ctx)
	if err != nil {
		return catalog.FacetValues{}, canonicalError(protocol.CodeInternal, safeContextError(err))
	}
	return values, nil
}

func (s *Service) SetFacets(ctx context.Context, gameID, genre, year, aliases string) error {
	writer, ok := s.catalog.(interface {
		SetFacets(context.Context, string, string, string, string) error
	})
	if !ok {
		return nil
	}
	if err := writer.SetFacets(ctx, gameID, genre, year, aliases); err != nil {
		return canonicalError(protocol.CodeInternal, safeContextError(err))
	}
	return nil
}

func (s *Service) SyncFacets(ctx context.Context) (int, error) {
	if strings.TrimSpace(s.metadataRoot) == "" {
		return 0, nil
	}
	info, err := os.Lstat(filepath.Join(s.metadataRoot, "cache.sqlite3"))
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return 0, nil
	}
	cache, err := metadata.OpenCache(ctx, metadata.CacheConfig{Root: s.metadataRoot, CredentialScope: s.metadataScope})
	if err != nil {
		return 0, canonicalError(protocol.CodeInternal, nil)
	}
	defer cache.Close()
	records, err := cache.CachedPresentations(ctx)
	if err != nil {
		return 0, canonicalError(protocol.CodeInternal, nil)
	}
	if len(records) == 0 {
		return 0, nil
	}
	platforms, _, _, mapped, err := cache.PlatformMapping()
	if err != nil {
		return 0, canonicalError(protocol.CodeInternal, nil)
	}
	byPlatformTitle := make(map[string]metadata.CachedPresentation, len(records))
	for _, record := range records {
		byPlatformTitle[record.PlatformID+"\x1f"+record.NormalizedTitle] = record
	}
	updated := 0
	cursor := ""
	for {
		page, err := s.catalog.QueryGames(ctx, catalog.Query{Grouped: false, Limit: catalog.MaxQueryLimit, Cursor: cursor, Sort: catalog.SortTitle})
		if err != nil {
			return updated, canonicalError(protocol.CodeInternal, safeContextError(err))
		}
		for _, game := range page.Games {
			title := game.Title
			if strings.TrimSpace(game.CanonicalTitle) != "" {
				title = game.CanonicalTitle
			}
			normalized, normErr := metadata.DecoratedTitle(title)
			if normErr != nil {
				normalized, normErr = metadata.NormalizeTitle(title)
			}
			if normErr != nil || normalized == "" {
				continue
			}
			platformID := string(game.System)
			if mapped {
				entry, found := platforms[string(game.System)]
				if !found || entry.ID == "" {
					continue
				}
				platformID = entry.ID
			}
			record, ok := byPlatformTitle[platformID+"\x1f"+normalized]
			if !ok || (strings.TrimSpace(record.Genre) == "" && strings.TrimSpace(record.Year) == "") {
				continue
			}
			if err := s.SetFacets(ctx, game.ID, record.Genre, record.Year, ""); err != nil {
				return updated, err
			}
			updated++
		}
		if page.NextCursor == "" {
			return updated, nil
		}
		cursor = page.NextCursor
	}
}

func (s *Service) queryRecents(ctx context.Context, query catalog.Query) (catalog.Page, error) {
	if s.users == nil {
		return catalog.Page{}, nil
	}
	requestedSort := query.Sort
	normalized, err := catalog.NormalizeQuery(query)
	if err != nil {
		return catalog.Page{}, canonicalError(protocol.CodeBadRequest, nil)
	}
	query = normalized
	limit := query.Limit
	ids, err := s.users.RecentIDs(ctx, 0)
	if err != nil {
		return catalog.Page{}, canonicalError(protocol.CodeInternal, safeContextError(err))
	}
	games, err := s.catalog.GamesByIDs(ctx, ids)
	if err != nil {
		return catalog.Page{}, canonicalError(protocol.CodeInternal, safeContextError(err))
	}
	grouped, hasGroup := s.catalog.(interface {
		GamesInGroup(context.Context, string, int) ([]catalog.Game, error)
	})
	filtered := make([]catalog.Game, 0, len(games))
	seenGroups := map[string]struct{}{}
	for _, game := range games {
		if hasGroup && query.Grouped && game.GroupKey != "" {
			if _, seen := seenGroups[game.GroupKey]; seen {
				continue
			}
			variants, err := grouped.GamesInGroup(ctx, game.GroupKey, catalog.UnboundedVariantLimit)
			if err != nil {
				return catalog.Page{}, canonicalError(protocol.CodeInternal, safeContextError(err))
			}
			picked, ok := preferredSurvivingDump(variants, query)
			if !ok {
				continue
			}
			seenGroups[game.GroupKey] = struct{}{}
			filtered = append(filtered, picked)
			continue
		}
		if !catalog.MatchesQueryFilters(game, query) {
			continue
		}
		filtered = append(filtered, game)
	}
	games = filtered
	if requestedSort == catalog.SortTitle || requestedSort == catalog.SortPlatform {
		catalog.OrderGames(games, requestedSort)
	}
	start := 0
	if query.Cursor != "" {
		cursorID, err := catalog.CursorGameID(query.Cursor)
		if err != nil {
			return catalog.Page{}, canonicalError(protocol.CodeBadRequest, nil)
		}
		start = len(games)
		for index, game := range games {
			if game.ID == cursorID {
				start = index + 1
				break
			}
		}
	}
	if start > len(games) {
		start = len(games)
	}
	end := start + limit
	page := catalog.Page{}
	if end < len(games) {
		page.Games = games[start:end]
		page.NextCursor = catalog.CursorFor(page.Games[len(page.Games)-1], query.Sort)
		return page, nil
	}
	page.Games = games[start:]
	return page, nil
}

func (s *Service) ScanMedia(ctx context.Context) error {
	if s.media == nil {
		return nil
	}
	games, err := s.catalog.Games(ctx)
	if err != nil {
		return canonicalError(protocol.CodeInternal, safeContextError(err))
	}
	if err := s.media.Scan(ctx, games); err != nil {
		return canonicalError(protocol.CodeInternal, safeContextError(err))
	}
	return nil
}

func (s *Service) GameMedia(ctx context.Context, gameID string) (librarymedia.GameMedia, error) {
	if s.media == nil {
		return librarymedia.GameMedia{}, nil
	}
	media, err := s.media.GameMedia(ctx, gameID)
	if err != nil {
		return librarymedia.GameMedia{}, canonicalError(protocol.CodeInternal, safeContextError(err))
	}
	return media, nil
}

func (s *Service) CoverHandle(ctx context.Context, gameID string) string {
	if s.media == nil {
		return ""
	}
	return s.media.CoverHandle(ctx, gameID)
}

func (s *Service) OpenMedia(ctx context.Context, handle string) (librarymedia.Opened, error) {
	if s.media == nil {
		return librarymedia.Opened{}, errors.New("media is unavailable")
	}
	return s.media.Open(ctx, handle)
}

func (s *Service) AttractIdleSeconds() int {
	return s.LibrarySettings().AttractIdleSeconds
}

func (s *Service) LibrarySettings() LibraryConfig {
	s.libraryMu.RLock()
	defer s.libraryMu.RUnlock()
	return s.librarySettingsSnapshot()
}

func (s *Service) librarySettingsSnapshot() LibraryConfig {
	seconds := s.attractIdle
	if seconds <= 0 {
		seconds = 60
	}
	regions := append([]string(nil), s.preferredRegions...)
	if len(regions) == 0 {
		regions = append([]string(nil), catalog.DefaultPreferredRegions...)
	}
	return LibraryConfig{
		AttractIdleSeconds: seconds,
		PreferredRegions:   regions,
		Libraries:          append([]catalog.Root(nil), s.roots...),
		Targets:            append([]TargetConfig(nil), s.targets...),
		SelectedTarget:     s.selectedTarget,
		WatchRoot:          s.watchRoot,
	}
}

func (s *Service) currentPreferredRegions() []string {
	return append([]string(nil), s.LibrarySettings().PreferredRegions...)
}

func (s *Service) applyPersistedLibraryOverlay() {
	overlay, ok, err := loadLibraryOverlay(s.libraryOverlayPath)
	if err != nil || !ok {
		return
	}
	s.attractIdle = overlay.AttractIdleSeconds
	s.preferredRegions = append([]string(nil), overlay.PreferredRegions...)
}

func (s *Service) SetLibrarySettings(ctx context.Context, next LibraryConfig) error {
	s.cancelTargetLookup()
	if err := ctx.Err(); err != nil {
		return err
	}
	releaseSettings, err := s.acquireLibrarySettings(ctx)
	if err != nil {
		return err
	}
	defer releaseSettings()
	if err := ctx.Err(); err != nil {
		return err
	}
	s.targetMu.Lock()
	s.libraryMu.Lock()
	rootsChanged, err := s.setLibrarySettingsLocked(next)
	s.libraryMu.Unlock()
	s.targetMu.Unlock()
	if err != nil {
		return err
	}
	if rootsChanged {
		_, err = s.scanLocked(ctx)
	}
	return err
}

var librarySettingsPatchStartHook func()

func (s *Service) PatchLibrarySettings(ctx context.Context, patch LibraryConfigPatch) error {
	s.cancelTargetLookup()
	if err := ctx.Err(); err != nil {
		return err
	}
	if librarySettingsPatchStartHook != nil {
		librarySettingsPatchStartHook()
	}
	releaseSettings, err := s.acquireLibrarySettings(ctx)
	if err != nil {
		return err
	}
	defer releaseSettings()
	if err := ctx.Err(); err != nil {
		return err
	}
	s.targetMu.Lock()
	s.libraryMu.Lock()
	next := s.librarySettingsSnapshot()
	if patch.AttractIdleSeconds != nil {
		next.AttractIdleSeconds = *patch.AttractIdleSeconds
	}
	if patch.PreferredRegions != nil {
		next.PreferredRegions = append([]string(nil), *patch.PreferredRegions...)
	}
	if patch.Libraries != nil {
		next.Libraries = append([]catalog.Root(nil), (*patch.Libraries)...)
	}
	if patch.Targets != nil {
		next.Targets = append([]TargetConfig(nil), (*patch.Targets)...)
	}
	if patch.SelectedTarget != nil {
		next.SelectedTarget = *patch.SelectedTarget
	}
	if patch.PrepareTarget != nil {
		found := false
		for i := range next.Targets {
			if next.Targets[i].Name != *patch.PrepareTarget {
				continue
			}
			found = true
			if next.Targets[i].TargetID == "" {
				next.Targets[i].TargetID, err = discovery.NewID()
			}
		}
		if !found || s.configPath == "" || err != nil {
			s.libraryMu.Unlock()
			s.targetMu.Unlock()
			return canonicalError(protocol.CodeBadRequest, nil)
		}
	}
	rootsChanged, err := s.setLibrarySettingsLocked(next)
	s.libraryMu.Unlock()
	s.targetMu.Unlock()
	if err != nil {
		return err
	}
	if rootsChanged {
		_, err = s.scanLocked(ctx)
	}
	return err
}

func (s *Service) setLibrarySettingsLocked(next LibraryConfig) (bool, error) {
	if next.Libraries == nil {
		next.Libraries = append([]catalog.Root(nil), s.roots...)
	}
	if next.Targets == nil {
		next.Targets = append([]TargetConfig(nil), s.targets...)
		if strings.TrimSpace(next.SelectedTarget) == "" {
			next.SelectedTarget = s.selectedTarget
		}
	}
	if len(next.Targets) == 0 {
		return false, canonicalError(protocol.CodeBadRequest, nil)
	}
	next.Libraries = assignLibraryRootIDs(next.Libraries)
	mergedTargets, err := s.mergeTargetAgentsLocked(next.Targets)
	if err != nil {
		return false, canonicalError(protocol.CodeBadRequest, nil)
	}
	next.Targets = mergedTargets
	selectedWrite := targetByName(next.Targets, next.SelectedTarget)
	normalized, err := NormalizeLibraryConfig(next)
	if err != nil {
		return false, canonicalError(protocol.CodeBadRequest, nil)
	}
	selected := targetByName(normalized.Targets, normalized.SelectedTarget)
	currentSelected := targetByName(s.targets, s.selectedTarget)
	s.executionMu.Lock()
	active := s.activeExecution != ""
	reconciled := s.selectedTargetReconciled
	repairAllowed := s.selectedTargetRepairAllowed
	s.executionMu.Unlock()
	selectedNameChanged := normalized.SelectedTarget != s.selectedTarget
	selectedRenamesCurrent := selectedNameChanged && strings.TrimSpace(selectedWrite.PreviousName) == s.selectedTarget
	selectedConnectionChanged := selected.Enabled != currentSelected.Enabled || selected.Address != currentSelected.Address || selected.Agent != currentSelected.Agent || (currentSelected.TargetID != "" && selected.TargetID != currentSelected.TargetID)
	selectedIdentityChanged := (!selectedRenamesCurrent && selectedNameChanged) || selectedConnectionChanged
	sameEnabledTargetRepair := repairAllowed && !selectedNameChanged && selected.Enabled && currentSelected.Enabled &&
		(selected.Address != currentSelected.Address || selected.Agent != currentSelected.Agent)
	offlineUnowned := !active && s.TargetConnection().State == "disconnected"
	if existing, ok := s.targetClients[s.selectedTarget].(interface{ HasKitGrant() bool }); ok {
		offlineUnowned = offlineUnowned && !existing.HasKitGrant()
	} else {
		// Legacy clients cannot establish absence of local lease authority.
		offlineUnowned = false
	}
	if selectedIdentityChanged && !reconciled && !sameEnabledTargetRepair && !offlineUnowned {
		return false, canonicalError(protocol.CodeBadRequest, nil)
	}
	if active && selected.TargetID != currentSelected.TargetID {
		return false, canonicalError(protocol.CodeBusy, nil)
	}
	if active && (selectedNameChanged || selectedConnectionChanged) {
		return false, canonicalError(protocol.CodeBadRequest, nil)
	}
	if s.targetSwitchLocked && ((!selectedRenamesCurrent && selectedNameChanged) || selectedConnectionChanged) {
		return false, canonicalError(protocol.CodeBadRequest, nil)
	}
	var selectedClient serviceClient
	if selected.Enabled {
		if !selectedConnectionChanged && (!selectedNameChanged || selectedRenamesCurrent) {
			selectedClient = s.targetClients[s.selectedTarget]
		} else if s.targetClientFactory == nil {
			if normalized.SelectedTarget == s.selectedTarget || selectedRenamesCurrent {
				selectedClient = s.targetClients[s.selectedTarget]
			}
			if selectedClient == nil {
				return false, canonicalError(protocol.CodeInternal, nil)
			}
		} else {
			selectedClient, err = s.targetClientFactory(selected)
			if err != nil {
				return false, canonicalError(protocol.CodeBadRequest, nil)
			}
		}
	}
	rootsChanged := !sameLibraryRoots(s.roots, normalized.Libraries)
	if err := s.persistAndPublishLibrarySettingsLocked(normalized, selectedClient, selectedIdentityChanged); err != nil {
		return false, err
	}
	return rootsChanged, nil
}

func (s *Service) mergeTargetAgentsLocked(next []TargetConfig) ([]TargetConfig, error) {
	current := make(map[string]string, len(s.targets))
	for _, target := range s.targets {
		current[target.Name] = target.Agent
	}
	merged := append([]TargetConfig(nil), next...)
	claimedCurrentNames := make(map[string]struct{}, len(merged))
	for index := range merged {
		previous := strings.TrimSpace(merged[index].PreviousName)
		currentName := previous
		if currentName == "" {
			if _, exists := current[merged[index].Name]; exists {
				currentName = merged[index].Name
			}
		}
		if currentName == "" {
			continue
		}
		agent, ok := current[currentName]
		if !ok {
			return nil, errors.New("previous target name is unknown")
		}
		if _, duplicate := claimedCurrentNames[currentName]; duplicate {
			return nil, errors.New("current target identity is claimed more than once")
		}
		claimedCurrentNames[currentName] = struct{}{}
		if merged[index].TargetID == "" {
			merged[index].TargetID = targetByName(s.targets, currentName).TargetID
		}
		if !merged[index].AgentSet {
			merged[index].Agent = agent
		}
	}
	return merged, nil
}

func assignLibraryRootIDs(roots []catalog.Root) []catalog.Root {
	assigned := append([]catalog.Root(nil), roots...)
	for index := range assigned {
		if strings.TrimSpace(assigned[index].ID) != "" {
			continue
		}
		path, err := normalizeRoot(assigned[index].Path)
		if err != nil {
			continue
		}
		digest := sha256.Sum256([]byte(string(assigned[index].System) + "\x00" + path))
		assigned[index].ID = "library-" + string(assigned[index].System) + "-" + hex.EncodeToString(digest[:6])
	}
	return assigned
}

func sameLibraryRoots(left, right []catalog.Root) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func (s *Service) persistAndPublishLibrarySettingsLocked(normalized LibraryConfig, selectedClient serviceClient, selectedIdentityChanged bool) error {
	s.configWriteMu.Lock()
	defer s.configWriteMu.Unlock()
	var previousOverlay []byte
	previousOverlayExists := false
	var previousConfig []byte
	previousConfigExists := false
	if s.configPath != "" {
		var err error
		previousConfig, previousConfigExists, err = snapshotPrivateFile(s.configPath)
		if err != nil {
			return canonicalError(protocol.CodeInternal, safeContextError(err))
		}
	}
	if s.libraryOverlayPath != "" {
		var err error
		previousOverlay, previousOverlayExists, err = snapshotLibraryOverlay(s.libraryOverlayPath)
		if err != nil {
			return canonicalError(protocol.CodeInternal, safeContextError(err))
		}
		if err := saveLibraryOverlay(s.libraryOverlayPath, normalized); err != nil {
			if restoreErr := restoreLibraryOverlay(s.libraryOverlayPath, previousOverlay, previousOverlayExists); restoreErr != nil {
				return canonicalError(protocol.CodeInternal, safeContextError(errors.Join(err, restoreErr)))
			}
			return canonicalError(protocol.CodeInternal, safeContextError(err))
		}
	}
	if s.configPath != "" {
		if err := writeCanonicalConfig(s.configPath, normalized.Libraries, normalized.Targets, normalized.SelectedTarget); err != nil {
			restoreErrors := []error{err, restorePrivateFile(s.configPath, previousConfig, previousConfigExists)}
			if s.libraryOverlayPath != "" {
				restoreErrors = append(restoreErrors, restoreLibraryOverlay(s.libraryOverlayPath, previousOverlay, previousOverlayExists))
			}
			return canonicalError(protocol.CodeInternal, safeContextError(errors.Join(restoreErrors...)))
		}
	}
	s.attractIdle = normalized.AttractIdleSeconds
	s.preferredRegions = append([]string(nil), normalized.PreferredRegions...)
	s.roots = append([]catalog.Root(nil), normalized.Libraries...)
	s.rootsByID = make(map[string]catalog.Root, len(s.roots))
	for _, root := range s.roots {
		s.rootsByID[root.ID] = root
	}
	s.targets = append([]TargetConfig(nil), normalized.Targets...)
	s.selectedTarget = normalized.SelectedTarget
	s.targetClients = make(map[string]serviceClient)
	if selectedClient != nil {
		s.targetClients[s.selectedTarget] = selectedClient
	}
	if selectedIdentityChanged {
		s.connectionMu.Lock()
		s.connection = TargetConnection{}
		s.nextLookup = time.Time{}
		s.lookupFailures = 0
		s.connectionMu.Unlock()
		s.executionMu.Lock()
		s.selectedTargetReconciled = !targetByName(s.targets, s.selectedTarget).Enabled
		s.selectedTargetRepairAllowed = false
		s.executionMu.Unlock()
	}
	return nil
}

func (s *Service) AttractPlaylist(ctx context.Context, limit int) ([]AttractItem, error) {
	if limit <= 0 || limit > 50 {
		limit = 24
	}
	items := make([]AttractItem, 0, limit)
	seen := map[string]struct{}{}
	pick := func(ids []string) {
		for _, id := range ids {
			if len(items) >= limit {
				return
			}
			if _, ok := seen[id]; ok {
				continue
			}
			media, _ := s.GameMedia(ctx, id)
			if media.Cover == "" && media.Backdrop == "" && media.Marquee == "" && media.Video == "" {
				continue
			}
			game, err := s.catalog.Game(ctx, id)
			if err != nil {
				continue
			}
			seen[id] = struct{}{}
			items = append(items, AttractItem{
				GameID: game.ID, Title: game.Title, Platform: game.System,
				Video: media.Video, Cover: media.Cover, Backdrop: media.Backdrop, Marquee: media.Marquee,
				Launchable: s.PlatformLaunchable(game.System),
			})
		}
	}
	if s.users != nil {
		favorites, _ := s.users.FavoriteIDs(ctx)
		pick(favorites)
		if len(items) < limit {
			recents, _ := s.users.RecentIDs(ctx, 100)
			pick(recents)
		}
	}
	if s.media != nil && len(items) < limit {
		extra, _ := s.media.GamesWithMedia(ctx, nil, limit)
		pick(extra)
	}
	return items, nil
}

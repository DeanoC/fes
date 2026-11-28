package fogcast

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/internal/metadata"
	"github.com/DeanoC/FogCast/libraryuser"
	"github.com/DeanoC/FogCast/protocol"
)

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
	coreEntryGameIDs, err := s.matchingCoreEntryGameIDs(ctx, query.CoreIDs)
	if err != nil {
		return catalog.Page{}, canonicalError(protocol.CodeInternal, safeContextError(err))
	}
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
		if !matchesCoreEntryGameID(game, coreEntryGameIDs) {
			continue
		}
		if hasGroup && query.Grouped && game.GroupKey != "" {
			variants, err := grouped.GamesInGroup(ctx, game.GroupKey, catalog.UnboundedVariantLimit)
			if err != nil {
				return catalog.Page{}, canonicalError(protocol.CodeInternal, safeContextError(err))
			}
			if picked, ok := preferredSurvivingDump(variants, query, coreEntryGameIDs); ok {
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

func (s *Service) matchingCoreEntryGameIDs(ctx context.Context, coreIDs []string) (map[string]struct{}, error) {
	if len(coreIDs) == 0 {
		return nil, nil
	}
	reader, ok := s.catalog.(interface {
		CoreEntries(context.Context) ([]catalog.CoreEntry, error)
	})
	if !ok {
		return map[string]struct{}{}, nil
	}
	entries, err := reader.CoreEntries(ctx)
	if err != nil {
		return nil, err
	}
	selectedCoreIDs := make(map[string]struct{}, len(coreIDs))
	for _, coreID := range coreIDs {
		selectedCoreIDs[coreID] = struct{}{}
	}
	gameIDs := make(map[string]struct{})
	for _, entry := range entries {
		if _, selected := selectedCoreIDs[entry.CoreID]; selected {
			gameIDs[entry.GameID] = struct{}{}
		}
	}
	return gameIDs, nil
}

func matchesCoreEntryGameID(game catalog.Game, coreEntryGameIDs map[string]struct{}) bool {
	if coreEntryGameIDs == nil {
		return true
	}
	_, ok := coreEntryGameIDs[game.ID]
	return ok
}

func preferredSurvivingDump(games []catalog.Game, query catalog.Query, coreEntryGameIDs map[string]struct{}) (catalog.Game, bool) {
	surviving := make([]catalog.Game, 0, len(games))
	for _, game := range games {
		if matchesCoreEntryGameID(game, coreEntryGameIDs) && catalog.MatchesQueryFilters(game, query) {
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
	coreEntryGameIDs, err := s.matchingCoreEntryGameIDs(ctx, query.CoreIDs)
	if err != nil {
		return catalog.Page{}, canonicalError(protocol.CodeInternal, safeContextError(err))
	}
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
		if !matchesCoreEntryGameID(game, coreEntryGameIDs) {
			continue
		}
		if hasGroup && query.Grouped && game.GroupKey != "" {
			if _, seen := seenGroups[game.GroupKey]; seen {
				continue
			}
			variants, err := grouped.GamesInGroup(ctx, game.GroupKey, catalog.UnboundedVariantLimit)
			if err != nil {
				return catalog.Page{}, canonicalError(protocol.CodeInternal, safeContextError(err))
			}
			picked, ok := preferredSurvivingDump(variants, query, coreEntryGameIDs)
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

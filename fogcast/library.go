package fogcast

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/DeanoC/FogCast-POC/catalog"
	"github.com/DeanoC/FogCast-POC/internal/metadata"
	"github.com/DeanoC/FogCast-POC/librarymedia"
	"github.com/DeanoC/FogCast-POC/libraryuser"
	"github.com/DeanoC/FogCast-POC/protocol"
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
	seconds := s.attractIdle
	if seconds <= 0 {
		seconds = 60
	}
	regions := append([]string(nil), s.preferredRegions...)
	if len(regions) == 0 {
		regions = append([]string(nil), catalog.DefaultPreferredRegions...)
	}
	return LibraryConfig{AttractIdleSeconds: seconds, PreferredRegions: regions}
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
	if err := ctx.Err(); err != nil {
		return err
	}
	normalized, err := NormalizeLibraryConfig(next)
	if err != nil {
		return canonicalError(protocol.CodeBadRequest, nil)
	}
	if s.libraryOverlayPath != "" {
		if err := saveLibraryOverlay(s.libraryOverlayPath, normalized); err != nil {
			return canonicalError(protocol.CodeInternal, safeContextError(err))
		}
	}
	s.libraryMu.Lock()
	s.attractIdle = normalized.AttractIdleSeconds
	s.preferredRegions = append([]string(nil), normalized.PreferredRegions...)
	s.libraryMu.Unlock()
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

package fogcast

import (
	"context"
	"errors"

	"github.com/DeanoC/FogCast-POC/catalog"
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

func (s *Service) queryRecents(ctx context.Context, query catalog.Query) (catalog.Page, error) {
	if s.users == nil {
		return catalog.Page{}, nil
	}
	limit := query.Limit
	if limit <= 0 {
		limit = catalog.DefaultQueryLimit
	}
	if limit > catalog.MaxQueryLimit {
		limit = catalog.MaxQueryLimit
	}
	ids, err := s.users.RecentIDs(ctx, 0)
	if err != nil {
		return catalog.Page{}, canonicalError(protocol.CodeInternal, safeContextError(err))
	}
	games, err := s.catalog.GamesByIDs(ctx, ids)
	if err != nil {
		return catalog.Page{}, canonicalError(protocol.CodeInternal, safeContextError(err))
	}
	filtered := make([]catalog.Game, 0, len(games))
	for _, game := range games {
		if query.Platform != "" && game.System != query.Platform {
			continue
		}
		if !catalog.MatchesText(game, query.Text) {
			continue
		}
		filtered = append(filtered, game)
	}
	games = filtered
	if query.Sort == catalog.SortTitle || query.Sort == catalog.SortPlatform {
		catalog.OrderGames(games, query.Sort)
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
	if s.attractIdle <= 0 {
		return 60
	}
	return s.attractIdle
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

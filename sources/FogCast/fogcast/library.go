package fogcast

import (
	"context"
	"errors"

	"github.com/DeanoC/FogCast/catalog"
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
	return userMutationError(s.users.MutateGame(ctx, func(m *libraryuser.GameMutation) error {
		if _, err := s.Game(ctx, gameID); err != nil {
			return err
		}
		return m.SetFavorite(ctx, gameID, favorite)
	}))
}

func userMutationError(err error) error {
	if err == nil {
		return nil
	}
	var apiErr *protocol.APIError
	if errors.As(err, &apiErr) {
		return err
	}
	if errors.Is(err, libraryuser.ErrNotFound) {
		return libraryuser.ErrNotFound
	}
	if errors.Is(err, libraryuser.ErrReservedID) || errors.Is(err, libraryuser.ErrInvalid) {
		return canonicalError(protocol.CodeBadRequest, nil)
	}
	return canonicalError(protocol.CodeInternal, safeContextError(err))
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
	return userMutationError(s.users.MutateGame(ctx, func(m *libraryuser.GameMutation) error {
		if source, ok := s.catalog.(interface {
			KitHostPlaySource(context.Context, string) (string, bool, error)
		}); ok {
			mapped, generated, err := source.KitHostPlaySource(ctx, gameID)
			if err != nil {
				return canonicalError(protocol.CodeInternal, safeContextError(err))
			}
			if generated {
				if mapped == "" {
					return nil
				}
				gameID = mapped
			}
		}
		if store, ok := s.catalog.(interface {
			RetainedCoreEntry(context.Context, string) (bool, error)
		}); ok {
			retained, err := store.RetainedCoreEntry(ctx, gameID)
			if err != nil {
				return canonicalError(protocol.CodeInternal, safeContextError(err))
			}
			if retained {
				return nil
			}
		}
		return m.RecordPlay(ctx, gameID)
	}))
}

func (s *Service) SetEditionPreference(ctx context.Context, query, platform, gameID string) (libraryuser.EditionPreference, error) {
	if s.users == nil {
		return libraryuser.EditionPreference{}, canonicalError(protocol.CodeInternal, nil)
	}
	if protocol.ValidateGameID(gameID) != nil {
		return libraryuser.EditionPreference{}, canonicalError(protocol.CodeBadRequest, nil)
	}
	var pref libraryuser.EditionPreference
	err := s.users.MutateGame(ctx, func(m *libraryuser.GameMutation) error {
		if store, ok := s.catalog.(interface {
			RetainedCoreEntry(context.Context, string) (bool, error)
		}); ok {
			retained, err := store.RetainedCoreEntry(ctx, gameID)
			if err != nil {
				return canonicalError(protocol.CodeInternal, safeContextError(err))
			}
			if retained {
				return canonicalError(protocol.CodeROMNotFound, nil)
			}
		}
		var err error
		pref, err = m.SetEditionPreference(ctx, query, platform, gameID)
		return err
	})
	return pref, userMutationError(err)
}

func (s *Service) EditionPreference(ctx context.Context, query, platform string) (libraryuser.EditionPreference, error) {
	if s.users == nil {
		return libraryuser.EditionPreference{}, libraryuser.ErrNotFound
	}
	pref, err := s.users.EditionPreference(ctx, query, platform)
	if err != nil {
		if errors.Is(err, libraryuser.ErrNotFound) {
			return libraryuser.EditionPreference{}, libraryuser.ErrNotFound
		}
		if errors.Is(err, libraryuser.ErrInvalid) {
			return libraryuser.EditionPreference{}, canonicalError(protocol.CodeBadRequest, nil)
		}
		return libraryuser.EditionPreference{}, canonicalError(protocol.CodeInternal, safeContextError(err))
	}
	return pref, nil
}

func (s *Service) EditionPreferences(ctx context.Context) ([]libraryuser.EditionPreference, error) {
	if s.users == nil {
		return []libraryuser.EditionPreference{}, nil
	}
	prefs, err := s.users.EditionPreferences(ctx)
	if err != nil {
		return nil, canonicalError(protocol.CodeInternal, safeContextError(err))
	}
	return prefs, nil
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
	return userMutationError(s.users.MutateGame(ctx, func(m *libraryuser.GameMutation) error {
		if _, err := m.Collection(ctx, collectionID); err != nil {
			return err
		}
		if _, err := s.Game(ctx, gameID); err != nil {
			return err
		}
		return m.SetCollectionMember(ctx, collectionID, gameID, member)
	}))
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

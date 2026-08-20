package hostapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/DeanoC/FogCast-POC/catalog"
	"github.com/DeanoC/FogCast-POC/fogcast"
	"github.com/DeanoC/FogCast-POC/librarymedia"
	"github.com/DeanoC/FogCast-POC/libraryuser"
	"github.com/DeanoC/FogCast-POC/protocol"
)

type libraryQueryService interface {
	QueryGames(context.Context, catalog.Query) (catalog.Page, error)
	Platforms(context.Context) ([]catalog.PlatformInfo, error)
}

type favoriteService interface {
	SetFavorite(context.Context, string, bool) error
	LibraryState(context.Context, string) (libraryuser.State, error)
	LibraryStates(context.Context, []string) (map[string]libraryuser.State, error)
}

type coverService interface {
	CoverHandle(context.Context, string) string
	GameMedia(context.Context, string) (librarymedia.GameMedia, error)
}

type mediaOpenService interface {
	OpenMedia(context.Context, string) (librarymedia.Opened, error)
}

type attractService interface {
	AttractPlaylist(context.Context, int) ([]fogcast.AttractItem, error)
	AttractIdleSeconds() int
}

func handleGamesList(w http.ResponseWriter, r *http.Request, service Service) {
	query, err := parseGameQuery(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "catalog query is invalid")
		return
	}
	var page catalog.Page
	if lister, ok := service.(libraryQueryService); ok {
		page, err = lister.QueryGames(r.Context(), query)
	} else {
		page, err = fallbackGamePage(r.Context(), service, query)
	}
	if err != nil {
		if errors.Is(err, catalog.ErrInvalidQuery) {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "catalog query is invalid")
			return
		}
		var apiErr *protocol.APIError
		if errors.As(err, &apiErr) {
			writeSessionError(w, err)
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL", "catalog is unavailable")
		return
	}
	result := gamesResult{Games: make([]gameResult, 0, len(page.Games)), NextCursor: page.NextCursor}
	ids := make([]string, 0, len(page.Games))
	for _, game := range page.Games {
		ids = append(ids, game.ID)
		result.Games = append(result.Games, publicGame(game))
	}
	states := map[string]libraryuser.State{}
	if users, ok := service.(favoriteService); ok {
		states, _ = users.LibraryStates(r.Context(), ids)
	}
	for index, game := range result.Games {
		if state, ok := states[game.ID]; ok {
			result.Games[index].Favorite = state.Favorite
		}
		if covers, ok := service.(coverService); ok {
			result.Games[index].Cover = covers.CoverHandle(r.Context(), game.ID)
		}
		result.Games[index] = enrichLaunchable(service, result.Games[index])
	}
	writeJSON(w, http.StatusOK, result)
}

func parseGameQuery(r *http.Request) (catalog.Query, error) {
	values := r.URL.Query()
	query := catalog.Query{
		Text:           strings.TrimSpace(values.Get("q")),
		Platform:       protocol.System(strings.TrimSpace(values.Get("platform"))),
		Collection:     strings.TrimSpace(values.Get("collection")),
		Region:         strings.TrimSpace(values.Get("region")),
		Genre:          strings.TrimSpace(values.Get("genre")),
		Year:           strings.TrimSpace(values.Get("year")),
		Availability:   strings.TrimSpace(values.Get("availability")),
		HidePrerelease: queryFlag(values.Get("hide_prerelease")),
		HideHacks:      queryFlag(values.Get("hide_hacks")),
		Grouped:        true,
		Cursor:         strings.TrimSpace(values.Get("cursor")),
	}
	switch strings.TrimSpace(values.Get("grouped")) {
	case "", "1", "true":
		query.Grouped = true
	case "0", "false":
		query.Grouped = false
	default:
		return catalog.Query{}, fmt.Errorf("unsupported grouped flag")
	}
	switch strings.TrimSpace(values.Get("sort")) {
	case "":
	case "title":
		query.Sort = catalog.SortTitle
	case "system", "platform":
		query.Sort = catalog.SortPlatform
	case "year":
		query.Sort = catalog.SortYear
	case "recently_added":
		query.Sort = catalog.SortAdded
	default:
		return catalog.Query{}, fmt.Errorf("unsupported catalog sort")
	}
	if raw := strings.TrimSpace(values.Get("limit")); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 1 {
			return catalog.Query{}, err
		}
		query.Limit = limit
	}
	return query, nil
}

func queryFlag(raw string) bool {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}

func fallbackGamePage(ctx context.Context, service Service, query catalog.Query) (catalog.Page, error) {
	var games []catalog.Game
	var err error
	if query.Text == "" {
		games, err = service.Games(ctx)
	} else {
		games, err = service.Search(ctx, query.Text)
	}
	if err != nil {
		return catalog.Page{}, err
	}
	filtered := make([]catalog.Game, 0, len(games))
	for _, game := range games {
		if !catalog.MatchesQueryFilters(game, query) {
			continue
		}
		filtered = append(filtered, game)
	}
	limit := query.Limit
	if limit <= 0 {
		return catalog.Page{Games: filtered}, nil
	}
	if limit > catalog.MaxQueryLimit {
		limit = catalog.MaxQueryLimit
	}
	if len(filtered) > limit {
		return catalog.Page{Games: filtered[:limit], NextCursor: filtered[limit-1].ID}, nil
	}
	return catalog.Page{Games: filtered}, nil
}

func handlePlatforms(w http.ResponseWriter, r *http.Request, service Service) {
	lister, ok := service.(libraryQueryService)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"platforms": []catalog.PlatformInfo{}})
		return
	}
	platforms, err := lister.Platforms(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL", "catalog is unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"platforms": platforms})
}

func handleFavorite(w http.ResponseWriter, r *http.Request, service Service, favorite bool) {
	id := r.PathValue("id")
	if id == "" || protocol.ValidateGameID(id) != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "game ID is invalid")
		return
	}
	users, ok := service.(favoriteService)
	if !ok {
		writeError(w, http.StatusNotFound, "GAME_NOT_FOUND", "game was not found")
		return
	}
	if err := users.SetFavorite(r.Context(), id, favorite); err != nil {
		writeSessionError(w, err)
		return
	}
	state, _ := users.LibraryState(r.Context(), id)
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "favorite": state.Favorite})
}

func handleAttract(w http.ResponseWriter, r *http.Request, service Service) {
	limit := 24
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "catalog query is invalid")
			return
		}
		limit = parsed
	}
	attractor, ok := service.(attractService)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"items": []fogcast.AttractItem{}, "idle_seconds": 60})
		return
	}
	items, err := attractor.AttractPlaylist(r.Context(), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL", "catalog is unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "idle_seconds": attractor.AttractIdleSeconds()})
}

func enrichGameResult(ctx context.Context, service Service, result gameResult) gameResult {
	if users, ok := service.(favoriteService); ok {
		if state, err := users.LibraryState(ctx, result.ID); err == nil {
			result.Favorite = state.Favorite
		}
	}
	if covers, ok := service.(coverService); ok {
		result.Cover = covers.CoverHandle(ctx, result.ID)
	}
	return enrichLaunchable(service, result)
}

type platformLaunchService interface {
	PlatformLaunchable(protocol.System) bool
}

func enrichLaunchable(service Service, result gameResult) gameResult {
	if policy, ok := service.(platformLaunchService); ok {
		result.Launchable = policy.PlatformLaunchable(result.System)
		return result
	}
	result.Launchable = catalog.Launchable(result.System)
	return result
}

func handleFacets(w http.ResponseWriter, r *http.Request, service Service) {
	faceted, ok := service.(interface {
		Facets(context.Context) (catalog.FacetValues, error)
	})
	if !ok {
		writeJSON(w, http.StatusOK, catalog.FacetValues{Genres: []string{}, Years: []string{}})
		return
	}
	values, err := faceted.Facets(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL", "catalog is unavailable")
		return
	}
	if values.Genres == nil {
		values.Genres = []string{}
	}
	if values.Years == nil {
		values.Years = []string{}
	}
	writeJSON(w, http.StatusOK, values)
}

func publicGameWithVariants(ctx context.Context, service Service, game catalog.Game) gameResult {
	result := publicGame(game)
	grouped, ok := service.(interface {
		GamesInGroup(context.Context, string) ([]catalog.Game, error)
	})
	if !ok || game.GroupKey == "" {
		return result
	}
	variants, err := grouped.GamesInGroup(ctx, game.GroupKey)
	if err != nil || len(variants) == 0 {
		return result
	}
	result.VariantCount = len(variants)
	if len(variants) > catalog.MaxVariantLimit {
		variants = variants[:catalog.MaxVariantLimit]
	}
	result.Variants = make([]gameResult, 0, len(variants))
	for _, variant := range variants {
		item := publicGame(variant)
		item.VariantCount = 1
		result.Variants = append(result.Variants, item)
	}
	return result
}

func overlayPresentation(ctx context.Context, service Service, game catalog.Game, result presentationResult) presentationResult {
	covers, ok := service.(coverService)
	if !ok {
		return result
	}
	media, err := covers.GameMedia(ctx, game.ID)
	if err != nil {
		return result
	}
	if media.Cover == "" && media.Backdrop == "" && media.Logo == "" && media.Marquee == "" && media.Video == "" && len(media.Screenshot) == 0 {
		return result
	}
	payload := presentationPayload{}
	if result.Presentation != nil {
		payload = *result.Presentation
	}
	if media.Cover != "" {
		payload.CoverArtworkHandle = media.Cover
	}
	if media.Backdrop != "" {
		payload.BackdropArtworkHandle = media.Backdrop
	}
	payload.LogoHandle = media.Logo
	payload.MarqueeHandle = media.Marquee
	payload.VideoHandle = media.Video
	payload.ScreenshotHandles = media.Screenshot
	result.Presentation = &payload
	if result.State != "ready" && (media.Cover != "" || media.Backdrop != "") {
		result.State = "ready"
	}
	return result
}

func librarymediaServe(w http.ResponseWriter, r *http.Request, opened librarymedia.Opened) {
	librarymedia.Serve(w, r, opened)
}

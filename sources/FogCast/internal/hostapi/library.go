package hostapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/librarymedia"
	"github.com/DeanoC/FogCast/libraryuser"
	"github.com/DeanoC/FogCast/protocol"
)

type libraryQueryService interface {
	QueryGames(context.Context, catalog.Query) (catalog.Page, error)
	Platforms(context.Context) ([]catalog.PlatformInfo, error)
}

type shelfNoticeService interface {
	LocalShelfNotice(context.Context) string
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
	result := gamesResult{Games: make([]gameResult, 0, len(page.Games)), NextCursor: page.NextCursor, CoreEntryKind: query.CoreEntryKind}
	ids := make([]string, 0, len(page.Games))
	for _, game := range page.Games {
		ids = append(ids, game.ID)
		result.Games = append(result.Games, publicGame(game))
	}
	states := map[string]libraryuser.State{}
	if users, ok := service.(favoriteService); ok {
		states, _ = users.LibraryStates(r.Context(), ids)
	}
	membership := map[string][]string{}
	if collections, ok := service.(collectionService); ok {
		membership, _ = collections.CollectionIDsByGame(r.Context(), ids)
	}
	presence, romKnown := romCachePresence(r.Context(), service)
	for index, game := range result.Games {
		if state, ok := states[game.ID]; ok {
			applyUserState(&result.Games[index], state)
		}
		if owned, ok := membership[game.ID]; ok {
			result.Games[index].Collections = owned
		}
		if covers, ok := service.(coverService); ok {
			result.Games[index].Cover = covers.CoverHandle(r.Context(), game.ID)
		}
		result.Games[index] = enrichLaunchable(r.Context(), service, result.Games[index])
		if index < len(page.Games) {
			applyROMCached(&result.Games[index], page.Games[index], presence, romKnown)
		}
	}
	if err := enrichCompositions(r.Context(), service, result.Games); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL", "catalog is unavailable")
		return
	}
	applyMeshReadiness(r.Context(), service, result.Games)
	if query.CoreEntryKind != "" {
		if err := attachFilteredCoreIdentities(r.Context(), service, result.Games); err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL", "catalog is unavailable")
			return
		}
	} else {
		attachCoreIdentities(r.Context(), service, result.Games)
	}
	if shelfNoticeApplies(query) {
		if notice, ok := service.(shelfNoticeService); ok {
			result.Notice = notice.LocalShelfNotice(r.Context())
		}
	}
	writeJSON(w, http.StatusOK, result)
}

func shelfNoticeApplies(query catalog.Query) bool {
	return query.Text == "" && query.Platform == "" && len(query.CoreIDs) == 0 && query.CoreEntryKind == "" && query.Collection == "" && query.Region == "" && query.Genre == "" && query.Year == "" && !query.HidePrerelease && !query.HideHacks && query.Cursor == "" && len(query.RestrictIDs) == 0 && len(query.ExcludeIDs) == 0
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
		CoreEntryKind:  strings.TrimSpace(values.Get("core_entry_kind")),
	}
	switch query.CoreEntryKind {
	case "", "machine", "title":
	default:
		return catalog.Query{}, fmt.Errorf("unsupported core entry kind")
	}
	if query.CoreEntryKind != "" && query.Collection != "" {
		return catalog.Query{}, fmt.Errorf("core entry kind cannot be combined with a collection")
	}
	for _, coreID := range values["core_id"] {
		if coreID = strings.TrimSpace(coreID); coreID != "" {
			query.CoreIDs = append(query.CoreIDs, coreID)
		}
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
	if query.CoreEntryKind != "" {
		return catalog.Page{}, catalog.ErrInvalidQuery
	}
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
	coreIDs := make(map[string]struct{}, len(query.CoreIDs))
	for _, coreID := range query.CoreIDs {
		coreIDs[coreID] = struct{}{}
	}
	coreByGame := map[string]string(nil)
	if len(coreIDs) > 0 {
		reader, ok := service.(coreIdentityService)
		if !ok {
			return catalog.Page{}, nil
		}
		entries, err := reader.CoreEntries(ctx)
		if err != nil {
			return catalog.Page{}, err
		}
		coreByGame = make(map[string]string, len(entries))
		for _, entry := range entries {
			coreByGame[entry.GameID] = entry.CoreID
		}
	}
	filtered := make([]catalog.Game, 0, len(games))
	for _, game := range games {
		if len(coreIDs) > 0 {
			if _, ok := coreIDs[coreByGame[game.ID]]; !ok {
				continue
			}
		}
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

type libraryCacheService interface {
	LibraryCache(context.Context) (fogcast.LibraryCache, error)
}

func handleLibraryCache(w http.ResponseWriter, r *http.Request, service Service) {
	provider, ok := service.(libraryCacheService)
	if !ok {
		writeJSON(w, http.StatusOK, fogcast.LibraryCache{})
		return
	}
	status, err := provider.LibraryCache(r.Context())
	if err != nil {
		writeJSON(w, http.StatusOK, fogcast.LibraryCache{})
		return
	}
	writeJSON(w, http.StatusOK, status)
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
	presence, known := romCachePresence(ctx, service)
	applyROMCached(&result, game, presence, known)
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
		applyROMCached(&item, variant, presence, known)
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
	if media.Cover == "" && media.Backdrop == "" && media.Logo == "" && media.Marquee == "" && media.Box3D == "" && media.Video == "" && len(media.Screenshot) == 0 {
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
	if media.Logo != "" {
		payload.LogoHandle = media.Logo
	}
	if media.Marquee != "" {
		payload.MarqueeHandle = media.Marquee
	}
	if media.Box3D != "" {
		payload.Box3DHandle = media.Box3D
	}
	payload.VideoHandle = media.Video
	payload.ScreenshotHandles = media.Screenshot
	result.Presentation = &payload
	if strings.EqualFold(result.State, "offline") && (media.Cover != "" || media.Backdrop != "" || media.Logo != "" || media.Marquee != "" || media.Box3D != "") {
		result.State = "ready"
	}
	return result
}

func librarymediaServe(w http.ResponseWriter, r *http.Request, opened librarymedia.Opened) {
	librarymedia.Serve(w, r, opened)
}

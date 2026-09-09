package hostapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/internal/systems"
	"github.com/DeanoC/FogCast/librarymedia"
	"github.com/DeanoC/FogCast/libraryuser"
	"github.com/DeanoC/FogCast/protocol"
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

type collectionService interface {
	Collections(context.Context) ([]libraryuser.Collection, error)
	UpsertCollection(context.Context, string, string) (libraryuser.Collection, error)
	DeleteCollection(context.Context, string) error
	SetCollectionMember(context.Context, string, string, bool) error
	GameCollectionIDs(context.Context, string) ([]string, error)
	CollectionIDsByGame(context.Context, []string) (map[string][]string, error)
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

type librarySettingsService interface {
	LibrarySettings() fogcast.LibraryConfig
	SetLibrarySettings(context.Context, fogcast.LibraryConfig) error
	PatchLibrarySettings(context.Context, fogcast.LibraryConfigPatch) error
}

type settingsWrite struct {
	PrepareTarget      *string                 `json:"prepare_target"`
	AttractIdleSeconds *int                    `json:"attract_idle_seconds"`
	PreferredRegions   *[]string               `json:"preferred_regions"`
	Libraries          *[]settingsLibraryWrite `json:"libraries"`
	Targets            *[]settingsTargetWrite  `json:"targets"`
	SelectedTarget     *string                 `json:"selected_target"`
}

type settingsLibraryWrite struct {
	ID     string          `json:"id"`
	System protocol.System `json:"system"`
	Root   string          `json:"root"`
}

type settingsTargetWrite struct {
	TargetID     string  `json:"target_id"`
	Name         string  `json:"name"`
	PreviousName string  `json:"original_name"`
	Address      string  `json:"address"`
	Agent        *string `json:"agent"`
	Enabled      bool    `json:"enabled"`
}

func handleLibrarySettings(w http.ResponseWriter, r *http.Request, service Service) {
	if r.Method == http.MethodGet {
		if err := rejectBody(w, r); err != nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "settings request body must be empty")
			return
		}
		writeJSON(w, http.StatusOK, publicLibrarySettings(defaultLibrarySettings(service)))
		return
	}
	writer, ok := service.(librarySettingsService)
	if !ok {
		writeError(w, http.StatusNotFound, "SETTINGS_UNAVAILABLE", "library settings are unavailable")
		return
	}
	empty, patch, err := decodeOptionalSettings(w, r)
	if err != nil {
		return
	}
	if !empty {
		if err := applyLibrarySettingsWrite(r, writer, patch); err != nil {
			var apiErr *protocol.APIError
			if errors.As(err, &apiErr) && apiErr.Code == protocol.CodeBadRequest {
				writeError(w, http.StatusBadRequest, "BAD_REQUEST", "library settings request is invalid")
				return
			}
			if errors.Is(err, errInvalidLibrarySettings) {
				writeError(w, http.StatusBadRequest, "BAD_REQUEST", "library settings request is invalid")
				return
			}
			writeSessionError(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, publicLibrarySettings(writer.LibrarySettings()))
}

var errInvalidLibrarySettings = errors.New("library settings request is invalid")

func applyLibrarySettingsWrite(r *http.Request, writer librarySettingsService, patch settingsWrite) error {
	var next fogcast.LibraryConfig
	if patch.AttractIdleSeconds != nil {
		next.AttractIdleSeconds = *patch.AttractIdleSeconds
	}
	if patch.PreferredRegions != nil {
		next.PreferredRegions = append([]string(nil), (*patch.PreferredRegions)...)
	}
	if patch.Libraries != nil {
		next.Libraries = make([]catalog.Root, 0, len(*patch.Libraries))
		for _, entry := range *patch.Libraries {
			next.Libraries = append(next.Libraries, catalog.Root{ID: entry.ID, System: entry.System, Path: entry.Root})
		}
	}
	if patch.Targets != nil {
		next.Targets = make([]fogcast.TargetConfig, 0, len(*patch.Targets))
		for _, entry := range *patch.Targets {
			target := fogcast.TargetConfig{Name: entry.Name, PreviousName: entry.PreviousName, Address: entry.Address, Enabled: entry.Enabled, TargetID: entry.TargetID}
			if entry.Agent != nil {
				target.Agent = *entry.Agent
				target.AgentSet = true
			}
			next.Targets = append(next.Targets, target)
		}
	}
	if patch.SelectedTarget != nil {
		next.SelectedTarget = *patch.SelectedTarget
	}
	if r.Method == http.MethodPut && (patch.AttractIdleSeconds == nil || patch.PreferredRegions == nil) {
		return errInvalidLibrarySettings
	}
	return writer.PatchLibrarySettings(r.Context(), fogcast.LibraryConfigPatch{
		PrepareTarget:      patch.PrepareTarget,
		AttractIdleSeconds: patch.AttractIdleSeconds,
		PreferredRegions:   patch.PreferredRegions,
		Libraries:          libraryRootsPointer(next.Libraries, patch.Libraries != nil),
		Targets:            targetConfigsPointer(next.Targets, patch.Targets != nil),
		SelectedTarget:     patch.SelectedTarget,
	})
}

func libraryRootsPointer(roots []catalog.Root, present bool) *[]catalog.Root {
	if !present {
		return nil
	}
	copy := append([]catalog.Root(nil), roots...)
	return &copy
}

func targetConfigsPointer(targets []fogcast.TargetConfig, present bool) *[]fogcast.TargetConfig {
	if !present {
		return nil
	}
	copy := append([]fogcast.TargetConfig(nil), targets...)
	return &copy
}

func defaultLibrarySettings(service Service) fogcast.LibraryConfig {
	if settings, ok := service.(librarySettingsService); ok {
		return settings.LibrarySettings()
	}
	if attractor, ok := service.(attractService); ok {
		return fogcast.LibraryConfig{
			AttractIdleSeconds: attractor.AttractIdleSeconds(),
			PreferredRegions:   append([]string(nil), catalog.DefaultPreferredRegions...),
		}
	}
	normalized, _ := fogcast.NormalizeLibraryConfig(fogcast.LibraryConfig{})
	return normalized
}

func publicLibrarySettings(settings fogcast.LibraryConfig) map[string]any {
	regions := settings.PreferredRegions
	if regions == nil {
		regions = []string{}
	}
	libraries := make([]map[string]any, 0, len(settings.Libraries))
	for _, root := range settings.Libraries {
		libraries = append(libraries, map[string]any{
			"id": root.ID, "system": root.System, "root": root.Path,
		})
	}
	targets := make([]map[string]any, 0, len(settings.Targets))
	for _, target := range settings.Targets {
		targets = append(targets, map[string]any{
			"name": target.Name, "address": target.Address, "enabled": target.Enabled, "target_id": target.TargetID,
			"agent_configured": strings.TrimSpace(target.Agent) != "",
		})
	}
	platforms := make([]map[string]any, 0)
	for _, row := range systems.Rows() {
		platforms = append(platforms, map[string]any{"id": row.PlatformID, "label": row.Label})
	}
	return map[string]any{
		"attract_idle_seconds": settings.AttractIdleSeconds,
		"preferred_regions":    regions,
		"libraries":            libraries,
		"targets":              targets,
		"selected_target":      settings.SelectedTarget,
		"systems":              platforms,
	}
}

func decodeOptionalSettings(w http.ResponseWriter, r *http.Request) (bool, settingsWrite, error) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "request body must contain one valid JSON object")
		return false, settingsWrite{}, err
	}
	if strings.TrimSpace(string(body)) == "" {
		return true, settingsWrite{}, nil
	}
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	var patch settingsWrite
	if err := decoder.Decode(&patch); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "request body must contain one valid JSON object")
		return false, settingsWrite{}, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "request body must contain exactly one JSON object")
		return false, settingsWrite{}, errors.New("trailing JSON")
	}
	return false, patch, nil
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
	membership := map[string][]string{}
	if collections, ok := service.(collectionService); ok {
		membership, _ = collections.CollectionIDsByGame(r.Context(), ids)
	}
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

func handleCollections(w http.ResponseWriter, r *http.Request, service Service) {
	collections, ok := service.(collectionService)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"collections": []collectionResult{}})
		return
	}
	listed, err := collections.Collections(r.Context())
	if err != nil {
		writeCollectionError(w, err)
		return
	}
	result := make([]collectionResult, 0, len(listed))
	for _, item := range listed {
		result = append(result, publicCollection(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{"collections": result})
}

func handleCollection(w http.ResponseWriter, r *http.Request, service Service, create bool) {
	id := r.PathValue("id")
	if err := libraryuser.ValidateCollectionID(id); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "collection ID is invalid")
		return
	}
	collections, ok := service.(collectionService)
	if !ok {
		writeError(w, http.StatusNotFound, "COLLECTION_NOT_FOUND", "collection was not found")
		return
	}
	if !create {
		if err := collections.DeleteCollection(r.Context(), id); err != nil {
			writeCollectionError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"id": id})
		return
	}
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	collection, err := collections.UpsertCollection(r.Context(), id, name)
	if err != nil {
		writeCollectionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, publicCollection(collection))
}

func handleCollectionMember(w http.ResponseWriter, r *http.Request, service Service, member bool) {
	collectionID := r.PathValue("id")
	gameID := r.PathValue("gameId")
	if err := libraryuser.ValidateCollectionID(collectionID); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "collection ID is invalid")
		return
	}
	if gameID == "" || protocol.ValidateGameID(gameID) != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "game ID is invalid")
		return
	}
	collections, ok := service.(collectionService)
	if !ok {
		writeError(w, http.StatusNotFound, "COLLECTION_NOT_FOUND", "collection was not found")
		return
	}
	if err := collections.SetCollectionMember(r.Context(), collectionID, gameID, member); err != nil {
		if errors.Is(err, libraryuser.ErrNotFound) {
			writeError(w, http.StatusNotFound, "COLLECTION_NOT_FOUND", "collection was not found")
			return
		}
		writeCollectionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": gameID, "collection": collectionID, "member": member})
}

func publicCollection(collection libraryuser.Collection) collectionResult {
	return collectionResult{ID: collection.ID, Name: collection.Name, CreatedAt: collection.CreatedAt}
}

func writeCollectionError(w http.ResponseWriter, err error) {
	if errors.Is(err, libraryuser.ErrNotFound) {
		writeError(w, http.StatusNotFound, "COLLECTION_NOT_FOUND", "collection was not found")
		return
	}
	if errors.Is(err, libraryuser.ErrReservedID) || errors.Is(err, libraryuser.ErrInvalid) {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "collection request is invalid")
		return
	}
	writeSessionError(w, err)
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

func applyUserState(result *gameResult, state libraryuser.State) {
	if result == nil {
		return
	}
	result.Favorite = state.Favorite
	if state.PlayCount > 0 {
		result.PlayCount = state.PlayCount
	}
	if state.LastPlayedAt > 0 {
		result.LastPlayedAt = state.LastPlayedAt
	}
}

func enrichGameResult(ctx context.Context, service Service, result gameResult) gameResult {
	if users, ok := service.(favoriteService); ok {
		if state, err := users.LibraryState(ctx, result.ID); err == nil {
			applyUserState(&result, state)
		}
	}
	if collections, ok := service.(collectionService); ok {
		if ids, err := collections.GameCollectionIDs(ctx, result.ID); err == nil && len(ids) > 0 {
			result.Collections = ids
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

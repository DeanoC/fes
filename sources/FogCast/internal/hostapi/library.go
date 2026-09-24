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

type editionPreferenceService interface {
	SetEditionPreference(context.Context, string, string, string) (libraryuser.EditionPreference, error)
	EditionPreferences(context.Context) ([]libraryuser.EditionPreference, error)
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

type editionPreferenceWrite struct {
	Query    string `json:"query"`
	Platform string `json:"platform"`
	GameID   string `json:"game_id"`
}

func handleEditionPreferences(w http.ResponseWriter, r *http.Request, service Service) {
	if err := rejectBody(w, r); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "edition preference request body must be empty")
		return
	}
	store, ok := service.(editionPreferenceService)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"preferences": []editionPreferenceResult{}})
		return
	}
	listed, err := store.EditionPreferences(r.Context())
	if err != nil {
		writeSessionError(w, err)
		return
	}
	result := make([]editionPreferenceResult, 0, len(listed))
	for _, item := range listed {
		result = append(result, publicEditionPreference(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{"preferences": result})
}

func handleSetEditionPreference(w http.ResponseWriter, r *http.Request, service Service) {
	store, ok := service.(editionPreferenceService)
	if !ok {
		writeError(w, http.StatusNotFound, "EDITION_PREFERENCE_UNAVAILABLE", "edition preferences are unavailable")
		return
	}
	var body editionPreferenceWrite
	if err := decodeSingleJSON(w, r, &body); err != nil {
		return
	}
	pref, err := store.SetEditionPreference(r.Context(), body.Query, body.Platform, body.GameID)
	if err != nil {
		if errors.Is(err, libraryuser.ErrInvalid) {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "edition preference request is invalid")
			return
		}
		writeSessionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, publicEditionPreference(pref))
}

func publicEditionPreference(pref libraryuser.EditionPreference) editionPreferenceResult {
	return editionPreferenceResult{
		Query:    pref.Query,
		Platform: pref.Platform,
		GameID:   pref.GameID,
		ChosenAt: pref.ChosenAt,
	}
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

func enrichGameResult(ctx context.Context, service Service, result gameResult) (gameResult, error) {
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
	result = enrichLaunchable(ctx, service, result)
	games := []gameResult{result}
	if err := enrichCompositions(ctx, service, games); err != nil {
		return result, err
	}
	applyMeshReadiness(ctx, service, games)
	return games[0], nil
}

type platformLaunchService interface {
	PlatformLaunchable(protocol.System) bool
}

func enrichLaunchable(ctx context.Context, service Service, result gameResult) gameResult {
	apply := func(game *gameResult) {
		if resolver, ok := service.(sessionExecutionService); ok {
			if execution, err := resolver.SessionExecution(ctx, game.ID); err == nil && execution != "" {
				game.Execution = execution
			}
		}
		game.Launchable = gameRowLaunchable(service, *game)
	}
	apply(&result)
	for i := range result.Variants {
		apply(&result.Variants[i])
	}
	return result
}

// gameRowLaunchable reports the catalog platform/target gate for one row.
// Installed FPGA packages stay launchable even when their browse system
// (Coleco, for example) has no host-emulator mapping. Raw cartridge rows
// still follow PlatformLaunchable / catalog.Launchable.
func gameRowLaunchable(service Service, result gameResult) bool {
	if result.Kind == catalog.SourceKindCorePackage || result.System == catalog.CorePlatform {
		return true
	}
	if policy, ok := service.(platformLaunchService); ok {
		return policy.PlatformLaunchable(result.System)
	}
	return catalog.Launchable(result.System)
}

type compositionService interface {
	CoreCompositions(context.Context, []string) (map[string]protocol.CoreComposition, error)
}

type meshReadyService interface {
	GamesMeshReady(context.Context, []string) (map[string]fogcast.GameMeshReady, bool)
}

// applyMeshReadiness writes ReadyHere onto each row when a mesh execute
// session is installed. The ensure seam stays off when the service does
// not report one: launchable and composition fields stay as they are.
func applyMeshReadiness(ctx context.Context, service Service, games []gameResult) {
	provider, ok := service.(meshReadyService)
	if !ok || len(games) == 0 {
		return
	}
	ids := make([]string, 0, len(games))
	for _, game := range games {
		ids = append(ids, game.ID)
		for _, variant := range game.Variants {
			ids = append(ids, variant.ID)
		}
	}
	decisions, on := provider.GamesMeshReady(ctx, ids)
	if !on {
		return
	}
	apply := func(game *gameResult) {
		decision, ok := decisions[game.ID]
		if !ok {
			return
		}
		ready := decision.Ready
		game.ReadyHere = &ready
		if ready {
			return
		}
		game.ReadyBlock = string(decision.Block)
		game.NextAction = decision.NextAction
	}
	for i := range games {
		apply(&games[i])
		for j := range games[i].Variants {
			apply(&games[i].Variants[j])
		}
	}
}

func enrichCompositions(ctx context.Context, service Service, games []gameResult) error {
	composer, ok := service.(compositionService)
	if !ok || len(games) == 0 {
		return nil
	}
	ids := make([]string, 0, len(games))
	for _, game := range games {
		ids = append(ids, game.ID)
		for _, variant := range game.Variants {
			ids = append(ids, variant.ID)
		}
	}
	comps, err := composer.CoreCompositions(ctx, ids)
	if err != nil {
		return err
	}
	if len(comps) == 0 {
		return nil
	}
	applyComposition := func(game *gameResult) {
		comp, ok := comps[game.ID]
		if !ok {
			return
		}
		game.FirmwareRequired = comp.FirmwareRequired
		if comp.FirmwareRequired {
			game.FirmwareReady = comp.FirmwareReady
		}
		game.ROMRequired = comp.ROMRequired
		game.ROMReady = comp.ROMReady
		game.ROMID = comp.ROMID
		game.ROMMediaID = comp.ROMMediaID
		game.ExpansionID = comp.ExpansionID
		game.ExpansionReady = comp.ExpansionReady
	}
	for i := range games {
		applyComposition(&games[i])
		for j := range games[i].Variants {
			applyComposition(&games[i].Variants[j])
		}
	}
	return nil
}

type libraryCacheService interface {
	LibraryCache(context.Context) (fogcast.LibraryCache, error)
}

type romCachedService interface {
	ROMCachePresence(context.Context) (map[string]bool, bool)
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

func romCachePresence(ctx context.Context, service Service) (map[string]bool, bool) {
	provider, ok := service.(romCachedService)
	if !ok {
		return nil, false
	}
	return provider.ROMCachePresence(ctx)
}

func applyROMCached(result *gameResult, game catalog.Game, presence map[string]bool, known bool) {
	if result == nil || !known {
		return
	}
	if game.Content == nil || strings.TrimSpace(game.Content.SHA256) == "" {
		return
	}
	cached := presence[string(game.System)+"/"+game.Content.SHA256]
	result.ROMCached = &cached
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

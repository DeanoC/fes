package hostapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast-POC/catalog"
	"github.com/DeanoC/FogCast-POC/fogcast"
	"github.com/DeanoC/FogCast-POC/internal/hostapi"
	"github.com/DeanoC/FogCast-POC/libraryuser"
	"github.com/DeanoC/FogCast-POC/protocol"
)

func TestPlatformsEndpointIsEmptyWithoutLibraryQuery(t *testing.T) {
	response := serve(t, hostapi.New(&fakeService{}), http.MethodGet, "/api/v1/platforms")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"platforms"`) {
		t.Fatalf("body = %s", response.Body.String())
	}
}

func TestGamesListDoesNotLookupMetadataAndKeepsIdentityFields(t *testing.T) {
	service := &fakeService{games: []catalog.Game{{
		ID: "snes-mario-test", Title: "Mario", System: protocol.SystemSNES,
		Kind: catalog.SourceKindRaw, State: catalog.SourceStateAvailable, RootOnline: true,
	}}}
	response := serve(t, hostapi.New(service), http.MethodGet, "/api/v1/games")
	if response.Code != http.StatusOK {
		t.Fatal(response.Body.String())
	}
	var result struct {
		Games []struct {
			ID         string `json:"id"`
			Platform   string `json:"platform"`
			Launchable bool   `json:"launchable"`
		} `json:"games"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Games) != 1 || result.Games[0].Platform != "snes" || !result.Games[0].Launchable {
		t.Fatalf("%+v", result.Games)
	}
}

func TestGamesListMarksCatalogOnlyPlatformBrowseOnly(t *testing.T) {
	service := &fakeService{games: []catalog.Game{{
		ID: "gbc-zelda-test", Title: "Zelda", System: "gbc",
		Kind: catalog.SourceKindRaw, State: catalog.SourceStateAvailable, RootOnline: true,
	}}}
	response := serve(t, hostapi.New(service), http.MethodGet, "/api/v1/games")
	if response.Code != http.StatusOK {
		t.Fatal(response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"launchable":false`) {
		t.Fatalf("browse-only launchable missing: %s", response.Body.String())
	}
}

func TestAttractEndpointReturnsBoundedPlaylistWithoutQueryService(t *testing.T) {
	response := serve(t, hostapi.New(&fakeService{}), http.MethodGet, "/api/v1/library/attract?limit=8")
	if response.Code != http.StatusOK {
		t.Fatal(response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"items"`) {
		t.Fatalf("body = %s", response.Body.String())
	}
}

func TestGamesListRejectsMalformedCursorAndInvalidSort(t *testing.T) {
	service := &queryFake{pageErr: catalog.ErrInvalidQuery}
	response := serve(t, hostapi.New(service), http.MethodGet, "/api/v1/games?cursor=not-a-cursor")
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"BAD_REQUEST"`) {
		t.Fatalf("cursor status = %d body=%s", response.Code, response.Body.String())
	}
	response = serve(t, hostapi.New(&fakeService{}), http.MethodGet, "/api/v1/games?sort=bogus")
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"BAD_REQUEST"`) {
		t.Fatalf("sort status = %d body=%s", response.Code, response.Body.String())
	}
}

func TestGamesListHonorsHostPlatformLaunchPolicy(t *testing.T) {
	service := &launchableFake{
		fakeService: fakeService{games: []catalog.Game{{
			ID: "gba-mario-test", Title: "Mario", System: "gba",
			Kind: catalog.SourceKindRaw, State: catalog.SourceStateAvailable, RootOnline: true,
		}}},
		launchable: map[protocol.System]bool{"gba": true},
	}
	response := serve(t, hostapi.New(service), http.MethodGet, "/api/v1/games")
	if response.Code != http.StatusOK {
		t.Fatal(response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"launchable":true`) {
		t.Fatalf("host-emulator launchable missing: %s", response.Body.String())
	}
}

func TestGamesListSendsDumpFieldsAndParsesQueryFacets(t *testing.T) {
	service := &queryCaptureFake{page: catalog.Page{Games: []catalog.Game{{
		ID: "snes-sonic-usa", Title: "Sonic (USA)", CanonicalTitle: "Sonic", Region: "usa",
		Revision: "a", DumpFlags: "", GroupKey: "snes\x1fsonic", VariantCount: 2, Year: "1991",
		System: protocol.SystemSNES, Kind: catalog.SourceKindRaw, State: catalog.SourceStateAvailable, RootOnline: true,
	}}}}
	response := serve(t, hostapi.New(service), http.MethodGet, "/api/v1/games?region=usa&hide_prerelease=1&sort=year&grouped=1")
	if response.Code != http.StatusOK {
		t.Fatal(response.Body.String())
	}
	if service.last.Region != "usa" || !service.last.HidePrerelease || service.last.Sort != catalog.SortYear || !service.last.Grouped {
		t.Fatalf("query = %#v", service.last)
	}
	if !strings.Contains(response.Body.String(), `"canonical_title":"Sonic"`) || !strings.Contains(response.Body.String(), `"variant_count":2`) {
		t.Fatalf("dump fields missing: %s", response.Body.String())
	}
}

func TestGameDetailIncludesBoundedVariants(t *testing.T) {
	usa := catalog.Game{
		ID: "snes-sonic-usa", Title: "Sonic (USA)", CanonicalTitle: "Sonic", Region: "usa",
		GroupKey: "snes\x1fsonic", System: protocol.SystemSNES, Kind: catalog.SourceKindRaw,
		State: catalog.SourceStateAvailable, RootOnline: true,
	}
	japan := catalog.Game{
		ID: "snes-sonic-japan", Title: "Sonic (Japan)", CanonicalTitle: "Sonic", Region: "japan",
		GroupKey: "snes\x1fsonic", System: protocol.SystemSNES, Kind: catalog.SourceKindRaw,
		State: catalog.SourceStateAvailable, RootOnline: true,
	}
	overflow := catalog.Game{
		ID: "snes-sonic-overflow", Title: "Sonic (Overflow)", CanonicalTitle: "Sonic", Region: "france",
		GroupKey: "snes\x1fsonic", System: protocol.SystemSNES, Kind: catalog.SourceKindRaw,
		State: catalog.SourceStateAvailable, RootOnline: true,
	}
	variants := []catalog.Game{usa, japan}
	for index := 0; index < catalog.MaxVariantLimit-1; index++ {
		variants = append(variants, catalog.Game{
			ID: fmt.Sprintf("snes-sonic-%02d", index), Title: fmt.Sprintf("Sonic (%02d)", index),
			CanonicalTitle: "Sonic", GroupKey: "snes\x1fsonic", System: protocol.SystemSNES,
			Kind: catalog.SourceKindRaw, State: catalog.SourceStateAvailable, RootOnline: true,
		})
	}
	variants[len(variants)-1] = overflow
	if len(variants) != catalog.MaxVariantLimit+1 {
		t.Fatalf("fixture size = %d, want %d", len(variants), catalog.MaxVariantLimit+1)
	}
	service := &groupedFake{fakeService: fakeService{game: usa}, variants: variants}
	response := serve(t, hostapi.New(service), http.MethodGet, "/api/v1/games/snes-sonic-usa")
	if response.Code != http.StatusOK {
		t.Fatal(response.Body.String())
	}
	var result struct {
		VariantCount int `json:"variant_count"`
		Variants     []struct {
			ID string `json:"id"`
		} `json:"variants"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.VariantCount != catalog.MaxVariantLimit+1 {
		t.Fatalf("variant_count = %d, want %d", result.VariantCount, catalog.MaxVariantLimit+1)
	}
	if len(result.Variants) != catalog.MaxVariantLimit {
		t.Fatalf("variants = %d, want %d", len(result.Variants), catalog.MaxVariantLimit)
	}
	ids := make([]string, 0, len(result.Variants))
	for _, variant := range result.Variants {
		ids = append(ids, variant.ID)
	}
	if !strings.Contains(strings.Join(ids, ","), "snes-sonic-japan") {
		t.Fatalf("bounded variants missing japan: %v", ids)
	}
	if strings.Contains(strings.Join(ids, ","), "snes-sonic-overflow") {
		t.Fatalf("bounded variants included overflow: %v", ids)
	}
}

func TestFacetsEndpointReturnsEmptyWithoutCatalog(t *testing.T) {
	response := serve(t, hostapi.New(&fakeService{}), http.MethodGet, "/api/v1/library/facets")
	if response.Code != http.StatusOK {
		t.Fatal(response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"genres"`) || !strings.Contains(response.Body.String(), `"years"`) {
		t.Fatalf("body = %s", response.Body.String())
	}
}

type queryFake struct {
	fakeService
	pageErr error
}

func (s *queryFake) QueryGames(_ context.Context, query catalog.Query) (catalog.Page, error) {
	return catalog.Page{}, s.pageErr
}

func (s *queryFake) Platforms(context.Context) ([]catalog.PlatformInfo, error) {
	return nil, nil
}

type queryCaptureFake struct {
	fakeService
	page catalog.Page
	last catalog.Query
}

func (s *queryCaptureFake) QueryGames(_ context.Context, query catalog.Query) (catalog.Page, error) {
	s.last = query
	return s.page, nil
}

func (s *queryCaptureFake) Platforms(context.Context) ([]catalog.PlatformInfo, error) {
	return nil, nil
}

type groupedFake struct {
	fakeService
	variants []catalog.Game
}

func (s *groupedFake) GamesInGroup(context.Context, string) ([]catalog.Game, error) {
	return append([]catalog.Game(nil), s.variants...), nil
}

type launchableFake struct {
	fakeService
	launchable map[protocol.System]bool
}

func (s *launchableFake) PlatformLaunchable(system protocol.System) bool {
	return s.launchable[system]
}

type collectionFake struct {
	fakeService
	last        catalog.Query
	page        catalog.Page
	collections map[string]libraryuser.Collection
	members     map[string]map[string]bool
}

func (s *collectionFake) QueryGames(_ context.Context, query catalog.Query) (catalog.Page, error) {
	s.last = query
	if s.page.Games != nil || s.page.NextCursor != "" {
		return s.page, nil
	}
	return catalog.Page{Games: append([]catalog.Game(nil), s.games...)}, nil
}

func (s *collectionFake) Platforms(context.Context) ([]catalog.PlatformInfo, error) {
	return nil, nil
}

func (s *collectionFake) Collections(context.Context) ([]libraryuser.Collection, error) {
	listed := make([]libraryuser.Collection, 0, len(s.collections))
	for _, item := range s.collections {
		listed = append(listed, item)
	}
	return listed, nil
}

func (s *collectionFake) UpsertCollection(_ context.Context, id, name string) (libraryuser.Collection, error) {
	if err := libraryuser.ValidateCollectionID(id); err != nil {
		return libraryuser.Collection{}, err
	}
	if name == "" {
		name = id
	}
	if err := libraryuser.ValidateCollectionName(name); err != nil {
		return libraryuser.Collection{}, err
	}
	if s.collections == nil {
		s.collections = map[string]libraryuser.Collection{}
	}
	item := s.collections[id]
	if item.CreatedAt == 0 {
		item.CreatedAt = 1
	}
	item.ID = id
	item.Name = name
	s.collections[id] = item
	return item, nil
}

func (s *collectionFake) DeleteCollection(_ context.Context, id string) error {
	if s.collections == nil {
		return libraryuser.ErrNotFound
	}
	if _, ok := s.collections[id]; !ok {
		return libraryuser.ErrNotFound
	}
	delete(s.collections, id)
	delete(s.members, id)
	return nil
}

func (s *collectionFake) SetCollectionMember(_ context.Context, collectionID, gameID string, member bool) error {
	if _, ok := s.collections[collectionID]; !ok {
		return libraryuser.ErrNotFound
	}
	if protocol.ValidateGameID(gameID) != nil {
		return libraryuser.ErrInvalid
	}
	if s.members == nil {
		s.members = map[string]map[string]bool{}
	}
	if s.members[collectionID] == nil {
		s.members[collectionID] = map[string]bool{}
	}
	if member {
		s.members[collectionID][gameID] = true
		return nil
	}
	delete(s.members[collectionID], gameID)
	return nil
}

func (s *collectionFake) GameCollectionIDs(_ context.Context, gameID string) ([]string, error) {
	ids := make([]string, 0)
	for collectionID, members := range s.members {
		if members[gameID] {
			ids = append(ids, collectionID)
		}
	}
	return ids, nil
}

func (s *collectionFake) CollectionIDsByGame(ctx context.Context, ids []string) (map[string][]string, error) {
	result := map[string][]string{}
	for _, id := range ids {
		owned, err := s.GameCollectionIDs(ctx, id)
		if err != nil {
			return nil, err
		}
		if len(owned) > 0 {
			result[id] = owned
		}
	}
	return result, nil
}

func TestCollectionsCRUDMirrorsFavoritesEmptyBody(t *testing.T) {
	service := &collectionFake{}
	handler := hostapi.New(service)
	created := serve(t, handler, http.MethodPut, "/api/v1/library/collections/weekend-queue?name=Weekend%20Queue")
	if created.Code != http.StatusOK || !strings.Contains(created.Body.String(), `"id":"weekend-queue"`) || !strings.Contains(created.Body.String(), `"name":"Weekend Queue"`) {
		t.Fatalf("create = %d %s", created.Code, created.Body.String())
	}
	listed := serve(t, handler, http.MethodGet, "/api/v1/library/collections")
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"weekend-queue"`) {
		t.Fatalf("list = %d %s", listed.Code, listed.Body.String())
	}
	added := serve(t, handler, http.MethodPut, "/api/v1/library/collections/weekend-queue/snes-mario-test")
	if added.Code != http.StatusOK || !strings.Contains(added.Body.String(), `"member":true`) || !strings.Contains(added.Body.String(), `"id":"snes-mario-test"`) {
		t.Fatalf("add = %d %s", added.Code, added.Body.String())
	}
	removed := serve(t, handler, http.MethodDelete, "/api/v1/library/collections/weekend-queue/snes-mario-test")
	if removed.Code != http.StatusOK || !strings.Contains(removed.Body.String(), `"member":false`) {
		t.Fatalf("remove = %d %s", removed.Code, removed.Body.String())
	}
	renamed := serve(t, handler, http.MethodPut, "/api/v1/library/collections/weekend-queue?name=Saturday")
	if renamed.Code != http.StatusOK || !strings.Contains(renamed.Body.String(), `"name":"Saturday"`) {
		t.Fatalf("rename = %d %s", renamed.Code, renamed.Body.String())
	}
	deleted := serve(t, handler, http.MethodDelete, "/api/v1/library/collections/weekend-queue")
	if deleted.Code != http.StatusOK || !strings.Contains(deleted.Body.String(), `"id":"weekend-queue"`) {
		t.Fatalf("delete = %d %s", deleted.Code, deleted.Body.String())
	}
	missing := serve(t, handler, http.MethodDelete, "/api/v1/library/collections/weekend-queue")
	if missing.Code != http.StatusNotFound || !strings.Contains(missing.Body.String(), `"COLLECTION_NOT_FOUND"`) {
		t.Fatalf("missing = %d %s", missing.Code, missing.Body.String())
	}
	reserved := serve(t, handler, http.MethodPut, "/api/v1/library/collections/favorites")
	if reserved.Code != http.StatusBadRequest || !strings.Contains(reserved.Body.String(), `"BAD_REQUEST"`) {
		t.Fatalf("reserved = %d %s", reserved.Code, reserved.Body.String())
	}
}

func TestGamesListPassesCustomCollectionAndSmartKeys(t *testing.T) {
	service := &collectionFake{
		fakeService: fakeService{games: []catalog.Game{{
			ID: "snes-mario-test", Title: "Mario", System: protocol.SystemSNES,
			Kind: catalog.SourceKindRaw, State: catalog.SourceStateAvailable, RootOnline: true,
		}}},
		collections: map[string]libraryuser.Collection{"weekend-queue": {ID: "weekend-queue", Name: "Weekend Queue", CreatedAt: 1}},
		members:     map[string]map[string]bool{"weekend-queue": {"snes-mario-test": true}},
	}
	response := serve(t, hostapi.New(service), http.MethodGet, "/api/v1/games?collection=weekend-queue")
	if response.Code != http.StatusOK {
		t.Fatal(response.Body.String())
	}
	if service.last.Collection != "weekend-queue" {
		t.Fatalf("query = %#v", service.last)
	}
	if !strings.Contains(response.Body.String(), `"collections":["weekend-queue"]`) {
		t.Fatalf("membership missing: %s", response.Body.String())
	}
	response = serve(t, hostapi.New(service), http.MethodGet, "/api/v1/games?collection=favorites")
	if response.Code != http.StatusOK || service.last.Collection != "favorites" {
		t.Fatalf("favorites query = %#v status=%d %s", service.last, response.Code, response.Body.String())
	}
}

func TestCollectionMemberRejectsInvalidGameID(t *testing.T) {
	service := &collectionFake{
		collections: map[string]libraryuser.Collection{"weekend-queue": {ID: "weekend-queue", Name: "Weekend Queue", CreatedAt: 1}},
	}
	response := serve(t, hostapi.New(service), http.MethodPut, "/api/v1/library/collections/weekend-queue/NotAGame")
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"BAD_REQUEST"`) {
		t.Fatalf("status = %d %s", response.Code, response.Body.String())
	}
}

type settingsFake struct {
	fakeService
	settings fogcast.LibraryConfig
}

func (s *settingsFake) LibrarySettings() fogcast.LibraryConfig {
	return s.settings
}

func (s *settingsFake) SetLibrarySettings(_ context.Context, next fogcast.LibraryConfig) error {
	s.settings = next
	return nil
}

func (s *settingsFake) PatchLibrarySettings(_ context.Context, patch fogcast.LibraryConfigPatch) error {
	next := s.settings
	if patch.AttractIdleSeconds != nil {
		next.AttractIdleSeconds = *patch.AttractIdleSeconds
	}
	if patch.PreferredRegions != nil {
		next.PreferredRegions = append([]string(nil), *patch.PreferredRegions...)
	}
	normalized, err := fogcast.NormalizeLibraryConfig(next)
	if err != nil {
		return &protocol.APIError{Code: protocol.CodeBadRequest, Message: "library settings request is invalid"}
	}
	s.settings = normalized
	return nil
}

func TestLibrarySettingsGetPutPatchEmptyOrJSON(t *testing.T) {
	service := &settingsFake{settings: fogcast.LibraryConfig{
		AttractIdleSeconds: 60,
		PreferredRegions:   []string{"usa", "world"},
	}}
	handler := hostapi.New(service)
	got := serve(t, handler, http.MethodGet, "/api/v1/library/settings")
	if got.Code != http.StatusOK || !strings.Contains(got.Body.String(), `"attract_idle_seconds":60`) || !strings.Contains(got.Body.String(), `"usa"`) {
		t.Fatalf("get = %d %s", got.Code, got.Body.String())
	}
	if strings.Contains(got.Body.String(), "token") || strings.Contains(got.Body.String(), "client_secret") || strings.Contains(got.Body.String(), "/api/v1/health") {
		t.Fatalf("get leaked secrets or health: %s", got.Body.String())
	}

	emptyPut := serveBody(t, handler, http.MethodPut, "/api/v1/library/settings", "")
	if emptyPut.Code != http.StatusOK || service.settings.AttractIdleSeconds != 60 {
		t.Fatalf("empty put = %d %s settings=%+v", emptyPut.Code, emptyPut.Body.String(), service.settings)
	}

	replaced := serveBody(t, handler, http.MethodPut, "/api/v1/library/settings", `{"attract_idle_seconds":12,"preferred_regions":["japan","europe"]}`)
	if replaced.Code != http.StatusOK || service.settings.AttractIdleSeconds != 12 || strings.Join(service.settings.PreferredRegions, ",") != "japan,europe" {
		t.Fatalf("put = %d %s settings=%+v", replaced.Code, replaced.Body.String(), service.settings)
	}

	patched := serveBody(t, handler, http.MethodPatch, "/api/v1/library/settings", `{"attract_idle_seconds":8}`)
	if patched.Code != http.StatusOK || service.settings.AttractIdleSeconds != 8 || strings.Join(service.settings.PreferredRegions, ",") != "japan,europe" {
		t.Fatalf("patch = %d %s settings=%+v", patched.Code, patched.Body.String(), service.settings)
	}
	patchedRegions := serveBody(t, handler, http.MethodPatch, "/api/v1/library/settings", `{"preferred_regions":["usa"]}`)
	if patchedRegions.Code != http.StatusOK || service.settings.AttractIdleSeconds != 8 || strings.Join(service.settings.PreferredRegions, ",") != "usa" {
		t.Fatalf("regions patch = %d %s settings=%+v", patchedRegions.Code, patchedRegions.Body.String(), service.settings)
	}
	overflow := serveBody(t, handler, http.MethodPut, "/api/v1/library/settings", fmt.Sprintf(`{"attract_idle_seconds":%d,"preferred_regions":["usa"]}`, fogcast.MaxAttractIdleSeconds+1000))
	if overflow.Code != http.StatusOK || service.settings.AttractIdleSeconds != fogcast.MaxAttractIdleSeconds {
		t.Fatalf("overflow put = %d %s settings=%+v", overflow.Code, overflow.Body.String(), service.settings)
	}

	unknown := serveBody(t, handler, http.MethodPut, "/api/v1/library/settings", `{"attract_idle_seconds":12,"preferred_regions":["usa"],"token":"nope"}`)
	if unknown.Code != http.StatusBadRequest || !strings.Contains(unknown.Body.String(), `"BAD_REQUEST"`) {
		t.Fatalf("unknown field = %d %s", unknown.Code, unknown.Body.String())
	}
	negative := serveBody(t, handler, http.MethodPatch, "/api/v1/library/settings", `{"attract_idle_seconds":-1}`)
	if negative.Code != http.StatusBadRequest {
		t.Fatalf("negative = %d %s", negative.Code, negative.Body.String())
	}
	missing := serve(t, hostapi.New(&fakeService{}), http.MethodPut, "/api/v1/library/settings")
	if missing.Code != http.StatusNotFound || !strings.Contains(missing.Body.String(), `"SETTINGS_UNAVAILABLE"`) {
		t.Fatalf("missing writer = %d %s", missing.Code, missing.Body.String())
	}
	defaults := serve(t, hostapi.New(&fakeService{}), http.MethodGet, "/api/v1/library/settings")
	if defaults.Code != http.StatusOK || !strings.Contains(defaults.Body.String(), `"attract_idle_seconds":60`) {
		t.Fatalf("defaults = %d %s", defaults.Code, defaults.Body.String())
	}
}

func serveBody(t *testing.T, handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Host = "127.0.0.1"
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

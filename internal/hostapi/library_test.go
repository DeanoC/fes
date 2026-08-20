package hostapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast-POC/catalog"
	"github.com/DeanoC/FogCast-POC/internal/hostapi"
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

func TestGamesListMarksUnmappedPlatformBrowseOnly(t *testing.T) {
	service := &fakeService{games: []catalog.Game{{
		ID: "nes-mario-test", Title: "Mario", System: "nes",
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
			ID: "nes-mario-test", Title: "Mario", System: "nes",
			Kind: catalog.SourceKindRaw, State: catalog.SourceStateAvailable, RootOnline: true,
		}}},
		launchable: map[protocol.System]bool{"nes": true},
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

package hostapi_test

import (
	"context"
	"encoding/json"
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
	response = serve(t, hostapi.New(&fakeService{}), http.MethodGet, "/api/v1/games?sort=year")
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

type queryFake struct {
	fakeService
	pageErr error
}

func (s *queryFake) QueryGames(context.Context, catalog.Query) (catalog.Page, error) {
	return catalog.Page{}, s.pageErr
}

func (s *queryFake) Platforms(context.Context) ([]catalog.PlatformInfo, error) {
	return nil, nil
}

type launchableFake struct {
	fakeService
	launchable map[protocol.System]bool
}

func (s *launchableFake) PlatformLaunchable(system protocol.System) bool {
	return s.launchable[system]
}

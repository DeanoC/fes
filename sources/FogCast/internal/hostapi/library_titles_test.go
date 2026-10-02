package hostapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/internal/meshcontent"
	"github.com/DeanoC/FogCast/protocol"
)

type libraryTitlesService struct {
	fakeService
	rows    []fogcast.MeshBackendRow
	skipped []fogcast.MeshSkip
}

func (s *libraryTitlesService) MeshBackendLibrary(context.Context) ([]fogcast.MeshBackendRow, []fogcast.MeshSkip) {
	return s.rows, s.skipped
}

func TestLibraryTitlesRoute(t *testing.T) {
	media := strings.Repeat("ab", 32)
	id, err := meshcontent.ParseContentID("sha256:" + media)
	if err != nil {
		t.Fatal(err)
	}
	const (
		coreID = "fpga-data-storm-1-00-39c4d68f01fa"
		romID  = "sms-data-storm-1-00-3558e845cf24"
		kitID  = "01234567-89ab-cdef-0123-456789abcdef"
	)
	row := fogcast.MeshBackendRow{
		TitleID:        coreID,
		System:         "sms",
		ContentIDs:     []meshcontent.ContentID{id},
		ContentSources: []fogcast.MeshContentSource{{ContentID: id, NodeIDs: []string{}}},
		Options: []fogcast.MeshBackendOption{{
			Entry: meshcontent.Entry{
				TitleID: coreID,
				System:  "sms",
				Execute: []meshcontent.Execute{{Kind: meshcontent.ExecuteFPGANative}},
				Slots: []meshcontent.Slot{
					meshcontent.PackageSlot(meshcontent.PackageABI{PackageID: media, ABI: "fes.application", Major: 1}),
					meshcontent.PrimaryMediaSlot(id),
				},
			},
			CoreID: "fes.sms",
			Nodes:  []fogcast.MeshBackendNode{{NodeID: kitID, Available: true}},
		}, {
			Entry: meshcontent.Entry{
				TitleID: romID,
				System:  "sms",
				Execute: []meshcontent.Execute{{Kind: meshcontent.ExecuteNativeEmu}},
				Slots:   []meshcontent.Slot{meshcontent.PrimaryMediaSlot(id)},
			},
			HostLocal: true,
			Nodes:     []fogcast.MeshBackendNode{},
		}},
	}
	service := &libraryTitlesService{
		fakeService: fakeService{games: []catalog.Game{{
			ID: "snes-mario", Title: "Mario", System: protocol.SystemSNES,
			State: catalog.SourceStateAvailable, RootOnline: true,
		}}},
		rows: []fogcast.MeshBackendRow{row},
	}
	handler := hostapi.New(service)
	response := serve(t, handler, http.MethodGet, "/api/v1/library/titles")
	if response.Code != http.StatusOK {
		t.Fatalf("status %d %s", response.Code, response.Body.String())
	}
	library, err := fogcast.MeshLibraryTitles(service.rows)
	if err != nil {
		t.Fatal(err)
	}
	want, err := json.Marshal(library)
	if err != nil {
		t.Fatal(err)
	}
	var gotBody, wantBody any
	if err := json.Unmarshal(response.Body.Bytes(), &gotBody); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(want, &wantBody); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotBody, wantBody) {
		t.Fatalf("body %s\nwant %s", response.Body.String(), want)
	}
	body := response.Body.String()
	for _, absent := range []string{"ready_here", "host_only", "path", `"node_ids":[`, `"nodes":null`} {
		if absent == `"node_ids":[` {
			if strings.Contains(body, `"node_ids":["`) {
				t.Fatalf("provenance in %s", body)
			}
			continue
		}
		if strings.Contains(body, absent) {
			t.Fatalf("document contains %q: %s", absent, body)
		}
	}
	queried := serve(t, handler, http.MethodGet, "/api/v1/library/titles?system=nes")
	if queried.Body.String() != response.Body.String() {
		t.Fatalf("query changed the library\n%s\n%s", queried.Body.String(), response.Body.String())
	}
	games := serve(t, handler, http.MethodGet, "/api/v1/games")
	if games.Code != http.StatusOK || !strings.Contains(games.Body.String(), `"games"`) || strings.Contains(games.Body.String(), "content_sources") || strings.Contains(games.Body.String(), coreID) {
		t.Fatalf("games changed: %d %s", games.Code, games.Body.String())
	}

	empty := serve(t, hostapi.New(&fakeService{}), http.MethodGet, "/api/v1/library/titles")
	if empty.Code != http.StatusOK || strings.TrimSpace(empty.Body.String()) != `{"titles":[]}` {
		t.Fatalf("empty library: %d %s", empty.Code, empty.Body.String())
	}

	service.rows = nil
	service.skipped = []fogcast.MeshSkip{{Reason: fogcast.MeshSkipCatalogUnavailable}}
	failed := serve(t, hostapi.New(service), http.MethodGet, "/api/v1/library/titles")
	if failed.Code != http.StatusInternalServerError || !strings.Contains(failed.Body.String(), "catalog is unavailable") || strings.Contains(failed.Body.String(), `"titles"`) {
		t.Fatalf("catalog failure: %d %s", failed.Code, failed.Body.String())
	}

	bad := meshcontent.ContentID{Algorithm: "sha256", Digest: "ZZ"}
	service.skipped = nil
	service.rows = []fogcast.MeshBackendRow{{
		TitleID:        coreID,
		System:         "sms",
		ContentIDs:     []meshcontent.ContentID{bad},
		ContentSources: []fogcast.MeshContentSource{{ContentID: bad, NodeIDs: []string{}}},
		Options:        row.Options,
	}}
	rejected := serve(t, hostapi.New(service), http.MethodGet, "/api/v1/library/titles")
	if rejected.Code != http.StatusInternalServerError || strings.Contains(rejected.Body.String(), "ZZ") || strings.Contains(rejected.Body.String(), `"titles"`) {
		t.Fatalf("malformed id: %d %s", rejected.Code, rejected.Body.String())
	}
}

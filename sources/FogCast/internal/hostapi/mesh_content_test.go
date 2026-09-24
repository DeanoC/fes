package hostapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/internal/discovery"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/internal/meshcontent"
	"github.com/DeanoC/FogCast/protocol"
)

func TestPhase2ContentShapeDoesNotChangePhase1Ready(t *testing.T) {
	bios := meshcontent.SumSHA256([]byte("bios"))
	entry := meshcontent.Entry{
		TitleID:    "coleco-frogger",
		System:     "coleco",
		Launchable: true,
		Execute:    []meshcontent.Execute{{Kind: discovery.ExecuteFPGANative}},
		Slots: []meshcontent.Slot{
			meshcontent.PackageSlot(meshcontent.PackageABI{
				PackageID: strings.Repeat("ab", 32),
				ABI:       "fes.application",
				Major:     1,
			}),
			meshcontent.BIOSSlot(bios),
		},
	}
	distant := meshcontent.NewCache()
	if err := distant.Hold(bios); err != nil {
		t.Fatal(err)
	}
	ready, block := meshcontent.ReadyHere(entry, meshcontent.Bound{
		Execute: true, LeaseFree: true, MeshMajorOK: true,
		Distant:  distant,
		Packages: []string{strings.Repeat("ab", 32)},
		ABIs:     []meshcontent.EligibleABI{{ID: "fes.application", Major: 1}},
	})
	if ready || block != meshcontent.BlockDistant {
		t.Fatalf("phase 2 helper ready=%v block=%s", ready, block)
	}
	if ready, block := discovery.ReadyForBoundExecutor(true, discovery.Advertisement{
		Capabilities: discovery.Capabilities{Execute: []discovery.Execute{{Kind: discovery.ExecuteFPGANative}}},
	}, nil); !ready || block != meshcontent.BlockNone {
		t.Fatal("phase 1 composition Ready flipped off")
	}

	id := "01234567-89ab-cdef-0123-456789abcdef"
	service := &meshInventoryService{fakeService: &fakeService{}, nodes: []fogcast.MeshNode{{
		NodeID: id, TargetID: id, Mesh: discovery.MeshProtocol,
	}}}
	response := httptest.NewRecorder()
	hostapi.New(service).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/v1/mesh/nodes", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status %d", response.Code)
	}
	body := response.Body.String()
	for _, absent := range []string{`"title_id"`, `"content"`, "sha256:", bios.Digest} {
		if strings.Contains(body, absent) {
			t.Fatalf("inventory grew a catalog field %s: %s", absent, body)
		}
	}
}

type meshReadyList struct {
	launchableFake
	on        bool
	decisions map[string]fogcast.GameMeshReady
}

func (m *meshReadyList) GamesMeshReady(context.Context, []string) (map[string]fogcast.GameMeshReady, bool) {
	return m.decisions, m.on
}

func (m *meshReadyList) SessionExecution(_ context.Context, id string) (string, error) {
	if id == "snes-mario" {
		return fogcast.ExecutionHostOnly, nil
	}
	return "", nil
}

func (m *meshReadyList) CoreCompositions(context.Context, []string) (map[string]protocol.CoreComposition, error) {
	return map[string]protocol.CoreComposition{
		"fpga-coleco-dk": {FirmwareRequired: true, FirmwareReady: true},
		"fpga-zx81-maze": {},
		"fpga-frogger":   {FirmwareRequired: true, FirmwareReady: false},
	}, nil
}

func meshReadyCatalog() []catalog.Game {
	return []catalog.Game{
		{ID: "fpga-coleco-dk", Title: "Donkey Kong", System: protocol.SystemColecoVision, Kind: catalog.SourceKindCorePackage, State: catalog.SourceStateAvailable, RootOnline: true},
		{ID: "fpga-zx81-maze", Title: "3D Monster Maze", System: "zx81", Kind: catalog.SourceKindCorePackage, State: catalog.SourceStateAvailable, RootOnline: true},
		{ID: "fpga-frogger", Title: "Frogger", System: protocol.SystemColecoVision, Kind: catalog.SourceKindCorePackage, State: catalog.SourceStateAvailable, RootOnline: true},
		{ID: "coleco-dk-cart", Title: "Donkey Kong", System: protocol.SystemColecoVision, Kind: catalog.SourceKindRaw, State: catalog.SourceStateAvailable, RootOnline: true},
		{ID: "snes-mario", Title: "Super Mario World", System: protocol.SystemSNES, Kind: catalog.SourceKindRaw, State: catalog.SourceStateAvailable, RootOnline: true},
	}
}

func TestGamesSeamOffKeepsLaunchableTitlesReady(t *testing.T) {
	service := &meshReadyList{
		launchableFake: launchableFake{
			fakeService: fakeService{games: meshReadyCatalog(), game: meshReadyCatalog()[0]},
			launchable:  map[protocol.System]bool{protocol.SystemColecoVision: false, "zx81": false, protocol.SystemSNES: true},
		},
	}
	response := serve(t, hostapi.New(service), http.MethodGet, "/api/v1/games")
	if response.Code != http.StatusOK {
		t.Fatal(response.Body.String())
	}
	body := response.Body.String()
	for _, absent := range []string{`"ready_here"`, `"ready_block"`, `"next_action"`} {
		if strings.Contains(body, absent) {
			t.Fatalf("seam off grew %s: %s", absent, body)
		}
	}
	var page struct {
		Games []hostclient.Game `json:"games"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	byID := map[string]hostclient.Game{}
	for _, game := range page.Games {
		byID[game.ID] = game
	}
	for _, id := range []string{"fpga-coleco-dk", "fpga-zx81-maze", "snes-mario"} {
		game := byID[id]
		if !game.Launchable || !game.LaunchEligible() || game.ReadyHere != nil {
			t.Fatalf("%s launchable=%v eligible=%v ready=%v exec=%s", id, game.Launchable, game.LaunchEligible(), game.ReadyHere, game.Execution)
		}
	}
	if byID["snes-mario"].Execution != fogcast.ExecutionHostOnly {
		t.Fatalf("host execution %s", byID["snes-mario"].Execution)
	}
	frogger := byID["fpga-frogger"]
	if !frogger.Launchable || frogger.LaunchEligible() || frogger.LaunchBlock() != hostclient.LaunchMissingFirmware {
		t.Fatalf("frogger launchable=%v block=%q", frogger.Launchable, frogger.LaunchBlock())
	}
	cart := byID["coleco-dk-cart"]
	if cart.Launchable || cart.LaunchBlock() != hostclient.LaunchBrowseOnly {
		t.Fatalf("raw cart launchable=%v block=%q", cart.Launchable, cart.LaunchBlock())
	}
}

func TestGamesReadyHereJSON(t *testing.T) {
	service := &meshReadyList{
		launchableFake: launchableFake{
			fakeService: fakeService{games: meshReadyCatalog(), game: meshReadyCatalog()[0]},
			launchable:  map[protocol.System]bool{protocol.SystemColecoVision: false, "zx81": false, protocol.SystemSNES: true},
		},
		on: true,
		decisions: map[string]fogcast.GameMeshReady{
			"fpga-coleco-dk": {Ready: false, Block: meshcontent.BlockDistant, NextAction: meshcontent.NextAction(meshcontent.BlockDistant)},
			"snes-mario":     {Ready: true},
		},
	}
	response := serve(t, hostapi.New(service), http.MethodGet, "/api/v1/games")
	if response.Code != http.StatusOK {
		t.Fatal(response.Body.String())
	}
	var page struct {
		Games []hostclient.Game `json:"games"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	byID := map[string]hostclient.Game{}
	for _, game := range page.Games {
		byID[game.ID] = game
	}
	coleco := byID["fpga-coleco-dk"]
	if !coleco.Launchable || coleco.ReadyHere == nil || *coleco.ReadyHere || coleco.ReadyBlock != string(meshcontent.BlockDistant) || coleco.NextAction != "fetch_here" || coleco.LaunchEligible() {
		t.Fatalf("distant coleco %+v", coleco)
	}
	if !strings.Contains(response.Body.String(), `"ready_here":false`) || !strings.Contains(response.Body.String(), `"next_action":"fetch_here"`) {
		t.Fatalf("json %s", response.Body.String())
	}
	mario := byID["snes-mario"]
	if mario.ReadyHere == nil || !*mario.ReadyHere || mario.NextAction != "" || !mario.LaunchEligible() {
		t.Fatalf("ready host title %+v", mario)
	}
	zx := byID["fpga-zx81-maze"]
	if zx.ReadyHere != nil || !zx.LaunchEligible() {
		t.Fatalf("unprojected zx81 %+v", zx)
	}
	detail := serve(t, hostapi.New(service), http.MethodGet, "/api/v1/games/fpga-coleco-dk")
	if detail.Code != http.StatusOK {
		t.Fatal(detail.Body.String())
	}
	var one hostclient.Game
	if err := json.Unmarshal(detail.Body.Bytes(), &one); err != nil {
		t.Fatal(err)
	}
	if one.ReadyHere == nil || *one.ReadyHere || one.ReadyBlock != "distant" || one.NextAction != "fetch_here" || !one.Launchable {
		t.Fatalf("detail %+v", one)
	}
}

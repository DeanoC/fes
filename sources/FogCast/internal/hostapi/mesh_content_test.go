package hostapi_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/internal/discovery"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/internal/meshcontent"
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
	if !discovery.ReadyForBoundExecutor(true, discovery.Advertisement{
		Capabilities: discovery.Capabilities{Execute: []discovery.Execute{{Kind: discovery.ExecuteFPGANative}}},
	}) {
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

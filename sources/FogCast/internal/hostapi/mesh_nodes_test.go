package hostapi_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/internal/discovery"
	"github.com/DeanoC/FogCast/internal/hostapi"
)

type meshInventoryService struct {
	*fakeService
	nodes []fogcast.MeshNode
}

func (s *meshInventoryService) MeshNodes() []fogcast.MeshNode { return s.nodes }

func TestMeshNodesRouteServesInventoryWithoutLeaseSecrets(t *testing.T) {
	id := "01234567-89ab-cdef-0123-456789abcdef"
	service := &meshInventoryService{fakeService: &fakeService{}, nodes: []fogcast.MeshNode{{
		NodeID:   id,
		TargetID: id,
		Mesh:     discovery.MeshProtocol,
		Cap:      "display_sink,execute:fpga_native,input_source",
		Capabilities: discovery.Capabilities{
			Execute:     []discovery.Execute{{Kind: discovery.ExecuteFPGANative}},
			DisplaySink: true,
			InputSource: true,
		},
	}}}
	response := httptest.NewRecorder()
	hostapi.New(service).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/v1/mesh/nodes", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status %d %s", response.Code, response.Body)
	}
	body := response.Body.String()
	for _, want := range []string{id, `"mesh":"1.0"`, `execute:fpga_native`, `"display_sink":true`} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %s: %s", want, body)
		}
	}
	if strings.Contains(strings.ToLower(body), "token") || strings.Contains(strings.ToLower(body), "lease") {
		t.Fatalf("inventory published a secret: %s", body)
	}
}

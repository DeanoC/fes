package hostapi_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/internal/hostapi"
)

type removeLauncherService struct {
	coreLibraryAPIService
	settings fogcast.LibraryConfig
	removed  []string
}

func (s *removeLauncherService) LibrarySettings() fogcast.LibraryConfig { return s.settings }
func (s *removeLauncherService) RemoveCoreEntry(_ context.Context, id string) error {
	s.removed = append(s.removed, id)
	return nil
}

func TestPairedLauncherCoreRemoval(t *testing.T) {
	service := &removeLauncherService{settings: twoKitSettings("kit-a")}
	handler, err := hostapi.NewLauncherHandler(hostapi.New(service), distinctKitConfig())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path, token, target string
		status              int
	}{
		{"/api/v1/library/core-entries/fpga-zx81", launcherTokenB, launcherIDB, http.StatusOK},
		{"/api/v1/library/core-entries/Nope", launcherTokenB, launcherIDB, http.StatusNotFound},
		{"/api/v1/library/core-entries/fpga-zx81/rom", launcherTokenB, launcherIDB, http.StatusNotFound},
		{"/api/v1/library/core-entries/fpga-zx81", "bad", launcherIDB, http.StatusUnauthorized},
		{"/api/v1/library/core-entries/fpga-zx81", launcherTokenB, "", http.StatusForbidden},
	} {
		response := kitCall(handler, http.MethodDelete, tc.path, tc.token, tc.target, "")
		if response.Code != tc.status {
			t.Fatalf("DELETE %s = %d %s", tc.path, response.Code, response.Body.String())
		}
		if tc.status == http.StatusOK && !strings.Contains(response.Body.String(), `"game_id":"fpga-zx81"`) {
			t.Fatalf("response %s", response.Body.String())
		}
	}
	if len(service.removed) != 1 || service.removed[0] != "fpga-zx81" {
		t.Fatalf("forwarded removals: %v", service.removed)
	}
}

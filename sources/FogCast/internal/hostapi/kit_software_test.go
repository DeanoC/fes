package hostapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/appliance/software"
	"github.com/DeanoC/FogCast/appliance/softwarecontrol"
	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/protocol"
)

func TestHealthProjectsActiveSoftwareWithoutOperationDetails(t *testing.T) {
	active, base, selected := strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64)
	private := "private-operation-detail"
	status := &softwarecontrol.Status{State: softwarecontrol.State{Selected: selected, Generation: 9, Pending: &softwarecontrol.Operation{Reason: private}, Receipts: map[string]softwarecontrol.Receipt{private: {Reason: private}}}, ActiveBundle: active, BaseIdentity: base, StorageError: private, ServiceFault: private}
	for _, tc := range []struct {
		name     string
		software *softwarecontrol.Status
		err      error
		want     bool
	}{
		{"managed", status, nil, true}, {"unmanaged", nil, nil, false}, {"unreachable", status, errors.New("unreachable"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := &fakeService{health: protocol.Health{APIVersion: "v1", Software: tc.software, Artifacts: &protocol.Artifacts{RuntimeCommit: strings.Repeat("d", 40)}}, healthErr: tc.err}
			response := serve(t, hostapi.New(service), http.MethodGet, "/api/v1/health")
			if strings.Contains(response.Body.String(), private) {
				t.Fatal("private supervisor status leaked")
			}
			var out struct {
				Target struct {
					APIVersion string         `json:"api_version"`
					Software   map[string]any `json:"software"`
				} `json:"target"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &out); err != nil {
				t.Fatal(err)
			}
			if tc.want {
				expected := map[string]any{"active_bundle": active, "base_identity": base, "selected": selected, "generation": float64(9)}
				if !reflect.DeepEqual(out.Target.Software, expected) || out.Target.APIVersion != "v1" {
					t.Fatalf("unexpected projection: %+v", out)
				}
			} else if out.Target.Software != nil {
				t.Fatal("missing/failed target software synthesized")
			}
			if tc.err != nil && out.Target.APIVersion != "" {
				t.Fatal("failed health forwarded stale contract")
			}
		})
	}
}

type inventoryService struct {
	*fakeService
	releases []software.Release
	err      error
	calls    int
	paired   string
}

func (s *inventoryService) KitSoftwareReleases(ctx context.Context) ([]software.Release, error) {
	s.calls++
	s.paired = fogcast.PairedTargetFromContext(ctx)
	return s.releases, s.err
}
func TestSoftwareInventoryRouteIsReadOnlyAndScoped(t *testing.T) {
	service := &inventoryService{fakeService: &fakeService{}, releases: []software.Release{{ID: strings.Repeat("a", 64), Manifest: software.Manifest{Interfaces: map[string]string{"runtime-protocol": "2"}}}}}
	handler := hostapi.New(service)
	req := httptest.NewRequest("GET", "/api/v1/kit/software/releases", nil)
	req.Host = "127.0.0.1"
	req = req.WithContext(fogcast.WithPairedTarget(req.Context(), "paired-kit"))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	var releases []software.Release
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &releases) != nil || !reflect.DeepEqual(releases, service.releases) || service.paired != "paired-kit" {
		t.Fatalf("bad inventory: %s", response.Body.String())
	}
	for _, method := range []string{"POST", "PUT", "DELETE"} {
		response = serve(t, handler, method, "/api/v1/kit/software/releases")
		if response.Code != 405 {
			t.Fatalf("method %s status %d", method, response.Code)
		}
	}
	response = serve(t, handler, "GET", "/api/v1/kit/software/releases?target=other")
	if response.Code != 400 || service.calls != 1 {
		t.Fatal("query changed target selection")
	}
	service.err = errors.New("private upstream failure")
	response = serve(t, handler, "GET", "/api/v1/kit/software/releases")
	if response.Code < 400 || strings.Contains(response.Body.String(), "private upstream") {
		t.Fatal("upstream error leaked")
	}
	service.err = nil
	service.releases = nil
	response = serve(t, handler, "GET", "/api/v1/kit/software/releases")
	if strings.TrimSpace(response.Body.String()) != "[]" {
		t.Fatal("empty inventory must be an array")
	}
}

func TestSoftwareInventoryPairedListenerAuthenticatesAndScopes(t *testing.T) {
	service := &inventoryService{fakeService: &fakeService{}}
	handler, err := hostapi.NewLauncherHandler(hostapi.New(service), hostapi.LauncherConfig{Token: launcherToken, TargetID: launcherID})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		token, target string
		want          int
	}{
		{"", launcherID, 401}, {launcherToken, "wrong", 403}, {launcherToken, launcherID, 200},
	} {
		req := launcherRequest("GET", "http://192.0.2.1:8789/api/v1/kit/software/releases", nil)
		req.Header.Set("Authorization", "Bearer "+tc.token)
		req.Header.Set("X-FogCast-Target-ID", tc.target)
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, req)
		if out.Code != tc.want {
			t.Fatalf("status %d want %d: %s", out.Code, tc.want, out.Body.String())
		}
	}
	if service.calls != 1 || service.paired != launcherID {
		t.Fatalf("wrong authenticated scope: calls=%d paired=%s", service.calls, service.paired)
	}
}

func (s *inventoryService) LibrarySettings() fogcast.LibraryConfig {
	return fogcast.LibraryConfig{SelectedTarget: "other", Targets: []fogcast.TargetConfig{{Name: "other", Enabled: true, TargetID: launcherIDB}, {Name: "paired", Enabled: true, TargetID: launcherID}}}
}

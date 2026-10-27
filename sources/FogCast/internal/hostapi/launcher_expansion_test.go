package hostapi_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/protocol"
)

const launcherIDOff = "19c0b6d4-7e11-4a2e-9c55-0a1b2c3d4e5f"
const launcherTokenOff = "disabled-kit-token-0123456789abcd"

type pairedExpansionService struct {
	settingsFake
	selects   int
	bound     [][4]string
	selected  string
	selectErr error
}

func (s *pairedExpansionService) Hardware(context.Context) ([]fogcast.HardwareMachine, error) {
	return []fogcast.HardwareMachine{{GameID: "fpga-zx81", Title: "ZX81"}}, nil
}
func (s *pairedExpansionService) ImportCoreExpansion(context.Context, int64, io.Reader) (catalog.CoreExpansion, error) {
	return catalog.CoreExpansion{}, nil
}
func (s *pairedExpansionService) CoreExpansions(context.Context) ([]catalog.CoreExpansion, error) {
	return nil, nil
}
func (s *pairedExpansionService) CoreEntryExpansion(context.Context, string) (catalog.CoreEntryExpansion, error) {
	return catalog.CoreEntryExpansion{}, nil
}
func (s *pairedExpansionService) SelectCoreEntryExpansion(_ context.Context, game, pkg, expected, id string) (catalog.CoreEntryExpansion, error) {
	s.selects++
	s.bound = append(s.bound, [4]string{game, pkg, expected, id})
	s.selected = s.LibrarySettings().SelectedTarget
	if s.selectErr != nil {
		return catalog.CoreEntryExpansion{}, s.selectErr
	}
	return catalog.CoreEntryExpansion{GameID: game, ExpansionID: id}, nil
}

func newPairedExpansion(t *testing.T) (*pairedExpansionService, *httptest.Server) {
	t.Helper()
	service := &pairedExpansionService{settingsFake: settingsFake{
		fakeService: fakeService{status: protocol.Status{State: protocol.StateIdle}},
		settings: fogcast.LibraryConfig{
			SelectedTarget: "kit-a",
			Targets: []fogcast.TargetConfig{
				{Name: "kit-a", Enabled: true, TargetID: launcherID},
				{Name: "kit-b", Enabled: true, TargetID: launcherIDB},
				{Name: "kit-off", Enabled: false, TargetID: launcherIDOff},
			},
		},
	}}
	handler, err := hostapi.NewLauncherHandler(hostapi.New(service), hostapi.LauncherConfig{Pairings: []hostapi.LauncherPairing{
		{Token: launcherToken, TargetID: launcherID},
		{Token: launcherTokenB, TargetID: launcherIDB},
		{Token: launcherTokenOff, TargetID: launcherIDOff},
	}})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return service, server
}

type launcherAuthRoundTripper struct {
	base      http.RoundTripper
	token, id string
}

func (t launcherAuthRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	if t.token != "" {
		clone.Header.Set("Authorization", "Bearer "+t.token)
	}
	if t.id != "" {
		clone.Header.Set("X-FogCast-Target-ID", t.id)
	}
	return t.base.RoundTrip(clone)
}

func pairedExpansionClient(server *httptest.Server, token, id string) *hostclient.Client {
	base := server.Client()
	clone := *base
	transport := base.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	clone.Transport = launcherAuthRoundTripper{base: transport, token: token, id: id}
	return hostclient.NewClient(server.URL, &clone)
}

func TestLauncherExpansionSelectionReachesHostService(t *testing.T) {
	service, server := newPairedExpansion(t)
	client := pairedExpansionClient(server, launcherTokenB, launcherIDB)
	pkg, card := strings.Repeat("a", 64), strings.Repeat("b", 64)
	ctx := context.Background()
	fit, err := client.SelectCoreEntryExpansion(ctx, "fpga-zx81", pkg, "", card)
	if err != nil || fit.GameID != "fpga-zx81" || fit.ExpansionID != card {
		t.Fatalf("fit %+v %v", fit, err)
	}
	removed, err := client.SelectCoreEntryExpansion(ctx, "fpga-zx81", pkg, card, "")
	if err != nil || removed.ExpansionID != "" {
		t.Fatalf("remove %+v %v", removed, err)
	}
	if service.selects != 2 || service.selected != "kit-a" || len(service.bound) != 2 ||
		service.bound[0] != [4]string{"fpga-zx81", pkg, "", card} || service.bound[1] != [4]string{"fpga-zx81", pkg, card, ""} {
		t.Fatalf("selection calls selected=%q %+v", service.selected, service.bound)
	}
	if service.launchCalls != 0 || service.coreCalls != 0 || service.settings.SelectedTarget != "kit-a" {
		t.Fatal("paired save changed the running session or foreground target")
	}
	view, err := client.Hardware(ctx)
	if err != nil || view.ExpansionSelection == nil || !*view.ExpansionSelection {
		t.Fatalf("paired hardware advertisement %+v %v", view.ExpansionSelection, err)
	}
}

func TestLauncherExpansionSelectionConflictIsNotRetried(t *testing.T) {
	service, server := newPairedExpansion(t)
	service.selectErr = &protocol.APIError{Code: protocol.CodeStaleRevision, Message: "core entry selection changed; refresh and retry"}
	client := pairedExpansionClient(server, launcherTokenB, launcherIDB)
	_, err := client.SelectCoreEntryExpansion(context.Background(), "fpga-zx81", strings.Repeat("a", 64), "", strings.Repeat("b", 64))
	if err == nil || !strings.Contains(err.Error(), "STALE_REVISION") || service.selects != 1 || service.launchCalls != 0 {
		t.Fatalf("conflict err=%v selects=%d", err, service.selects)
	}
}

func TestLauncherExpansionSelectionRejectsBeforeService(t *testing.T) {
	service, server := newPairedExpansion(t)
	pkg := strings.Repeat("a", 64)
	body := `{"package_id":"` + pkg + `","expected_expansion_id":"","expansion_id":"` + strings.Repeat("b", 64) + `"}`
	call := func(method, path, token, id, payload string) int {
		t.Helper()
		req, err := http.NewRequest(method, server.URL+path, strings.NewReader(payload))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if id != "" {
			req.Header.Set("X-FogCast-Target-ID", id)
		}
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body)
		return resp.StatusCode
	}
	for _, tc := range []struct {
		name, method, path, token, id string
		want                          int
	}{
		{"bad bearer", http.MethodPut, "/api/v1/library/core-entries/fpga-zx81/expansion", "not-a-real-token-0123456789abcd", launcherIDB, http.StatusUnauthorized},
		{"missing target", http.MethodPut, "/api/v1/library/core-entries/fpga-zx81/expansion", launcherTokenB, "", http.StatusForbidden},
		{"mismatched target", http.MethodPut, "/api/v1/library/core-entries/fpga-zx81/expansion", launcherToken, launcherIDB, http.StatusForbidden},
		{"disabled target", http.MethodPut, "/api/v1/library/core-entries/fpga-zx81/expansion", launcherTokenOff, launcherIDOff, http.StatusForbidden},
		{"other foreground query", http.MethodPut, "/api/v1/library/core-entries/fpga-zx81/expansion?target=kit-a", launcherTokenB, launcherIDB, http.StatusForbidden},
		{"malformed game", http.MethodPut, "/api/v1/library/core-entries/Nope/expansion", launcherTokenB, launcherIDB, http.StatusNotFound},
		{"extra suffix", http.MethodPut, "/api/v1/library/core-entries/fpga-zx81/expansion/extra", launcherTokenB, launcherIDB, http.StatusNotFound},
		{"slot write", http.MethodPut, "/api/v1/library/core-entries/fpga-zx81/expansions/1", launcherTokenB, launcherIDB, http.StatusNotFound},
		{"presentation", http.MethodPut, "/api/v1/core-expansions/" + strings.Repeat("a", 64) + "/presentation", launcherTokenB, launcherIDB, http.StatusNotFound},
		{"edition", http.MethodPut, "/api/v1/library/edition-preferences", launcherTokenB, launcherIDB, http.StatusNotFound},
		{"read selection", http.MethodGet, "/api/v1/library/core-entries/fpga-zx81/expansion", launcherTokenB, launcherIDB, http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := call(tc.method, tc.path, tc.token, tc.id, body); got != tc.want {
				t.Fatalf("status %d, want %d", got, tc.want)
			}
		})
	}
	if service.selects != 0 || service.launchCalls != 0 || service.settings.SelectedTarget != "kit-a" {
		t.Fatalf("rejected request reached selection or moved the foreground: selects=%d target=%s", service.selects, service.settings.SelectedTarget)
	}
}

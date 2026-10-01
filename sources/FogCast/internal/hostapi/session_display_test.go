package hostapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/protocol"
)

type displayHostService struct {
	fakeService
	calls    int
	settings fogcast.LibraryConfig
	target   string
}

func (s *displayHostService) LibrarySettings() fogcast.LibraryConfig { return s.settings }
func (s *displayHostService) TargetIDForName(name string) string {
	for _, target := range s.settings.Targets {
		if target.Name == name {
			return target.TargetID
		}
	}
	return ""
}
func (s *displayHostService) Status(ctx context.Context) (protocol.Status, error) {
	return s.StatusTarget(ctx, fogcast.SessionTargetFromContext(ctx))
}
func (s *displayHostService) StatusTarget(_ context.Context, target string) (protocol.Status, error) {
	s.target = target
	return s.status, nil
}
func (s *displayHostService) SetSessionDisplay(ctx context.Context, visible bool, b protocol.DevelopmentMediaBinding) (protocol.Status, error) {
	s.calls++
	s.target = fogcast.SessionTargetFromContext(ctx)
	if !b.MatchesSessionDisplay(s.status) {
		return s.status, protocol.SessionDisplayIdentityError()
	}
	return s.status, nil
}

func TestPairedDisplayKeepsFullObservedBindingAndRejectsForeignSession(t *testing.T) {
	id := strings.Repeat("a", 64)
	s := &displayHostService{fakeService: fakeService{status: protocol.Status{State: protocol.StateActive, Development: true,
		CorePackage: &protocol.CorePackageStatus{PackageID: id, Generation: 9, ABI: protocol.RuntimeContract{ID: "fes.simple-computer", Major: 1},
			ActiveInterfaces: []protocol.RuntimeInterface{{ID: "fes.media.blob", Major: 1}, {ID: "fes.memory.hps-ddr", Major: 1}, {ID: "fes.video.session-display", Major: 1}}}}},
		settings: fogcast.LibraryConfig{SelectedTarget: "kit-b", Targets: []fogcast.TargetConfig{{Name: "kit-a", TargetID: launcherID, Enabled: true}, {Name: "kit-b", TargetID: launcherIDB, Enabled: true}}}}
	api := hostapi.New(s)
	launcher, err := hostapi.NewLauncherHandler(api, hostapi.LauncherConfig{Token: launcherToken, TargetID: launcherID})
	if err != nil {
		t.Fatal(err)
	}
	read := launcherRequest(http.MethodGet, "http://192.0.2.1:8789/api/v1/session", nil)
	w := httptest.NewRecorder()
	launcher.ServeHTTP(w, read)
	var observed struct {
		ID          string                      `json:"id"`
		Target      string                      `json:"target"`
		TargetID    string                      `json:"target_id"`
		CorePackage *protocol.CorePackageStatus `json:"core_package"`
	}
	if json.Unmarshal(w.Body.Bytes(), &observed) != nil || w.Code != 200 || observed.ID == "" || observed.Target != "kit-a" || observed.TargetID != launcherID || !protocol.SessionDisplayCapable(observed.CorePackage) || s.target != "kit-a" {
		t.Fatalf("paired observation lost binding: %d %s target=%s", w.Code, w.Body, s.target)
	}
	foreign := serve(t, api, http.MethodGet, "/api/v1/session?target=kit-b")
	var other struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(foreign.Body.Bytes(), &other)
	b := protocol.DevelopmentMediaBinding{PackageID: id, Generation: 9, Target: "kit-a", TargetID: launcherID}
	for _, tc := range []struct {
		session string
		binding protocol.DevelopmentMediaBinding
		body    string
		want    int
	}{
		{observed.ID, b, `{"visible":true}`, 200}, {observed.ID, b, `{"visible":false}`, 200},
		{other.ID, b, `{"visible":true}`, 409}, {"stale-session", b, `{"visible":true}`, 409},
		{observed.ID, protocol.DevelopmentMediaBinding{PackageID: id, Generation: 10, Target: "kit-a", TargetID: launcherID}, `{"visible":true}`, 409},
		{observed.ID, protocol.DevelopmentMediaBinding{PackageID: id, Generation: 9, Target: "kit-b", TargetID: launcherID}, `{"visible":true}`, 403},
		{observed.ID, b, `{"visible":null}`, 400}, {observed.ID, b, `{"visible":true} {}`, 400},
	} {
		r := launcherRequest(http.MethodPost, "http://192.0.2.1:8789/api/v1/session/display", strings.NewReader(tc.body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set(protocol.HostSessionIDHeader, tc.session)
		tc.binding.SetHeaders(r.Header)
		out := httptest.NewRecorder()
		launcher.ServeHTTP(out, r)
		if out.Code != tc.want {
			t.Fatalf("session=%s status=%d want=%d body=%s", tc.session, out.Code, tc.want, out.Body)
		}
	}
	if s.calls != 3 {
		t.Fatalf("foreign or malformed request dispatched: %d", s.calls)
	}
}

func TestPairedTapeImportBoundLeavesNextStartManagementProtected(t *testing.T) {
	s := &coreMediaAPIService{}
	handler, err := hostapi.NewLauncherHandler(hostapi.New(s), hostapi.LauncherConfig{Token: launcherToken, TargetID: launcherID})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		bytes int
		want  int
	}{{1, 200}, {16384, 200}, {16385, 400}, {0, 400}} {
		r := launcherRequest(http.MethodPost, "http://192.0.2.1:8789/api/v1/core-media", strings.NewReader(strings.Repeat("x", tc.bytes)))
		r.Header.Set("Content-Type", "application/octet-stream")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("bytes=%d status=%d body=%s", tc.bytes, w.Code, w.Body)
		}
	}
	if s.calls != 2 {
		t.Fatalf("unbounded tape imported: %d", s.calls)
	}
}

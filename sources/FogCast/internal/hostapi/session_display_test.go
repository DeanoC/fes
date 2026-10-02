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
	calls      int
	mediaCalls int
	settings   fogcast.LibraryConfig
	target     string
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

func (s *displayHostService) ReplaceLiveMedia(_ context.Context, _, _ string, b protocol.DevelopmentMediaBinding) (protocol.Status, error) {
	s.mediaCalls++
	s.target = b.Target
	return s.status, nil
}

func (s *displayHostService) ClearLiveMedia(_ context.Context, b protocol.DevelopmentMediaBinding) (protocol.Status, error) {
	s.mediaCalls++
	s.target = b.Target
	return s.status, nil
}

func TestPublicCapturedMutationRejectsAnotherTargetsSessionID(t *testing.T) {
	id := strings.Repeat("a", 64)
	s := &displayHostService{fakeService: fakeService{status: protocol.Status{State: protocol.StateActive, Development: true,
		CorePackage: &protocol.CorePackageStatus{PackageID: id, Generation: 9, ABI: protocol.RuntimeContract{ID: "fes.simple-computer", Major: 1},
			ActiveInterfaces: []protocol.RuntimeInterface{{ID: "fes.media.blob", Major: 1}, {ID: "fes.memory.hps-ddr", Major: 1}, {ID: "fes.video.session-display", Major: 1}}}}},
		settings: fogcast.LibraryConfig{Targets: []fogcast.TargetConfig{{Name: "kit-a", TargetID: launcherID, Enabled: true}, {Name: "kit-b", TargetID: launcherIDB, Enabled: true}}}}
	api := hostapi.New(s)
	sessions := map[string]string{}
	for _, target := range []string{"kit-a", "kit-b"} {
		var observed struct {
			ID string `json:"id"`
		}
		response := serve(t, api, http.MethodGet, "/api/v1/session?target="+target)
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &observed) != nil || observed.ID == "" {
			t.Fatalf("observe %s: %s", target, response.Body)
		}
		sessions[target] = observed.ID
	}
	for _, tc := range []struct{ path, body string }{
		{"/api/v1/session/display", `{"visible":true}`},
		{"/api/v1/session/display", `{"visible":false}`},
		{"/api/v1/session/live-media", `{"media_id":"` + strings.Repeat("b", 64) + `","name":"second.p"}`},
		{"/api/v1/session/live-media/clear", ""},
	} {
		for _, target := range []string{"kit-a", "kit-b"} {
			other, targetID := "kit-b", launcherID
			if target == "kit-b" {
				other, targetID = "kit-a", launcherIDB
			}
			r := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
			r.Host = "127.0.0.1"
			if tc.body != "" {
				r.Header.Set("Content-Type", "application/json")
			}
			r.Header.Set(protocol.HostSessionIDHeader, sessions[other])
			protocol.DevelopmentMediaBinding{PackageID: id, Generation: 9, Target: target, TargetID: targetID}.SetHeaders(r.Header)
			w := httptest.NewRecorder()
			api.ServeHTTP(w, r)
			if w.Code != 409 || s.calls != 0 || s.mediaCalls != 0 {
				t.Fatalf("cross-target %s: status=%d display=%d media=%d body=%s", tc.path, w.Code, s.calls, s.mediaCalls, w.Body)
			}
		}
	}
}

func TestPublicCapturedMutationRejectsReboundTargetID(t *testing.T) {
	for _, scoped := range []bool{false, true} {
		for _, tc := range []struct{ name, captured, current string }{
			{"replacement-kit", launcherID, launcherIDB},
			{"lost-identity", launcherID, ""},
			{"new-identity", "", launcherIDB},
		} {
			name := tc.name + "/foreground"
			if scoped {
				name = tc.name + "/target"
			}
			t.Run(name, func(t *testing.T) {
				id := strings.Repeat("a", 64)
				s := &displayHostService{fakeService: fakeService{sessionTarget: "kit-a", sessionTargetID: tc.captured,
					status: protocol.Status{State: protocol.StateActive, Development: true,
						CorePackage: &protocol.CorePackageStatus{PackageID: id, Generation: 9,
							ABI:              protocol.RuntimeContract{ID: "fes.simple-computer", Major: 1},
							ActiveInterfaces: []protocol.RuntimeInterface{{ID: "fes.media.blob", Major: 1}, {ID: "fes.memory.hps-ddr", Major: 1}, {ID: "fes.video.session-display", Major: 1}}}}},
					settings: fogcast.LibraryConfig{Targets: []fogcast.TargetConfig{{Name: "kit-a", TargetID: tc.captured, Enabled: true}}}}
				api := hostapi.New(s)
				path := "/api/v1/session"
				if scoped {
					path += "?target=kit-a"
				}
				var observed struct {
					ID       string `json:"id"`
					TargetID string `json:"target_id"`
				}
				response := serve(t, api, http.MethodGet, path)
				if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &observed) != nil || observed.ID == "" || observed.TargetID != tc.captured {
					t.Fatalf("observe captured identity: %d %s", response.Code, response.Body)
				}
				// Keep the name, package and generation while rebinding its kit.
				s.sessionTargetID = tc.current
				s.settings.Targets[0].TargetID = tc.current
				for _, mutation := range []struct{ path, body string }{
					{"/api/v1/session/display", `{"visible":true}`},
					{"/api/v1/session/display", `{"visible":false}`},
					{"/api/v1/session/live-media", `{"media_id":"` + strings.Repeat("b", 64) + `","name":"second.p"}`},
					{"/api/v1/session/live-media/clear", ""},
				} {
					for _, binding := range []struct {
						id   string
						want int
					}{{tc.captured, 409}, {tc.current, 200}} {
						r := httptest.NewRequest(http.MethodPost, mutation.path, strings.NewReader(mutation.body))
						r.Host = "127.0.0.1"
						if mutation.body != "" {
							r.Header.Set("Content-Type", "application/json")
						}
						r.Header.Set(protocol.HostSessionIDHeader, observed.ID)
						protocol.DevelopmentMediaBinding{PackageID: id, Generation: 9, Target: "kit-a", TargetID: binding.id}.SetHeaders(r.Header)
						before := s.calls + s.mediaCalls
						w := httptest.NewRecorder()
						api.ServeHTTP(w, r)
						wantCalls := before
						if binding.want == 200 {
							wantCalls++
						}
						if w.Code != binding.want || s.calls+s.mediaCalls != wantCalls {
							t.Fatalf("%s target_id=%q: status=%d want=%d calls=%d want=%d body=%s", mutation.path, binding.id,
								w.Code, binding.want, s.calls+s.mediaCalls, wantCalls, w.Body)
						}
					}
				}
			})
		}
	}
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

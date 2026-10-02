package hostapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/host"
	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/internal/meshcontent"
	"github.com/DeanoC/FogCast/kitlease"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
)

const launcherID = "73dc9f5f-1a12-4a95-a820-a9b4e600769a"
const launcherIDB = "67c5f4e2-d288-49bb-9049-39ecf39cf6f6"
const launcherToken = "12345678901234567890123456789012"
const launcherTokenB = "abcdefghijklmnopqrstuvwxyz012345"

type launcherService struct{ fakeService }

func (*launcherService) TargetConnection() fogcast.TargetConnection {
	return fogcast.TargetConnection{TargetID: launcherID, State: "ready"}
}

type pairedLeaseService struct {
	launcherService
	target string
}

func (s *pairedLeaseService) PairedTargetKitLeaseStatus(_ context.Context, target string) (kitlease.Status, error) {
	s.target = target
	return kitlease.Status{State: "held", Owner: "fogcast@kit", Purpose: "play", Generation: "gen-7", ExpiresInMS: 12000}, nil
}

func (s *pairedLeaseService) PairedTargetStatus(_ context.Context, target string) (kitlease.Status, bool, bool, error) {
	s.target = target
	return kitlease.Status{State: "held", Owner: "fogcast@kit", Generation: "gen-7", ExpiresInMS: 12000}, true, true, nil
}

func TestLauncherKitLeaseStatusIsScopedToAuthenticatedTarget(t *testing.T) {
	service := &pairedLeaseService{}
	handler, err := hostapi.NewLauncherHandler(hostapi.New(service), hostapi.LauncherConfig{Pairings: []hostapi.LauncherPairing{{Token: launcherToken, TargetID: launcherID}, {Token: launcherTokenB, TargetID: launcherIDB}}})
	if err != nil {
		t.Fatal(err)
	}
	req := launcherRequest("GET", "http://192.0.2.1:8789/api/v1/launcher/kit-lease", nil)
	req.Header.Set("X-FogCast-Target-ID", launcherIDB)
	req.Header.Set("Authorization", "Bearer "+launcherTokenB)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK || service.target != launcherIDB || !strings.Contains(w.Body.String(), `"generation":"gen-7"`) || !strings.Contains(w.Body.String(), `"target_reachable":true`) || !strings.Contains(w.Body.String(), `"target_ready":true`) || strings.Contains(w.Body.String(), "address") {
		t.Fatalf("lease projection: target=%q status=%d body=%s", service.target, w.Code, w.Body.String())
	}
}

// pairedSnapshotService models the target-keyed views exposed by the real
// FogCast service. The handlers must pass the authenticated paired target in
// context for every health and cache read.
type pairedSnapshotService struct {
	settingsFake
	healthByTarget   map[string]protocol.Health
	cacheByTarget    map[string]fogcast.LibraryCache
	presenceByTarget map[string]map[string]bool
}

func (s *pairedSnapshotService) Health(ctx context.Context) (protocol.Health, error) {
	return s.healthByTarget[fogcast.PairedTargetFromContext(ctx)], nil
}
func (s *pairedSnapshotService) Games(context.Context) ([]catalog.Game, error) {
	return []catalog.Game{{ID: "snes-mario", Title: "Mario", System: protocol.SystemSNES, Kind: catalog.SourceKindRaw, State: catalog.SourceStateAvailable, RootOnline: true, Content: &catalog.Content{SHA256: strings.Repeat("ab", 32), Size: 1, Extension: "sfc"}}}, nil
}
func (s *pairedSnapshotService) Game(context.Context, string) (catalog.Game, error) {
	return catalog.Game{ID: "snes-mario", Title: "Mario", System: protocol.SystemSNES, Kind: catalog.SourceKindRaw, State: catalog.SourceStateAvailable, RootOnline: true, Content: &catalog.Content{SHA256: strings.Repeat("ab", 32), Size: 1, Extension: "sfc"}}, nil
}
func (s *pairedSnapshotService) ROMCachePresence(ctx context.Context) (map[string]bool, bool) {
	return s.presenceByTarget[fogcast.PairedTargetFromContext(ctx)], true
}
func (s *pairedSnapshotService) LibraryCache(ctx context.Context) (fogcast.LibraryCache, error) {
	return s.cacheByTarget[fogcast.PairedTargetFromContext(ctx)], nil
}

func TestLauncherPairedHealthAndCacheAreTargetScoped(t *testing.T) {
	key := string(protocol.SystemSNES) + "/" + strings.Repeat("ab", 32)
	service := &pairedSnapshotService{
		settingsFake:     settingsFake{settings: fogcast.LibraryConfig{SelectedTarget: "kit-a", Targets: []fogcast.TargetConfig{{Name: "kit-a", Enabled: true, TargetID: launcherID}, {Name: "kit-b", Enabled: true, TargetID: launcherIDB}}}},
		healthByTarget:   map[string]protocol.Health{launcherID: {Ready: true}, launcherIDB: {Ready: false}},
		cacheByTarget:    map[string]fogcast.LibraryCache{launcherID: {ROM: fogcast.ROMCacheStatus{UsedBytes: 11, MaxBytes: 100, FreeBytes: 89, Reachable: true}}, launcherIDB: {ROM: fogcast.ROMCacheStatus{UsedBytes: 22, MaxBytes: 200, FreeBytes: 178, Reachable: true}}},
		presenceByTarget: map[string]map[string]bool{launcherID: {key: true}, launcherIDB: {key: false}},
	}
	h, err := hostapi.NewLauncherHandler(hostapi.New(service), hostapi.LauncherConfig{Pairings: []hostapi.LauncherPairing{{Token: launcherToken, TargetID: launcherID}, {Token: launcherTokenB, TargetID: launcherIDB}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		id, token string
		ready     bool
		used      int
		cached    bool
	}{{launcherID, launcherToken, true, 11, true}, {launcherIDB, launcherTokenB, false, 22, false}} {
		for _, path := range []string{"/api/v1/health", "/api/v1/games", "/api/v1/games/snes-mario", "/api/v1/library/cache"} {
			req := launcherRequest("GET", "http://127.0.0.1:8789"+path, nil)
			req.Header.Set("X-FogCast-Target-ID", tc.id)
			req.Header.Set("Authorization", "Bearer "+tc.token)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("%s %s: %d %s", tc.id, path, w.Code, w.Body.String())
			}
			switch path {
			case "/api/v1/health":
				var result struct {
					Target struct {
						Ready bool `json:"ready"`
					} `json:"target"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				if result.Target.Ready != tc.ready {
					t.Errorf("%s health leaked: %s", tc.id, w.Body.String())
				}
			case "/api/v1/games", "/api/v1/games/snes-mario":
				want := `"rom_cached":false`
				if tc.cached {
					want = `"rom_cached":true`
				}
				if !strings.Contains(w.Body.String(), want) {
					t.Errorf("%s %s cache: %s", tc.id, path, w.Body.String())
				}
			case "/api/v1/library/cache":
				if !strings.Contains(w.Body.String(), fmt.Sprintf(`"used_bytes":%d`, tc.used)) {
					t.Errorf("%s library cache: %s", tc.id, w.Body.String())
				}
			}
		}
	}
	req := launcherRequest("GET", "http://127.0.0.1:8789/api/v1/health", nil)
	req.Header.Set("X-FogCast-Target-ID", launcherID)
	req.Header.Set("Authorization", "Bearer "+launcherTokenB)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "TARGET_MISMATCH") {
		t.Fatalf("cross-target identity: %d %s", w.Code, w.Body.String())
	}
}

func TestLauncherPairedHealthDoesNotWaitForOtherKitLaunch(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	finishLaunch := func() { releaseOnce.Do(func() { close(release) }) }
	service := &pairedSnapshotService{settingsFake: settingsFake{settings: fogcast.LibraryConfig{SelectedTarget: "kit-a", Targets: []fogcast.TargetConfig{{Name: "kit-a", Enabled: true, TargetID: launcherID}, {Name: "kit-b", Enabled: true, TargetID: launcherIDB}}}}, healthByTarget: map[string]protocol.Health{launcherID: {Ready: true}, launcherIDB: {Ready: false}}, cacheByTarget: map[string]fogcast.LibraryCache{}, presenceByTarget: map[string]map[string]bool{}}
	service.launchHook = func(context.Context) { close(started); <-release }
	h, err := hostapi.NewLauncherHandler(hostapi.New(service), hostapi.LauncherConfig{Pairings: []hostapi.LauncherPairing{{Token: launcherToken, TargetID: launcherID}, {Token: launcherTokenB, TargetID: launcherIDB}}})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, launcherRequest("POST", "http://127.0.0.1:8789/api/v1/session/launch", strings.NewReader(`{"game_id":"snes-mario"}`)))
		close(done)
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		finishLaunch()
		t.Fatal("launch did not start")
	}
	defer finishLaunch()
	req := launcherRequest("GET", "http://127.0.0.1:8789/api/v1/health", nil)
	req.Header.Set("X-FogCast-Target-ID", launcherIDB)
	req.Header.Set("Authorization", "Bearer "+launcherTokenB)
	w := httptest.NewRecorder()
	finished := make(chan struct{})
	go func() { h.ServeHTTP(w, req); close(finished) }()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("B health blocked behind A launch")
	}
	var result struct {
		Target struct {
			Ready bool `json:"ready"`
		} `json:"target"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusOK || result.Target.Ready {
		t.Fatalf("B health: %d %s", w.Code, w.Body.String())
	}
	finishLaunch()
	<-done
}

func launcherHandler(t *testing.T, input host.RemoteInputController) http.Handler {
	t.Helper()
	api := hostapi.New(&launcherService{}, hostapi.WithRemoteInput(input))
	handler, err := hostapi.NewLauncherHandler(api, hostapi.LauncherConfig{Token: launcherToken, TargetID: launcherID})
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

func launcherRequest(method, path string, body io.Reader) *http.Request {
	req, _ := http.NewRequest(method, path, body)
	req.Header.Set("Authorization", "Bearer "+launcherToken)
	req.Header.Set("X-FogCast-Target-ID", launcherID)
	return req
}
func TestLauncherRestrictionAndAuthentication(t *testing.T) {
	handler := launcherHandler(t, nil)
	for _, tc := range []struct {
		name, method, path, token, id string
		want                          int
	}{
		{"catalogue", "GET", "/api/v1/games", launcherToken, launcherID, 200},
		{"library titles", "GET", "/api/v1/library/titles", launcherToken, launcherID, 200},
		{"library titles no token", "GET", "/api/v1/library/titles", "", launcherID, 401},
		{"attract", "GET", "/api/v1/library/attract", launcherToken, launcherID, 200},
		{"library cache", "GET", "/api/v1/library/cache", launcherToken, launcherID, 200},
		{"library collections", "GET", "/api/v1/library/collections", launcherToken, launcherID, 200},
		{"library facets", "GET", "/api/v1/library/facets", launcherToken, launcherID, 200},
		{"presentation", "GET", "/api/v1/presentation/games/snes-mario", launcherToken, launcherID, 200},
		{"presentation junk", "GET", "/api/v1/presentation/games/Nope", launcherToken, launcherID, 404},
		{"presentation traversal", "GET", "/api/v1/presentation/games/../secret", launcherToken, launcherID, 404},
		{"no token", "GET", "/api/v1/games", "", launcherID, 401},
		{"wrong token", "GET", "/api/v1/games", "wrong", launcherID, 401},
		{"wrong identity", "GET", "/api/v1/games", launcherToken, "different", 403},
		{"settings", "GET", "/api/v1/library/settings", launcherToken, launcherID, 404},
		{"development", "POST", "/api/v1/session/development-rbf", launcherToken, launcherID, 404},
		{"input detach", "POST", "/api/v1/session/input/detach", launcherToken, launcherID, 404},
		{"browser", "GET", "/", launcherToken, launcherID, 404},
		{"method", "DELETE", "/api/v1/games", launcherToken, launcherID, 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := launcherRequest(tc.method, "http://192.0.2.1:8789"+tc.path, nil)
			req.Header.Set("Authorization", "Bearer "+tc.token)
			req.Header.Set("X-FogCast-Target-ID", tc.id)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
	api := hostapi.New(&launcherService{})
	w := httptest.NewRecorder()
	api.ServeHTTP(w, launcherRequest("GET", "http://192.0.2.1/api/v1/games", nil))
	if w.Code != 403 {
		t.Fatalf("browser API exposed: %d", w.Code)
	}
}

func TestLauncherAllowsMeshContentGET(t *testing.T) {
	handler := launcherHandler(t, nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, launcherRequest("GET", "http://192.0.2.1:8789/api/v1/mesh/content/source?id=sha256:"+strings.Repeat("ab", 32), nil))
	if w.Code == http.StatusNotFound && strings.Contains(w.Body.String(), "launcher operation is unavailable") {
		t.Fatalf("source was not a launcher operation: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, launcherRequest("GET", "http://192.0.2.1:8789/api/v1/mesh/content/object?id=sha256:"+strings.Repeat("ab", 32), nil))
	if w.Code == http.StatusNotFound && strings.Contains(w.Body.String(), "launcher operation is unavailable") {
		t.Fatalf("object was not a launcher operation: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, launcherRequest("POST", "http://192.0.2.1:8789/api/v1/mesh/content/object", nil))
	if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "launcher operation is unavailable") {
		t.Fatalf("rejected object: %d %s", w.Code, w.Body.String())
	}
}

type meshSettingsService struct {
	settingsFake
	body []byte
}

func (m *meshSettingsService) MeshContentAdvertises(_ context.Context, id meshcontent.ContentID) bool {
	return id == meshcontent.SumSHA256(m.body)
}

func (m *meshSettingsService) OpenMeshContent(_ context.Context, id meshcontent.ContentID) (io.ReadCloser, error) {
	if id != meshcontent.SumSHA256(m.body) {
		return nil, os.ErrNotExist
	}
	return io.NopCloser(bytes.NewReader(m.body)), nil
}

func TestLauncherMeshContentAllowsUnselectedKit(t *testing.T) {
	const siblingID = "84ed0a60-2b23-5ba6-b931-bac5f71187ab"
	payload := []byte("sibling-bios")
	id := meshcontent.SumSHA256(payload)
	sourcePath := "http://192.0.2.1:8789/api/v1/mesh/content/source?id=" + id.String()
	objectPath := "http://192.0.2.1:8789/api/v1/mesh/content/object?id=" + id.String()
	gamesPath := "http://192.0.2.1:8789/api/v1/games"
	titlesPath := "http://192.0.2.1:8789/api/v1/library/titles"
	targets := []fogcast.TargetConfig{
		{Name: "kit-a", Enabled: true, TargetID: launcherID},
		{Name: "kit-b", Enabled: true, TargetID: siblingID},
	}
	handlerFor := func(selected, configured string, kits []fogcast.TargetConfig) http.Handler {
		t.Helper()
		service := &meshSettingsService{
			settingsFake: settingsFake{settings: fogcast.LibraryConfig{SelectedTarget: selected, Targets: kits}},
			body:         payload,
		}
		handler, err := hostapi.NewLauncherHandler(hostapi.New(service), hostapi.LauncherConfig{Token: launcherToken, TargetID: configured})
		if err != nil {
			t.Fatal(err)
		}
		return handler
	}
	serve := func(handler http.Handler, method, path, targetID, token string) *httptest.ResponseRecorder {
		t.Helper()
		req := launcherRequest(method, path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-FogCast-Target-ID", targetID)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}

	paired := handlerFor("kit-a", launcherID, targets)
	w := serve(paired, http.MethodGet, sourcePath, siblingID, launcherToken)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"advertises":true`) {
		t.Fatalf("sibling source: %d %s", w.Code, w.Body.String())
	}
	w = serve(paired, http.MethodGet, objectPath, siblingID, launcherToken)
	if w.Code != http.StatusOK || w.Body.String() != string(payload) {
		t.Fatalf("sibling object: %d %s", w.Code, w.Body.String())
	}
	// Sibling is enabled but not paired to this token, so catalogue stays
	// forbidden. Content reads still admit any enabled configured kit.
	w = serve(paired, http.MethodGet, gamesPath, siblingID, launcherToken)
	if w.Code != http.StatusForbidden {
		t.Fatalf("sibling catalogue: %d %s", w.Code, w.Body.String())
	}
	w = serve(paired, http.MethodGet, titlesPath, siblingID, launcherToken)
	if w.Code != http.StatusForbidden {
		t.Fatalf("sibling library titles: %d %s", w.Code, w.Body.String())
	}
	w = serve(paired, http.MethodGet, sourcePath, "11111111-1111-1111-1111-111111111111", launcherToken)
	if w.Code != http.StatusForbidden {
		t.Fatalf("unknown kit: %d %s", w.Code, w.Body.String())
	}
	w = serve(paired, http.MethodGet, sourcePath, siblingID, "wrong")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("sibling auth: %d %s", w.Code, w.Body.String())
	}

	disabled := handlerFor("kit-a", launcherID, []fogcast.TargetConfig{
		{Name: "kit-a", Enabled: true, TargetID: launcherID},
		{Name: "kit-b", Enabled: false, TargetID: siblingID},
	})
	w = serve(disabled, http.MethodGet, sourcePath, siblingID, launcherToken)
	if w.Code != http.StatusForbidden {
		t.Fatalf("disabled sibling: %d %s", w.Code, w.Body.String())
	}

	// The listener is paired to B while A stays selected. Catalogue reads are
	// served to that paired kit; selection no longer rejects them.
	moved := handlerFor("kit-a", siblingID, targets)
	w = serve(moved, http.MethodGet, sourcePath, siblingID, launcherToken)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"advertises":true`) {
		t.Fatalf("configured sibling while A selected: %d %s", w.Code, w.Body.String())
	}
	w = serve(moved, http.MethodGet, gamesPath, siblingID, launcherToken)
	if w.Code != http.StatusOK {
		t.Fatalf("configured sibling catalogue: %d %s", w.Code, w.Body.String())
	}

	// Selection moved to B. A stays paired and enabled, so A's content read
	// and A's catalogue read are both admitted.
	reselected := handlerFor("kit-b", launcherID, targets)
	w = serve(reselected, http.MethodGet, objectPath, launcherID, launcherToken)
	if w.Code != http.StatusOK || w.Body.String() != string(payload) {
		t.Fatalf("paired content after selection moved: %d %s", w.Code, w.Body.String())
	}
	w = serve(reselected, http.MethodGet, gamesPath, launcherID, launcherToken)
	if w.Code != http.StatusOK {
		t.Fatalf("paired catalogue after selection moved: %d %s", w.Code, w.Body.String())
	}
}

func TestLauncherAllowsArtworkGET(t *testing.T) {
	handler := launcherHandler(t, nil)
	handle := strings.Repeat("ab", 32)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, launcherRequest("GET", "http://192.0.2.1:8789/api/v1/presentation/artwork/"+handle, nil))
	if w.Code != 404 || !strings.Contains(w.Body.String(), "ARTWORK_UNAVAILABLE") {
		t.Fatalf("admitted artwork: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, launcherRequest("GET", "http://192.0.2.1:8789/api/v1/presentation/artwork/nope", nil))
	if w.Code != 404 || !strings.Contains(w.Body.String(), "launcher operation is unavailable") {
		t.Fatalf("rejected artwork: %d %s", w.Code, w.Body.String())
	}
}

func TestLauncherAllowsPresentationGameGET(t *testing.T) {
	handler := launcherHandler(t, nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, launcherRequest("GET", "http://192.0.2.1:8789/api/v1/presentation/games/snes-mario", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"state":"unconfigured"`) {
		t.Fatalf("admitted presentation: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, launcherRequest("GET", "http://192.0.2.1:8789/api/v1/presentation/games/Not-A-Slug", nil))
	if w.Code != 404 || !strings.Contains(w.Body.String(), "launcher operation is unavailable") {
		t.Fatalf("rejected presentation: %d %s", w.Code, w.Body.String())
	}
}

type launcherInput struct {
	mu      sync.Mutex
	source  *launcherSource
	session string
}

func (*launcherInput) Attach(context.Context, string) error { return nil }
func (*launcherInput) Detach(context.Context, string) error { return nil }
func (*launcherInput) Status() host.RemoteInputStatus {
	return host.RemoteInputStatus{State: host.RemoteInputAttached, Ready: true, SessionID: "123"}
}
func (i *launcherInput) ClaimSource(id string) (host.RemoteInputEventSource, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if id != "123" {
		return nil, host.ErrRemoteInputInvalid
	}
	if i.source != nil {
		return nil, host.ErrRemoteInputBusy
	}
	s := &launcherSource{input: i, closed: make(chan struct{}), events: make(chan remoteinput.Event, 4)}
	i.source = s
	return s, nil
}

type launcherSource struct {
	input  *launcherInput
	closed chan struct{}
	events chan remoteinput.Event
}

func (*launcherSource) Check() error { return nil }
func (s *launcherSource) SendEvent(_ context.Context, e remoteinput.Event, _ time.Time) error {
	s.events <- e
	return nil
}
func (s *launcherSource) Close() error {
	s.input.mu.Lock()
	defer s.input.mu.Unlock()
	if s.input.source == s {
		s.input.source = nil
		close(s.closed)
	}
	return nil
}

func TestLauncherStreamDisconnectAndTimeoutRelease(t *testing.T) {
	for _, timeout := range []bool{false, true} {
		t.Run(map[bool]string{false: "disconnect", true: "timeout"}[timeout], func(t *testing.T) {
			input := &launcherInput{}
			server := httptest.NewServer(launcherHandler(t, input))
			defer server.Close()
			reader, writer := io.Pipe()
			defer writer.Close()
			req := launcherRequest("POST", server.URL+"/api/v1/launcher/input?session_id=123", reader)
			response, err := server.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != 200 {
				t.Fatalf("stream: %d", response.StatusCode)
			}
			var ready struct {
				Ready bool `json:"ready"`
			}
			if err = json.NewDecoder(response.Body).Decode(&ready); err != nil || !ready.Ready {
				t.Fatalf("ready=%v err=%v", ready, err)
			}
			input.mu.Lock()
			source := input.source
			input.mu.Unlock()
			if source == nil {
				t.Fatal("not claimed")
			}
			_, err = io.WriteString(writer, "{\"event\":{\"Device\":1,\"Kind\":1,\"Action\":1,\"Code\":104}}\n")
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-source.events:
			case <-time.After(time.Second):
				t.Fatal("event not delivered")
			}
			if !timeout {
				writer.Close()
			}
			select {
			case <-source.closed:
			case <-time.After(2 * time.Second):
				t.Fatal("source not released")
			}
		})
	}
}

func TestLauncherInputClaimsPairedTargetsOwnBridge(t *testing.T) {
	inputA, inputB := &launcherInput{}, &launcherInput{}
	service := newKitService("kit-a")
	service.settingsFake.sessionTarget = "kit-a"
	service.settingsFake.sessionTargetID = launcherID
	service.settingsFake.playSessions = []fogcast.PlaySession{{Target: "kit-a", TargetID: launcherID, GameID: "pong"}, {Target: "kit-b", TargetID: launcherIDB, GameID: "pong"}}
	api := hostapi.New(service, hostapi.WithRemoteInputFactory(func(target string) host.RemoteInputController {
		if target == "kit-a" {
			return inputA
		}
		if target == "kit-b" {
			return inputB
		}
		return nil
	}))
	handler, err := hostapi.NewLauncherHandler(api, distinctKitConfig())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	reader, writer := io.Pipe()
	defer writer.Close()
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/launcher/input?session_id=123", reader)
	req.Header.Set("Authorization", "Bearer "+launcherTokenB)
	req.Header.Set("X-FogCast-Target-ID", launcherIDB)
	response, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("stream status=%d", response.StatusCode)
	}
	var ready map[string]any
	if err := json.NewDecoder(response.Body).Decode(&ready); err != nil {
		t.Fatal(err)
	}
	inputB.mu.Lock()
	bSource := inputB.source
	inputB.mu.Unlock()
	inputA.mu.Lock()
	aSource := inputA.source
	inputA.mu.Unlock()
	if bSource == nil || aSource != nil {
		t.Fatalf("paired B stream source: B=%v A=%v", bSource != nil, aSource != nil)
	}
	if _, err := io.WriteString(writer, "{\"event\":{\"Device\":1,\"Kind\":1,\"Action\":1,\"Code\":104}}\n"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-bSource.events:
	case <-time.After(time.Second):
		t.Fatal("kit B input was not delivered to B bridge")
	}
	writer.Close()
}

func TestLauncherStreamRejectsStaleSession(t *testing.T) {
	server := httptest.NewServer(launcherHandler(t, &launcherInput{}))
	defer server.Close()
	response, err := server.Client().Do(launcherRequest("POST", server.URL+"/api/v1/launcher/input?session_id=old", strings.NewReader("{}\n")))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 409 {
		t.Fatalf("stale: %d", response.StatusCode)
	}
}

func TestLauncherRejectsConfiguredTargetMismatchBeforeDispatch(t *testing.T) {
	service := &settingsFake{settings: fogcast.LibraryConfig{SelectedTarget: "other", Targets: []fogcast.TargetConfig{{Name: "other", Enabled: true, TargetID: "a3cdbf5f-1a12-4a95-a820-a9b4e600769a"}}}}
	api := hostapi.New(service)
	handler, err := hostapi.NewLauncherHandler(api, hostapi.LauncherConfig{Token: launcherToken, TargetID: launcherID})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, launcherRequest("POST", "http://192.0.2.1/api/v1/session/launch", strings.NewReader(`{"game_id":"pong"}`)))
	if w.Code != 403 || service.launchCalls != 0 {
		t.Fatalf("status=%d launchCalls=%d", w.Code, service.launchCalls)
	}
}

func TestLauncherStreamAdmitsPlaySessionKeyboard(t *testing.T) {
	input := &launcherInput{}
	server := httptest.NewServer(launcherHandler(t, input))
	defer server.Close()
	reader, writer := io.Pipe()
	defer writer.Close()
	response, err := server.Client().Do(launcherRequest("POST", server.URL+"/api/v1/launcher/input?session_id=123", reader))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("stream: %d", response.StatusCode)
	}
	var ready struct {
		Ready bool `json:"ready"`
	}
	if err = json.NewDecoder(response.Body).Decode(&ready); err != nil || !ready.Ready {
		t.Fatalf("ready=%v err=%v", ready, err)
	}
	input.mu.Lock()
	source := input.source
	input.mu.Unlock()
	if source == nil {
		t.Fatal("not claimed")
	}
	_, err = io.WriteString(writer, `{"event":{"Device":0,"Kind":0,"Action":1,"Code":260}}`+"\n")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case e := <-source.events:
		if e.Device != remoteinput.DeviceKeyboard || e.Code != 260 {
			t.Fatalf("event %#v", e)
		}
	case <-time.After(time.Second):
		t.Fatal("keyboard event not delivered")
	}
}

func TestLauncherStreamRejectsMalformedAndCompetingSource(t *testing.T) {
	for _, body := range []string{"{invalid}\n", `{"event":{"Device":9,"Kind":0,"Action":1,"Code":2}}` + "\n", strings.Repeat("x", 4097) + "\n"} {
		t.Run(body[:8], func(t *testing.T) {
			input := &launcherInput{}
			server := httptest.NewServer(launcherHandler(t, input))
			defer server.Close()
			reader, writer := io.Pipe()
			defer writer.Close()
			response, err := server.Client().Do(launcherRequest("POST", server.URL+"/api/v1/launcher/input?session_id=123", reader))
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			input.mu.Lock()
			source := input.source
			input.mu.Unlock()
			if source == nil {
				t.Fatal("source absent")
			}
			second, err := server.Client().Do(launcherRequest("POST", server.URL+"/api/v1/launcher/input?session_id=123", strings.NewReader("{}\n")))
			if err != nil {
				t.Fatal(err)
			}
			second.Body.Close()
			if second.StatusCode != 409 {
				t.Fatalf("competing source: %d", second.StatusCode)
			}
			_, _ = io.WriteString(writer, body)
			select {
			case <-source.closed:
			case <-time.After(2 * time.Second):
				t.Fatal("invalid input retained source")
			}
			select {
			case event := <-source.events:
				t.Fatalf("invalid input forwarded: %#v", event)
			default:
			}
		})
	}
}

// Ensure calls back through the paired listener while Launch holds the target guard.
func TestLauncherServesReadsDuringLaunch(t *testing.T) {
	for _, endpoint := range []string{"source", "object", "games"} {
		t.Run(endpoint, func(t *testing.T) {
			started, release := make(chan struct{}), make(chan struct{})
			service := &meshSettingsService{
				settingsFake: settingsFake{settings: fogcast.LibraryConfig{
					SelectedTarget: "kit", Targets: []fogcast.TargetConfig{{Name: "kit", Enabled: true, TargetID: launcherID}},
				}}, body: []byte("synthetic cold cartridge"),
			}
			service.launchHook = func(context.Context) { close(started); <-release }
			handler, err := hostapi.NewLauncherHandler(hostapi.New(service), hostapi.LauncherConfig{Token: launcherToken, TargetID: launcherID})
			if err != nil {
				t.Fatal(err)
			}
			launchDone := make(chan struct{})
			go func() {
				handler.ServeHTTP(httptest.NewRecorder(), launcherRequest("POST", "http://192.0.2.1/api/v1/session/launch", strings.NewReader(`{"game_id":"snes-mario"}`)))
				close(launchDone)
			}()
			select {
			case <-started:
			case <-time.After(time.Second):
				close(release)
				t.Fatal("launch did not enter service")
			}
			readDone := make(chan *httptest.ResponseRecorder, 1)
			go func() {
				w := httptest.NewRecorder()
				path := "http://192.0.2.1/api/v1/mesh/content/" + endpoint + "?id=" + meshcontent.SumSHA256(service.body).String()
				if endpoint == "games" {
					path = "http://192.0.2.1/api/v1/games"
				}
				handler.ServeHTTP(w, launcherRequest("GET", path, nil))
				readDone <- w
			}()
			select {
			case w := <-readDone:
				if w.Code != http.StatusOK {
					t.Errorf("content response: %d %s", w.Code, w.Body.String())
				}
				if endpoint == "object" && w.Body.String() != string(service.body) {
					t.Error("wrong ROM bytes")
				}
				if endpoint == "source" && !strings.Contains(w.Body.String(), `"advertises":true`) {
					t.Error("source did not advertise ROM")
				}
			case <-time.After(time.Second):
				t.Error("content read blocked behind launch")
			}
			close(release)
			<-launchDone
		})
	}
}

// launcherKitService is a library-backed launcher fake. LaunchOn records the
// explicit target. SessionTarget and PlaySessions come from the embedded fake.
type launcherKitService struct {
	settingsFake
	launchOnTarget string
	launchOnCalls  int
}

func (s *launcherKitService) LaunchOn(ctx context.Context, gameID, target string, progress fogcast.ProgressFunc) (protocol.CachedLaunchResponse, error) {
	s.launchOnCalls++
	s.launchOnTarget = target
	response, err := s.settingsFake.LaunchOn(ctx, gameID, target, progress)
	if err == nil && response.Status.State == protocol.StateActive {
		name := target
		for _, configured := range s.settings.Targets {
			if configured.Name == name {
				play := fogcast.PlaySession{Target: name, TargetID: configured.TargetID, Execution: fogcast.ExecutionFPGANative, GameID: gameID}
				if response.Status.System != nil {
					play.System = *response.Status.System
				}
				plays := s.playSessions[:0]
				for _, existing := range s.playSessions {
					if existing.Target != name {
						plays = append(plays, existing)
					}
				}
				s.playSessions = append(plays, play)
				break
			}
		}
	}
	return response, err
}

func twoKitSettings(selected string, kits ...fogcast.TargetConfig) fogcast.LibraryConfig {
	if len(kits) == 0 {
		kits = []fogcast.TargetConfig{
			{Name: "kit-a", Enabled: true, TargetID: launcherID},
			{Name: "kit-b", Enabled: true, TargetID: launcherIDB},
		}
	}
	return fogcast.LibraryConfig{SelectedTarget: selected, Targets: kits}
}

func newKitService(selected string, kits ...fogcast.TargetConfig) *launcherKitService {
	return &launcherKitService{settingsFake: settingsFake{settings: twoKitSettings(selected, kits...)}}
}

func kitHandler(t *testing.T, service *launcherKitService, config hostapi.LauncherConfig) http.Handler {
	t.Helper()
	handler, err := hostapi.NewLauncherHandler(hostapi.New(service), config)
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

func kitCall(handler http.Handler, method, path, token, id, body string) *httptest.ResponseRecorder {
	var reader io.Reader
	if body != "" || method == http.MethodPost {
		reader = strings.NewReader(body)
	}
	req := launcherRequest(method, "http://192.0.2.1:8789"+path, reader)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-FogCast-Target-ID", id)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w
}

func distinctKitConfig() hostapi.LauncherConfig {
	return hostapi.LauncherConfig{Pairings: []hostapi.LauncherPairing{
		{Token: launcherToken, TargetID: launcherID},
		{Token: launcherTokenB, TargetID: launcherIDB},
	}}
}

func TestLibraryTitlesIsPairedReadNotContentRead(t *testing.T) {
	handler := kitHandler(t, newKitService("kit-a"), distinctKitConfig())
	if w := kitCall(handler, http.MethodGet, "/api/v1/library/titles", launcherToken, launcherID, ""); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"titles":[]`) {
		t.Fatalf("paired read: %d %s", w.Code, w.Body.String())
	}
	w := kitCall(handler, http.MethodGet, "/api/v1/library/titles", launcherToken, launcherIDB, "")
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "TARGET_MISMATCH") {
		t.Fatalf("cross-target library titles: %d %s", w.Code, w.Body.String())
	}
	w = kitCall(handler, http.MethodGet, "/api/v1/library/titles", "", launcherID, "")
	if w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), "UNAUTHORIZED") {
		t.Fatalf("missing bearer: %d %s", w.Code, w.Body.String())
	}
}

func TestLauncherDistinctTokens(t *testing.T) {
	handler := kitHandler(t, newKitService("kit-a"), distinctKitConfig())
	if w := kitCall(handler, http.MethodGet, "/api/v1/games", launcherToken, launcherID, ""); w.Code != http.StatusOK {
		t.Fatalf("kit A games: %d %s", w.Code, w.Body.String())
	}
	if w := kitCall(handler, http.MethodGet, "/api/v1/games", launcherTokenB, launcherIDB, ""); w.Code != http.StatusOK {
		t.Fatalf("kit B games: %d %s", w.Code, w.Body.String())
	}
	w := kitCall(handler, http.MethodGet, "/api/v1/games", launcherToken, launcherIDB, "")
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "TARGET_MISMATCH") {
		t.Fatalf("A token on B: %d %s", w.Code, w.Body.String())
	}
	w = kitCall(handler, http.MethodGet, "/api/v1/games", "not-a-launcher-token", launcherID, "")
	if w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), "UNAUTHORIZED") {
		t.Fatalf("bad token: %d %s", w.Code, w.Body.String())
	}
}

func TestLauncherUnselectedKitReadsAndSession(t *testing.T) {
	service := newKitService("kit-a")
	handler := kitHandler(t, service, distinctKitConfig())
	if w := kitCall(handler, http.MethodGet, "/api/v1/games", launcherTokenB, launcherIDB, ""); w.Code != http.StatusOK {
		t.Fatalf("kit B games while A selected: %d %s", w.Code, w.Body.String())
	}
	w := kitCall(handler, http.MethodGet, "/api/v1/session", launcherTokenB, launcherIDB, "")
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("kit B session: %d %s", w.Code, w.Body.String())
	}
	decoded, err := hostclient.DecodeSession(w.Code, w.Body.Bytes())
	if err != nil || decoded.ID != "" || decoded.Target != "kit-b" || decoded.TargetID != launcherIDB || decoded.State != "idle" || decoded.GameID != "" {
		t.Fatalf("idle view %#v err=%v body=%s", decoded, err, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "game_id") {
		t.Fatalf("idle view kept an empty game: %s", w.Body.String())
	}
	service.playSessions = []fogcast.PlaySession{{
		Target: "kit-b", TargetID: launcherIDB, Execution: "fpga_native", GameID: "pong", System: protocol.SystemPong,
	}}
	w = kitCall(handler, http.MethodGet, "/api/v1/session", launcherTokenB, launcherIDB, "")
	decoded, err = hostclient.DecodeSession(w.Code, w.Body.Bytes())
	if err != nil || w.Code != http.StatusOK || decoded.State != "active" || decoded.TargetID != launcherIDB || decoded.GameID != "pong" || decoded.System != "pong" || decoded.Execution != "fpga_native" || decoded.ID == "" {
		t.Fatalf("active view %#v err=%v body=%s", decoded, err, w.Body.String())
	}
	w = kitCall(handler, http.MethodGet, "/api/v1/status", launcherTokenB, launcherIDB, "")
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "TARGET_MISMATCH") {
		t.Fatalf("kit B status: %d %s", w.Code, w.Body.String())
	}
	w = kitCall(handler, http.MethodGet, "/api/v1/session/input", launcherTokenB, launcherIDB, "")
	if w.Code == http.StatusForbidden || strings.Contains(w.Body.String(), "TARGET_MISMATCH") {
		t.Fatalf("kit B input was cross-target rejected: %d %s", w.Code, w.Body.String())
	}
}

func TestLauncherLaunchNamesRequestingKit(t *testing.T) {
	service := newKitService("kit-a")
	handler := kitHandler(t, service, distinctKitConfig())
	w := kitCall(handler, http.MethodPost, "/api/v1/session/launch", launcherTokenB, launcherIDB, `{"game_id":"pong"}`)
	if w.Code != http.StatusOK || service.launchOnCalls != 1 || service.launchOnTarget != "kit-b" {
		t.Fatalf("launch status=%d calls=%d target=%q body=%s", w.Code, service.launchOnCalls, service.launchOnTarget, w.Body.String())
	}
	rejected := newKitService("kit-a")
	handler = kitHandler(t, rejected, distinctKitConfig())
	w = kitCall(handler, http.MethodPost, "/api/v1/session/launch", launcherTokenB, launcherIDB, `{"game_id":"pong","target":"kit-a"}`)
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "TARGET_MISMATCH") || rejected.launchOnCalls != 0 {
		t.Fatalf("foreign target status=%d calls=%d body=%s", w.Code, rejected.launchOnCalls, w.Body.String())
	}
	w = kitCall(handler, http.MethodPost, "/api/v1/session/launch", launcherTokenB, launcherIDB, `{"game_id":"pong"}{}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "request body must contain one valid JSON object") || rejected.launchOnCalls != 0 {
		t.Fatalf("trailing body status=%d calls=%d body=%s", w.Code, rejected.launchOnCalls, w.Body.String())
	}
}

func TestLauncherRejectsUnpairedAndDisabledKit(t *testing.T) {
	disabled := newKitService("kit-a",
		fogcast.TargetConfig{Name: "kit-a", Enabled: true, TargetID: launcherID},
		fogcast.TargetConfig{Name: "kit-b", Enabled: false, TargetID: launcherIDB},
	)
	handler := kitHandler(t, disabled, distinctKitConfig())
	w := kitCall(handler, http.MethodGet, "/api/v1/games", launcherTokenB, launcherIDB, "")
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "TARGET_MISMATCH") {
		t.Fatalf("disabled: %d %s", w.Code, w.Body.String())
	}
	w = kitCall(handler, http.MethodGet, "/api/v1/games", launcherToken, "11111111-1111-4111-8111-111111111111", "")
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "TARGET_MISMATCH") {
		t.Fatalf("unpaired: %d %s", w.Code, w.Body.String())
	}
	w = kitCall(handler, http.MethodGet, "/api/v1/games", "00000000000000000000000000000000", launcherID, "")
	if w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), "UNAUTHORIZED") {
		t.Fatalf("bad token: %d %s", w.Code, w.Body.String())
	}
}

// A bearer identifies exactly one kit. Stop, status and input are
// owner-scoped, so one token paired with two target ids is refused.
func TestLauncherRejectsASharedBearer(t *testing.T) {
	for _, config := range []hostapi.LauncherConfig{
		{Token: launcherToken, TargetID: launcherID, Pairings: []hostapi.LauncherPairing{{Token: launcherToken, TargetID: launcherIDB}}},
		{Pairings: []hostapi.LauncherPairing{{Token: launcherToken, TargetID: launcherID}, {Token: launcherToken, TargetID: launcherIDB}}},
	} {
		err := config.Validate()
		if !errors.Is(err, hostapi.ErrLauncherSharedToken) || !strings.Contains(err.Error(), "per-kit bearer") || strings.Contains(err.Error(), launcherToken) {
			t.Fatalf("shared bearer: %v", err)
		}
		if handler, err := hostapi.NewLauncherHandler(hostapi.New(newKitService("kit-a")), config); err == nil || handler != nil {
			t.Fatal("shared bearer built a launcher handler")
		}
	}
}

func TestLauncherStopIsScopedToPairedKit(t *testing.T) {
	// Either paired kit may stop its own target; the bearer selects the scope.
	owner := newKitService("kit-a")
	owner.sessionTarget = "kit-b"
	owner.sessionTargetID = launcherIDB
	handler := kitHandler(t, owner, distinctKitConfig())
	w := kitCall(handler, http.MethodPost, "/api/v1/session/stop", launcherToken, launcherID, "")
	if w.Code != http.StatusOK || len(owner.stopCtxErrs) != 1 {
		t.Fatalf("A scoped stop status=%d stops=%d body=%s", w.Code, len(owner.stopCtxErrs), w.Body.String())
	}
	w = kitCall(handler, http.MethodPost, "/api/v1/session/stop", launcherTokenB, launcherIDB, "")
	if w.Code != http.StatusOK || len(owner.stopCtxErrs) != 2 {
		t.Fatalf("owner stop status=%d stops=%d body=%s", w.Code, len(owner.stopCtxErrs), w.Body.String())
	}
	w = kitCall(handler, http.MethodPost, "/api/v1/session/stop", launcherTokenB, launcherIDB, `{"target":"kit-a"}`)
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "TARGET_MISMATCH") || len(owner.stopCtxErrs) != 2 {
		t.Fatalf("cross-target stop status=%d stops=%d body=%s", w.Code, len(owner.stopCtxErrs), w.Body.String())
	}

	// No reported session id: the selected target is the owner.
	selected := newKitService("kit-a")
	handler = kitHandler(t, selected, distinctKitConfig())
	w = kitCall(handler, http.MethodPost, "/api/v1/session/stop", launcherTokenB, launcherIDB, "")
	if w.Code != http.StatusOK || len(selected.stopCtxErrs) != 1 {
		t.Fatalf("paired stop status=%d stops=%d body=%s", w.Code, len(selected.stopCtxErrs), w.Body.String())
	}
	w = kitCall(handler, http.MethodPost, "/api/v1/session/stop", launcherToken, launcherID, "")
	if w.Code != http.StatusOK || len(selected.stopCtxErrs) != 2 {
		t.Fatalf("selected stop status=%d stops=%d body=%s", w.Code, len(selected.stopCtxErrs), w.Body.String())
	}
}

func TestLauncherConfigValidate(t *testing.T) {
	validToken := strings.Repeat("c", 32)
	id := func(n int) string { return fmt.Sprintf("00000000-0000-4000-8000-%012x", n) }
	if err := (hostapi.LauncherConfig{}).Validate(); err == nil || err.Error() != "invalid launcher configuration" {
		t.Fatalf("empty: %v", err)
	}
	dup := hostapi.LauncherConfig{Pairings: []hostapi.LauncherPairing{
		{Token: validToken, TargetID: launcherID},
		{Token: launcherTokenB, TargetID: launcherID},
	}}
	if err := dup.Validate(); err == nil || err.Error() != "invalid launcher configuration" {
		t.Fatalf("duplicate: %v", err)
	}
	mixed := hostapi.LauncherConfig{
		Token: launcherToken, TargetID: launcherID,
		Pairings: []hostapi.LauncherPairing{{Token: launcherTokenB, TargetID: launcherIDB}},
	}
	if err := mixed.Validate(); err != nil {
		t.Fatalf("mixed: %v", err)
	}
	mixed.Pairings[0].Token = launcherToken
	if err := mixed.Validate(); !errors.Is(err, hostapi.ErrLauncherSharedToken) {
		t.Fatalf("mixed shared: %v", err)
	}
	tooMany := make([]hostapi.LauncherPairing, 33)
	for i := range tooMany {
		tooMany[i] = hostapi.LauncherPairing{Token: validToken + fmt.Sprintf("%02d", i), TargetID: id(i + 1)}
	}
	if err := (hostapi.LauncherConfig{Pairings: tooMany}).Validate(); err == nil || err.Error() != "invalid launcher configuration" {
		t.Fatalf("over 32: %v", err)
	}
	if err := (hostapi.LauncherConfig{Token: launcherToken}).Validate(); err == nil {
		t.Fatal("half top-level accepted")
	}
	if (hostapi.LauncherConfig{Token: launcherToken, TargetID: launcherID}).Validate() != nil {
		t.Fatal("single form rejected")
	}
}

// Kit A is playing and owns the foreground session. Kit B's launch must
// not preempt it: 409, no launch, no stop, A's play untouched. A's own
// relaunch still reaches LaunchOn. Once A stops (the service drops its
// play), B's launch goes through on kit B.
func TestLauncherSecondKitLaunchPreservesFirstPlay(t *testing.T) {
	playA := fogcast.PlaySession{Target: "kit-a", TargetID: launcherID, Execution: "fpga_native", GameID: "pong", System: protocol.SystemPong}
	service := newKitService("kit-a")
	service.sessionTarget, service.sessionTargetID = "kit-a", launcherID
	service.playSessions = []fogcast.PlaySession{playA}
	pongID := "pong"
	service.launch = protocol.CachedLaunchResponse{Status: protocol.Status{State: protocol.StateActive, GameID: &pongID, System: systemPtr(protocol.SystemPong)}}
	service.stopHook = func(context.Context) (protocol.Status, error) {
		plays := service.playSessions[:0]
		for _, play := range service.playSessions {
			if play.Target != "kit-a" {
				plays = append(plays, play)
			}
		}
		service.playSessions = plays
		service.sessionTarget, service.sessionTargetID = "", ""
		return protocol.Status{State: protocol.StateIdle}, nil
	}
	handler := kitHandler(t, service, distinctKitConfig())

	w := kitCall(handler, http.MethodPost, "/api/v1/session/launch", launcherTokenB, launcherIDB, `{"game_id":"pong"}`)
	if w.Code != http.StatusOK || service.launchOnCalls != 1 || service.launchOnTarget != "kit-b" {
		t.Fatalf("cross-kit launch: %d calls=%d target=%q %s", w.Code, service.launchOnCalls, service.launchOnTarget, w.Body.String())
	}
	if len(service.stopCtxErrs) != 0 || len(service.playSessions) != 2 || service.playSessions[0] != playA || service.playSessions[1].Target != "kit-b" {
		t.Fatalf("A disturbed: launches=%d stops=%d plays=%+v", service.launchOnCalls, len(service.stopCtxErrs), service.playSessions)
	}
	// Kit A's paired session view still reports A's play.
	if w := kitCall(handler, http.MethodGet, "/api/v1/session", launcherToken, launcherID, ""); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"game_id":"pong"`) {
		t.Fatalf("A view after B launch: %d %s", w.Code, w.Body.String())
	}

	w = kitCall(handler, http.MethodPost, "/api/v1/session/launch", launcherToken, launcherID, `{"game_id":"pong"}`)
	if w.Code != http.StatusOK || service.launchOnCalls != 2 || service.launchOnTarget != "kit-a" {
		t.Fatalf("A relaunch: %d calls=%d target=%q %s", w.Code, service.launchOnCalls, service.launchOnTarget, w.Body.String())
	}

	w = kitCall(handler, http.MethodPost, "/api/v1/session/stop", launcherToken, launcherID, "")
	if w.Code != http.StatusOK || len(service.stopCtxErrs) != 1 || len(service.playSessions) != 1 || service.playSessions[0].Target != "kit-b" {
		t.Fatalf("A stop: %d stops=%d %s", w.Code, len(service.stopCtxErrs), w.Body.String())
	}
	w = kitCall(handler, http.MethodPost, "/api/v1/session/launch", launcherTokenB, launcherIDB, `{"game_id":"pong"}`)
	if w.Code != http.StatusOK || service.launchOnCalls != 3 || service.launchOnTarget != "kit-b" {
		t.Fatalf("B launch after A stopped: %d calls=%d target=%q %s", w.Code, service.launchOnCalls, service.launchOnTarget, w.Body.String())
	}
}

// A background play on another kit also blocks, even when neither kit is
// the reported session owner. A play with no id or name match fails closed.
func TestLauncherRefusesLaunchWhileAnyOtherKitPlays(t *testing.T) {
	for _, play := range []fogcast.PlaySession{
		{Target: "kit-a", TargetID: launcherID, Execution: "fpga_native", GameID: "pong"},
		{Target: "unnamed", Execution: "fpga_native", GameID: "pong"},
	} {
		service := newKitService("kit-a")
		service.playSessions = []fogcast.PlaySession{play}
		handler := kitHandler(t, service, distinctKitConfig())
		w := kitCall(handler, http.MethodPost, "/api/v1/session/launch", launcherTokenB, launcherIDB, `{"game_id":"pong"}`)
		if w.Code != http.StatusOK || service.launchOnCalls != 1 || service.launchOnTarget != "kit-b" {
			t.Fatalf("play %+v: %d calls=%d %s", play, w.Code, service.launchOnCalls, w.Body.String())
		}
	}
	// B's own play (matched by id, or by name when the play has no id) does not block B.
	for _, play := range []fogcast.PlaySession{
		{Target: "kit-b", TargetID: launcherIDB, Execution: "fpga_native", GameID: "pong"},
		{Target: "kit-b", Execution: "fpga_native", GameID: "pong"},
	} {
		service := newKitService("kit-a")
		service.playSessions = []fogcast.PlaySession{play}
		handler := kitHandler(t, service, distinctKitConfig())
		w := kitCall(handler, http.MethodPost, "/api/v1/session/launch", launcherTokenB, launcherIDB, `{"game_id":"pong"}`)
		if w.Code != http.StatusOK || service.launchOnTarget != "kit-b" {
			t.Fatalf("own play %+v: %d target=%q %s", play, w.Code, service.launchOnTarget, w.Body.String())
		}
	}
}

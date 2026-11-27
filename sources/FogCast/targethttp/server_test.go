// SPDX-License-Identifier: GPL-3.0-or-later

package targethttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/kitlease"
	"github.com/DeanoC/FogCast/protocol"
)

type fakeController struct {
	mu       sync.Mutex
	state    protocol.State
	coreID   string
	software *protocol.Identity
	stopFn   func(context.Context) (protocol.Status, *protocol.APIError)
}

func (c *fakeController) Health(agentVersion string) protocol.Health {
	return protocol.Health{
		APIVersion:   "v1",
		BootID:       "test-boot",
		AgentVersion: agentVersion,
		Ready:        true,
		Software:     c.software,
	}
}

func (c *fakeController) Status() protocol.Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	var core *string
	if c.coreID != "" {
		v := c.coreID
		core = &v
	}
	return protocol.Status{
		State:        c.state,
		ObservedCore: core,
	}
}

func (c *fakeController) Stop(ctx context.Context) (protocol.Status, *protocol.APIError) {
	if c.stopFn != nil {
		return c.stopFn(ctx)
	}
	c.mu.Lock()
	c.state = protocol.StateIdle
	c.coreID = ""
	c.mu.Unlock()
	return c.Status(), nil
}

type fakeStore struct {
	mu           sync.Mutex
	packages     []corepackage.Inspection
	selections   map[string]string
	onSelections func()
}

func newFakeStore() *fakeStore {
	return &fakeStore{selections: make(map[string]string)}
}

func (s *fakeStore) List(context.Context) ([]corepackage.Inspection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]corepackage.Inspection(nil), s.packages...), nil
}

func (s *fakeStore) Read(_ context.Context, packageID string) (corepackage.Inspection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.packages {
		if p.PackageID == packageID {
			return p, nil
		}
	}
	return corepackage.Inspection{}, corepackage.ErrPackageNotFound
}

func (s *fakeStore) Selections(context.Context) ([]protocol.KitCoreSelection, error) {
	if s.onSelections != nil {
		s.onSelections()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]protocol.KitCoreSelection, 0, len(s.selections))
	for coreID, pkgID := range s.selections {
		out = append(out, protocol.KitCoreSelection{CoreID: coreID, PackageID: pkgID})
	}
	return out, nil
}

func (s *fakeStore) Install(_ context.Context, size int64, r io.Reader) (corepackage.Inspection, bool, error) {
	data, err := io.ReadAll(r)
	if err != nil || int64(len(data)) != size {
		return corepackage.Inspection{}, false, corepackage.ErrInvalidPackage
	}
	pkgID := strings.Repeat("a", 64)
	if len(data) > 0 && data[0] == 'b' {
		pkgID = strings.Repeat("b", 64)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.packages {
		if p.PackageID == pkgID {
			return p, false, nil
		}
	}
	pkg := corepackage.Inspection{PackageID: pkgID}
	pkg.Descriptor.Core.ID = "zx81"
	s.packages = append(s.packages, pkg)
	return pkg, true, nil
}

func (s *fakeStore) Select(_ context.Context, coreID string, req protocol.KitCoreSelectionRequest) *protocol.APIError {
	s.mu.Lock()
	defer s.mu.Unlock()
	if req.ExpectedPackageID != "" && s.selections[coreID] != req.ExpectedPackageID {
		return &protocol.APIError{Code: protocol.CodeCoreSelectionConflict, Message: "selection changed"}
	}
	found := false
	for _, p := range s.packages {
		if p.PackageID == req.PackageID {
			found = true
			if p.Descriptor.Core.ID != coreID {
				return &protocol.APIError{Code: protocol.CodeBadRequest, Message: "package belongs to another core"}
			}
		}
	}
	if !found {
		return &protocol.APIError{Code: protocol.CodeCorePackageNotFound, Message: "package not found"}
	}
	s.selections[coreID] = req.PackageID
	return nil
}

type fakeLauncher struct {
	ctrl *fakeController
}

func (l *fakeLauncher) Launch(_ context.Context, req LaunchRequest) (protocol.Status, *protocol.APIError) {
	l.ctrl.mu.Lock()
	l.ctrl.state = protocol.StateActive
	l.ctrl.coreID = req.CoreID
	l.ctrl.mu.Unlock()
	return l.ctrl.Status(), nil
}

type fakeDiagnostics struct{}

func (fakeDiagnostics) Diagnostics(context.Context) (any, *protocol.APIError) {
	return map[string]any{"uptime_ticks": 42, "board": "de25nano"}, nil
}

func TestConfigValidation(t *testing.T) {
	if _, err := New(Config{}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("nil controller = %v, want ErrInvalidConfig", err)
	}
	if _, err := New(Config{Controller: &fakeController{}}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("missing token without AllowUnauthenticated = %v, want ErrInvalidConfig", err)
	}
	if _, err := New(Config{Controller: &fakeController{}, Token: "secret", MaxUploadBytes: -1}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("negative MaxUploadBytes = %v, want ErrInvalidConfig", err)
	}
}

func TestRoutesAuthRedactionInventoryReconcileAndStats(t *testing.T) {
	ctrl := &fakeController{
		state: protocol.StateIdle,
		software: &protocol.Identity{
			ActiveBundle: "bundle-1",
			BaseIdentity: "base-1",
			Selected:     "bundle-1",
			Generation:   3,
		},
	}
	store := newFakeStore()
	h, err := New(Config{
		Controller:   ctrl,
		Token:        "test-token-123",
		TargetID:     "11111111-2222-4333-8444-555555555555",
		AgentVersion: "tamago-test",
		Packages:     store,
		Launcher:     &fakeLauncher{ctrl: ctrl},
		Diagnostics:  fakeDiagnostics{},
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	authReq := func(method, path, contentType string, body io.Reader, token string) *http.Response {
		req, err := http.NewRequest(method, srv.URL+path, body)
		if err != nil {
			t.Fatal(err)
		}
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}

	// Unauthenticated /v1/health succeeds with BOTH TargetID and Software redacted.
	r := authReq(http.MethodGet, "/v1/health", "", nil, "")
	var health protocol.Health
	if err := json.NewDecoder(r.Body).Decode(&health); err != nil {
		t.Fatal(err)
	}
	_ = r.Body.Close()
	if r.StatusCode != http.StatusOK || health.TargetID != "" || health.BootID != "" || health.Software != nil || health.AgentVersion != "tamago-test" {
		t.Fatalf("unauthenticated health must redact TargetID and Software: status=%d %+v", r.StatusCode, health)
	}

	// Authenticated /v1/health preserves both TargetID and Software.
	r = authReq(http.MethodGet, "/v1/health", "", nil, "test-token-123")
	_ = json.NewDecoder(r.Body).Decode(&health)
	_ = r.Body.Close()
	if health.TargetID != "11111111-2222-4333-8444-555555555555" || health.BootID != "test-boot" || health.Software == nil || health.Software.ActiveBundle != "bundle-1" {
		t.Fatalf("authenticated health missing TargetID/Software: %+v", health)
	}

	// Unauthenticated /v1/status rejects 401.
	r = authReq(http.MethodGet, "/v1/status", "", nil, "wrong-token")
	_ = r.Body.Close()
	if r.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status wrong token = %d, want 401", r.StatusCode)
	}

	// Install package -> 201 Created, reinstall -> 200 OK.
	payload := []byte("aaaa-fake-archive")
	r = authReq(http.MethodPost, "/v1/core-packages", "application/octet-stream", bytes.NewReader(payload), "test-token-123")
	var inst protocol.KitCoreInstall
	_ = json.NewDecoder(r.Body).Decode(&inst)
	_ = r.Body.Close()
	if r.StatusCode != http.StatusCreated || !inst.Created || inst.Package.PackageID != strings.Repeat("a", 64) {
		t.Fatalf("POST /v1/core-packages status=%d %+v", r.StatusCode, inst)
	}

	r = authReq(http.MethodPost, "/v1/core-packages", "application/octet-stream", bytes.NewReader(payload), "test-token-123")
	_ = json.NewDecoder(r.Body).Decode(&inst)
	_ = r.Body.Close()
	if r.StatusCode != http.StatusOK || inst.Created {
		t.Fatalf("duplicate POST /v1/core-packages status=%d %+v", r.StatusCode, inst)
	}

	// Select package -> 200 OK, conflict -> 409 Conflict.
	selBody := `{"expected_package_id":"","package_id":"` + strings.Repeat("a", 64) + `"}`
	r = authReq(http.MethodPut, "/v1/core-selections/zx81", "application/json", strings.NewReader(selBody), "test-token-123")
	_ = r.Body.Close()
	if r.StatusCode != http.StatusOK {
		t.Fatalf("PUT selection status=%d", r.StatusCode)
	}

	badSel := `{"expected_package_id":"` + strings.Repeat("0", 64) + `","package_id":"` + strings.Repeat("a", 64) + `"}`
	r = authReq(http.MethodPut, "/v1/core-selections/zx81", "application/json", strings.NewReader(badSel), "test-token-123")
	_ = r.Body.Close()
	if r.StatusCode != http.StatusConflict {
		t.Fatalf("conflict PUT selection status=%d, want 409", r.StatusCode)
	}

	// Simulate install+selection race between List and Selections: handler must reconcile via Read.
	racedPkgID := strings.Repeat("b", 64)
	store.onSelections = func() {
		store.onSelections = nil
		store.mu.Lock()
		defer store.mu.Unlock()
		pkg := corepackage.Inspection{PackageID: racedPkgID}
		pkg.Descriptor.Core.ID = "c64"
		store.packages = append(store.packages, pkg)
		store.selections["c64"] = racedPkgID
	}
	r = authReq(http.MethodGet, "/v1/core-packages", "", nil, "test-token-123")
	var list protocol.KitCorePackages
	_ = json.NewDecoder(r.Body).Decode(&list)
	_ = r.Body.Close()
	if r.StatusCode != http.StatusOK || len(list.Packages) != 2 || len(list.Selections) != 2 {
		t.Fatalf("GET /v1/core-packages race reconcile = %+v", list)
	}

	// GET /v1/core-selections.
	r = authReq(http.MethodGet, "/v1/core-selections", "", nil, "test-token-123")
	var sels []protocol.KitCoreSelection
	_ = json.NewDecoder(r.Body).Decode(&sels)
	_ = r.Body.Close()
	if r.StatusCode != http.StatusOK || len(sels) != 2 {
		t.Fatalf("GET /v1/core-selections = %d %+v", r.StatusCode, sels)
	}

	// Launch -> active -> Stop (reject non-empty body, allow empty) -> idle.
	r = authReq(http.MethodPost, "/v1/launch", "application/json", strings.NewReader(`{"core_id":"zx81"}`), "test-token-123")
	var st protocol.Status
	_ = json.NewDecoder(r.Body).Decode(&st)
	_ = r.Body.Close()
	if r.StatusCode != http.StatusOK || st.State != protocol.StateActive || st.ObservedCore == nil || *st.ObservedCore != "zx81" {
		t.Fatalf("POST /v1/launch = status=%d %+v", r.StatusCode, st)
	}

	r = authReq(http.MethodPost, "/v1/stop", "", strings.NewReader("unexpected"), "test-token-123")
	_ = r.Body.Close()
	if r.StatusCode != http.StatusBadRequest {
		t.Fatalf("non-empty stop body status=%d, want 400", r.StatusCode)
	}

	r = authReq(http.MethodPost, "/v1/stop", "", nil, "test-token-123")
	_ = json.NewDecoder(r.Body).Decode(&st)
	_ = r.Body.Close()
	if r.StatusCode != http.StatusOK || st.State != protocol.StateIdle {
		t.Fatalf("POST /v1/stop = status=%d %+v", r.StatusCode, st)
	}

	// Diagnostics.
	r = authReq(http.MethodGet, "/v1/diagnostics", "", nil, "test-token-123")
	_ = r.Body.Close()
	if r.StatusCode != http.StatusOK {
		t.Fatalf("GET /v1/diagnostics status=%d", r.StatusCode)
	}

	stats := h.Stats()
	if stats.Requests < 10 || stats.AuthFailures != 1 || stats.Errors < 3 || stats.ActiveRequests != 0 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
}

type leaseReporter struct {
	status kitlease.Status
	calls  int
}

func (l *leaseReporter) Status() kitlease.Status {
	l.calls++
	return l.status
}

func TestLeaseUsesCurrentAuthorityAndRequiresBearerEvenInOpenMode(t *testing.T) {
	const token = "a+/._~-=="
	lease := &leaseReporter{}
	handler, err := New(Config{Controller: &fakeController{}, KitLease: lease, Token: token, AllowUnauthenticated: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, headers := range [][]string{nil, {"Bearer "}, {"Bearer =invalid"}, {"Bearer wrong"}, {"Bearer " + token, "Bearer " + token}} {
		req := httptest.NewRequest(http.MethodGet, "/v1/kit/lease", nil)
		for _, v := range headers {
			req.Header.Add("Authorization", v)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized || lease.calls != 0 {
			t.Fatalf("unauthenticated lease status %d, authority reads %d", w.Code, lease.calls)
		}
	}
	for _, state := range []kitlease.Status{{State: "held", Generation: "g1", Owner: "local", Purpose: "playing"}, {State: "blocked", Generation: "g2", Reason: "card removed"}} {
		lease.status = state
		req := httptest.NewRequest(http.MethodGet, "/v1/kit/lease", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		var got kitlease.Status
		if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &got) != nil || got != state {
			t.Fatalf("lease response %d: %s", w.Code, w.Body.String())
		}
	}
	if lease.calls != 2 {
		t.Fatalf("authority reads = %d", lease.calls)
	}
}

func TestOpenHealthPublishesTargetAndSoftwareWithPrivateBootID(t *testing.T) {
	const target = "11111111-2222-4333-8444-555555555555"
	controller := &fakeController{software: &protocol.Identity{ActiveBundle: "open-bundle"}}
	handler, err := New(Config{Controller: controller, TargetID: target, AllowUnauthenticated: true})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/health", nil))
	var health protocol.Health
	if err := json.Unmarshal(w.Body.Bytes(), &health); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusOK || health.TargetID != target || health.Software == nil || health.Software.ActiveBundle != "open-bundle" || health.BootID != "" {
		t.Fatalf("open health %d: %+v", w.Code, health)
	}
}

func TestMissingLeaseProviderAndReadOnlyMutations(t *testing.T) {
	controller := &fakeController{state: protocol.StateActive, coreID: "zx81"}
	h, err := New(Config{Controller: controller, Token: "fixture", ReadOnly: true, Packages: newFakeStore(), Launcher: &fakeLauncher{ctrl: controller}})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		method, path string
		code         int
	}{
		{"GET", "/v1/kit/lease", http.StatusServiceUnavailable},
		{"POST", "/v1/stop", http.StatusNotFound},
		{"POST", "/v1/launch", http.StatusNotFound},
		{"POST", "/v1/core-packages", http.StatusMethodNotAllowed},
		{"PUT", "/v1/core-selections/zx81", http.StatusNotFound},
		{"POST", "/v1/kit/claim", http.StatusNotFound},
	} {
		req := httptest.NewRequest(test.method, test.path, nil)
		req.Header.Set("Authorization", "Bearer fixture")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != test.code {
			t.Errorf("%s %s = %d, want %d", test.method, test.path, w.Code, test.code)
		}
	}
	if controller.Status().State != protocol.StateActive {
		t.Fatal("read-only agent stopped the core")
	}
}

func TestConfiguredBearerUsesWireSyntax(t *testing.T) {
	for _, token := range []string{"=", "a=b", "two words", "line\nbreak", "value:secret"} {
		if ValidBearerToken(token) {
			t.Fatal("invalid configured bearer accepted")
		}
		if _, err := New(Config{Controller: &fakeController{}, Token: token}); !errors.Is(err, ErrInvalidConfig) || strings.Contains(err.Error(), token) {
			t.Fatal("expected a redacted configuration error")
		}
	}
	if ValidBearerToken("") || !ValidBearerToken("Aaz09._-~+/==") {
		t.Fatal("bearer grammar boundary")
	}
}

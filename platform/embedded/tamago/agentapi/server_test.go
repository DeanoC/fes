// SPDX-License-Identifier: GPL-3.0-or-later

package agentapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/fes/platform/embedded/tamago/network"
	gnet "github.com/usbarmory/go-net"
)

type fakeController struct {
	mu     sync.Mutex
	state  protocol.State
	coreID string
	stopFn func(context.Context) (protocol.Status, *protocol.APIError)
}

func (c *fakeController) Health(agentVersion string) protocol.Health {
	return protocol.Health{
		APIVersion:   "1",
		AgentVersion: agentVersion,
		Ready:        true,
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
	mu         sync.Mutex
	packages   []corepackage.Inspection
	selections map[string]string
}

func newFakeStore() *fakeStore {
	return &fakeStore{selections: make(map[string]string)}
}

func (s *fakeStore) List(context.Context) ([]corepackage.Inspection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]corepackage.Inspection(nil), s.packages...), nil
}

func (s *fakeStore) Selections(context.Context) ([]protocol.KitCoreSelection, error) {
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

func TestRoutesAuthPackagesLaunchAndStats(t *testing.T) {
	ctrl := &fakeController{state: protocol.StateIdle}
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

	// Unauthenticated /v1/health succeeds without TargetID.
	r := authReq(http.MethodGet, "/v1/health", "", nil, "")
	var health protocol.Health
	if err := json.NewDecoder(r.Body).Decode(&health); err != nil {
		t.Fatal(err)
	}
	_ = r.Body.Close()
	if r.StatusCode != http.StatusOK || health.TargetID != "" || health.AgentVersion != "tamago-test" {
		t.Fatalf("unauthenticated health = status=%d %+v", r.StatusCode, health)
	}

	// Authenticated /v1/health includes TargetID.
	r = authReq(http.MethodGet, "/v1/health", "", nil, "test-token-123")
	_ = json.NewDecoder(r.Body).Decode(&health)
	_ = r.Body.Close()
	if health.TargetID != "11111111-2222-4333-8444-555555555555" {
		t.Fatalf("authenticated health missing TargetID: %+v", health)
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

	// List packages & selections.
	r = authReq(http.MethodGet, "/v1/core-packages", "", nil, "test-token-123")
	var list protocol.KitCorePackages
	_ = json.NewDecoder(r.Body).Decode(&list)
	_ = r.Body.Close()
	if r.StatusCode != http.StatusOK || len(list.Packages) != 1 || len(list.Selections) != 1 {
		t.Fatalf("GET /v1/core-packages = %+v", list)
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
	if stats.Requests < 9 || stats.AuthFailures != 1 || stats.Errors < 3 || stats.ActiveRequests != 0 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
}

type wireDev struct {
	in  <-chan []byte
	out chan<- []byte
}

func (d wireDev) Receive(buf []byte) (int, error) {
	select {
	case f := <-d.in:
		return copy(buf, f), nil
	default:
		return 0, nil
	}
}

func (d wireDev) Transmit(frame []byte) error {
	select {
	case d.out <- bytes.Clone(frame):
		return nil
	default:
		return errors.New("queue full")
	}
}

func (d wireDev) Err() error { return nil }

func TestAgentAPIOverRealEthernetFrames(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cToS := make(chan []byte, 256)
	sToC := make(chan []byte, 256)
	serverStack := gnet.NewGVisorStack(1)
	clientStack := gnet.NewLnetoStack(gnet.DefaultLnetoStackConfig())

	serverIface := &gnet.Interface{
		Stack:         serverStack,
		NetworkDevice: wireDev{in: cToS, out: sToC},
	}
	if err := serverIface.Init("192.0.2.10/24", "02:aa:00:00:00:01", ""); err != nil {
		t.Fatal(err)
	}
	linkClient, err := network.NewLink(clientStack, wireDev{in: sToC, out: cToS}, "192.0.2.20/24", "02:aa:00:00:00:02", "")
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _ = serverIface.Start(ctx) }()
	go func() { defer wg.Done(); _ = linkClient.Start(ctx) }()
	defer func() {
		cancel()
		wg.Wait()
	}()

	sock, err := serverStack.Socket(ctx, "tcp4", syscall.AF_INET, syscall.SOCK_STREAM, &net.TCPAddr{IP: net.IPv4(192, 0, 2, 10), Port: 8080}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ln, ok := sock.(net.Listener)
	if !ok {
		t.Fatalf("expected net.Listener, got %T", sock)
	}

	ctrl := &fakeController{state: protocol.StateIdle}
	handler, err := New(Config{
		Controller:           ctrl,
		AllowUnauthenticated: true,
		TargetID:             "11111111-2222-4333-8444-555555555555",
		Packages:             newFakeStore(),
		Launcher:             &fakeLauncher{ctrl: ctrl},
	})
	if err != nil {
		t.Fatal(err)
	}

	httpSrv := &http.Server{Handler: handler}
	go func() { _ = httpSrv.Serve(ln) }()
	defer func() { _ = httpSrv.Close() }()

	transport := &http.Transport{
		DialContext: func(dctx context.Context, networkName, address string) (net.Conn, error) {
			raw, err := clientStack.Socket(dctx, "tcp4", syscall.AF_INET, syscall.SOCK_STREAM, nil, &net.TCPAddr{IP: net.IPv4(192, 0, 2, 10), Port: 8080})
			if err != nil {
				return nil, err
			}
			conn, ok := raw.(net.Conn)
			if !ok {
				return nil, errors.New("unexpected client conn type")
			}
			return conn, nil
		},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}

	resp, err := client.Post("http://192.0.2.10:8080/v1/launch", "application/json", strings.NewReader(`{"core_id":"c64"}`))
	if err != nil {
		t.Fatal(err)
	}
	var st protocol.Status
	err = json.NewDecoder(resp.Body).Decode(&st)
	_ = resp.Body.Close()
	if err != nil || resp.StatusCode != http.StatusOK || st.State != protocol.StateActive {
		t.Fatalf("remote launch over lneto frames failed: status=%d st=%+v err=%v", resp.StatusCode, st, err)
	}

	healthResp, err := client.Get("http://192.0.2.10:8080/v1/health")
	if err != nil {
		t.Fatal(err)
	}
	var health protocol.Health
	_ = json.NewDecoder(healthResp.Body).Decode(&health)
	_ = healthResp.Body.Close()
	if healthResp.StatusCode != http.StatusOK || health.TargetID == "" {
		t.Fatalf("health over lneto frames = %d %+v", healthResp.StatusCode, health)
	}
}

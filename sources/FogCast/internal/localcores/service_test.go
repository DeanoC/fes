package localcores

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/kitlease"
)

type fakeRuntime struct {
	mu        sync.Mutex
	loads     [][2]string
	stops     int
	loadErr   error
	stopErr   error
	onLoad    func()
	onStop    func()
	admission context.Context
	operation context.Context
}

func (f *fakeRuntime) LoadCore(admission, operation context.Context, path, packageID string) error {
	f.mu.Lock()
	f.admission = admission
	f.operation = operation
	f.loads = append(f.loads, [2]string{path, packageID})
	err := f.loadErr
	f.mu.Unlock()
	if f.onLoad != nil {
		f.onLoad()
	}
	return err
}

func (f *fakeRuntime) Stop(admission, operation context.Context) error {
	f.mu.Lock()
	f.admission = admission
	f.operation = operation
	f.stops++
	err := f.stopErr
	f.mu.Unlock()
	if f.onStop != nil {
		f.onStop()
	}
	return err
}

func (f *fakeRuntime) contexts() (admission, operation context.Context) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.admission, f.operation
}

func (f *fakeRuntime) calls() (loads [][2]string, stops int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][2]string(nil), f.loads...), f.stops
}

func waitFree(t *testing.T, manager *kitlease.Manager) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if manager.Status().State == "free" {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("lease state %s", manager.Status().State)
}

func claimHost(t *testing.T, manager *kitlease.Manager) {
	t.Helper()
	waitFree(t, manager)
	if _, err := manager.Claim(kitlease.ClaimRequest{
		RequestID: strings.Repeat("ab", 16),
		Owner:     "fogcast@host",
		Purpose:   "play",
	}); err != nil {
		t.Fatal(err)
	}
}

func testService(t *testing.T, runtime *fakeRuntime) (*Service, *kitlease.Manager, string, string) {
	t.Helper()
	root := t.TempDir()
	packages := filepath.Join(root, "pkgs")
	selections := filepath.Join(root, "sel")
	if err := os.Mkdir(packages, 0o755); err != nil || os.Mkdir(selections, 0o755) != nil {
		t.Fatal(err)
	}
	installCore(t, packages, selections, "fes.pong", "FES Pong", "fes.simple-game", coreOpts{})
	installCore(t, packages, selections, "fes.coleco", "FES Coleco", "fes.application", coreOpts{})
	manager := kitlease.New(time.Minute, func(context.Context) error { return nil })
	t.Cleanup(manager.Close)
	waitFree(t, manager)
	service := New(manager, runtime, Roots{Selections: selections, Packages: packages})
	t.Cleanup(service.Close)
	return service, manager, selections, packages
}

func post(handler http.Handler, path string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, path, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func errorCode(t *testing.T, response *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body.Error
}

func TestLaunchRefusedWhenHostHoldsLease(t *testing.T) {
	runtime := &fakeRuntime{}
	service, manager, _, packages := testService(t, runtime)
	claimHost(t, manager)
	cores := service.List()
	var pong Core
	for _, core := range cores {
		if core.CoreID == "fes.pong" {
			pong = core
		}
	}
	if pong.PackageID == "" {
		t.Fatal("pong missing")
	}
	handler := Handler(service)
	response := post(handler, "/v1/local/cores/"+pong.PackageID+"/launch")
	if response.Code != http.StatusConflict || errorCode(t, response) != "in_use" {
		t.Fatalf("launch %d %s", response.Code, response.Body.String())
	}
	loads, stops := runtime.calls()
	if len(loads) != 0 || stops != 0 {
		t.Fatalf("runtime calls loads=%v stops=%d", loads, stops)
	}
	status := manager.Status()
	if status.Owner != "fogcast@host" || status.State != "held" {
		t.Fatalf("lease changed %+v", status)
	}
	_ = packages
}

func TestStopRefusedForForeignOwner(t *testing.T) {
	runtime := &fakeRuntime{}
	service, manager, _, _ := testService(t, runtime)
	claimHost(t, manager)
	response := post(Handler(service), "/v1/local/stop")
	if response.Code != http.StatusConflict || errorCode(t, response) != "in_use" {
		t.Fatalf("stop %d %s", response.Code, response.Body.String())
	}
	_, stops := runtime.calls()
	if stops != 0 {
		t.Fatalf("runtime stop calls %d", stops)
	}
	if manager.Status().Owner != "fogcast@host" {
		t.Fatalf("owner %s", manager.Status().Owner)
	}
}

func TestLaunchAndStopByKitLocalOwner(t *testing.T) {
	runtime := &fakeRuntime{}
	service, manager, _, _ := testService(t, runtime)
	var pong Core
	for _, core := range service.List() {
		if core.CoreID == "fes.pong" {
			pong = core
		}
	}
	handler := Handler(service)
	launched := post(handler, "/v1/local/cores/"+pong.PackageID+"/launch")
	if launched.Code != http.StatusOK {
		t.Fatalf("launch %d %s", launched.Code, launched.Body.String())
	}
	status := manager.Status()
	if status.State != "held" || status.Owner != kitlease.HostlessOwner || status.Purpose != kitlease.LocalCorePurpose {
		t.Fatalf("lease %+v", status)
	}
	loads, _ := runtime.calls()
	if len(loads) != 1 || loads[0][0] != pong.installPath || loads[0][1] != pong.PackageID {
		t.Fatalf("load %+v want %s %s", loads, pong.installPath, pong.PackageID)
	}
	again := post(handler, "/v1/local/cores/"+pong.PackageID+"/launch")
	if again.Code != http.StatusConflict || errorCode(t, again) != "in_use" {
		t.Fatalf("second launch %d %s", again.Code, again.Body.String())
	}
	loads, _ = runtime.calls()
	if len(loads) != 1 {
		t.Fatalf("second launch called runtime %+v", loads)
	}
	stopped := post(handler, "/v1/local/stop")
	if stopped.Code != http.StatusOK {
		t.Fatalf("stop %d %s", stopped.Code, stopped.Body.String())
	}
	_, stops := runtime.calls()
	if stops != 1 {
		t.Fatalf("stops %d", stops)
	}
	waitFree(t, manager)
	if manager.Status().Owner != "" {
		t.Fatalf("lease still owned %+v", manager.Status())
	}
}

func TestBlockedAndUnknownLaunchMakeNoRuntimeCall(t *testing.T) {
	runtime := &fakeRuntime{}
	service, manager, _, _ := testService(t, runtime)
	var coleco string
	for _, core := range service.List() {
		if core.CoreID == "fes.coleco" {
			coleco = core.PackageID
		}
	}
	handler := Handler(service)
	blocked := post(handler, "/v1/local/cores/"+coleco+"/launch")
	if blocked.Code != http.StatusConflict || errorCode(t, blocked) != "blocked" {
		t.Fatalf("blocked %d %s", blocked.Code, blocked.Body.String())
	}
	unknown := post(handler, "/v1/local/cores/"+strings.Repeat("cd", 32)+"/launch")
	if unknown.Code != http.StatusNotFound || errorCode(t, unknown) != "not_found" {
		t.Fatalf("unknown %d %s", unknown.Code, unknown.Body.String())
	}
	loads, stops := runtime.calls()
	if len(loads) != 0 || stops != 0 {
		t.Fatalf("runtime calls loads=%v stops=%d", loads, stops)
	}
	if manager.Status().State != "free" {
		t.Fatalf("lease %s", manager.Status().State)
	}
}

func TestFailedLoadReleasesLease(t *testing.T) {
	runtime := &fakeRuntime{loadErr: context.DeadlineExceeded}
	service, manager, _, _ := testService(t, runtime)
	var pong string
	for _, core := range service.List() {
		if core.CoreID == "fes.pong" {
			pong = core.PackageID
		}
	}
	response := post(Handler(service), "/v1/local/cores/"+pong+"/launch")
	if response.Code != http.StatusServiceUnavailable || errorCode(t, response) != "unavailable" {
		t.Fatalf("launch %d %s", response.Code, response.Body.String())
	}
	loads, stops := runtime.calls()
	if len(loads) != 1 || stops != 0 {
		t.Fatalf("loads=%v stops=%d", loads, stops)
	}
	waitFree(t, manager)
}

func pongID(t *testing.T, service *Service) string {
	t.Helper()
	for _, core := range service.List() {
		if core.CoreID == "fes.pong" {
			return core.PackageID
		}
	}
	t.Fatal("pong missing")
	return ""
}

type countingLease struct {
	*kitlease.Manager
	mu     sync.Mutex
	renews int
}

func (c *countingLease) Renew(token string) (kitlease.Grant, error) {
	c.mu.Lock()
	c.renews++
	c.mu.Unlock()
	return c.Manager.Renew(token)
}

func (c *countingLease) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.renews
}

func TestLaunchRenewsLeasePastTTLUntilStop(t *testing.T) {
	const ttl = time.Second
	runtime := &fakeRuntime{}
	root := t.TempDir()
	packages := filepath.Join(root, "pkgs")
	selections := filepath.Join(root, "sel")
	if err := os.Mkdir(packages, 0o755); err != nil || os.Mkdir(selections, 0o755) != nil {
		t.Fatal(err)
	}
	installCore(t, packages, selections, "fes.pong", "FES Pong", "fes.simple-game", coreOpts{})
	manager := kitlease.New(ttl, func(context.Context) error { return nil })
	t.Cleanup(manager.Close)
	waitFree(t, manager)
	gate := &countingLease{Manager: manager}
	service := New(gate, runtime, Roots{Selections: selections, Packages: packages})
	t.Cleanup(service.Close)
	id := pongID(t, service)
	if _, err := service.Launch(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	launched := time.Now()
	deadline := time.Now().Add(5 * time.Second)
	for {
		status := manager.Status()
		if status.State != "held" || status.Purpose != kitlease.LocalCorePurpose {
			t.Fatalf("lease dropped before renewal: %+v", status)
		}
		if time.Since(launched) > ttl && gate.count() > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("lease was not renewed past the TTL")
		}
		time.Sleep(ttl / 10)
	}
	if err := service.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitFree(t, manager)
	stopped := gate.count()
	time.Sleep(ttl)
	if gate.count() != stopped {
		t.Fatalf("renew continued after stop: %d -> %d", stopped, gate.count())
	}
	if manager.Status().State != "free" {
		t.Fatalf("lease %+v", manager.Status())
	}
}

func TestShutdownStopsLeaseRenewal(t *testing.T) {
	const ttl = time.Second
	runtime := &fakeRuntime{}
	root := t.TempDir()
	packages := filepath.Join(root, "pkgs")
	selections := filepath.Join(root, "sel")
	if err := os.Mkdir(packages, 0o755); err != nil || os.Mkdir(selections, 0o755) != nil {
		t.Fatal(err)
	}
	installCore(t, packages, selections, "fes.pong", "FES Pong", "fes.simple-game", coreOpts{})
	manager := kitlease.New(ttl, func(context.Context) error { return nil })
	t.Cleanup(manager.Close)
	waitFree(t, manager)
	gate := &countingLease{Manager: manager}
	service := New(gate, runtime, Roots{Selections: selections, Packages: packages})
	t.Cleanup(service.Close)
	if _, err := service.Launch(context.Background(), pongID(t, service)); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for gate.count() < 1 {
		if time.Now().After(deadline) {
			t.Fatal("renew did not run before shutdown")
		}
		time.Sleep(10 * time.Millisecond)
	}
	service.Close()
	stopped := gate.count()
	time.Sleep(ttl / 2)
	if gate.count() != stopped {
		t.Fatalf("renew continued after shutdown: %d -> %d", stopped, gate.count())
	}
	waitFree(t, manager)
}

type remainingLease struct {
	*kitlease.Manager
	mu        sync.Mutex
	renews    int
	remaining int64
}

func (r *remainingLease) Claim(request kitlease.ClaimRequest) (kitlease.Grant, error) {
	grant, err := r.Manager.Claim(request)
	if err != nil {
		return grant, err
	}
	grant.Status.ExpiresInMS = r.remaining
	return grant, nil
}

func (r *remainingLease) Renew(token string) (kitlease.Grant, error) {
	r.mu.Lock()
	r.renews++
	remaining := r.remaining
	r.mu.Unlock()
	grant, err := r.Manager.Renew(token)
	if err != nil {
		return grant, err
	}
	grant.Status.ExpiresInMS = remaining
	return grant, nil
}

func (r *remainingLease) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.renews
}

func TestRenewArmsAtClaimUsingTimeRemaining(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	runtime := &fakeRuntime{onLoad: func() {
		once.Do(func() { close(entered) })
		<-release
	}}
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	root := t.TempDir()
	packages := filepath.Join(root, "pkgs")
	selections := filepath.Join(root, "sel")
	if err := os.Mkdir(packages, 0o755); err != nil || os.Mkdir(selections, 0o755) != nil {
		t.Fatal(err)
	}
	installCore(t, packages, selections, "fes.pong", "FES Pong", "fes.simple-game", coreOpts{})
	manager := kitlease.New(time.Minute, func(context.Context) error { return nil })
	t.Cleanup(manager.Close)
	waitFree(t, manager)
	gate := &remainingLease{Manager: manager, remaining: 80}
	service := New(gate, runtime, Roots{Selections: selections, Packages: packages})
	t.Cleanup(service.Close)
	done := make(chan error, 1)
	go func() {
		_, err := service.Launch(context.Background(), pongID(t, service))
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("load did not start")
	}
	deadline := time.Now().Add(time.Second)
	for gate.count() < 2 {
		if manager.Status().State != "held" {
			t.Fatalf("lease dropped during load: %+v", manager.Status())
		}
		if time.Now().After(deadline) {
			t.Fatalf("renew did not stay armed during load (count=%d)", gate.count())
		}
		time.Sleep(5 * time.Millisecond)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestClientCancelDoesNotAbortLoadOrStop(t *testing.T) {
	t.Run("load", func(t *testing.T) {
		entered := make(chan struct{})
		release := make(chan struct{})
		var once sync.Once
		runtime := &fakeRuntime{onLoad: func() {
			once.Do(func() { close(entered) })
			<-release
		}}
		t.Cleanup(func() {
			select {
			case <-release:
			default:
				close(release)
			}
		})
		service, _, _, _ := testService(t, runtime)
		request, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() {
			_, err := service.Launch(request, pongID(t, service))
			done <- err
		}()
		select {
		case <-entered:
		case <-time.After(2 * time.Second):
			t.Fatal("load did not start")
		}
		cancel()
		time.Sleep(30 * time.Millisecond)
		admission, operation := runtime.contexts()
		if admission == nil || admission.Err() == nil {
			t.Fatal("admission was not the client context")
		}
		if operation == nil || operation.Err() != nil {
			t.Fatalf("client cancel aborted load: %v", operation.Err())
		}
		close(release)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	})
	t.Run("stop", func(t *testing.T) {
		entered := make(chan struct{})
		release := make(chan struct{})
		var once sync.Once
		runtime := &fakeRuntime{onStop: func() {
			once.Do(func() { close(entered) })
			<-release
		}}
		t.Cleanup(func() {
			select {
			case <-release:
			default:
				close(release)
			}
		})
		service, _, _, _ := testService(t, runtime)
		if _, err := service.Launch(context.Background(), pongID(t, service)); err != nil {
			t.Fatal(err)
		}
		request, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() {
			done <- service.Stop(request)
		}()
		select {
		case <-entered:
		case <-time.After(2 * time.Second):
			t.Fatal("stop did not start")
		}
		cancel()
		time.Sleep(30 * time.Millisecond)
		admission, operation := runtime.contexts()
		if admission == nil || admission.Err() == nil {
			t.Fatal("admission was not the client context")
		}
		if operation == nil || operation.Err() != nil {
			t.Fatalf("client cancel aborted stop: %v", operation.Err())
		}
		close(release)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	})
}

func TestSealedZX81LaunchIsBlocked(t *testing.T) {
	runtime := &fakeRuntime{}
	root := t.TempDir()
	packages := filepath.Join(root, "pkgs")
	selections := filepath.Join(root, "sel")
	if err := os.Mkdir(packages, 0o755); err != nil || os.Mkdir(selections, 0o755) != nil {
		t.Fatal(err)
	}
	installCore(t, packages, selections, "fes.zx81", "ZX81  (sealed)", "fes.simple-computer", coreOpts{
		rom: []byte("{\"format\":1}\n"),
	})
	manager := kitlease.New(time.Minute, func(context.Context) error { return nil })
	t.Cleanup(manager.Close)
	waitFree(t, manager)
	service := New(manager, runtime, Roots{Selections: selections, Packages: packages})
	t.Cleanup(service.Close)
	var id string
	for _, core := range service.List() {
		if core.CoreID != "fes.zx81" {
			continue
		}
		id = core.PackageID
		if core.Needs != "firmware" || core.Block != "Needs firmware" || core.Launchable {
			t.Fatalf("classified %+v", core)
		}
	}
	if id == "" {
		t.Fatal("zx81 missing")
	}
	response := post(Handler(service), "/v1/local/cores/"+id+"/launch")
	if response.Code != http.StatusConflict || errorCode(t, response) != "blocked" {
		t.Fatalf("launch %d %s", response.Code, response.Body.String())
	}
	loads, stops := runtime.calls()
	if len(loads) != 0 || stops != 0 {
		t.Fatalf("runtime calls loads=%v stops=%d", loads, stops)
	}
	if manager.Status().State != "free" {
		t.Fatalf("lease %s", manager.Status().State)
	}
}

func TestHostClaimDuringLoadIsBusy(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	runtime := &fakeRuntime{onLoad: func() {
		once.Do(func() { close(entered) })
		<-release
	}}
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	service, manager, _, _ := testService(t, runtime)
	id := pongID(t, service)
	done := make(chan error, 1)
	go func() {
		_, err := service.Launch(context.Background(), id)
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("load did not start")
	}
	_, err := manager.Claim(kitlease.ClaimRequest{
		RequestID: strings.Repeat("ab", 16),
		Owner:     "fogcast@host",
		Purpose:   "play",
	})
	if !errors.Is(err, kitlease.ErrBusy) {
		t.Fatalf("host claim %v", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	loads, stops := runtime.calls()
	if len(loads) != 1 || stops != 0 {
		t.Fatalf("loads=%v stops=%d", loads, stops)
	}
	status := manager.Status()
	if status.State != "held" || status.Owner != kitlease.HostlessOwner || status.Purpose != kitlease.LocalCorePurpose {
		t.Fatalf("lease %+v", status)
	}
}

func TestBlockedAndRevokingLeaseLaunchReturnsConflict(t *testing.T) {
	t.Run("blocked", func(t *testing.T) {
		runtime := &fakeRuntime{}
		manager := kitlease.New(time.Minute, func(context.Context) error {
			return errors.New("cleanup failed")
		})
		t.Cleanup(manager.Close)
		deadline := time.Now().Add(2 * time.Second)
		for manager.Status().State != "blocked" {
			if time.Now().After(deadline) {
				t.Fatalf("lease state %s", manager.Status().State)
			}
			time.Sleep(time.Millisecond)
		}
		service, id := pongService(t, manager, runtime)
		response := post(Handler(service), "/v1/local/cores/"+id+"/launch")
		if response.Code != http.StatusConflict || errorCode(t, response) != "in_use" {
			t.Fatalf("launch %d %s", response.Code, response.Body.String())
		}
		loads, stops := runtime.calls()
		if len(loads) != 0 || stops != 0 {
			t.Fatalf("runtime calls loads=%v stops=%d", loads, stops)
		}
	})
	t.Run("revoking", func(t *testing.T) {
		runtime := &fakeRuntime{}
		hold := make(chan struct{})
		manager := kitlease.New(time.Minute, func(ctx context.Context) error {
			select {
			case <-hold:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
		t.Cleanup(manager.Close)
		t.Cleanup(func() {
			select {
			case <-hold:
			default:
				close(hold)
			}
		})
		if manager.Status().State != "revoking" {
			t.Fatalf("lease state %s", manager.Status().State)
		}
		service, id := pongService(t, manager, runtime)
		response := post(Handler(service), "/v1/local/cores/"+id+"/launch")
		if response.Code != http.StatusConflict || errorCode(t, response) != "in_use" {
			t.Fatalf("launch %d %s", response.Code, response.Body.String())
		}
		loads, stops := runtime.calls()
		if len(loads) != 0 || stops != 0 {
			t.Fatalf("runtime calls loads=%v stops=%d", loads, stops)
		}
	})
}

func pongService(t *testing.T, manager *kitlease.Manager, runtime Runtime) (*Service, string) {
	t.Helper()
	root := t.TempDir()
	packages := filepath.Join(root, "pkgs")
	selections := filepath.Join(root, "sel")
	if err := os.Mkdir(packages, 0o755); err != nil || os.Mkdir(selections, 0o755) != nil {
		t.Fatal(err)
	}
	installCore(t, packages, selections, "fes.pong", "FES Pong", "fes.simple-game", coreOpts{})
	service := New(manager, runtime, Roots{Selections: selections, Packages: packages})
	t.Cleanup(service.Close)
	return service, pongID(t, service)
}

type foreignGrant struct {
	releases int
}

func (g *foreignGrant) Status() kitlease.Status {
	return kitlease.Status{State: "free"}
}

func (g *foreignGrant) Claim(kitlease.ClaimRequest) (kitlease.Grant, error) {
	return kitlease.Grant{
		Token:  "foreign-token",
		Status: kitlease.Status{State: "held", Owner: "fogcast@host", Purpose: "play"},
	}, nil
}

func (g *foreignGrant) Renew(string) (kitlease.Grant, error) {
	return kitlease.Grant{}, errors.New("renew")
}

func (g *foreignGrant) Release(token string) (kitlease.Status, error) {
	if token != "foreign-token" {
		return kitlease.Status{}, errors.New("token")
	}
	g.releases++
	return kitlease.Status{State: "free"}, nil
}

func (g *foreignGrant) Begin(string) (context.Context, func(), error) {
	return nil, nil, errors.New("begin")
}

func TestClaimWithoutLocalSessionReleases(t *testing.T) {
	runtime := &fakeRuntime{}
	root := t.TempDir()
	packages := filepath.Join(root, "pkgs")
	selections := filepath.Join(root, "sel")
	if err := os.Mkdir(packages, 0o755); err != nil || os.Mkdir(selections, 0o755) != nil {
		t.Fatal(err)
	}
	installCore(t, packages, selections, "fes.pong", "FES Pong", "fes.simple-game", coreOpts{})
	gate := &foreignGrant{}
	service := New(gate, runtime, Roots{Selections: selections, Packages: packages})
	t.Cleanup(service.Close)
	_, err := service.Launch(context.Background(), pongID(t, service))
	if !errors.Is(err, errInUse) {
		t.Fatalf("launch %v", err)
	}
	if gate.releases != 1 {
		t.Fatalf("releases %d", gate.releases)
	}
	loads, stops := runtime.calls()
	if len(loads) != 0 || stops != 0 {
		t.Fatalf("runtime calls loads=%v stops=%d", loads, stops)
	}
}

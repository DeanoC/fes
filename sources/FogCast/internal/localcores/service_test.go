package localcores

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/kitlease"
)

type fakeRuntime struct {
	mu        sync.Mutex
	loads     [][2]string
	romBytes  [][]byte
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

func (f *fakeRuntime) LoadCartridge(admission, operation context.Context, path, packageID string, rom []byte) error {
	f.mu.Lock()
	f.admission = admission
	f.operation = operation
	f.romBytes = append(f.romBytes, append([]byte(nil), rom...))
	err := f.loadErr
	f.mu.Unlock()
	if f.onLoad != nil {
		f.onLoad()
	}
	return err
}

func (f *fakeRuntime) roms() [][]byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([][]byte, len(f.romBytes))
	for i, rom := range f.romBytes {
		out[i] = append([]byte(nil), rom...)
	}
	return out
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
	installCore(t, packages, selections, "fes.coleco", "FES Coleco", "fes.application", coreOpts{cartridge: true})
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

func TestRevokeWhileRunningClearsRunStatus(t *testing.T) {
	t.Run("takeover", func(t *testing.T) {
		runtime := &fakeRuntime{}
		service, manager, _, _ := testService(t, runtime)
		if _, err := service.Launch(context.Background(), pongID(t, service)); err != nil {
			t.Fatal(err)
		}
		if st := service.RunStatus(); !st.Running || st.Phase != phaseRunning {
			t.Fatalf("before %+v", st)
		}
		status := manager.Status()
		_, err := manager.Takeover(kitlease.TakeoverRequest{
			ClaimRequest: kitlease.ClaimRequest{
				RequestID: strings.Repeat("cd", 16),
				Owner:     "fogcast@host",
				Purpose:   "play",
			},
			ExpectedGeneration: status.Generation,
			Reason:             "operator takeover",
		})
		if !errors.Is(err, kitlease.ErrBusy) {
			t.Fatalf("takeover %v", err)
		}
		// Renew interval is a quarter of the minute TTL. Clearing has to
		// follow the cancelled lease context, not that tick.
		waitSessionCleared(t, service, time.Second)
	})
	t.Run("expiry", func(t *testing.T) {
		const ttl = 2 * time.Second
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
		// Report a long remainder so the renew timer is not the signal.
		gate := &remainingLease{Manager: manager, remaining: 120_000}
		service := New(gate, runtime, Roots{Selections: selections, Packages: packages})
		t.Cleanup(service.Close)
		if _, err := service.Launch(context.Background(), pongID(t, service)); err != nil {
			t.Fatal(err)
		}
		waitSessionCleared(t, service, 5*time.Second)
		if gate.count() != 0 {
			t.Fatalf("cleared by renew, not the lease context: %d", gate.count())
		}
	})
	t.Run("renew failure", func(t *testing.T) {
		runtime := &fakeRuntime{}
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
		gate := &failingRenewLease{Manager: manager, remaining: 4_000}
		service := New(gate, runtime, Roots{Selections: selections, Packages: packages})
		t.Cleanup(service.Close)
		if _, err := service.Launch(context.Background(), pongID(t, service)); err != nil {
			t.Fatal(err)
		}
		waitSessionCleared(t, service, 4*time.Second)
		if gate.count() < 1 {
			t.Fatal("renew did not fail")
		}
	})
}

type failingRenewLease struct {
	*kitlease.Manager
	mu        sync.Mutex
	renews    int
	remaining int64
}

func (f *failingRenewLease) Claim(request kitlease.ClaimRequest) (kitlease.Grant, error) {
	grant, err := f.Manager.Claim(request)
	if err != nil {
		return grant, err
	}
	grant.Status.ExpiresInMS = f.remaining
	return grant, nil
}

func (f *failingRenewLease) Renew(string) (kitlease.Grant, error) {
	f.mu.Lock()
	f.renews++
	f.mu.Unlock()
	return kitlease.Grant{}, errors.New("renew")
}

func (f *failingRenewLease) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.renews
}

func waitSessionCleared(t *testing.T, service *Service, budget time.Duration) {
	t.Helper()
	deadline := time.Now().Add(budget)
	for {
		st := service.RunStatus()
		service.mu.Lock()
		token := service.token
		phase := service.phase
		pkg := service.runPackage
		service.mu.Unlock()
		if !st.Running && st.Phase != phaseRunning && token == "" && phase == phaseIdle && pkg == "" && st.PackageID == "" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("session still live status=%+v token=%q phase=%s package=%s", st, token, phase, pkg)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestRunStatusLaunchingThenRunningThenIdle(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	runtime := &fakeRuntime{onLoad: func() {
		close(started)
		<-release
	}}
	service, _, _, _ := testService(t, runtime)
	var pong Core
	for _, core := range service.List() {
		if core.CoreID == "fes.pong" && core.Launchable {
			pong = core
		}
	}
	if pong.PackageID == "" {
		t.Fatal("launchable pong missing")
	}
	errc := make(chan error, 1)
	go func() {
		_, err := service.Launch(context.Background(), pong.PackageID)
		errc <- err
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("load did not start")
	}
	if st := service.RunStatus(); st.Phase != phaseLaunching || st.Running || st.PackageID != pong.PackageID || st.CoreID != pong.CoreID {
		t.Fatalf("launching %+v", st)
	}
	close(release)
	if err := <-errc; err != nil {
		t.Fatal(err)
	}
	if st := service.RunStatus(); st.Phase != phaseRunning || !st.Running || st.PackageID != pong.PackageID || st.CoreID != pong.CoreID {
		t.Fatalf("running %+v", st)
	}
	response := httptest.NewRecorder()
	Handler(service).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/local/status", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"running":true`) || !strings.Contains(response.Body.String(), `"core_id":"`+pong.CoreID+`"`) {
		t.Fatalf("status %d %s", response.Code, response.Body.String())
	}
	if err := service.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if st := service.RunStatus(); st.Phase != phaseIdle || st.Running || st.CoreID != "" || st.PackageID != "" {
		t.Fatalf("idle %+v", st)
	}
}

func TestDurableSelectionChangesOnlyFutureLaunch(t *testing.T) {
	runtime := &fakeRuntime{}
	service, _, _, _ := testService(t, runtime)
	factory := service.List()[1]
	for _, core := range service.List() {
		if core.CoreID == "fes.pong" {
			factory = core
		}
	}
	var selected []Core
	service.SetInventory(func() ([]Core, error) { return selected, nil })
	if _, err := service.Launch(context.Background(), factory.PackageID); err != nil {
		t.Fatal(err)
	}
	upgraded := factory
	upgraded.PackageID = strings.Repeat("c", 64)
	upgraded.installPath = "/durable/" + upgraded.PackageID
	selected = []Core{upgraded}
	listed := service.List()
	if len(listed) != 2 {
		t.Fatalf("factory inventory changed: %+v", listed)
	}
	for _, core := range listed {
		if core.CoreID == factory.CoreID && core.PackageID != upgraded.PackageID {
			t.Fatal("selected package did not override factory")
		}
	}
	if service.RunStatus().PackageID != factory.PackageID {
		t.Fatal("selection replaced running core")
	}
	if loads, stops := runtime.calls(); len(loads) != 1 || stops != 0 {
		t.Fatal("selection mutated runtime")
	}
	if err := service.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Launch(context.Background(), factory.PackageID); !errors.Is(err, errNotFound) {
		t.Fatalf("old selection launch %v", err)
	}
	if _, err := service.Launch(context.Background(), upgraded.PackageID); err != nil {
		t.Fatal(err)
	}
	loads, _ := runtime.calls()
	if len(loads) != 2 || loads[1][0] != upgraded.installPath || loads[1][1] != upgraded.PackageID {
		t.Fatalf("wrong future launch: %+v", loads)
	}
	if err := service.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	service.SetInventory(func() ([]Core, error) { return nil, errors.New("corrupt selection") })
	if len(service.List()) != 0 {
		t.Fatal("corrupt selection silently fell back to factory")
	}
}

func TestLocalLaunchRespectsUpdateAdmissionBeforeClaim(t *testing.T) {
	runtime := &fakeRuntime{}
	service, manager, _, _ := testService(t, runtime)
	service.SetAdmission(func(context.Context) (context.Context, func(), error) { return nil, nil, errors.New("trial") })
	for _, core := range service.List() {
		if core.CoreID == "fes.pong" {
			if _, err := service.Launch(context.Background(), core.PackageID); !errors.Is(err, errInUse) {
				t.Fatalf("launch %v", err)
			}
		}
	}
	if loads, stops := runtime.calls(); len(loads) != 0 || stops != 0 {
		t.Fatal("blocked update touched hardware")
	}
	if manager.Status().State != "free" {
		t.Fatal("blocked update claimed lease")
	}
}

func TestApplianceUpdateCancelsAndDrainsLocalLaunch(t *testing.T) {
	started := make(chan struct{})
	runtime := &fakeRuntime{}
	service, manager, _, _ := testService(t, runtime)
	updateCtx, cancelUpdate := context.WithCancel(context.Background())
	defer cancelUpdate()
	drained := make(chan struct{})
	service.SetAdmission(func(context.Context) (context.Context, func(), error) {
		return updateCtx, func() { close(drained) }, nil
	})
	runtime.onLoad = func() { close(started); _, operation := runtime.contexts(); <-operation.Done() }
	var id string
	for _, core := range service.List() {
		if core.CoreID == "fes.pong" {
			id = core.PackageID
		}
	}
	result := make(chan error, 1)
	go func() { _, err := service.Launch(context.Background(), id); result <- err }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("local launch did not start")
	}
	cancelUpdate()
	select {
	case err := <-result:
		if !errors.Is(err, errUnavailable) {
			t.Fatalf("launch %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("update did not cancel local launch")
	}
	select {
	case <-drained:
	default:
		t.Fatal("local launch did not leave update admission")
	}
	if service.RunStatus().Running {
		t.Fatal("canceled launch published running session")
	}
	waitFree(t, manager)
}

// A missing durable store must answer GET /v1/local/cores without opening
// factory payloads. Reading those files is what grew the kit agent until the
// OOM killer ran.
func TestUnavailableStoreListIsBounded(t *testing.T) {
	root := t.TempDir()
	selections := filepath.Join(root, "selections")
	packages := filepath.Join(root, "packages")
	if err := os.Mkdir(selections, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(packages, 0o755); err != nil {
		t.Fatal(err)
	}
	packageID := strings.Repeat("ab", 32)
	install := filepath.Join(packages, packageID)
	if err := os.Mkdir(install, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := []byte("format = 2\n")
	if err := os.WriteFile(filepath.Join(install, "manifest.toml"), manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	payload, err := os.Create(filepath.Join(install, "core.rbf"))
	if err != nil {
		t.Fatal(err)
	}
	const payloadBytes = 4 << 20
	if _, err := io.Copy(payload, io.LimitReader(zeroReader{}, payloadBytes)); err != nil {
		t.Fatal(err)
	}
	if err := payload.Close(); err != nil {
		t.Fatal(err)
	}
	selection := "core_id = \"fes.pong\"\npackage_id = \"" + packageID + "\"\npayload_sha256 = \"" + strings.Repeat("cd", 32) + "\"\ninstall_path = \"" + install + "\"\n"
	if err := os.WriteFile(filepath.Join(selections, "fes-pong.package.toml"), []byte(selection), 0o644); err != nil {
		t.Fatal(err)
	}
	service := New(nil, nil, Roots{Selections: selections, Packages: packages})
	service.SetInventory(func() ([]Core, error) {
		return nil, errors.New("durable core package storage is unavailable")
	})

	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	started := time.Now()
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		response := httptest.NewRecorder()
		Handler(service).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/local/cores", nil))
		done <- response
	}()
	var response *httptest.ResponseRecorder
	select {
	case response = <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("GET /v1/local/cores hung while the durable store was unavailable")
	}
	elapsed := time.Since(started)
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	if response.Code != http.StatusOK || strings.TrimSpace(response.Body.String()) != "[]" {
		t.Fatalf("list %d %s", response.Code, response.Body.String())
	}
	if elapsed > time.Second {
		t.Fatalf("list took %s", elapsed)
	}
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 1<<20 {
		t.Fatalf("list allocated %d bytes while the store was unavailable", allocated)
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

func useFactoryListGate(t *testing.T, fn func(context.Context) error) {
	t.Helper()
	factoryListGateMu.Lock()
	factoryListGate = fn
	factoryListGateMu.Unlock()
	t.Cleanup(func() {
		factoryListGateMu.Lock()
		factoryListGate = nil
		factoryListGateMu.Unlock()
	})
}

// An open empty durable store still lists the factory catalog. The stock
// agent streams payload hashes; this path must do the same when the store
// is available, including a multi-megabyte ROM map, and a second request
// must reuse that result. A digest-matching map that fails structural
// parsing is streamed and omitted.
func TestOpenStoreFactoryListIsBounded(t *testing.T) {
	root := t.TempDir()
	selections := filepath.Join(root, "selections")
	packages := filepath.Join(root, "packages")
	if err := os.Mkdir(selections, 0o755); err != nil || os.Mkdir(packages, 0o755) != nil {
		t.Fatal(err)
	}
	const payloadBytes = 2 << 20
	installSizedCore(t, packages, selections, "fes.menu", "FES Menu", "fes.menu", payloadBytes, 0, "")
	installSizedCore(t, packages, selections, "fes.pong", "FES Pong", "fes.simple-game", payloadBytes, 0, "")
	installSizedCore(t, packages, selections, "fes.ramtest", "FES RAM Tester", "fes.application", payloadBytes, 0, "")
	installSizedCore(t, packages, selections, "fes.sms", "FES Master System", "fes.simple-computer", payloadBytes, payloadBytes, "cartridge")
	installSizedCore(t, packages, selections, "fes.coleco", "FES Coleco", "fes.application", payloadBytes, 1024, "cartridge")
	installSizedCore(t, packages, selections, "fes.zx81", "ZX81", "fes.simple-computer", payloadBytes, 1024, "firmware")

	service := New(nil, nil, Roots{Selections: selections, Packages: packages})
	t.Cleanup(service.Close)
	service.SetInventory(func() ([]Core, error) { return []Core{}, nil })

	var scans atomic.Int32
	useFactoryListGate(t, func(context.Context) error {
		scans.Add(1)
		return nil
	})

	body := assertBoundedList(t, service, 3*time.Second, 1<<20)
	var cores []Core
	if err := json.Unmarshal([]byte(body), &cores); err != nil {
		t.Fatal(err)
	}
	got := byCore(cores)
	if _, ok := got["fes.menu"]; ok {
		t.Fatal("menu was listed")
	}
	// The format-3 fixtures are digest-matching zero maps. Structural validation
	// omits them, and streaming that multi-megabyte map stays inside the budget.
	if len(got) != 2 {
		t.Fatalf("cores %+v", got)
	}
	for _, id := range []string{"fes.pong", "fes.ramtest"} {
		if !got[id].Launchable || got[id].Needs != "none" || got[id].Name == "" {
			t.Fatalf("%s %+v", id, got[id])
		}
	}
	for _, id := range []string{"fes.sms", "fes.coleco", "fes.zx81"} {
		if _, ok := got[id]; ok {
			t.Fatalf("%s listed with a malformed ROM map", id)
		}
	}
	if scans.Load() != 1 {
		t.Fatalf("factory scans %d", scans.Load())
	}
	again := assertBoundedList(t, service, time.Second, 1<<20)
	if again != body {
		t.Fatalf("cached list changed\n%s\n%s", body, again)
	}
	if scans.Load() != 1 {
		t.Fatalf("cached list scanned again: %d", scans.Load())
	}
}

func TestReadingSelectionsDoesNotRescanFactory(t *testing.T) {
	root := t.TempDir()
	selections := filepath.Join(root, "selections")
	packages := filepath.Join(root, "packages")
	if err := os.Mkdir(selections, 0o755); err != nil || os.Mkdir(packages, 0o755) != nil {
		t.Fatal(err)
	}
	installCore(t, packages, selections, "fes.pong", "FES Pong", "fes.simple-game", coreOpts{})
	service := New(nil, nil, Roots{Selections: selections, Packages: packages})
	t.Cleanup(service.Close)
	var scans atomic.Int32
	useFactoryListGate(t, func(context.Context) error {
		scans.Add(1)
		return nil
	})
	durableID := strings.Repeat("ab", 32)
	service.SetInventory(func() ([]Core, error) {
		entries, err := os.ReadDir(selections)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if _, err := os.ReadFile(filepath.Join(selections, entry.Name())); err != nil {
				return nil, err
			}
		}
		return []Core{{
			CoreID: "fes.pong", PackageID: durableID, Name: "Durable Pong",
			ABI: "fes.simple-game", Needs: "none", Launchable: true,
		}}, nil
	})
	body := listCores(t, service)
	if !strings.Contains(body, durableID) || scans.Load() != 1 {
		t.Fatalf("scans %d body %s", scans.Load(), body)
	}
	again := listCores(t, service)
	if again != body || scans.Load() != 1 {
		t.Fatalf("reading selections rescanned the factory catalog: %d\n%s", scans.Load(), again)
	}
}

func TestFactoryListStopsWhenClientCancels(t *testing.T) {
	root := t.TempDir()
	selections := filepath.Join(root, "selections")
	packages := filepath.Join(root, "packages")
	if err := os.Mkdir(selections, 0o755); err != nil || os.Mkdir(packages, 0o755) != nil {
		t.Fatal(err)
	}
	installSizedCore(t, packages, selections, "fes.pong", "FES Pong", "fes.simple-game", 1<<20, 0, "")
	service := New(nil, nil, Roots{Selections: selections, Packages: packages})
	t.Cleanup(service.Close)
	service.SetInventory(func() ([]Core, error) { return []Core{}, nil })

	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	var scans atomic.Int32
	useFactoryListGate(t, func(ctx context.Context) error {
		if scans.Add(1) == 1 {
			close(started)
		}
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	t.Cleanup(func() { once.Do(func() { close(release) }) })

	ctx, cancel := context.WithCancel(context.Background())
	firstDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/v1/local/cores", nil).WithContext(ctx)
		Handler(service).ServeHTTP(response, request)
		firstDone <- response
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("factory scan did not start")
	}
	cancel()
	var first *httptest.ResponseRecorder
	select {
	case first = <-firstDone:
	case <-time.After(time.Second):
		t.Fatal("handler kept running after the client cancelled")
	}
	if first.Body.Len() != 0 || first.Header().Get("Content-Type") != "" {
		t.Fatalf("cancelled list wrote %d %s", first.Code, first.Body.String())
	}

	secondDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		response := httptest.NewRecorder()
		Handler(service).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/local/cores", nil))
		secondDone <- response
	}()
	select {
	case response := <-secondDone:
		t.Fatalf("second list did not share the in-flight scan: %d %s", response.Code, response.Body.String())
	case <-time.After(200 * time.Millisecond):
	}
	once.Do(func() { close(release) })
	var second *httptest.ResponseRecorder
	select {
	case second = <-secondDone:
	case <-time.After(2 * time.Second):
		t.Fatal("shared scan did not finish")
	}
	if second.Code != http.StatusOK || !strings.Contains(second.Body.String(), `"core_id":"fes.pong"`) {
		t.Fatalf("list %d %s", second.Code, second.Body.String())
	}
	if scans.Load() != 1 {
		t.Fatalf("retries scanned %d times", scans.Load())
	}

	dead, stop := context.WithCancel(context.Background())
	stop()
	startedAt := time.Now()
	response := httptest.NewRecorder()
	Handler(service).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/local/cores", nil).WithContext(dead))
	if time.Since(startedAt) > 200*time.Millisecond || response.Body.Len() != 0 || response.Header().Get("Content-Type") != "" {
		t.Fatalf("dead client waited %s and wrote %d %s", time.Since(startedAt), response.Code, response.Body.String())
	}
	if scans.Load() != 1 {
		t.Fatalf("dead client started a scan: %d", scans.Load())
	}
}

func TestFactoryCacheDropsUnexpectedPackageEntry(t *testing.T) {
	root := t.TempDir()
	selections := filepath.Join(root, "selections")
	packages := filepath.Join(root, "packages")
	if err := os.Mkdir(selections, 0o755); err != nil || os.Mkdir(packages, 0o755) != nil {
		t.Fatal(err)
	}
	id := installCore(t, packages, selections, "fes.pong", "FES Pong", "fes.simple-game", coreOpts{})
	service := New(nil, nil, Roots{Selections: selections, Packages: packages})
	t.Cleanup(service.Close)
	service.SetInventory(func() ([]Core, error) { return []Core{}, nil })

	var scans atomic.Int32
	useFactoryListGate(t, func(context.Context) error {
		scans.Add(1)
		return nil
	})

	first := listCores(t, service)
	if !strings.Contains(first, `"core_id":"fes.pong"`) {
		t.Fatalf("list %s", first)
	}
	if again := listCores(t, service); again != first || scans.Load() != 1 {
		t.Fatalf("cache scans %d\n%s", scans.Load(), again)
	}
	if err := os.WriteFile(filepath.Join(packages, id, "core.rbf.tmp"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	stale := listCores(t, service)
	if strings.Contains(stale, "fes.pong") || scans.Load() != 2 {
		t.Fatalf("scans %d body %s", scans.Load(), stale)
	}
	if err := os.Remove(filepath.Join(packages, id, "core.rbf.tmp")); err != nil {
		t.Fatal(err)
	}
	restored := listCores(t, service)
	if !strings.Contains(restored, `"core_id":"fes.pong"`) || scans.Load() != 3 {
		t.Fatalf("scans %d body %s", scans.Load(), restored)
	}
}

func TestFactoryScanStaysSingleWhenFingerprintChanges(t *testing.T) {
	root := t.TempDir()
	selections := filepath.Join(root, "selections")
	packages := filepath.Join(root, "packages")
	if err := os.Mkdir(selections, 0o755); err != nil || os.Mkdir(packages, 0o755) != nil {
		t.Fatal(err)
	}
	id := installCore(t, packages, selections, "fes.pong", "FES Pong", "fes.simple-game", coreOpts{})
	service := New(nil, nil, Roots{Selections: selections, Packages: packages})
	t.Cleanup(service.Close)
	service.SetInventory(func() ([]Core, error) { return []Core{}, nil })

	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	var scans atomic.Int32
	useFactoryListGate(t, func(ctx context.Context) error {
		if scans.Add(1) == 1 {
			close(started)
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	})
	t.Cleanup(func() { once.Do(func() { close(release) }) })

	firstDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		response := httptest.NewRecorder()
		Handler(service).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/local/cores", nil))
		firstDone <- response
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("factory scan did not start")
	}
	if err := os.WriteFile(filepath.Join(packages, id, "core.rbf.tmp"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	secondDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		response := httptest.NewRecorder()
		Handler(service).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/local/cores", nil))
		secondDone <- response
	}()
	select {
	case response := <-secondDone:
		t.Fatalf("fingerprint change started a second scan: %d %s", response.Code, response.Body.String())
	case <-time.After(200 * time.Millisecond):
	}
	if scans.Load() != 1 {
		t.Fatalf("overlapping scans %d", scans.Load())
	}
	once.Do(func() { close(release) })
	var first, second *httptest.ResponseRecorder
	select {
	case first = <-firstDone:
	case <-time.After(2 * time.Second):
		t.Fatal("first list did not finish")
	}
	select {
	case second = <-secondDone:
	case <-time.After(2 * time.Second):
		t.Fatal("second list did not finish")
	}
	if scans.Load() != 2 {
		t.Fatalf("follow-up scans %d", scans.Load())
	}
	for _, response := range []*httptest.ResponseRecorder{first, second} {
		if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "fes.pong") {
			t.Fatalf("list %d %s", response.Code, response.Body.String())
		}
	}
}

// The first list after Warm starts must return while the catalog warm is
// still running, and a later list must be the finished catalog. The warm
// itself must return before that scan.
func TestFirstListDuringWarmIsFastThenComplete(t *testing.T) {
	root := t.TempDir()
	selections := filepath.Join(root, "selections")
	packages := filepath.Join(root, "packages")
	if err := os.Mkdir(selections, 0o755); err != nil || os.Mkdir(packages, 0o755) != nil {
		t.Fatal(err)
	}
	installCore(t, packages, selections, "fes.pong", "FES Pong", "fes.simple-game", coreOpts{})
	service := New(nil, nil, Roots{Selections: selections, Packages: packages})
	t.Cleanup(service.Close)

	releaseInventory := make(chan struct{})
	var releaseInventoryOnce sync.Once
	t.Cleanup(func() { releaseInventoryOnce.Do(func() { close(releaseInventory) }) })
	durableID := strings.Repeat("ab", 32)
	service.SetInventory(func() ([]Core, error) {
		<-releaseInventory
		return []Core{{
			CoreID: "fes.coleco", PackageID: durableID, Name: "Durable Coleco",
			ABI: "fes.application", Needs: "none", Launchable: true,
		}}, nil
	})

	started := make(chan struct{})
	releaseScan := make(chan struct{})
	var releaseScanOnce sync.Once
	var scans atomic.Int32
	useFactoryListGate(t, func(ctx context.Context) error {
		if scans.Add(1) == 1 {
			close(started)
		}
		select {
		case <-releaseScan:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	t.Cleanup(func() { releaseScanOnce.Do(func() { close(releaseScan) }) })

	warmAt := time.Now()
	service.Warm()
	if time.Since(warmAt) > 100*time.Millisecond {
		t.Fatalf("Warm blocked startup for %s", time.Since(warmAt))
	}

	code, body, elapsed := timedList(t, service, context.Background())
	if code != http.StatusServiceUnavailable || !strings.Contains(body, `"error":"warming"`) || elapsed >= time.Second {
		t.Fatalf("in-progress list %d in %s: %s", code, elapsed, body)
	}
	if scans.Load() != 0 {
		t.Fatalf("warming list started %d factory scans", scans.Load())
	}

	dead, cancel := context.WithCancel(context.Background())
	cancel()
	cancelledAt := time.Now()
	cancelled := httptest.NewRecorder()
	Handler(service).ServeHTTP(cancelled, httptest.NewRequest(http.MethodGet, "/v1/local/cores", nil).WithContext(dead))
	if time.Since(cancelledAt) > 200*time.Millisecond || cancelled.Body.Len() != 0 || cancelled.Header().Get("Content-Type") != "" {
		t.Fatalf("cancelled list waited %s and wrote %d %s", time.Since(cancelledAt), cancelled.Code, cancelled.Body.String())
	}

	releaseInventoryOnce.Do(func() { close(releaseInventory) })
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("factory warm did not start")
	}
	code, body, elapsed = timedList(t, service, context.Background())
	if code != http.StatusServiceUnavailable || !strings.Contains(body, `"error":"warming"`) || elapsed >= time.Second || scans.Load() != 1 {
		t.Fatalf("list during factory warm %d in %s scans %d: %s", code, elapsed, scans.Load(), body)
	}

	releaseScanOnce.Do(func() { close(releaseScan) })
	deadline := time.Now().Add(2 * time.Second)
	var finished string
	for {
		code, body, elapsed = timedList(t, service, context.Background())
		if elapsed >= time.Second {
			t.Fatalf("list took %s", elapsed)
		}
		if code == http.StatusOK && strings.Contains(body, `"core_id":"fes.pong"`) && strings.Contains(body, durableID) {
			finished = body
			break
		}
		if code != http.StatusServiceUnavailable || !strings.Contains(body, `"error":"warming"`) || !time.Now().Before(deadline) {
			t.Fatalf("warm result %d %s", code, body)
		}
	}
	if scans.Load() != 1 {
		t.Fatalf("warm scanned %d times", scans.Load())
	}
	if again := listCores(t, service); again != finished || scans.Load() != 1 {
		t.Fatalf("cached list scans %d\n%s\n%s", scans.Load(), finished, again)
	}
}

func TestWarmSkipsFactoryWhenInventoryFails(t *testing.T) {
	root := t.TempDir()
	selections := filepath.Join(root, "selections")
	packages := filepath.Join(root, "packages")
	if err := os.Mkdir(selections, 0o755); err != nil || os.Mkdir(packages, 0o755) != nil {
		t.Fatal(err)
	}
	installCore(t, packages, selections, "fes.pong", "FES Pong", "fes.simple-game", coreOpts{})
	service := New(nil, nil, Roots{Selections: selections, Packages: packages})
	t.Cleanup(service.Close)
	var scans atomic.Int32
	useFactoryListGate(t, func(context.Context) error {
		scans.Add(1)
		return nil
	})
	service.SetInventory(func() ([]Core, error) {
		return nil, errors.New("durable core package storage is unavailable")
	})
	service.Warm()

	deadline := time.Now().Add(2 * time.Second)
	for {
		code, body, elapsed := timedList(t, service, context.Background())
		if elapsed >= time.Second {
			t.Fatalf("list took %s", elapsed)
		}
		if code == http.StatusOK && strings.TrimSpace(body) == "[]" {
			break
		}
		if code != http.StatusServiceUnavailable || !strings.Contains(body, `"error":"warming"`) || !time.Now().Before(deadline) {
			t.Fatalf("list %d %s", code, body)
		}
	}
	if scans.Load() != 0 {
		t.Fatalf("failed inventory still scanned factory %d times", scans.Load())
	}
}

func timedList(t *testing.T, service *Service, ctx context.Context) (int, string, time.Duration) {
	t.Helper()
	started := time.Now()
	response := httptest.NewRecorder()
	Handler(service).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/local/cores", nil).WithContext(ctx))
	return response.Code, response.Body.String(), time.Since(started)
}

func listCores(t *testing.T, service *Service) string {
	t.Helper()
	response := httptest.NewRecorder()
	Handler(service).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/local/cores", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("list %d %s", response.Code, response.Body.String())
	}
	return response.Body.String()
}

func assertBoundedList(t *testing.T, service *Service, limit time.Duration, allocBudget uint64) string {
	t.Helper()
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	started := time.Now()
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		response := httptest.NewRecorder()
		Handler(service).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/local/cores", nil))
		done <- response
	}()
	var response *httptest.ResponseRecorder
	select {
	case response = <-done:
	case <-time.After(limit + time.Second):
		t.Fatal("GET /v1/local/cores hung with the durable store open")
	}
	elapsed := time.Since(started)
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	if response.Code != http.StatusOK {
		t.Fatalf("list %d %s", response.Code, response.Body.String())
	}
	if elapsed > limit {
		t.Fatalf("list took %s", elapsed)
	}
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > allocBudget {
		t.Fatalf("list allocated %d bytes", allocated)
	}
	return response.Body.String()
}

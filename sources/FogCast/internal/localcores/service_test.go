package localcores

import (
	"context"
	"encoding/json"
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
	mu      sync.Mutex
	loads   [][2]string
	stops   int
	loadErr error
}

func (f *fakeRuntime) LoadCore(_ context.Context, path, packageID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.loads = append(f.loads, [2]string{path, packageID})
	return f.loadErr
}

func (f *fakeRuntime) Stop(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stops++
	return nil
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
	return New(manager, runtime, Roots{Selections: selections, Packages: packages}), manager, selections, packages
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

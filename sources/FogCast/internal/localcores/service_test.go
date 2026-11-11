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
)

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
	service := New(Roots{Selections: selections, Packages: packages})
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

	service := New(Roots{Selections: selections, Packages: packages})
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
	service := New(Roots{Selections: selections, Packages: packages})
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
	service := New(Roots{Selections: selections, Packages: packages})
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
	service := New(Roots{Selections: selections, Packages: packages})
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
	service := New(Roots{Selections: selections, Packages: packages})
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
	service := New(Roots{Selections: selections, Packages: packages})
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
	service := New(Roots{Selections: selections, Packages: packages})
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

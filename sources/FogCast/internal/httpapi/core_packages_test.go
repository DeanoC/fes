package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/internal/httpapi"
	"github.com/DeanoC/FogCast/internal/kitlease"
	"github.com/DeanoC/FogCast/internal/kitpackages"
	"github.com/DeanoC/FogCast/protocol"
)

func installedPackageFixture(t *testing.T, manifestName string) ([]byte, corepackage.Inspection) {
	t.Helper()
	root := t.TempDir()
	fixtureRoot := filepath.Join("..", "..", "corepackage", "testdata", "core-bundle-v2")
	for _, item := range []struct{ source, target string }{{"manifests/" + manifestName + ".toml", "manifest.toml"}, {"payloads/fes-fixture.rbf", "core.rbf"}} {
		data, err := os.ReadFile(filepath.Join(fixtureRoot, item.source))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, item.target), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	inspection, err := corepackage.InspectPackage(root)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := corepackage.CanonicalArchive(root)
	if err != nil {
		t.Fatal(err)
	}
	return archive, inspection
}

func corePackageRequest(t *testing.T, server *httptest.Server, method, path, contentType, token string, body []byte) (int, []byte) {
	t.Helper()
	request, err := http.NewRequest(method, server.URL+path, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	request.Header.Set("Content-Type", contentType)
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, data
}

func packageSelectionBody(t *testing.T, expected, desired string) []byte {
	t.Helper()
	body, err := json.Marshal(protocol.KitCoreSelectionRequest{ExpectedPackageID: expected, PackageID: desired})
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestDurableCorePackagesAuthenticatedInstallSelectReopenWithoutHardware(t *testing.T) {
	archive, expected := installedPackageFixture(t, "valid-basic")
	storeRoot := durableHTTPStoreRoot(t)
	store, err := kitpackages.New(storeRoot)
	if err != nil {
		t.Fatal(err)
	}
	controller := &fakeController{status: protocol.Status{State: protocol.StateActive}}
	var inspections atomic.Int32
	inspect := func(_ context.Context, length int64, body io.Reader) (protocol.CoreInspection, *protocol.APIError) {
		data, err := io.ReadAll(body)
		if err != nil || length != int64(len(archive)) || !bytes.Equal(data, archive) {
			return protocol.CoreInspection{}, &protocol.APIError{Code: protocol.CodeInvalidArchive, Message: "wrong archive"}
		}
		inspections.Add(1)
		return protocol.CoreInspection{PackageID: expected.PackageID, Descriptor: expected.Descriptor, Compatible: true}, nil
	}
	lease := kitlease.New(time.Minute, nil)
	server := httptest.NewServer(httpapi.New(controller, "test-token", "test", discardLogger(), httpapi.WithKitLease(lease), httpapi.WithCorePackages(store, inspect)))
	defer server.Close()
	for _, token := range []string{"", "wrong-token"} {
		if status, _ := corePackageRequest(t, server, "POST", "/v1/core-packages", "application/octet-stream", token, archive); status != http.StatusUnauthorized {
			t.Fatalf("auth = %d", status)
		}
	}
	for index := range 2 {
		status, data := corePackageRequest(t, server, "POST", "/v1/core-packages", "application/octet-stream", "test-token", archive)
		var result protocol.KitCoreInstall
		if err := json.Unmarshal(data, &result); err != nil {
			t.Fatal(err)
		}
		if status != []int{http.StatusCreated, http.StatusOK}[index] || result.Created != (index == 0) || !reflect.DeepEqual(result.Package, expected) {
			t.Fatalf("install = %d %s", status, data)
		}
	}
	status, data := corePackageRequest(t, server, "GET", "/v1/core-packages", "", "test-token", nil)
	var inventory protocol.KitCorePackages
	if err := json.Unmarshal(data, &inventory); err != nil {
		t.Fatal(err)
	}
	if status != 200 || len(inventory.Packages) != 1 || len(inventory.Selections) != 0 {
		t.Fatalf("install selected automatically: %s", data)
	}
	status, data = corePackageRequest(t, server, "PUT", "/v1/core-selections/"+expected.Descriptor.Core.ID, "application/json", "test-token", packageSelectionBody(t, "", expected.PackageID))
	if status != 200 {
		t.Fatalf("select = %d %s", status, data)
	}
	if inspections.Load() != 1 || controller.stopCalls != 0 || controller.statusCalls != 0 || controller.launchCalls != 0 {
		t.Fatal("installation or selection mutated execution")
	}
	if state := lease.Status().State; state == "held" {
		t.Fatal("installation or selection acquired execution lease")
	}
	reopened, err := kitpackages.New(storeRoot)
	if err != nil {
		t.Fatal(err)
	}
	selections, err := reopened.Selections(context.Background())
	if err != nil || len(selections) != 1 || selections[0].PackageID != expected.PackageID {
		t.Fatalf("reopen = %v %v", selections, err)
	}
}

func TestDurableCorePackagesConcurrentSelectionCAS(t *testing.T) {
	archiveA, a := installedPackageFixture(t, "valid-basic")
	archiveB, b := installedPackageFixture(t, "valid-semver-prerelease-build")
	store, err := kitpackages.New(durableHTTPStoreRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, archive := range [][]byte{archiveA, archiveB} {
		if _, _, err := store.Install(context.Background(), int64(len(archive)), bytes.NewReader(archive)); err != nil {
			t.Fatal(err)
		}
	}
	ready := make(chan struct{}, 2)
	proceed := make(chan struct{})
	inspect := func(_ context.Context, _ int64, body io.Reader) (protocol.CoreInspection, *protocol.APIError) {
		data, _ := io.ReadAll(body)
		value := a
		if bytes.Equal(data, archiveB) {
			value = b
		}
		ready <- struct{}{}
		<-proceed
		return protocol.CoreInspection{PackageID: value.PackageID, Descriptor: value.Descriptor, Compatible: true}, nil
	}
	server := httptest.NewServer(httpapi.New(&fakeController{}, "test-token", "test", discardLogger(), httpapi.WithCorePackages(store, inspect)))
	defer server.Close()
	statuses := make(chan int, 2)
	var wait sync.WaitGroup
	for _, value := range []corepackage.Inspection{a, b} {
		body := packageSelectionBody(t, "", value.PackageID)
		wait.Add(1)
		go func() {
			defer wait.Done()
			status, _ := corePackageRequest(t, server, "PUT", "/v1/core-selections/"+a.Descriptor.Core.ID, "application/json", "test-token", body)
			statuses <- status
		}()
	}
	for range 2 {
		select {
		case <-ready:
		case <-time.After(5 * time.Second):
			t.Fatal("concurrent inspectors did not arrive")
		}
	}
	close(proceed)
	wait.Wait()
	close(statuses)
	count := map[int]int{}
	for status := range statuses {
		count[status]++
	}
	if count[200] != 1 || count[409] != 1 {
		t.Fatalf("competing selections = %v", count)
	}
}

func TestDurableCorePackagesRejectUnavailableCorruptForeignAndUnsupported(t *testing.T) {
	archive, value := installedPackageFixture(t, "valid-basic")
	storeRoot := durableHTTPStoreRoot(t)
	store, err := kitpackages.New(storeRoot)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Install(context.Background(), int64(len(archive)), bytes.NewReader(archive)); err != nil {
		t.Fatal(err)
	}
	var inspected atomic.Int32
	inspect := func(_ context.Context, _ int64, _ io.Reader) (protocol.CoreInspection, *protocol.APIError) {
		inspected.Add(1)
		return protocol.CoreInspection{PackageID: value.PackageID, Descriptor: value.Descriptor, CompatibilityError: &protocol.APIError{Code: protocol.CodeUnsupportedOperation, Phase: "compatibility", Message: "ABI unavailable"}}, nil
	}
	server := httptest.NewServer(httpapi.New(&fakeController{}, "test-token", "test", discardLogger(), httpapi.WithCorePackages(store, inspect)))
	defer server.Close()
	for _, test := range []struct {
		method, path, content string
		body                  []byte
		want                  int
	}{
		{"POST", "/v1/core-packages", "application/octet-stream", []byte("corrupt archive"), 400},
		{"POST", "/v1/core-packages", "application/json", archive, 400},
		{"PUT", "/v1/core-selections/fes.foreign", "application/json", packageSelectionBody(t, "", value.PackageID), 400},
		{"PUT", "/v1/core-selections/fes.pong", "application/json", []byte(`{"package_id":"` + value.PackageID + `","unexpected":1}`), 400},
		{"PUT", "/v1/core-selections/fes.pong", "application/json", packageSelectionBody(t, "", strings.Repeat("0", 64)), 404},
		{"PUT", "/v1/core-selections/fes.pong", "application/json", packageSelectionBody(t, "", value.PackageID), 409},
	} {
		status, data := corePackageRequest(t, server, test.method, test.path, test.content, "test-token", test.body)
		if status != test.want {
			t.Fatalf("%s %s = %d %s", test.method, test.path, status, data)
		}
	}
	if inspected.Load() != 1 {
		t.Fatalf("foreign/invalid inspected runtime %d times", inspected.Load())
	}
	selections, err := store.Selections(context.Background())
	if err != nil || len(selections) != 0 {
		t.Fatalf("rejection changed selections: %v %v", selections, err)
	}
	unavailable := httptest.NewServer(httpapi.New(&fakeController{}, "test-token", "test", discardLogger(), httpapi.WithCorePackages(nil, nil)))
	defer unavailable.Close()
	for _, method := range []string{"GET", "POST"} {
		status, data := corePackageRequest(t, unavailable, method, "/v1/core-packages", "application/octet-stream", "test-token", archive)
		if status != 503 || !bytes.Contains(data, []byte(protocol.CodeCorePackageStoreUnavailable)) {
			t.Fatalf("unavailable = %d %s", status, data)
		}
	}
	noInspector := httptest.NewServer(httpapi.New(&fakeController{}, "test-token", "test", discardLogger(), httpapi.WithCorePackages(store, nil)))
	defer noInspector.Close()
	status, _ := corePackageRequest(t, noInspector, "PUT", "/v1/core-selections/fes.pong", "application/json", "test-token", packageSelectionBody(t, "", value.PackageID))
	if status != 503 {
		t.Fatalf("missing inspector = %d", status)
	}
}

func durableHTTPStoreRoot(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "library")
	t.Cleanup(func() {
		_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err == nil && entry.IsDir() {
				return os.Chmod(path, 0700)
			}
			return nil
		})
	})
	return root
}

func TestDurableCoreSelectionRejectsInspectionIdentityAndRetainedCorruption(t *testing.T) {
	archive, value := installedPackageFixture(t, "valid-basic")
	root := durableHTTPStoreRoot(t)
	store, err := kitpackages.New(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Install(context.Background(), int64(len(archive)), bytes.NewReader(archive)); err != nil {
		t.Fatal(err)
	}
	var inspections atomic.Int32
	inspect := func(context.Context, int64, io.Reader) (protocol.CoreInspection, *protocol.APIError) {
		inspections.Add(1)
		return protocol.CoreInspection{PackageID: strings.Repeat("f", 64), Descriptor: value.Descriptor, Compatible: true}, nil
	}
	server := httptest.NewServer(httpapi.New(&fakeController{}, "test-token", "test", discardLogger(), httpapi.WithCorePackages(store, inspect)))
	defer server.Close()
	body := packageSelectionBody(t, "", value.PackageID)
	status, data := corePackageRequest(t, server, "PUT", "/v1/core-selections/fes.pong", "application/json", "test-token", body)
	if status != 502 || !bytes.Contains(data, []byte(protocol.CodeDigestMismatch)) {
		t.Fatalf("inspection mismatch = %d %s", status, data)
	}
	archivePath := filepath.Join(root, "archives", value.PackageID+".fcore")
	if err := os.Chmod(archivePath, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(archivePath, []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	status, _ = corePackageRequest(t, server, "PUT", "/v1/core-selections/fes.pong", "application/json", "test-token", body)
	if status == 200 || inspections.Load() != 1 {
		t.Fatalf("corrupted retained archive selected or inspected: %d, %d", status, inspections.Load())
	}
	status, _ = corePackageRequest(t, server, "GET", "/v1/core-packages", "", "test-token", nil)
	if status == 200 {
		t.Fatal("corrupted inventory published")
	}
}

package httpapi_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/httpapi"
	"github.com/DeanoC/FogCast/internal/input"
	"github.com/DeanoC/FogCast/internal/kitlease"
	contractlease "github.com/DeanoC/FogCast/kitlease"
	"golang.org/x/sys/unix"
)

func claimKit(t *testing.T, manager *kitlease.Manager) kitlease.Grant {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		grant, err := manager.Claim(kitlease.ClaimRequest{RequestID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Owner: "tests", Purpose: "test"})
		if err == nil {
			return grant
		}
		if time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestKitLeaseGuardsPhysicalRoutes(t *testing.T) {
	manager := kitlease.New(time.Minute, func(context.Context) error { return nil })
	defer manager.Close()
	grant := claimKit(t, manager)
	controller := &fakeController{}
	handler := httpapi.New(controller, "bearer", "test", nil, httpapi.WithKitLease(manager))
	for _, path := range []string{"/v1/stop", "/v1/development/rbf", "/v1/development/core", "/v1/development/reboot", "/v1/input/attach", "/v1/input/detach", "/v1/input/stream", "/v1/cast/start", "/v1/cast/stop", "/v1/mesh/content/pull", "/v1/mesh/content/link"} {
		for _, token := range []string{"", "foreign"} {
			request := httptest.NewRequest(http.MethodPost, path, nil)
			request.Header.Set("Authorization", "Bearer bearer")
			request.Header.Set(httpapi.KitLeaseHeader, token)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusForbidden {
				t.Fatalf("%s: %d", path, response.Code)
			}
		}
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/stop", nil)
	request.Header.Set("Authorization", "Bearer bearer")
	request.Header.Set(httpapi.KitLeaseHeader, grant.Token)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 200 || controller.stopCalls != 1 {
		t.Fatalf("valid owner stop: %d calls %d", response.Code, controller.stopCalls)
	}
}

func TestHostlessOwnerCannotCastOrDirectLaunch(t *testing.T) {
	manager := kitlease.New(time.Minute, func(context.Context) error { return nil })
	defer manager.Close()
	deadline := time.Now().Add(time.Second)
	var grant kitlease.Grant
	for {
		var err error
		grant, err = manager.Claim(kitlease.ClaimRequest{RequestID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Owner: kitlease.HostlessOwner, Purpose: kitlease.HostlessPurpose})
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(time.Millisecond)
	}
	content := &fakeContentController{}
	handler := httpapi.New(&fakeController{}, "bearer", "test", nil, httpapi.WithKitLease(manager), httpapi.WithContent(content))
	for _, path := range []string{"/v1/cast/start", "/v1/development/rbf", "/v1/mesh/content/pull", "/v1/mesh/content/link"} {
		request := httptest.NewRequest(http.MethodPost, path, nil)
		request.Header.Set("Authorization", "Bearer bearer")
		request.Header.Set(httpapi.KitLeaseHeader, grant.Token)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusForbidden {
			t.Fatalf("%s: %d", path, response.Code)
		}
	}
	launch := httptest.NewRequest(http.MethodPost, "/v2/launch", nil)
	launch.Header.Set("Authorization", "Bearer bearer")
	launch.Header.Set(httpapi.KitLeaseHeader, grant.Token)
	launchResponse := httptest.NewRecorder()
	handler.ServeHTTP(launchResponse, launch)
	if launchResponse.Code != http.StatusNotFound {
		t.Fatal("retired cached launch route remains")
	}
	stop := httptest.NewRequest(http.MethodPost, "/v1/stop", nil)
	stop.Header.Set("Authorization", "Bearer bearer")
	stop.Header.Set(httpapi.KitLeaseHeader, grant.Token)
	stopResponse := httptest.NewRecorder()
	handler.ServeHTTP(stopResponse, stop)
	if stopResponse.Code != 200 {
		t.Fatalf("hostless stop: %d", stopResponse.Code)
	}
	inputHandler := httpapi.New(&fakeController{}, "bearer", "test", nil, httpapi.WithKitLease(manager), httpapi.WithInput(&leaseStreamController{}))
	attach := httptest.NewRequest(http.MethodPost, "/v1/input/attach", strings.NewReader(`{"session":1,"token":"0123456789abcdef","core":"SNES"}`))
	attach.Header.Set("Authorization", "Bearer bearer")
	attach.Header.Set("Content-Type", "application/json")
	attach.Header.Set(httpapi.KitLeaseHeader, grant.Token)
	attachResponse := httptest.NewRecorder()
	inputHandler.ServeHTTP(attachResponse, attach)
	if attachResponse.Code == http.StatusForbidden {
		t.Fatal("hostless owner denied local input attach")
	}
}

func TestKitLocalCorePurposeCannotUseHostRoutes(t *testing.T) {
	manager := kitlease.New(time.Minute, func(context.Context) error { return nil })
	defer manager.Close()
	deadline := time.Now().Add(time.Second)
	var grant kitlease.Grant
	for {
		var err error
		grant, err = manager.Claim(kitlease.ClaimRequest{
			RequestID: "cccccccccccccccccccccccccccccccc",
			Owner:     kitlease.HostlessOwner,
			Purpose:   kitlease.LocalCorePurpose,
		})
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(time.Millisecond)
	}
	if grant.Status.Owner != kitlease.HostlessOwner || !kitlease.ForeignHID(grant.Status) {
		t.Fatalf("host must see kit-local lease as foreign: %+v", grant.Status)
	}
	handler := httpapi.New(&fakeController{}, "bearer", "test", nil, httpapi.WithKitLease(manager))
	for _, path := range []string{"/v1/stop", "/v1/input/attach", "/v1/cast/start", "/v1/development/rbf"} {
		request := httptest.NewRequest(http.MethodPost, path, nil)
		request.Header.Set("Authorization", "Bearer bearer")
		request.Header.Set(httpapi.KitLeaseHeader, grant.Token)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusForbidden {
			t.Fatalf("%s: %d", path, response.Code)
		}
	}
	listed := httptest.NewRequest(http.MethodGet, "/v1/local/cores", nil)
	listedResponse := httptest.NewRecorder()
	handler.ServeHTTP(listedResponse, listed)
	if listedResponse.Code != http.StatusNotFound {
		t.Fatalf("network list: %d", listedResponse.Code)
	}
	packageID := strings.Repeat("ab", 32)
	for _, check := range []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/v1/local/stop"},
		{http.MethodPost, "/v1/local/cores/" + packageID + "/launch"},
		{http.MethodGet, "/v1/local/status"},
	} {
		for _, headers := range []map[string]string{
			{},
			{"Authorization": "Bearer bearer"},
			{"Authorization": "Bearer bearer", httpapi.KitLeaseHeader: grant.Token},
			{httpapi.KitLeaseHeader: grant.Token},
		} {
			request := httptest.NewRequest(check.method, check.path, nil)
			for key, value := range headers {
				request.Header.Set(key, value)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusNotFound {
				t.Fatalf("%s %s headers %v: %d", check.method, check.path, headers, response.Code)
			}
		}
	}
}

type leaseStreamController struct{ backend net.Conn }

func (*leaseStreamController) Attach(context.Context, input.Spec) error { return nil }
func (*leaseStreamController) Detach(context.Context, uint64) error     { return nil }
func (c *leaseStreamController) OpenStream(context.Context, uint64) (net.Conn, error) {
	return c.backend, nil
}
func (*leaseStreamController) Close() error { return nil }

func TestKitLeaseExpiryClosesHijackedInputBeforeCleanup(t *testing.T) {
	cleanup := make(chan struct{}, 2)
	manager := kitlease.New(150*time.Millisecond, func(context.Context) error { cleanup <- struct{}{}; return nil })
	defer manager.Close()
	grant := claimKit(t, manager)
	<-cleanup
	backend, peer := net.Pipe()
	defer peer.Close()
	server := httptest.NewServer(httpapi.New(&fakeController{}, "bearer", "test", nil, httpapi.WithKitLease(manager), httpapi.WithInput(&leaseStreamController{backend})))
	defer server.Close()
	conn, err := net.Dial("tcp", server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(2 * time.Second))
	fmt.Fprintf(conn, "CONNECT /v1/input/stream HTTP/1.1\r\nHost: kit\r\nAuthorization: Bearer bearer\r\nX-FogCast-Input-Session: 1\r\n%s: %s\r\n\r\n", httpapi.KitLeaseHeader, grant.Token)
	reader := bufio.NewReader(conn)
	response, err := http.ReadResponse(reader, nil)
	if err != nil || response.StatusCode != 200 {
		t.Fatalf("CONNECT: %v %v", response, err)
	}
	if _, err := reader.ReadByte(); err == nil {
		t.Fatal("input connection remained usable")
	}
	select {
	case <-cleanup:
	case <-time.After(time.Second):
		t.Fatal("cleanup stuck behind stream")
	}
}

func TestReleasedOwnerCannotStopReplacement(t *testing.T) {
	manager := kitlease.New(time.Minute, func(context.Context) error { return nil })
	defer manager.Close()
	old := claimKit(t, manager)
	if _, err := manager.Release(old.Token); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	var replacement kitlease.Grant
	for {
		var err error
		replacement, err = manager.Claim(kitlease.ClaimRequest{RequestID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Owner: "next", Purpose: "test"})
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(time.Millisecond)
	}
	controller := &fakeController{}
	handler := httpapi.New(controller, "bearer", "test", nil, httpapi.WithKitLease(manager))
	for _, token := range []string{old.Token, replacement.Token} {
		request := httptest.NewRequest(http.MethodPost, "/v1/stop", nil)
		request.Header.Set("Authorization", "Bearer bearer")
		request.Header.Set(httpapi.KitLeaseHeader, token)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		want := http.StatusOK
		if token == old.Token {
			want = http.StatusForbidden
		}
		if response.Code != want {
			t.Fatalf("Stop returned %d want %d", response.Code, want)
		}
	}
	if controller.stopCalls != 1 {
		t.Fatalf("Stop calls %d", controller.stopCalls)
	}
}

func TestKitLeaseExpiryInterruptsStalledUpload(t *testing.T) {
	testLeaseExpiryInterruptsStalledUpload(t, "/v1/development/rbf")
}

func TestKitLeaseExpiryInterruptsStalledCoreUpload(t *testing.T) {
	testLeaseExpiryInterruptsStalledUpload(t, "/v1/development/core")
}

func testLeaseExpiryInterruptsStalledUpload(t *testing.T, path string) {
	t.Helper()
	cleanup := make(chan struct{}, 2)
	manager := kitlease.New(150*time.Millisecond, func(context.Context) error { cleanup <- struct{}{}; return nil })
	defer manager.Close()
	grant := claimKit(t, manager)
	<-cleanup
	server := httptest.NewServer(httpapi.New(&fakeController{}, "bearer", "test", nil, httpapi.WithKitLease(manager), httpapi.WithDevelopment(&fakeDevelopmentController{})))
	defer server.Close()
	conn, err := net.Dial("tcp", server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	// Announce an upload but deliberately never finish its body. Revocation must
	// unblock the handler before cleanup can acquire the physical controller.
	fmt.Fprintf(conn, "POST %s HTTP/1.1\r\nHost: kit\r\nAuthorization: Bearer bearer\r\n%s: %s\r\nContent-Type: application/octet-stream\r\nContent-Length: 100\r\n\r\nx", path, httpapi.KitLeaseHeader, grant.Token)
	select {
	case <-cleanup:
	case <-time.After(2 * time.Second):
		t.Fatal("cleanup stuck behind unfinished upload")
	}
}

func TestKitLeaseTakeoverRequiresReasonAndCurrentGeneration(t *testing.T) {
	manager := kitlease.New(time.Minute, func(context.Context) error { return nil })
	defer manager.Close()
	old := claimKit(t, manager)
	handler := httpapi.New(&fakeController{}, "bearer", "test", nil, httpapi.WithKitLease(manager))
	takeover := func(generation, reason string) int {
		body, _ := json.Marshal(map[string]string{"request_id": "cccccccccccccccccccccccccccccccc", "owner": "operator", "purpose": "recover", "expected_generation": generation, "reason": reason})
		request := httptest.NewRequest(http.MethodPost, "/v1/kit/takeover", strings.NewReader(string(body)))
		request.Header.Set("Authorization", "Bearer bearer")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response.Code
	}
	if got := takeover(old.Status.Generation, ""); got != http.StatusBadRequest {
		t.Fatalf("missing reason: %d", got)
	}
	if got := takeover("stale", "operator intervention"); got != http.StatusForbidden {
		t.Fatalf("stale generation: %d", got)
	}
	if got := takeover(old.Status.Generation, "operator intervention"); got != http.StatusConflict {
		t.Fatalf("revocation: %d", got)
	}
	deadline := time.Now().Add(time.Second)
	for {
		got := takeover(old.Status.Generation, "operator intervention")
		if got == http.StatusOK {
			break
		}
		if got != http.StatusConflict || time.Now().After(deadline) {
			t.Fatalf("takeover retry: %d", got)
		}
		time.Sleep(time.Millisecond)
	}
	if _, _, err := manager.Begin(old.Token); err == nil {
		t.Fatal("old owner admitted after takeover")
	}
}

func TestMaintenanceRoutesAuthenticateFenceAndFinish(t *testing.T) {
	root := t.TempDir()
	manager := kitlease.New(time.Minute, func(context.Context) error { return nil }, kitlease.WithMaintenanceFile(filepath.Join(root, "kit-maintenance.json"), filepath.Join(root, "kit-maintenance.lock"), "12345678-1234-1234-1234-123456789abc"))
	defer manager.Close()
	execution := claimKit(t, manager)
	controller := &fakeController{}
	handler := httpapi.New(controller, "bearer", "test", nil, httpapi.WithKitLease(manager))
	request := func(method, path, body, bearer, lease string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if bearer != "" {
			r.Header.Set("Authorization", "Bearer "+bearer)
		}
		r.Header.Set(httpapi.KitLeaseHeader, lease)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	start := `{"request_id":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","purpose":"compatible software"}`
	for _, path := range []string{"/v1/kit/maintenance/start", "/v1/kit/maintenance/end", "/v1/kit/maintenance/resume"} {
		if w := request("POST", path, start, "", execution.Token); w.Code != 401 {
			t.Fatalf("unauthenticated %s: %d", path, w.Code)
		}
	}
	if w := request("GET", "/v1/kit/maintenance", "", "", ""); w.Code != 401 {
		t.Fatal(w.Code)
	}
	for _, body := range []string{`{}`, start + `{}`, `{"request_id":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","purpose":"compatible software","extra":1}`} {
		if w := request("POST", "/v1/kit/maintenance/start", body, "bearer", execution.Token); w.Code != 400 {
			t.Fatalf("invalid start: %d", w.Code)
		}
	}
	if w := request("POST", "/v1/kit/maintenance/start", start, "bearer", "foreign"); w.Code != 403 {
		t.Fatal(w.Code)
	}
	w := request("POST", "/v1/kit/maintenance/start", start, "bearer", execution.Token)
	if w.Code != 200 {
		t.Fatalf("start: %d %s", w.Code, w.Body)
	}
	var grant contractlease.MaintenanceGrant
	if err := json.Unmarshal(w.Body.Bytes(), &grant); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for manager.MaintenanceStatus().State != "ready" {
		if time.Now().After(deadline) {
			t.Fatal(manager.MaintenanceStatus())
		}
		time.Sleep(time.Millisecond)
	}
	for _, lease := range []string{execution.Token, grant.Token, "foreign"} {
		if w := request("POST", "/v1/stop", "", "bearer", lease); w.Code != 403 {
			t.Fatalf("maintenance admitted stop: %d", w.Code)
		}
	}
	if controller.stopCalls != 0 {
		t.Fatal("physical mutation reached controller")
	}
	status := request("GET", "/v1/kit/maintenance", "", "bearer", "")
	if strings.Contains(status.Body.String(), grant.Token) || strings.Contains(status.Body.String(), execution.Token) {
		t.Fatal("status leaked private tokens")
	}
	endBody, err := json.Marshal(contractlease.MaintenanceEndRequest{OperationID: grant.Status.OperationID, ExpectedGeneration: grant.Status.Generation})
	if err != nil {
		t.Fatal(err)
	}
	if w := request("POST", "/v1/kit/maintenance/end", string(endBody), "bearer", execution.Token); w.Code != 403 {
		t.Fatal("execution token finished reservation", w.Code)
	}
	if w := request("POST", "/v1/kit/maintenance/resume", string(endBody), "bearer", grant.Token); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w := request("POST", "/v1/kit/maintenance/end", string(endBody), "bearer", grant.Token); w.Code != 200 {
		t.Fatal(w.Code)
	}
	deadline = time.Now().Add(time.Second)
	for manager.Status().State != "free" {
		if time.Now().After(deadline) {
			t.Fatal(manager.Status())
		}
		time.Sleep(time.Millisecond)
	}
	if w := request("POST", "/v1/kit/maintenance/end", string(endBody), "bearer", grant.Token); w.Code != 200 || !strings.Contains(w.Body.String(), `"state":"none"`) {
		t.Fatal("finish retry", w.Code, w.Body)
	}
}

func TestMaintenanceStatusUnavailableUntilPersistedReservationLoaded(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "kit-maintenance.json")
	lockPath := filepath.Join(root, "kit-maintenance.lock")
	options := []kitlease.Option{kitlease.WithMaintenanceFile(marker, lockPath, "12345678-1234-1234-1234-123456789abc")}
	original := kitlease.New(time.Minute, func(context.Context) error { return nil }, options...)
	execution := claimKit(t, original)
	grant, err := original.StartMaintenance(execution.Token, contractlease.MaintenanceStartRequest{RequestID: strings.Repeat("b", 32), Purpose: "software maintenance"})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for original.MaintenanceStatus().State != "ready" {
		if time.Now().After(deadline) {
			t.Fatal(original.MaintenanceStatus())
		}
		time.Sleep(time.Millisecond)
	}
	original.Close()
	lock, err := os.OpenFile(lockPath, os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err = unix.Flock(int(lock.Fd()), unix.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	replacement := kitlease.New(time.Minute, func(context.Context) error { return nil }, options...)
	defer replacement.Close()
	handler := httpapi.New(&fakeController{}, "bearer", "test", nil, httpapi.WithKitLease(replacement))
	status := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/v1/kit/maintenance", nil)
		r.Header.Set("Authorization", "Bearer bearer")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if w := status(); w.Code != http.StatusServiceUnavailable || strings.Contains(w.Body.String(), `"state":"none"`) {
		t.Fatal("pending persisted load reported no reservation", w.Code, w.Body)
	}
	unix.Flock(int(lock.Fd()), unix.LOCK_UN)
	deadline = time.Now().Add(time.Second)
	for replacement.MaintenanceStatus().State != "blocked" {
		if time.Now().After(deadline) {
			t.Fatal(replacement.MaintenanceStatus())
		}
		time.Sleep(time.Millisecond)
	}
	w := status()
	var active contractlease.MaintenanceStatus
	if err = json.Unmarshal(w.Body.Bytes(), &active); err != nil || w.Code != 200 || active.OperationID != grant.Status.OperationID || active.Generation != grant.Status.Generation {
		t.Fatal("loaded reservation identity lost", w.Code, err)
	}
	replacement.Close()
	if err = os.WriteFile(marker, []byte(`{"schema":99}`), 0600); err != nil {
		t.Fatal(err)
	}
	corrupt := kitlease.New(time.Minute, func(context.Context) error { return nil }, options...)
	defer corrupt.Close()
	handler = httpapi.New(&fakeController{}, "bearer", "test", nil, httpapi.WithKitLease(corrupt))
	deadline = time.Now().Add(time.Second)
	for corrupt.Status().State != "blocked" {
		if time.Now().After(deadline) {
			t.Fatal(corrupt.Status())
		}
		time.Sleep(time.Millisecond)
	}
	if w := status(); w.Code != http.StatusServiceUnavailable {
		t.Fatal("corrupt identity was reported as known", w.Code, w.Body)
	}
}

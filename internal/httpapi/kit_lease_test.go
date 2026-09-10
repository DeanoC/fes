package httpapi_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/httpapi"
	"github.com/DeanoC/FogCast/internal/input"
	"github.com/DeanoC/FogCast/internal/kitlease"
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
	for _, path := range []string{"/v1/launch", "/v1/stop", "/v2/launch", "/v1/development/rbf", "/v1/development/core", "/v1/development/reboot", "/v1/input/attach", "/v1/input/detach", "/v1/input/stream", "/v1/cast/start", "/v1/cast/stop"} {
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
	for _, path := range []string{"/v1/launch", "/v1/cast/start", "/v1/development/rbf", "/v1/input/attach"} {
		request := httptest.NewRequest(http.MethodPost, path, nil)
		request.Header.Set("Authorization", "Bearer bearer")
		request.Header.Set(httpapi.KitLeaseHeader, grant.Token)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusForbidden {
			t.Fatalf("%s: %d", path, response.Code)
		}
	}
	stop := httptest.NewRequest(http.MethodPost, "/v1/stop", nil)
	stop.Header.Set("Authorization", "Bearer bearer")
	stop.Header.Set(httpapi.KitLeaseHeader, grant.Token)
	stopResponse := httptest.NewRecorder()
	handler.ServeHTTP(stopResponse, stop)
	if stopResponse.Code != 200 {
		t.Fatalf("hostless stop: %d", stopResponse.Code)
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

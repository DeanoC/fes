package hostapi_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/DeanoC/FogCast/host"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/targetclient"
)

func TestSessionLeaseLossPreservesOwnershipRefusal(t *testing.T) {
	for _, operation := range []string{"status", "stop", "launch", "input-reconcile"} {
		t.Run(operation, func(t *testing.T) {
			// Wrapping must retain ownership classification without exposing internals.
			lost := fmt.Errorf("private diagnostic: %w", targetclient.ErrKitLeaseLost)
			svc := &fakeService{statusErr: lost, launchErr: lost}
			input := &fakeRemoteInput{status: host.RemoteInputStatus{State: host.RemoteInputDetached}}
			path, method := "/api/v1/session", http.MethodGet
			if operation == "stop" {
				path, method = "/api/v1/session/stop", http.MethodPost
			}
			if operation == "launch" {
				svc.statusErr = nil
				path, method = "/api/v1/session/launch", http.MethodPost
			}
			if operation == "input-reconcile" {
				svc.statusErr = nil
				core := "Pong"
				svc.status = protocol.Status{State: protocol.StateActive, ObservedCore: &core}
				input.attachErr = lost
			}
			handler := hostapi.New(svc, hostapi.WithRemoteInput(input))
			var responseCode int
			var responseBody string
			if operation == "launch" {
				r := launchSession(t, handler, "game")
				responseCode, responseBody = r.Code, r.Body.String()
			} else {
				r := serve(t, handler, method, path)
				responseCode, responseBody = r.Code, r.Body.String()
			}
			if responseCode != http.StatusForbidden || !strings.Contains(responseBody, `"code":"KIT_LEASE_DENIED"`) || strings.Contains(responseBody, "private diagnostic") {
				t.Fatalf("response = %d %s", responseCode, responseBody)
			}
			if len(svc.stopCtxErrs) != 0 || len(input.detach) != 0 {
				t.Fatalf("lease refusal performed teardown: stops=%d detach=%v", len(svc.stopCtxErrs), input.detach)
			}
		})
	}
}

func TestSessionReadTransportFailureRemainsUnavailable(t *testing.T) {
	svc := &fakeService{statusErr: errors.New("private transport failure")}
	r := serve(t, hostapi.New(svc), http.MethodGet, "/api/v1/session")
	if r.Code != http.StatusServiceUnavailable || !strings.Contains(r.Body.String(), `"code":"TARGET_UNAVAILABLE"`) || strings.Contains(r.Body.String(), "private transport") {
		t.Fatalf("response=%d %s", r.Code, r.Body)
	}
}

func TestSessionInputReconcileUsesRealLeaseAndBridge(t *testing.T) {
	for _, failure := range []string{"local-loss", "claim-denied", "attach-denied", "loss-before-connect", "unknown-attach-failure"} {
		t.Run(failure, func(t *testing.T) {
			var claims, attaches atomic.Int32
			var invalidate func()
			peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v1/kit/claim":
					claims.Add(1)
					if failure == "claim-denied" {
						w.WriteHeader(http.StatusForbidden)
						fmt.Fprint(w, `{"error":{"code":"KIT_LEASE_DENIED","message":"private target details"}}`)
						return
					}
					fmt.Fprint(w, `{"token":"test-grant","status":{"state":"held","generation":"test-generation","expires_in_ms":60000}}`)
				case "/v1/input/attach":
					attaches.Add(1)
					if failure == "loss-before-connect" {
						invalidate()
						fmt.Fprint(w, `{"ready":true}`)
						return
					}
					if r.Header.Get(targetclient.KitLeaseHeader) != "test-grant" {
						t.Error("input attach omitted exact grant")
					}
					if failure == "unknown-attach-failure" {
						w.WriteHeader(http.StatusInternalServerError)
						fmt.Fprint(w, "private transport details")
						return
					}
					w.WriteHeader(http.StatusForbidden)
					fmt.Fprint(w, `{"error":{"code":"KIT_LEASE_DENIED","message":"private target details"}}`)
				case "/v1/kit/release":
					fmt.Fprint(w, `{"state":"free"}`)
				default:
					t.Errorf("unexpected request %s", r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer peer.Close()
			base, _ := url.Parse(peer.URL)
			lease := targetclient.NewKitLease(base, "bearer", peer.Client(), "host-A", "test")
			defer lease.Close(context.Background())
			invalidate = func() {
				targetclient.NewClient(base, "bearer", peer.Client()).WithKitLease(lease).InvalidateKitSession()
			}
			if failure == "local-loss" {
				targetclient.NewClient(base, "bearer", peer.Client()).WithKitLease(lease).InvalidateKitSession()
			}
			starter, err := host.NewHTTPBridgeStarter(host.HTTPBridgeStarterConfig{BaseURL: base, Token: "bearer", HTTPClient: peer.Client(), KitLease: lease})
			if err != nil {
				t.Fatal(err)
			}
			input, err := host.NewRemoteInput(host.RemoteInputConfig{Starter: starter})
			if err != nil {
				t.Fatal(err)
			}
			defer input.Close()
			core := "Pong"
			svc := &fakeService{status: protocol.Status{State: protocol.StateActive, ObservedCore: &core}}
			r := serve(t, hostapi.New(svc, hostapi.WithRemoteInput(input)), http.MethodGet, "/api/v1/session")
			want := http.StatusForbidden
			code := "KIT_LEASE_DENIED"
			if failure == "unknown-attach-failure" {
				want = http.StatusServiceUnavailable
				code = "TARGET_UNAVAILABLE"
			}
			if r.Code != want || !strings.Contains(r.Body.String(), `"code":"`+code+`"`) || strings.Contains(r.Body.String(), "private") {
				t.Fatalf("response=%d %s", r.Code, r.Body)
			}
			expectedClaims, expectedAttaches := int32(1), int32(1)
			if failure == "local-loss" {
				expectedClaims, expectedAttaches = 0, 0
			}
			if failure == "claim-denied" {
				expectedAttaches = 0
			}
			if claims.Load() != expectedClaims || attaches.Load() != expectedAttaches || len(svc.stopCtxErrs) != 0 {
				t.Fatalf("claims=%d attaches=%d stops=%d", claims.Load(), attaches.Load(), len(svc.stopCtxErrs))
			}
		})
	}
}

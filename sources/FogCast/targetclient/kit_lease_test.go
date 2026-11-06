package targetclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/protocol"
)

func TestKitMutationMatchesAMeshPrefixOnly(t *testing.T) {
	if gated, acquire := kitMutation("/v1/zx81-tape/acknowledge"); !gated || acquire {
		t.Fatalf("ZX81 SAVE acknowledgement gated %v acquire %v", gated, acquire)
	}
	if gated, acquire := kitMutation("/agent/v1/mesh/content/pull"); !gated || !acquire {
		t.Fatalf("pull gated %v acquire %v", gated, acquire)
	}
	if gated, acquire := kitMutation("/agent/v1/mesh/content/link"); !gated || acquire {
		t.Fatalf("link gated %v acquire %v", gated, acquire)
	}
	if gated, _ := kitMutation("/agent/v1/launch"); gated {
		t.Fatal("prefix widened a non-mesh route")
	}
	if gated, acquire := kitMutation("/v1/mesh/content/pull"); !gated || !acquire {
		t.Fatalf("exact pull gated %v acquire %v", gated, acquire)
	}
}

func TestPrefixedMeshPullClaims(t *testing.T) {
	claims := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/kit/release" {
			fmt.Fprint(w, `{"state":"free"}`)
			return
		}
		if r.URL.Path != "/v1/kit/claim" {
			t.Errorf("path %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		claims++
		fmt.Fprintf(w, `{"status":{"state":"held","generation":"one","expires_at":%q,"expires_in_ms":60000},"token":"lease-secret"}`, time.Now().Add(time.Minute).Format(time.RFC3339))
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	lease := NewKitLease(u, "bearer", server.Client(), "test", "mesh")
	defer lease.Close(context.Background())
	client := NewClient(u, "bearer", server.Client()).WithKitLease(lease)
	request, err := http.NewRequest(http.MethodPost, server.URL+"/agent/v1/mesh/content/pull", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.AuthorizeMutation(request); err != nil {
		t.Fatal(err)
	}
	if claims != 1 || request.Header.Get(KitLeaseHeader) != "lease-secret" {
		t.Fatalf("claims %d header %q", claims, request.Header.Get(KitLeaseHeader))
	}
}

func TestKitLeaseSharedMutationAndNoForeignStop(t *testing.T) {
	claims, mutations := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/kit/claim":
			claims++
			fmt.Fprintf(w, `{"status":{"state":"held","generation":"one","expires_at":%q,"expires_in_ms":60000},"token":"lease-secret"}`, time.Now().Add(time.Minute).Format(time.RFC3339))
		default:
			mutations++
			if r.Header.Get("X-FogCast-Kit-Lease") != "lease-secret" {
				t.Error("missing lease")
			}
			fmt.Fprint(w, `{"state":"idle"}`)
		}
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	lease := NewKitLease(u, "bearer", server.Client(), "test", "game")
	defer lease.Close(context.Background())
	c := NewClient(u, "bearer", server.Client()).WithKitLease(lease)
	if _, err := c.Stop(context.Background()); err == nil {
		t.Fatal("foreign stop allowed")
	}
	if claims != 0 || mutations != 0 {
		t.Fatal("stop claimed or mutated")
	}
	for i := 0; i < 2; i++ {
		r, _ := http.NewRequest("POST", server.URL+"/v1/launch", nil)
		if err := lease.Authorize(r, true); err != nil {
			t.Fatal(err)
		}
		if r.Header.Get("X-FogCast-Kit-Lease") != "lease-secret" {
			t.Fatal("missing token")
		}
	}
	if claims != 1 {
		t.Fatalf("claims=%d", claims)
	}
	if owned, generation := c.MeshKitLease(); !owned || generation != "one" {
		t.Fatalf("owned=%v generation=%q", owned, generation)
	}
}

func TestKitLeaseAcquireReconcilesFreeTargetBeforeFreshClaim(t *testing.T) {
	var mu sync.Mutex
	claimCount := 0
	ownership := "held"
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		requests = append(requests, r.Method+" "+r.URL.Path)
		switch r.Method + " " + r.URL.Path {
		case "POST /v1/kit/claim":
			claimCount++
			token, generation := "old-token", "old-generation"
			if claimCount == 2 {
				token, generation = "fresh-token", "fresh-generation"
				ownership = "held"
			}
			fmt.Fprintf(w, `{"status":{"state":"held","generation":%q,"expires_in_ms":60000},"token":%q}`, generation, token)
		case "GET /v1/kit/lease":
			if ownership == "held" && claimCount == 1 {
				fmt.Fprint(w, `{"state":"held","generation":"old-generation"}`)
			} else {
				fmt.Fprint(w, `{"state":"free"}`)
			}
		case "POST /v1/stop":
			if r.Header.Get(KitLeaseHeader) != "fresh-token" {
				t.Error("recovery Stop did not use the fresh grant")
			}
			fmt.Fprint(w, `{"state":"idle"}`)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	lease := NewKitLease(u, "bearer", server.Client(), "test", "recovery")
	client := NewClient(u, "bearer", server.Client()).WithKitLease(lease)
	defer client.InvalidateKitSession()
	request, _ := http.NewRequest(http.MethodPost, server.URL+"/v1/stop", nil)
	if err := lease.Authorize(request, true); err != nil {
		t.Fatal("initial claim:", err)
	}
	mu.Lock()
	ownership = "free" // target reboot discarded the cached grant
	mu.Unlock()
	if err := lease.Acquire(context.Background()); err != nil {
		t.Fatal("reconciled claim:", err)
	}
	if _, err := client.Stop(context.Background()); err != nil {
		t.Fatal("Stop with fresh claim:", err)
	}
	want := []string{"POST /v1/kit/claim", "GET /v1/kit/lease", "POST /v1/kit/claim", "POST /v1/stop"}
	mu.Lock()
	gotClaims, gotRequests := claimCount, append([]string(nil), requests...)
	mu.Unlock()
	if gotClaims != 2 || fmt.Sprint(gotRequests) != fmt.Sprint(want) {
		t.Fatalf("claim count=%d requests=%v want=%v", gotClaims, gotRequests, want)
	}
}

func TestKitLeaseAcquireRefusesAnotherHeldGeneration(t *testing.T) {
	claims, stops := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "POST /v1/kit/claim":
			claims++
			fmt.Fprint(w, `{"status":{"state":"held","generation":"old-generation","expires_in_ms":60000},"token":"old-token"}`)
		case "GET /v1/kit/lease":
			fmt.Fprint(w, `{"state":"held","generation":"foreign-generation"}`)
		case "POST /v1/stop":
			stops++
			t.Error("Stop ran despite a foreign grant")
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	lease := NewKitLease(u, "bearer", server.Client(), "test", "recovery")
	defer func() {
		lease.mu.Lock()
		lease.invalidateLocked()
		lease.mu.Unlock()
	}()
	request, _ := http.NewRequest(http.MethodPost, server.URL+"/v1/stop", nil)
	if err := lease.Authorize(request, true); err != nil {
		t.Fatal("initial claim:", err)
	}
	err := lease.Acquire(context.Background())
	var api *protocol.APIError
	if !errors.As(err, &api) || api.Code != protocol.CodeKitLeaseBusy {
		t.Fatalf("foreign ownership error = %v, want KIT_LEASE_BUSY", err)
	}
	if claims != 1 || stops != 0 {
		t.Fatalf("claims=%d stops=%d, want 1 claim and no Stop", claims, stops)
	}
}

func TestKitLeaseClaimFailureNeverMutates(t *testing.T) {
	mutations := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/kit/claim" {
			mutations++
		}
		http.Error(w, "not found", 404)
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	lease := NewKitLease(u, "bearer", server.Client(), "test", "game")
	c := NewClient(u, "bearer", server.Client()).WithKitLease(lease)
	if err := c.doJSON(context.Background(), "POST", "/v1/launch", nil, &struct{}{}); err == nil {
		t.Fatal("unsupported target accepted")
	}
	if mutations != 0 {
		t.Fatal("unguarded mutation")
	}
}

func TestKitLeaseRenewalLossNeverReclaimsOrStops(t *testing.T) {
	claims, stops := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/kit/claim":
			claims++
			fmt.Fprintf(w, `{"status":{"state":"held","generation":"one","expires_at":%q,"expires_in_ms":60000},"token":"secret"}`, time.Now().Add(time.Minute).Format(time.RFC3339))
		case "/v1/kit/renew":
			http.Error(w, `{"error":{"code":"BUSY","message":"expired"}}`, 409)
		case "/v1/stop":
			stops++
			fmt.Fprint(w, `{"state":"idle"}`)
		}
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	l := NewKitLease(u, "bearer", server.Client(), "test", "game")
	defer l.Close(context.Background())
	r, _ := http.NewRequest("POST", server.URL+"/v1/launch", nil)
	if err := l.Authorize(r, true); err != nil {
		t.Fatal(err)
	}
	l.renew(context.Background())
	if err := l.Authorize(r, true); err == nil {
		t.Fatal("lost lease reclaimed")
	}
	if _, err := NewClient(u, "bearer", server.Client()).WithKitLease(l).Stop(context.Background()); err == nil {
		t.Fatal("stale cleanup permitted")
	}
	if claims != 1 || stops != 0 {
		t.Fatalf("claims=%d stops=%d", claims, stops)
	}
}

func TestKitLeaseIgnoresTargetWallClockAndRequiresRemainingDuration(t *testing.T) {
	for _, remaining := range []int64{90000, 0} {
		t.Run(fmt.Sprint(remaining), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/kit/release" {
					fmt.Fprint(w, `{"state":"free"}`)
					return
				}
				fmt.Fprintf(w, `{"status":{"state":"held","generation":"one","expires_at":"1970-01-01T00:01:30Z","expires_in_ms":%d},"token":"skew-safe"}`, remaining)
			}))
			defer server.Close()
			u, _ := url.Parse(server.URL)
			l := NewKitLease(u, "bearer", server.Client(), "test", "game")
			defer l.Close(context.Background())
			r, _ := http.NewRequest("POST", server.URL+"/v1/launch", nil)
			err := l.Authorize(r, true)
			if remaining == 0 {
				if err == nil {
					t.Fatal("zero remaining duration accepted")
				}
				return
			}
			if err != nil {
				t.Fatalf("1970 target should be usable: %v", err)
			}
			l.renew(context.Background())
			if err := l.Authorize(r, false); err != nil {
				t.Fatalf("skewed renewal: %v", err)
			}
		})
	}
}

func TestKitLeaseChargesNetworkTimeAgainstRemainingDuration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(20 * time.Millisecond)
		fmt.Fprint(w, `{"status":{"state":"held","generation":"one","expires_in_ms":1},"token":"expired-on-arrival"}`)
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	l := NewKitLease(u, "bearer", server.Client(), "test", "game")
	defer l.Close(context.Background())
	r, _ := http.NewRequest("POST", server.URL+"/v1/launch", nil)
	if err := l.Authorize(r, true); err == nil {
		t.Fatal("expired response authorized mutation")
	}
}

func TestCancelledOldRenewerCannotInvalidateReplacementLease(t *testing.T) {
	claims := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/kit/claim":
			claims++
			fmt.Fprintf(w, `{"status":{"state":"held","generation":"generation-%d","expires_in_ms":90000},"token":"token-%d"}`, claims, claims)
		case "/v1/kit/release":
			fmt.Fprint(w, `{"state":"free"}`)
		case "/v1/kit/renew":
			t.Error("cancelled old renewer dispatched a renewal")
			http.Error(w, "unexpected renewal", http.StatusInternalServerError)
		}
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	lease := NewKitLease(u, "bearer", server.Client(), "test", "game")
	defer lease.Close(context.Background())
	request, _ := http.NewRequest(http.MethodPost, server.URL+"/v1/launch", nil)
	if err := lease.Authorize(request, true); err != nil {
		t.Fatal(err)
	}
	oldContext, cancelOld := context.WithCancel(context.Background())
	cancelOld()
	if err := lease.Release(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := lease.Authorize(request, true); err != nil {
		t.Fatal(err)
	}
	lease.renew(oldContext)
	if err := lease.Authorize(request, false); err != nil {
		t.Fatalf("old renewer invalidated new ownership: %v", err)
	}
	if got := request.Header.Get(KitLeaseHeader); got != "token-2" {
		t.Fatalf("replacement token = %q", got)
	}
}

func TestKitLeaseReleaseGrantDropsTheToken(t *testing.T) {
	releases := 0
	lease, _ := newHeldKitLease(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/kit/claim":
			fmt.Fprintf(w, `{"status":{"state":"held","generation":"one","expires_in_ms":60000},"token":"lease-secret"}`)
		case "/v1/kit/release":
			releases++
			if r.Header.Get(KitLeaseHeader) == "" {
				t.Error("release omitted the kit lease header")
			}
			fmt.Fprint(w, `{"state":"free"}`)
		default:
			http.Error(w, "unexpected", http.StatusInternalServerError)
		}
	})
	notHeld, err := lease.ReleaseGrant(context.Background())
	if err != nil || notHeld || lease.Held() {
		t.Fatalf("notHeld %v held %v err %v", notHeld, lease.Held(), err)
	}
	held, lost, renewing := kitLeaseGrantState(lease)
	if held || lost || renewing {
		t.Fatalf("after release held %v lost %v renewing %v", held, lost, renewing)
	}
	notHeld, err = lease.ReleaseGrant(context.Background())
	if err != nil || notHeld || releases != 1 {
		t.Fatalf("second notHeld %v releases %d err %v", notHeld, releases, err)
	}
}

func TestKitLeaseReleaseGrantForgetsMissingGrant(t *testing.T) {
	claims := 0
	lease, client := newHeldKitLease(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/kit/claim":
			claims++
			fmt.Fprintf(w, `{"status":{"state":"held","generation":"g%d","expires_in_ms":60000},"token":"lease-%d"}`, claims, claims)
		case "/v1/kit/release":
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `{"error":{"code":"KIT_LEASE_REQUIRED","message":"KIT_LEASE_REQUIRED"}}`)
		default:
			http.Error(w, "unexpected", http.StatusInternalServerError)
		}
	})
	notHeld, err := lease.ReleaseGrant(context.Background())
	if err != nil || !notHeld || lease.Held() {
		t.Fatalf("notHeld %v held %v err %v", notHeld, lease.Held(), err)
	}
	held, lost, renewing := kitLeaseGrantState(lease)
	if held || lost || renewing {
		t.Fatalf("forgotten grant held %v lost %v renewing %v", held, lost, renewing)
	}
	if err := client.AcquireContentPullLease(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !lease.Held() || claims != 2 {
		t.Fatalf("reclaim held %v claims %d", lease.Held(), claims)
	}
}

func TestKitLeaseReleaseGrantConflictIsNotHeld(t *testing.T) {
	lease, _ := newHeldKitLease(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/kit/claim":
			fmt.Fprintf(w, `{"status":{"state":"held","generation":"one","expires_in_ms":60000},"token":"lease-secret"}`)
		case "/v1/kit/release":
			w.WriteHeader(http.StatusConflict)
			fmt.Fprint(w, `{"error":{"code":"KIT_LEASE_BUSY","message":"KIT_LEASE_BUSY"}}`)
		default:
			http.Error(w, "unexpected", http.StatusInternalServerError)
		}
	})
	notHeld, err := lease.ReleaseGrant(context.Background())
	if err != nil || !notHeld || lease.Held() {
		t.Fatalf("notHeld %v held %v err %v", notHeld, lease.Held(), err)
	}
	_, lost, renewing := kitLeaseGrantState(lease)
	if lost || renewing {
		t.Fatalf("lost %v renewing %v", lost, renewing)
	}
}

func TestKitLeaseReleaseGrantNotFoundIsNotHeld(t *testing.T) {
	lease, _ := newHeldKitLease(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/kit/claim":
			fmt.Fprintf(w, `{"status":{"state":"held","generation":"one","expires_in_ms":60000},"token":"lease-secret"}`)
		case "/v1/kit/release":
			http.Error(w, "missing", http.StatusNotFound)
		default:
			http.Error(w, "unexpected", http.StatusInternalServerError)
		}
	})
	notHeld, err := lease.ReleaseGrant(context.Background())
	if err != nil || !notHeld || lease.Held() {
		t.Fatalf("notHeld %v held %v err %v", notHeld, lease.Held(), err)
	}
}

func TestKitLeaseReleaseGrantServerErrorKeepsTheToken(t *testing.T) {
	lease, _ := newHeldKitLease(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/kit/claim":
			fmt.Fprintf(w, `{"status":{"state":"held","generation":"one","expires_in_ms":60000},"token":"lease-secret"}`)
		case "/v1/kit/release":
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, `{"error":{"code":"INTERNAL","message":"release failed"}}`)
		default:
			http.Error(w, "unexpected", http.StatusInternalServerError)
		}
	})
	notHeld, err := lease.ReleaseGrant(context.Background())
	if err == nil || notHeld || !lease.Held() {
		t.Fatalf("notHeld %v held %v err %v", notHeld, lease.Held(), err)
	}
	held, lost, renewing := kitLeaseGrantState(lease)
	if !held || lost || !renewing {
		t.Fatalf("held %v lost %v renewing %v", held, lost, renewing)
	}
}

func TestKitLeaseReleaseKeepsGrantWhenKitRequiresTheLease(t *testing.T) {
	lease, _ := newHeldKitLease(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/kit/claim":
			fmt.Fprintf(w, `{"status":{"state":"held","generation":"one","expires_in_ms":60000},"token":"lease-secret"}`)
		case "/v1/kit/release":
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `{"error":{"code":"KIT_LEASE_REQUIRED","message":"KIT_LEASE_REQUIRED"}}`)
		default:
			http.Error(w, "unexpected", http.StatusInternalServerError)
		}
	})
	if err := lease.Release(context.Background()); err == nil {
		t.Fatal("release succeeded")
	}
	held, lost, renewing := kitLeaseGrantState(lease)
	if !held || lost || !renewing {
		t.Fatalf("held %v lost %v renewing %v", held, lost, renewing)
	}
}

func TestClientReleaseKitGrantRequiresALease(t *testing.T) {
	var client *Client
	notHeld, err := client.ReleaseKitGrant(context.Background())
	if notHeld || !errors.Is(err, ErrKitLeaseLost) {
		t.Fatalf("nil client notHeld %v err %v", notHeld, err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	notHeld, err = NewClient(u, "bearer", server.Client()).ReleaseKitGrant(context.Background())
	if notHeld || !errors.Is(err, ErrKitLeaseLost) {
		t.Fatalf("unleased client notHeld %v err %v", notHeld, err)
	}
}

func newHeldKitLease(t *testing.T, handler http.HandlerFunc) (*KitLease, *Client) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	lease := NewKitLease(u, "bearer", server.Client(), "test", "game")
	t.Cleanup(func() { _ = lease.Close(context.Background()) })
	client := NewClient(u, "bearer", server.Client()).WithKitLease(lease)
	request, err := http.NewRequest(http.MethodPost, server.URL+"/v1/launch", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.Authorize(request, true); err != nil {
		t.Fatal(err)
	}
	return lease, client
}

func kitLeaseGrantState(lease *KitLease) (held, lost, renewing bool) {
	lease.mu.Lock()
	defer lease.mu.Unlock()
	return lease.grant.Token != "", lease.lost, lease.cancel != nil
}

func TestKitLeaseFailedReleaseKeepsGrantForRetry(t *testing.T) {
	releases := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/kit/claim":
			fmt.Fprintf(w, `{"status":{"state":"held","generation":"one","expires_in_ms":60000},"token":"keep-me"}`)
		case "/v1/kit/release":
			releases++
			if r.Header.Get(KitLeaseHeader) != "keep-me" {
				t.Errorf("release token = %q", r.Header.Get(KitLeaseHeader))
			}
			if releases == 1 {
				http.Error(w, `{"error":{"code":"INTERNAL","message":"release failed"}}`, http.StatusInternalServerError)
				return
			}
			fmt.Fprint(w, `{"state":"free"}`)
		default:
			http.Error(w, "unexpected", http.StatusInternalServerError)
		}
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	lease := NewKitLease(u, "bearer", server.Client(), "test", "game")
	defer lease.Close(context.Background())
	request, _ := http.NewRequest(http.MethodPost, server.URL+"/v1/launch", nil)
	if err := lease.Authorize(request, true); err != nil {
		t.Fatal(err)
	}
	if err := lease.Release(context.Background()); err == nil {
		t.Fatal("first release succeeded")
	}
	if !lease.Held() {
		t.Fatal("failed release dropped local grant")
	}
	if err := lease.Release(context.Background()); err != nil {
		t.Fatal(err)
	}
	if lease.Held() {
		t.Fatal("successful retry still held")
	}
	if releases != 2 {
		t.Fatalf("releases=%d", releases)
	}
}

// Expiry can pass while a suspended host retains its token and before the
// renewal goroutine runs. A completed physical load must not revive that grant.
func TestCoreLoadRejectsExpiredGrantBeforeRenewalRuns(t *testing.T) {
	for _, expired := range []bool{false, true} {
		name := "held"
		if expired {
			name = "expired token retained"
		}
		t.Run(name, func(t *testing.T) {
			var lease *KitLease
			var claims, loads atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v1/kit/claim":
					claims.Add(1)
					fmt.Fprint(w, `{"status":{"state":"held","generation":"one","expires_in_ms":60000},"token":"owned"}`)
				case "/v1/development/core":
					loads.Add(1)
					if expired {
						lease.mu.Lock()
						lease.localExpiry = time.Now().Add(-time.Second)
						lease.mu.Unlock()
					}
					json.NewEncoder(w).Encode(protocol.Status{State: protocol.StateActive, Development: true,
						CorePackage: &protocol.CorePackageStatus{PackageID: strings.Repeat("a", 64), Generation: 1,
							ABI: protocol.RuntimeContract{ID: "fes.application", Major: 1}, BuildID: strings.Repeat("b", 32), PersistenceMode: "volatile"}})
				default:
					t.Errorf("unexpected request or replay: %s", r.URL.Path)
					http.Error(w, "unexpected", http.StatusInternalServerError)
				}
			}))
			defer server.Close()
			base, _ := url.Parse(server.URL)
			lease = NewKitLease(base, "secret", server.Client(), "host", "launch")
			client := NewClient(base, "secret", server.Client()).WithKitLease(lease)
			defer client.InvalidateKitSession()
			status, err := client.LoadCore(context.Background(), 1, strings.NewReader("x"))
			if expired {
				var api *protocol.APIError
				if !errors.Is(err, ErrKitLeaseLost) || !errors.As(err, &api) || api.Phase != "recovery" || status.State == protocol.StateActive || status.CorePackage != nil {
					t.Fatalf("expired grant admitted late active reply: status=%+v err=%v", status, err)
				}
				lease.mu.Lock()
				token, lost := lease.grant.Token, lease.lost
				lease.mu.Unlock()
				if token != "owned" || lost {
					t.Fatal("fixture required a retained token without renewal invalidation")
				}
			} else if err != nil || status.State != protocol.StateActive {
				t.Fatalf("fresh grant rejected: %+v %v", status, err)
			}
			if claims.Load() != 1 || loads.Load() != 1 {
				t.Fatalf("claims=%d loads=%d", claims.Load(), loads.Load())
			}
		})
	}
}

type failingLocalRenewTransport struct {
	base http.RoundTripper
	fail atomic.Bool
}

func (t *failingLocalRenewTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Path == "/v1/kit/renew" && t.fail.Load() {
		return nil, errors.New("temporary connection failure")
	}
	return t.base.RoundTrip(r)
}

func TestLocalLeaseRetriesTransientRenewalWithinGrace(t *testing.T) {
	var renews atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/kit/local/claim" && r.URL.Path != "/v1/kit/renew" {
			t.Errorf("unexpected request %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if r.URL.Path == "/v1/kit/renew" {
			if r.Header.Get(KitLeaseHeader) != "original-token" {
				t.Error("renewal did not preserve original grant")
			}
			if renews.Add(1) == 1 {
				http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
				return
			}
		}
		fmt.Fprint(w, `{"status":{"state":"held","generation":"original-generation","expires_in_ms":600000},"token":"original-token"}`)
	}))
	defer server.Close()
	base, _ := url.Parse(server.URL)
	httpClient := *server.Client()
	transport := &failingLocalRenewTransport{base: httpClient.Transport}
	httpClient.Transport = transport
	lease := NewLocalKitLease(base, "bearer", &httpClient)
	defer lease.Close(context.Background())
	request, _ := http.NewRequest(http.MethodPost, server.URL+"/v1/launch", nil)
	if err := lease.Authorize(request, true); err != nil {
		t.Fatal(err)
	}
	expiry := lease.localExpiry
	transport.fail.Store(true)
	lease.renew(context.Background())
	if lease.lost || lease.CurrentToken() != "original-token" || !lease.localExpiry.Equal(expiry) {
		t.Fatal("transport error discarded or extended local authority")
	}
	transport.fail.Store(false)
	lease.renew(context.Background())
	if lease.lost || lease.CurrentToken() != "original-token" || !lease.localExpiry.Equal(expiry) {
		t.Fatal("transient server error discarded or extended local authority")
	}
	lease.renew(context.Background())
	if lease.lost || !lease.localExpiry.After(expiry) || lease.CurrentToken() != "original-token" {
		t.Fatal("later renewal failed to recover the original grant")
	}
	if err := lease.Authorize(request, false); err != nil {
		t.Fatalf("recovered grant unusable: %v", err)
	}
}

func TestLocalLeaseRejectsLossOrChangedGrant(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"forbidden", http.StatusForbidden, `{"error":{"code":"KIT_LEASE_REQUIRED"}}`},
		{"conflict", http.StatusConflict, `{"error":{"code":"KIT_LEASE_BUSY"}}`},
		{"unauthorized", http.StatusUnauthorized, `{"error":{"code":"UNAUTHORIZED"}}`},
		{"changed token", http.StatusOK, `{"status":{"state":"held","generation":"original-generation","expires_in_ms":600000},"token":"foreign-token"}`},
		{"changed generation", http.StatusOK, `{"status":{"state":"held","generation":"replacement-generation","expires_in_ms":600000},"token":"original-token"}`},
		{"malformed success", http.StatusOK, `{}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var resumes, claims atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v1/kit/local/claim":
					claims.Add(1)
					fmt.Fprint(w, `{"status":{"state":"held","generation":"original-generation","expires_in_ms":600000},"token":"original-token"}`)
				case "/v1/kit/renew":
					w.WriteHeader(tc.status)
					fmt.Fprint(w, tc.body)
				case "/v1/kit/local/resume":
					resumes.Add(1)
					t.Error("lost grant was automatically resumed")
				default:
					t.Errorf("unexpected request %s", r.URL.Path)
				}
			}))
			defer server.Close()
			base, _ := url.Parse(server.URL)
			lease := NewLocalKitLease(base, "bearer", server.Client())
			defer lease.Close(context.Background())
			request, _ := http.NewRequest(http.MethodPost, server.URL+"/v1/launch", nil)
			if err := lease.Authorize(request, true); err != nil {
				t.Fatal(err)
			}
			lease.renew(context.Background())
			if !lease.lost || lease.CurrentToken() != "" {
				t.Fatal("invalid renewal retained authority")
			}
			if err := lease.resumeLocal(context.Background()); err != nil {
				t.Fatal(err)
			}
			request.Header.Del(KitLeaseHeader)
			if err := lease.Authorize(request, true); !errors.Is(err, ErrKitLeaseLost) || request.Header.Get(KitLeaseHeader) != "" {
				t.Fatalf("lost grant authorized mutation: %v", err)
			}
			if claims.Load() != 1 || resumes.Load() != 0 {
				t.Fatalf("claims=%d resumes=%d", claims.Load(), resumes.Load())
			}
		})
	}
}

func TestLocalLeaseDoesNotRetryAfterGraceExpires(t *testing.T) {
	var renews atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/kit/renew" {
			renews.Add(1)
			http.Error(w, "unavailable", 503)
			return
		}
		fmt.Fprint(w, `{"status":{"state":"held","generation":"generation","expires_in_ms":600000},"token":"token"}`)
	}))
	defer server.Close()
	base, _ := url.Parse(server.URL)
	lease := NewLocalKitLease(base, "bearer", server.Client())
	defer lease.Close(context.Background())
	request, _ := http.NewRequest(http.MethodPost, server.URL+"/v1/launch", nil)
	if err := lease.Authorize(request, true); err != nil {
		t.Fatal(err)
	}
	lease.renew(context.Background())
	if lease.lost {
		t.Fatal("temporary failure lost valid grant")
	}
	lease.mu.Lock()
	lease.localExpiry = time.Now().Add(-time.Second)
	lease.mu.Unlock()
	lease.renew(context.Background())
	if !lease.lost || lease.CurrentToken() != "" || renews.Load() != 1 {
		t.Fatalf("expired grant retried: lost=%v renews=%d", lease.lost, renews.Load())
	}
}

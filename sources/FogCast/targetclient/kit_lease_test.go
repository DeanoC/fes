package targetclient

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

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

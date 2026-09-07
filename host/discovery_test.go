package host

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"
)

func TestAdoptEndpointDiscardsOldGrantWithoutCleanup(t *testing.T) {
	mutations := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			mutations++
		}
		json.NewEncoder(w).Encode(KitOwnership{State: "free"})
	}))
	defer srv.Close()
	old, _ := url.Parse("http://127.0.0.1:1")
	next, _ := url.Parse(srv.URL)
	lease := NewKitLease(old, "secret", srv.Client(), "host", "test")
	lease.grant.Token = "stale"
	lease.grant.Status.Generation = "old"
	c := NewClient(old, "secret", srv.Client()).WithKitLease(lease)
	ownership, err := c.AdoptEndpoint(context.Background(), next, true)
	if err != nil || ownership.State != "free" {
		t.Fatalf("%+v %v", ownership, err)
	}
	if lease.currentToken() != "" || lease.Endpoint().String() != srv.URL {
		t.Fatal("stale lease or endpoint")
	}
	if err := lease.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if mutations != 0 {
		t.Fatalf("discovery performed %d mutations", mutations)
	}
}

func TestSameBootEndpointAdoptionValidatesGrantAndMovesInputAndRenewal(t *testing.T) {
	for _, generation := range []string{"same", "different"} {
		t.Run(generation, func(t *testing.T) {
			var renews atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer secret" {
					t.Error("missing bearer")
				}
				switch r.URL.Path {
				case "/v1/kit/lease":
					json.NewEncoder(w).Encode(KitOwnership{State: "held", Generation: generation, Owner: "host"})
				case "/v1/kit/renew":
					renews.Add(1)
					if r.Header.Get(KitLeaseHeader) != "token" {
						t.Error("missing grant")
					}
					json.NewEncoder(w).Encode(kitLeaseGrant{Token: "token", Status: kitLeaseStatus{State: "held", Generation: "same", ExpiresInMS: 60000}})
				default:
					t.Errorf("unexpected mutation %s", r.URL.Path)
				}
			}))
			defer srv.Close()
			old, _ := url.Parse("http://127.0.0.1:1")
			next, _ := url.Parse(srv.URL)
			lease := NewKitLease(old, "secret", srv.Client(), "host", "test")
			lease.grant = kitLeaseGrant{Token: "token", Status: kitLeaseStatus{State: "held", Generation: "same", ExpiresInMS: 60000}}
			lease.localExpiry = time.Now().Add(time.Minute)
			c := NewClient(old, "secret", srv.Client()).WithKitLease(lease)
			starter, err := NewHTTPBridgeStarter(HTTPBridgeStarterConfig{BaseURL: old, Token: "secret", KitLease: lease})
			if err != nil {
				t.Fatal(err)
			}
			ownership, err := c.AdoptEndpoint(context.Background(), next, false)
			if err != nil {
				t.Fatal(err)
			}
			if ownership.Owned != (generation == "same") {
				t.Fatal("incorrect local ownership")
			}
			if starter.endpoint("/v1/input/attach").Host != next.Host {
				t.Fatal("input endpoint stale")
			}
			lease.renew(context.Background())
			want := int32(0)
			if generation == "same" {
				want = 1
			}
			if renews.Load() != want {
				t.Fatal("renewal used stale ownership")
			}
		})
	}
}

func TestRemoteInputInvalidationDoesNotStopPreviousBridge(t *testing.T) {
	stopped := false
	bridge := &discoveryBridge{stopped: &stopped}
	conn, peer := net.Pipe()
	defer peer.Close()
	remote := &RemoteInput{bridge: bridge, conn: conn, state: RemoteInputAttached, ready: true, session: 7}
	remote.Invalidate()
	peer.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := peer.Read(make([]byte, 1)); err != io.EOF {
		t.Fatalf("old input stream not closed: %v", err)
	}
	if stopped || remote.bridge != nil || remote.Status().State != RemoteInputDetached {
		t.Fatal("invalidation sent cleanup or retained stale input")
	}
}

type discoveryBridge struct{ stopped *bool }

func (b *discoveryBridge) Ready() <-chan struct{}     { ch := make(chan struct{}); close(ch); return ch }
func (b *discoveryBridge) Endpoint() string           { return "127.0.0.1:1" }
func (b *discoveryBridge) Stop(context.Context) error { *b.stopped = true; return nil }

func TestIdentityProbeDoesNotFollowRedirect(t *testing.T) {
	var redirected atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected.Add(1) }))
	defer destination.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	base, _ := url.Parse(redirect.URL)
	if _, err := NewClient(base, "secret", redirect.Client()).Health(context.Background()); err == nil {
		t.Fatal("redirect accepted")
	}
	if redirected.Load() != 0 {
		t.Fatal("identity credentials followed redirect")
	}
}

func TestRebootInvalidatesGrantBeforeOwnershipProbeFailure(t *testing.T) {
	var mutations atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			mutations.Add(1)
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	base, _ := url.Parse(server.URL)
	lease := NewKitLease(base, "secret", server.Client(), "host", "test")
	lease.grant = kitLeaseGrant{Token: "old", Status: kitLeaseStatus{State: "held", Generation: "old"}}
	lease.localExpiry = time.Now().Add(time.Minute)
	client := NewClient(base, "secret", server.Client()).WithKitLease(lease)
	if _, err := client.AdoptEndpoint(context.Background(), base, true); err == nil {
		t.Fatal("accepted failed ownership probe")
	}
	if client.HasKitGrant() {
		t.Fatal("retained rebooted grant")
	}
	lease.renew(context.Background())
	if err := lease.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if mutations.Load() != 0 {
		t.Fatal("stale renewal or release")
	}
}

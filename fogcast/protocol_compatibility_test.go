package fogcast

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/targetclient"
)

func TestProtocolAdmissionBeforeMutationAndDiscoveryAdoption(t *testing.T) {
	const id = "f2bb8d43-3cf5-4407-9a11-dfb7cb0086aa"
	for _, discovered := range []bool{false, true} {
		for _, body := range []string{
			`{"api_version":"v2","ready":true}`,
			`{"ready":true}`,
			`{"api_version":1,"ready":true}`,
			`null`,
		} {
			t.Run(body+map[bool]string{false: "/address", true: "/discovery"}[discovered], func(t *testing.T) {
				var other atomic.Int32
				peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path != "/v1/health" {
						other.Add(1)
						http.Error(w, "unexpected", 500)
						return
					}
					// A matching ID must not make an invalid contract admissible.
					reply := strings.Replace(body, `"ready":true`, `"ready":true,"target_id":"`+id+`"`, 1)
					w.Write([]byte(reply))
				}))
				defer peer.Close()
				address, targetID := peer.URL, ""
				if discovered {
					address, targetID = "http://127.0.0.1:1", id
				}
				base, _ := url.Parse(address)
				client := targetclient.NewClient(base, "secret", peer.Client())
				s := newService(Config{Targets: []TargetConfig{{Name: "kit", Enabled: true, Address: address, Agent: "secret", TargetID: targetID}}, SelectedTarget: "kit", RequestTimeout: time.Second}, Paths{}, &fakeServiceCatalog{}, &fakeServiceScanner{}, &fakeServicePreparer{}, client)
				s.resolveTarget = func(context.Context, string) ([]string, error) { return []string{peer.URL}, nil }
				if _, err := s.LoadDevelopmentRBF(context.Background(), 1, strings.NewReader("x")); err == nil {
					t.Fatal("invalid peer admitted")
				}
				if other.Load() != 0 {
					t.Fatalf("peer received %d non-health calls", other.Load())
				}
				if client.EndpointURL().String() != address {
					t.Fatal("invalid discovered peer adopted")
				}
			})
		}
	}
}

func TestV1DifferentBuildRevisionsRemainCompatible(t *testing.T) {
	artifacts := &protocol.Artifacts{RuntimeCommit: strings.Repeat("b", 40), AgentRevision: strings.Repeat("c", 40)}
	health := protocol.Health{APIVersion: "v1", Ready: true, Artifacts: artifacts}
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/stop":
			json.NewEncoder(w).Encode(protocol.Status{State: protocol.StateIdle})
		case "/v1/health":
			json.NewEncoder(w).Encode(health)
		case "/v1/status":
			json.NewEncoder(w).Encode(protocol.Status{State: protocol.StateIdle})
		case "/v1/kit/lease":
			json.NewEncoder(w).Encode(targetclient.KitOwnership{State: "free"})
		default:
			t.Errorf("unexpected %s", r.URL.Path)
			http.Error(w, "unexpected", 500)
		}
	}))
	defer peer.Close()
	base, _ := url.Parse(peer.URL)
	s := newService(Config{Targets: []TargetConfig{{Name: "kit", Enabled: true, Address: peer.URL, Agent: "secret"}}, SelectedTarget: "kit", RequestTimeout: time.Second}, Paths{}, &fakeServiceCatalog{}, &fakeServiceScanner{}, &fakeServicePreparer{}, targetclient.NewClient(base, "secret", peer.Client()))
	got, err := s.Health(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Artifacts, artifacts) {
		t.Fatal("provenance changed")
	}
	if c := s.TargetConnection(); c.State != "ready" {
		t.Fatalf("connection=%+v", c)
	}
	if err := s.incompatibleTargetError(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Stop(context.Background()); err != nil {
		t.Fatalf("different build revisions blocked mutation: %v", err)
	}
}

func TestAddressOnlyStopNeedsProtocolHealthNotStatus(t *testing.T) {
	for _, version := range []string{"v1", "", "v2"} {
		t.Run("api="+version, func(t *testing.T) {
			var stops, statuses atomic.Int32
			peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v1/health":
					json.NewEncoder(w).Encode(protocol.Health{APIVersion: version, Ready: true})
				case "/v1/status", "/v1/kit/lease":
					statuses.Add(1)
					http.Error(w, "status unavailable", http.StatusServiceUnavailable)
				case "/v1/stop":
					stops.Add(1)
					json.NewEncoder(w).Encode(protocol.Status{State: protocol.StateIdle})
				default:
					t.Errorf("unexpected %s", r.URL.Path)
					http.Error(w, "unexpected", 500)
				}
			}))
			defer peer.Close()
			base, _ := url.Parse(peer.URL)
			s := newService(Config{Targets: []TargetConfig{{Name: "kit", Enabled: true, Address: peer.URL, Agent: "secret"}}, SelectedTarget: "kit", RequestTimeout: time.Second},
				Paths{}, &fakeServiceCatalog{}, &fakeServiceScanner{}, &fakeServicePreparer{}, targetclient.NewClient(base, "secret", peer.Client()))
			_, err := s.Stop(context.Background())
			if (err == nil) != (version == "v1") {
				t.Fatalf("Stop: %v", err)
			}
			want := int32(0)
			if version == "v1" {
				want = 1
			}
			if stops.Load() != want || statuses.Load() != 0 {
				t.Fatalf("stops=%d status/lease=%d", stops.Load(), statuses.Load())
			}
		})
	}
}

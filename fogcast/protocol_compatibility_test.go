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

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/targetclient"
)

func TestLaunchOnDisabledSelectedUsesBoundTargetProtocol(t *testing.T) {
	for _, id := range []string{"", "f2bb8d43-3cf5-4407-9a11-dfb7cb0086aa"} {
		for _, version := range []string{"", "v2", "v1"} {
			t.Run(id+"/"+version, func(t *testing.T) {
				var mutations, healths atomic.Int32
				game := serviceGame(catalog.Content{})
				peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method != http.MethodGet {
						mutations.Add(1)
					}
					switch r.URL.Path {
					case "/v1/health":
						healths.Add(1)
						json.NewEncoder(w).Encode(protocol.Health{APIVersion: version, TargetID: id, Ready: true})
					case "/v1/status":
						json.NewEncoder(w).Encode(protocol.Status{State: protocol.StateIdle})
					case "/v1/kit/lease":
						json.NewEncoder(w).Encode(targetclient.KitOwnership{State: "free"})
					case "/v1/launch":
						core := "SNES"
						json.NewEncoder(w).Encode(protocol.Status{State: protocol.StateActive, GameID: &game.ID, System: &game.System, ExpectedCore: &core, ObservedCore: &core})
					default:
						http.Error(w, "unexpected path", 500)
					}
				}))
				defer peer.Close()
				base, _ := url.Parse(peer.URL)
				s := newService(Config{
					Libraries:      []catalog.Root{{ID: game.LibraryID, System: game.System, Path: t.TempDir()}},
					Targets:        []TargetConfig{{Name: "disabled", Enabled: false}, {Name: "bound", Enabled: true, Address: peer.URL, Agent: "secret", TargetID: id}},
					SelectedTarget: "disabled", RequestTimeout: time.Second, UploadTimeout: time.Second,
					FPGAROMPaths: map[string]string{game.ID: "/media/fat/games/SNES/test.sfc"},
				}, Paths{}, &fakeServiceCatalog{games: []catalog.Game{game}}, &fakeServiceScanner{}, &fakeServicePreparer{}, nil,
					withTargetClientFactory(func(TargetConfig) (serviceClient, error) {
						return targetclient.NewClient(base, "secret", peer.Client()), nil
					}))
				_, err := s.LaunchOn(context.Background(), game.ID, "bound", nil)
				if version == "v1" {
					if err != nil || mutations.Load() != 1 || healths.Load() == 0 {
						t.Fatalf("compatible launch: err=%v mutations=%d health=%d", err, mutations.Load(), healths.Load())
					}
				} else if err == nil || mutations.Load() != 0 || healths.Load() == 0 {
					t.Fatalf("invalid peer bypassed admission: err=%v mutations=%d health=%d", err, mutations.Load(), healths.Load())
				}
				if s.selectedTarget != "disabled" {
					t.Fatal("selected target rewritten")
				}
			})
		}
	}
}

func TestBoundTargetDeterminesDiscoveryMode(t *testing.T) {
	const id = "f2bb8d43-3cf5-4407-9a11-dfb7cb0086aa"
	for _, boundIdentity := range []bool{false, true} {
		t.Run(map[bool]string{false: "identity-selected-address-bound", true: "address-selected-identity-bound-wrong-ID"}[boundIdentity], func(t *testing.T) {
			var mutations, statusCalls atomic.Int32
			peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					mutations.Add(1)
				}
				switch r.URL.Path {
				case "/v1/health":
					json.NewEncoder(w).Encode(protocol.Health{APIVersion: "v1", Ready: true, TargetID: "wrong-id"})
				case "/v1/stop":
					json.NewEncoder(w).Encode(protocol.Status{State: protocol.StateIdle})
				default:
					statusCalls.Add(1)
					http.Error(w, "status/lease unavailable", 503)
				}
			}))
			defer peer.Close()
			selectedID, boundID := id, ""
			if boundIdentity {
				selectedID, boundID = "", id
			}
			base, _ := url.Parse(peer.URL)
			client := targetclient.NewClient(base, "secret", peer.Client())
			s := newService(Config{Targets: []TargetConfig{
				{Name: "selected", Enabled: true, Address: peer.URL, Agent: "secret", TargetID: selectedID},
				{Name: "bound", Enabled: true, Address: peer.URL, Agent: "secret", TargetID: boundID},
			}, SelectedTarget: "selected", RequestTimeout: time.Second}, Paths{}, &fakeServiceCatalog{}, &fakeServiceScanner{}, &fakeServicePreparer{}, client)
			s.targetClients["bound"] = client
			s.activeTarget, s.activeExecution = "bound", ExecutionFPGANative
			s.resolveTarget = func(context.Context, string) ([]string, error) { return []string{peer.URL}, nil }
			_, err := s.Stop(context.Background())
			if boundIdentity {
				if err == nil || mutations.Load() != 0 {
					t.Fatalf("wrong bound identity admitted: err=%v mutations=%d", err, mutations.Load())
				}
			} else if err != nil || mutations.Load() != 1 {
				t.Fatalf("address-only Stop acquired discovery dependency: err=%v mutations=%d", err, mutations.Load())
			}
			if statusCalls.Load() != 0 {
				t.Fatalf("unexpected status/lease calls=%d", statusCalls.Load())
			}
		})
	}
}

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

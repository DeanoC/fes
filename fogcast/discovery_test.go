package fogcast

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/DeanoC/FogCast/catalog"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/targetclient"
)

func TestReconnectValidatesIdentityAndNeverMutates(t *testing.T) {
	const id = "f2bb8d43-3cf5-4407-9a11-dfb7cb0086aa"
	for _, tc := range []struct {
		name, id  string
		duplicate bool
		want      bool
	}{{"matching", id, false, true}, {"wrong", "b54f2c65-cf14-4643-a042-d09825463d44", false, false}, {"duplicate", id, true, false}, {"authentication", id, false, false}} {
		t.Run(tc.name, func(t *testing.T) {
			mutations := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" {
					mutations++
				}
				if r.Header.Get("Authorization") != "Bearer secret" || tc.name == "authentication" {
					w.WriteHeader(401)
					return
				}
				switch r.URL.Path {
				case "/v1/health":
					json.NewEncoder(w).Encode(protocol.Health{APIVersion: "v1", TargetID: tc.id, BootID: "new", Ready: true})
				case "/v1/kit/lease":
					json.NewEncoder(w).Encode(targetclient.KitOwnership{State: "free"})
				case "/v1/status":
					json.NewEncoder(w).Encode(protocol.Status{State: protocol.StateIdle})
				}
			}))
			defer srv.Close()
			base, _ := url.Parse("http://127.0.0.1:1")
			client := targetclient.NewClient(base, "secret", srv.Client()).WithKitLease(targetclient.NewKitLease(base, "secret", srv.Client(), "host", "test"))
			s := newService(Config{Targets: []TargetConfig{{Name: "kit", Enabled: true, Address: base.String(), Agent: "secret", TargetID: id}}, SelectedTarget: "kit", RequestTimeout: time.Second}, Paths{}, &fakeServiceCatalog{}, &fakeServiceScanner{}, &fakeServicePreparer{}, client)
			s.resolveTarget = func(context.Context, string) ([]string, error) {
				if tc.duplicate {
					return []string{srv.URL, "http://127.0.0.1:2"}, nil
				}
				return []string{srv.URL}, nil
			}
			_, err := s.Health(context.Background())
			if (err == nil) != tc.want {
				t.Fatalf("health err %v want success %v", err, tc.want)
			}
			if tc.want && s.TargetConnection().State != "ready" {
				t.Fatalf("%+v", s.TargetConnection())
			}
			if mutations != 0 {
				t.Fatalf("mutations %d", mutations)
			}
		})
	}
}

func TestReconnectBootChangeClearsLocalSessionBeforeNewLaunch(t *testing.T) {
	const id = "f2bb8d43-3cf5-4407-9a11-dfb7cb0086aa"
	var mu sync.Mutex
	boot := "old"
	state := protocol.StateIdle
	generation := "one"
	held := false
	var claims, launches, stops int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/v1/health":
			json.NewEncoder(w).Encode(protocol.Health{APIVersion: "v1", TargetID: id, BootID: boot, Ready: true})
		case "/v1/kit/lease":
			leaseState := "free"
			if held {
				leaseState = "held"
			}
			json.NewEncoder(w).Encode(targetclient.KitOwnership{State: leaseState, Generation: generation, Owner: "test"})
		case "/v1/kit/claim":
			claims++
			held = true
			json.NewEncoder(w).Encode(map[string]any{"status": map[string]any{"state": "held", "generation": generation, "expires_in_ms": 60000}, "token": generation})
		case "/v1/launch":
			launches++
			state = protocol.StateActive
			json.NewEncoder(w).Encode(protocol.Status{State: state, GameID: stringPtr("pong"), System: systemPtr(protocol.SystemPong), ExpectedCore: stringPtr("Pong"), ObservedCore: stringPtr("Pong")})
		case "/v1/status":
			json.NewEncoder(w).Encode(protocol.Status{State: state})
		default:
			stops++
			t.Errorf("unexpected cleanup %s", r.URL.Path)
		}
	}))
	defer server.Close()
	base, _ := url.Parse(server.URL)
	lease := targetclient.NewKitLease(base, "secret", server.Client(), "test", "test")
	client := targetclient.NewClient(base, "secret", server.Client()).WithKitLease(lease)
	game := catalog.Game{ID: "pong", System: "pong", LibraryID: "builtin-pong", Kind: "builtin", State: catalog.SourceStateAvailable, RootOnline: true}
	s := newService(Config{Targets: []TargetConfig{{Name: "kit", TargetID: id, Address: server.URL, Agent: "secret", Enabled: true}}, SelectedTarget: "kit", RequestTimeout: time.Second}, Paths{}, &fakeServiceCatalog{games: []catalog.Game{game}}, &fakeServiceScanner{}, &fakeServicePreparer{}, client)
	defer func() {
		mu.Lock()
		held = false
		mu.Unlock()
		_, _ = client.AdoptEndpoint(context.Background(), base, true)
	}()
	resets := 0
	s.SetTargetReset(func() { resets++ })
	if _, err := s.Launch(context.Background(), "pong", nil); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	boot = "new"
	generation = "two"
	held = false
	state = protocol.StateIdle
	mu.Unlock()
	if _, err := s.Health(context.Background()); err != nil {
		t.Fatal(err)
	}
	if resets != 1 || s.activeExecution != "" || client.HasKitGrant() {
		t.Fatalf("stale session reset=%d execution=%s grant=%v", resets, s.activeExecution, client.HasKitGrant())
	}
	var concurrent sync.WaitGroup
	for i := 0; i < 6; i++ {
		concurrent.Add(1)
		go func() {
			defer concurrent.Done()
			if _, err := s.Status(context.Background()); err != nil {
				t.Error(err)
			}
		}()
	}
	concurrent.Add(1)
	go func() {
		defer concurrent.Done()
		if _, err := s.Launch(context.Background(), "pong", nil); err != nil {
			t.Error(err)
		}
	}()
	concurrent.Wait()
	mu.Lock()
	defer mu.Unlock()
	if claims != 2 || launches != 2 || stops != 0 {
		t.Fatalf("claim=%d launch=%d cleanup=%d", claims, launches, stops)
	}
	// Discard final test grant locally to stop renewal without mutating the peer.
}

func TestReconnectCancellationBackoffAndDuplicateConcurrentLookup(t *testing.T) {
	base, _ := url.Parse("http://127.0.0.1:1")
	s := newService(Config{Targets: []TargetConfig{{Name: "kit", Enabled: true, TargetID: "f2bb8d43-3cf5-4407-9a11-dfb7cb0086aa", Address: base.String(), Agent: "secret"}}, SelectedTarget: "kit", RequestTimeout: time.Second}, Paths{}, &fakeServiceCatalog{}, &fakeServiceScanner{}, &fakeServicePreparer{}, targetclient.NewClient(base, "secret", nil))
	var calls atomic.Int32
	entered := make(chan struct{})
	s.resolveTarget = func(ctx context.Context, _ string) ([]string, error) {
		calls.Add(1)
		close(entered)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	finished := make(chan struct{})
	go func() { defer close(finished); _, _ = s.Health(context.Background()) }()
	<-entered
	s.cancelTargetLookup()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("lookup did not cancel")
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = s.Health(context.Background()) }()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("backoff/concurrent lookup count %d", calls.Load())
	}
	s.connectionMu.Lock()
	if delay := time.Until(s.nextLookup); delay <= 0 || delay > time.Second {
		t.Fatalf("first backoff %v", delay)
	}
	s.connectionMu.Unlock()
	for i := 1; i < 7; i++ {
		s.connectionFailed(s.TargetConnection(), errors.New("unavailable"))
	}
	s.connectionMu.Lock()
	delay := time.Until(s.nextLookup)
	s.connectionMu.Unlock()
	if delay < 14*time.Second || delay > 15*time.Second {
		t.Fatalf("capped backoff %v", delay)
	}
}

func TestDevelopmentRecoveryResolvesChangedAddressWithoutReplayingMutation(t *testing.T) {
	const id = "f2bb8d43-3cf5-4407-9a11-dfb7cb0086aa"
	var mutations atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			mutations.Add(1)
		}
		switch r.URL.Path {
		case "/v1/health":
			json.NewEncoder(w).Encode(protocol.Health{APIVersion: "v1", TargetID: id, BootID: "new", Ready: true})
		case "/v1/kit/lease":
			json.NewEncoder(w).Encode(targetclient.KitOwnership{State: "free"})
		case "/v1/status":
			json.NewEncoder(w).Encode(protocol.Status{State: protocol.StateIdle})
		}
	}))
	defer server.Close()
	old, _ := url.Parse("http://127.0.0.1:1")
	client := targetclient.NewClient(old, "secret", server.Client()).WithKitLease(targetclient.NewKitLease(old, "secret", server.Client(), "test", "test"))
	s := newService(Config{Targets: []TargetConfig{{Name: "kit", TargetID: id, Address: old.String(), Agent: "secret", Enabled: true}}, SelectedTarget: "kit", RequestTimeout: time.Second}, Paths{}, &fakeServiceCatalog{}, &fakeServiceScanner{}, &fakeServicePreparer{}, client)
	s.resolveTarget = func(context.Context, string) ([]string, error) { return []string{server.URL}, nil }
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	status, err := waitForDevelopmentRecovery(ctx, client, "old", s.developmentRecoveryHealth(client, "old"))
	if err != nil || status.State != protocol.StateIdle || client.EndpointURL().String() != server.URL {
		t.Fatalf("status=%+v err=%v endpoint=%s", status, err, client.EndpointURL())
	}
	if mutations.Load() != 0 {
		t.Fatal("replayed mutation")
	}
}

func TestDisableCancelsLookupAndLeavesUnownedOfflineTargetDisabled(t *testing.T) {
	base, _ := url.Parse("http://127.0.0.1:1")
	target := TargetConfig{Name: "kit", Enabled: true, Address: base.String(), Agent: "secret", TargetID: "f2bb8d43-3cf5-4407-9a11-dfb7cb0086aa"}
	s := newService(Config{Targets: []TargetConfig{target}, SelectedTarget: "kit", RequestTimeout: time.Second}, Paths{}, &fakeServiceCatalog{}, &fakeServiceScanner{}, &fakeServicePreparer{}, targetclient.NewClient(base, "secret", nil))
	entered := make(chan struct{})
	done := make(chan struct{})
	s.resolveTarget = func(ctx context.Context, _ string) ([]string, error) {
		close(entered)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	go func() { defer close(done); _, _ = s.Health(context.Background()) }()
	<-entered
	target.Enabled = false
	targets := []TargetConfig{target}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := s.PatchLibrarySettings(ctx, LibraryConfigPatch{Targets: &targets}); err != nil {
		t.Fatal(err)
	}
	<-done
	if s.LibrarySettings().Targets[0].Enabled {
		t.Fatal("disable did not publish")
	}
}

func TestSameBootAddressChangeInvalidatesLocalInputHandle(t *testing.T) {
	const id = "f2bb8d43-3cf5-4407-9a11-dfb7cb0086aa"
	var mutations atomic.Int32
	peer := func() *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "GET" {
				mutations.Add(1)
			}
			switch r.URL.Path {
			case "/v1/health":
				json.NewEncoder(w).Encode(protocol.Health{APIVersion: "v1", TargetID: id, BootID: "same", Ready: true})
			case "/v1/kit/lease":
				json.NewEncoder(w).Encode(targetclient.KitOwnership{State: "free"})
			case "/v1/status":
				json.NewEncoder(w).Encode(protocol.Status{State: protocol.StateIdle})
			}
		}))
	}
	old := peer()
	next := peer()
	defer next.Close()
	base, _ := url.Parse(old.URL)
	client := targetclient.NewClient(base, "secret", old.Client()).WithKitLease(targetclient.NewKitLease(base, "secret", old.Client(), "host", "test"))
	s := newService(Config{Targets: []TargetConfig{{Name: "kit", TargetID: id, Enabled: true, Address: old.URL, Agent: "secret"}}, SelectedTarget: "kit", RequestTimeout: time.Second}, Paths{}, &fakeServiceCatalog{}, &fakeServiceScanner{}, &fakeServicePreparer{}, client)
	if _, err := s.Health(context.Background()); err != nil {
		t.Fatal(err)
	}
	conn, remote := net.Pipe()
	defer remote.Close()
	defer conn.Close()
	s.SetTargetReset(func() { conn.Close() })
	old.Close()
	s.resolveTarget = func(context.Context, string) ([]string, error) { return []string{next.URL}, nil }
	if _, err := s.Status(context.Background()); err != nil {
		t.Fatal(err)
	}
	remote.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := remote.Read(make([]byte, 1)); err != io.EOF {
		t.Fatalf("old input transport remains open: %v", err)
	}
	if client.EndpointURL().String() != next.URL || mutations.Load() != 0 {
		t.Fatal("wrong endpoint or unexpected mutation")
	}
}

func TestProtocolMismatchIsVisibleAndDoesNotLaunch(t *testing.T) {
	const id = "f2bb8d43-3cf5-4407-9a11-dfb7cb0086aa"
	var launches int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/health":
			json.NewEncoder(w).Encode(protocol.Health{
				APIVersion: "v2", Ready: true, TargetID: id, BootID: "boot",
				Artifacts: &protocol.Artifacts{RuntimeCommit: strings.Repeat("b", 40)},
			})
		case "/v1/kit/lease":
			json.NewEncoder(w).Encode(targetclient.KitOwnership{State: "free"})
		case "/v1/status":
			json.NewEncoder(w).Encode(protocol.Status{State: protocol.StateIdle})
		case "/v1/launch":
			launches++
			json.NewEncoder(w).Encode(protocol.Status{State: protocol.StateActive})
		default:
			t.Fatalf("unexpected %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	base, _ := url.Parse(srv.URL)
	s := newService(Config{Targets: []TargetConfig{{Name: "kit", Enabled: true, Address: srv.URL, Agent: "secret", TargetID: id}}, SelectedTarget: "kit", RequestTimeout: time.Second}, Paths{}, &fakeServiceCatalog{games: []catalog.Game{{ID: "megadrive-sonic-the-hedgehog-2-world-rev-a-a6e9fedc03e1", System: protocol.SystemMegaDrive}}}, &fakeServiceScanner{}, &fakeServicePreparer{}, targetclient.NewClient(base, "secret", srv.Client()))
	if _, err := s.Health(context.Background()); err == nil {
		t.Fatal("unsupported protocol admitted")
	}
	if got := s.TargetConnection(); got.State != "version_mismatch" || got.Message == "" {
		t.Fatalf("%+v", got)
	}
	if _, err := s.Launch(context.Background(), "megadrive-sonic-the-hedgehog-2-world-rev-a-a6e9fedc03e1", nil); err == nil {
		t.Fatal("launch succeeded")
	} else if apiErr, ok := err.(*protocol.APIError); !ok || apiErr.Code != protocol.CodeVersionMismatch {
		t.Fatalf("err=%v", err)
	}
	if launches != 0 {
		t.Fatalf("target launch calls %d", launches)
	}
}

func TestLegacyPeerReportsReadinessAndActivityWithoutLeaseExtension(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status protocol.Status
		want   string
	}{{"not ready", protocol.Status{State: protocol.StateIdle}, "connecting"}, {"active", protocol.Status{State: protocol.StateActive}, "active"}, {"recovery", protocol.Status{State: protocol.StateStopping, Recovery: protocol.RecoveryRebootRequired}, "recovery-required"}} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v1/health":
					json.NewEncoder(w).Encode(protocol.Health{APIVersion: "v1", Ready: false})
				case "/v1/status":
					json.NewEncoder(w).Encode(tc.status)
				case "/v1/kit/lease":
					http.NotFound(w, r)
				default:
					t.Fatalf("unexpected operation %s", r.URL.Path)
				}
			}))
			defer srv.Close()
			base, _ := url.Parse(srv.URL)
			s := newService(Config{Targets: []TargetConfig{{Name: "kit", Enabled: true, Address: srv.URL, Agent: "secret"}}, SelectedTarget: "kit", RequestTimeout: time.Second}, Paths{}, &fakeServiceCatalog{}, &fakeServiceScanner{}, &fakeServicePreparer{}, targetclient.NewClient(base, "secret", srv.Client()))
			if _, err := s.Health(context.Background()); err != nil {
				t.Fatal(err)
			}
			if got := s.TargetConnection().State; got != tc.want {
				t.Fatalf("state=%s want=%s", got, tc.want)
			}
		})
	}
}

func TestTargetInvalidationClearsPackageAssociationAndRejection(t *testing.T) {
	for _, execution := range []string{ExecutionFPGADevelopment, ExecutionHostOnly} {
		t.Run(execution, func(t *testing.T) {
			base, _ := url.Parse("http://127.0.0.1:8182")
			client := targetclient.NewClient(base, "test", nil)
			s := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, client)
			s.activeExecution = execution
			s.activeGameID = "prior"
			s.activePackageID = "package"
			s.activePackageGeneration = 7
			s.packageRejection = &protocol.APIError{Code: protocol.CodeUnrecognizedCore, Phase: "identity"}
			s.invalidateTargetSession(client)
			if s.activePackageID != "" || s.activePackageGeneration != 0 || s.packageRejection != nil {
				t.Fatal("obsolete target package state survived invalidation")
			}
			if execution == ExecutionHostOnly && s.activeGameID != "prior" {
				t.Fatal("target invalidation discarded independent host owner")
			}
		})
	}
}

package fogcast

import (
	"context"
	"errors"
	"testing"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/internal/discovery"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/targetclient"
)

func TestObserveMeshKeepsPhase0BindAndSilenceDoesNotRelease(t *testing.T) {
	id := "01234567-89ab-cdef-0123-456789abcdef"
	other := "fedcba98-7654-3210-fedc-ba9876543210"
	s := newService(Config{}, Paths{}, &fakeServiceCatalog{}, &fakeServiceScanner{}, &fakeServicePreparer{}, &fakeServiceClient{})
	lease := &targetclient.KitLease{}
	s.stoppedKitLeases = []*targetclient.KitLease{lease}
	s.connection = TargetConnection{State: "ready", Address: "http://192.0.2.10:8182", TargetID: id}
	ttl := 30
	var calls int
	s.collectNodes = func(context.Context) ([]discovery.ObservedNode, error) {
		calls++
		if calls == 1 {
			return []discovery.ObservedNode{
				{
					NodeID: id, TargetID: id, Mesh: discovery.MeshProtocol,
					Cap:          "display_sink,execute:fpga_native,input_source",
					Capabilities: discovery.KitCapabilities(),
					Address:      "http://198.51.100.20:8182",
					TTLSeconds:   &ttl,
				},
				{NodeID: other, TargetID: other, Address: "http://192.0.2.11:8182"},
			}, nil
		}
		if calls == 2 {
			return nil, errors.New("browse failed")
		}
		return nil, nil
	}

	nodes, err := s.ObserveMesh(context.Background())
	if err != nil || len(nodes) != 2 {
		t.Fatalf("collect = %#v err=%v", nodes, err)
	}
	if nodes[0].NodeID != id || nodes[0].Mesh != discovery.MeshProtocol || nodes[0].Cap == "" || nodes[0].SilenceReleasesLease() {
		t.Fatalf("bound row %#v", nodes[0])
	}
	if nodes[0].TTLSeconds == nil || *nodes[0].TTLSeconds != 30 || nodes[1].TTLSeconds != nil {
		t.Fatalf("ttl %#v %#v", nodes[0].TTLSeconds, nodes[1].TTLSeconds)
	}
	if !nodes[0].Capabilities.DisplaySink || len(nodes[0].Capabilities.Execute) != 1 || nodes[0].Capabilities.Execute[0].Kind != discovery.ExecuteFPGANative {
		t.Fatalf("caps %#v", nodes[0].Capabilities)
	}
	if got := s.TargetConnection(); got.State != "ready" || got.Address != "http://192.0.2.10:8182" || got.TargetID != id {
		t.Fatalf("inventory adopted an endpoint %+v", got)
	}

	kept, err := s.ObserveMesh(context.Background())
	if err == nil || len(kept) != 2 {
		t.Fatalf("browse failure replaced inventory %#v err=%v", kept, err)
	}
	silent, err := s.ObserveMesh(context.Background())
	if err != nil || len(silent) != 0 {
		t.Fatalf("silence = %#v err=%v", silent, err)
	}
	if len(s.stoppedKitLeases) != 1 || s.stoppedKitLeases[0] != lease {
		t.Fatal("advertisement silence released the kit lease")
	}
	if got := s.TargetConnection(); got.State != "ready" || got.Address != "http://192.0.2.10:8182" {
		t.Fatalf("silence changed the Phase 0 bind %+v", got)
	}
}

func TestForeignLeaseBlocksFPGALaunchOnTheBusyKit(t *testing.T) {
	game := serviceGame(catalog.Content{})
	client := &fakeServiceClient{}
	store := &fakeServiceCatalog{games: []catalog.Game{game}}
	s := newTestService(store, &fakeServicePreparer{}, client)
	lease := &targetclient.KitLease{}
	s.stoppedKitLeases = []*targetclient.KitLease{lease}
	s.connection = TargetConnection{State: "busy", Owner: "other-shell"}

	for _, target := range []string{"", "dev"} {
		_, err := s.LaunchOn(context.Background(), game.ID, target, nil)
		var api *protocol.APIError
		if !errors.As(err, &api) || api.Code != protocol.CodeKitLeaseDenied {
			t.Fatalf("target %q = %v", target, err)
		}
	}
	if client.launchCalls != 0 || client.nativeLaunchCalls != 0 || client.coreCalls != 0 || client.healthCalls != 0 || client.statusCalls != 0 || client.stopCalls != 0 {
		t.Fatalf("foreign kit launch contacted the target: %+v", client)
	}
	if len(s.stoppedKitLeases) != 1 || s.stoppedKitLeases[0] != lease {
		t.Fatal("foreign denial released the retained grant")
	}

	s.connection = TargetConnection{State: "ready", Address: "http://192.0.2.10:8182"}
	_, err := s.Launch(context.Background(), game.ID, nil)
	var api *protocol.APIError
	if errors.As(err, &api) && api.Code == protocol.CodeKitLeaseDenied {
		t.Fatal("owned shell was treated as a foreign lease")
	}
	if store.gameCalls == 0 {
		t.Fatal("owned launch did not enter catalog admission")
	}
}

func TestForeignLeaseAllowsHostOnlyLaunch(t *testing.T) {
	identity := protocol.ContentIdentity{SHA256: serviceDigest, Size: 3, Extension: "sfc"}
	game := serviceGame(catalog.Content{})
	game.Content = nil
	prepared := preparedServiceFixture(t, []byte("rom"), identity)
	client := &fakeServiceClient{}
	adapter := &fakeHostExecutor{}
	s := newTestServiceWithExecution(&fakeServiceCatalog{games: []catalog.Game{game}}, &fakeServicePreparer{prepared: prepared}, client, ExecutionPolicy{
		Resolver: ExecutionResolverFunc(func(context.Context, catalog.Game) (string, error) { return ExecutionHostOnly, nil }),
		Host:     adapter,
	})
	s.connection = TargetConnection{State: "busy", Owner: "other-shell"}

	response, err := s.Launch(context.Background(), game.ID, nil)
	if err != nil {
		t.Fatalf("host launch = %v", err)
	}
	if response.Status.State != protocol.StateActive || adapter.launchCalls != 1 || adapter.contentPath != "rom" {
		t.Fatalf("response=%+v calls=%d", response.Status, adapter.launchCalls)
	}
	if client.launchCalls != 0 || client.coreCalls != 0 || client.healthCalls != 0 || client.statusCalls != 0 {
		t.Fatalf("host launch used the busy kit: %+v", client)
	}
	if s.activeExecution != ExecutionHostOnly || s.activeTarget != "" {
		t.Fatalf("execution=%q target=%q", s.activeExecution, s.activeTarget)
	}
}

func TestForeignLeaseAllowsDifferentNamedTarget(t *testing.T) {
	s, dev, spare, entry := namedPackageFixture(t)
	s.connection = TargetConnection{State: "busy", Owner: "other-shell", Address: "http://192.0.2.10:8182", TargetID: ""}

	_, err := s.Launch(context.Background(), entry.GameID, nil)
	var api *protocol.APIError
	if !errors.As(err, &api) || api.Code != protocol.CodeKitLeaseDenied {
		t.Fatalf("default kit launch = %v", err)
	}
	if dev.coreCalls != 0 || spare.coreCalls != 0 {
		t.Fatalf("default kit was contacted dev=%d spare=%d", dev.coreCalls, spare.coreCalls)
	}

	if _, err = s.LaunchOn(context.Background(), entry.GameID, "spare", nil); err != nil {
		t.Fatalf("spare launch = %v", err)
	}
	if spare.coreCalls != 1 || dev.coreCalls != 0 {
		t.Fatalf("loads dev=%d spare=%d", dev.coreCalls, spare.coreCalls)
	}
	if s.selectedTarget != "dev" {
		t.Fatalf("selected target changed to %s", s.selectedTarget)
	}
}

func TestForeignLeaseSpareLaunchAfterStickyHostOnlyUsesSpare(t *testing.T) {
	s, dev, spare, entry := namedPackageFixture(t)
	host := &fakeHostExecutor{}
	s.hostExecutor = host
	s.connection = TargetConnection{State: "busy", Owner: "other-shell", Address: "http://192.0.2.10:8182"}
	s.executionMu.Lock()
	s.activeExecution = ExecutionHostOnly
	s.activeTarget = "host"
	s.activeGameID = "prior-host"
	s.executionMu.Unlock()

	_, err := s.LaunchOn(context.Background(), entry.GameID, "", nil)
	var api *protocol.APIError
	if !errors.As(err, &api) || api.Code != protocol.CodeKitLeaseDenied {
		t.Fatalf("default kit launch = %v", err)
	}
	if _, err = s.LaunchOn(context.Background(), entry.GameID, "dev", nil); !errors.As(err, &api) || api.Code != protocol.CodeKitLeaseDenied {
		t.Fatalf("named busy kit launch = %v", err)
	}
	if dev.coreCalls != 0 || spare.coreCalls != 0 || dev.statusCalls != 0 || dev.healthCalls != 0 || dev.stopCalls != 0 {
		t.Fatalf("denied launch contacted a kit dev core=%d status=%d health=%d stop=%d spare=%d", dev.coreCalls, dev.statusCalls, dev.healthCalls, dev.stopCalls, spare.coreCalls)
	}

	if _, err = s.LaunchOn(context.Background(), entry.GameID, "spare", nil); err != nil {
		t.Fatalf("spare launch = %v", err)
	}
	if spare.coreCalls != 1 || dev.coreCalls != 0 || dev.statusCalls != 0 || dev.healthCalls != 0 || dev.stopCalls != 0 {
		t.Fatalf("loads dev core=%d status=%d health=%d stop=%d spare=%d", dev.coreCalls, dev.statusCalls, dev.healthCalls, dev.stopCalls, spare.coreCalls)
	}
	if host.stopCalls != 1 {
		t.Fatalf("host stop calls = %d", host.stopCalls)
	}
	if s.selectedTarget != "dev" {
		t.Fatalf("selected target changed to %s", s.selectedTarget)
	}
	s.executionMu.Lock()
	gotTarget, gotExec := s.activeTarget, s.activeExecution
	s.executionMu.Unlock()
	if gotTarget != "spare" || (gotExec != ExecutionFPGANative && gotExec != ExecutionFPGADevelopment) {
		t.Fatalf("session target=%q execution=%q", gotTarget, gotExec)
	}
}

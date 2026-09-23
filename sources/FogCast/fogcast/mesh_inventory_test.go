package fogcast

import (
	"context"
	"errors"
	"testing"

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

func TestForeignLeasePlayDoesNotClaimAndOwnedLaunchStillAdmits(t *testing.T) {
	s := newService(Config{}, Paths{}, &fakeServiceCatalog{}, &fakeServiceScanner{}, &fakeServicePreparer{}, &fakeServiceClient{})
	s.connection = TargetConnection{State: "busy", Owner: "other-shell"}
	_, err := s.Launch(context.Background(), "snes-mario", nil)
	var api *protocol.APIError
	if !errors.As(err, &api) || api.Code != protocol.CodeKitLeaseDenied {
		t.Fatalf("foreign play = %v", err)
	}
	if s.catalog.(*fakeServiceCatalog).gameCalls != 0 {
		t.Fatal("foreign play reached catalog admission")
	}

	s.connection = TargetConnection{State: "ready", Address: "http://192.0.2.10:8182"}
	_, err = s.Launch(context.Background(), "snes-mario", nil)
	if errors.As(err, &api) && api.Code == protocol.CodeKitLeaseDenied {
		t.Fatal("owned shell was treated as a foreign lease")
	}
	if s.catalog.(*fakeServiceCatalog).gameCalls == 0 {
		t.Fatal("owned launch did not enter Phase 0 admission")
	}
}

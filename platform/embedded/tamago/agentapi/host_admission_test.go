// SPDX-License-Identifier: GPL-3.0-or-later

package agentapi_test

import (
	"context"
	"net/netip"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/kitlease"
	"github.com/DeanoC/FogCast/targetclient"
	"github.com/DeanoC/fes/platform/embedded/tamago/agentapi"
	"github.com/DeanoC/fes/platform/embedded/tamago/mdns"
	"github.com/DeanoC/fes/platform/embedded/tamago/network"
)

const (
	admissionTargetID = "b6b6a651-af05-4282-a44e-e0998a1d7b60"
	admissionBootID   = "14f82f71-bbca-4c7e-bb81-174aa04069c4"
	admissionBearer   = "native-admission-test-credential"
)

type admissionLease struct {
	mu     sync.Mutex
	status kitlease.Status
}

func (l *admissionLease) Status() kitlease.Status {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.status
}

func (l *admissionLease) set(status kitlease.Status) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.status = status
}

func startAdmissionTarget(t *testing.T, ctx context.Context, link *network.Link, mac string, ownership *admissionLease) *agentapi.Service {
	t.Helper()
	target, err := agentapi.NewService(agentapi.ServiceConfig{
		Endpoint:      agentapi.NewLinkEndpoint(link, mac),
		Port:          8080,
		TargetID:      admissionTargetID,
		Token:         admissionBearer,
		Controller:    agentapi.NewStateController(agentapi.StateControllerConfig{BootID: admissionBootID}),
		KitLease:      ownership,
		ReadOnly:      true,
		AdvertiseMDNS: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := target.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := target.Stop(stopCtx); err != nil {
			t.Error(err)
		}
	})
	return target
}

// Use the host's actual target and lease clients against the address received
// over Ethernet. The Linux multicast test also exercises the desktop resolver.
func TestFogCastHostClientAdmitsNativeTarget(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	t.Cleanup(cancel)
	targetLink, hostLink, targetMAC, hostMAC, cleanup := setupTestLinkPair(t)
	t.Cleanup(cleanup)
	ownership := &admissionLease{status: kitlease.Status{State: "free"}}
	startAdmissionTarget(t, ctx, targetLink, targetMAC, ownership)

	checkHostAdmission(t, ctx, hostLink, hostMAC, ownership, targetLink.Prefix().Addr())
}

func TestReboundEndpointAdvertisesFreshAddressAndKeepsPairedAuthority(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	t.Cleanup(cancel)
	old, host, station, hostMAC, closePair := setupTestLinkPair(t)
	t.Cleanup(closePair)
	owner := &admissionLease{status: kitlease.Status{State: "free"}}
	service := startAdmissionTarget(t, ctx, old, station, owner)
	checkHostAdmission(t, ctx, host, hostMAC, owner, old.Prefix().Addr())
	replacement, host, station, mac, closeReplacement := setupTestLinkPairAt(t, "192.0.2.88/24")
	t.Cleanup(closeReplacement)
	endpoint := agentapi.NewLinkEndpoint(replacement, station)
	if _, err := service.OnNewEndpoint(endpoint); err == nil {
		t.Fatal("rebound while old endpoint still serving")
	}
	if err := service.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	fresh, err := service.OnNewEndpoint(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	if err = fresh.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer fresh.Stop(ctx)
	checkHostAdmission(t, ctx, host, mac, owner, replacement.Prefix().Addr())
}

func checkHostAdmission(t *testing.T, ctx context.Context, hostLink *network.Link, hostMAC [6]byte, ownership *admissionLease, want netip.Addr) {
	t.Helper()
	ads, err := mdns.Browse(ctx, hostLink, mdns.BrowseConfig{
		LocalMAC: hostMAC, LocalIP: hostLink.Prefix().Addr(), StopOnFirst: true,
	}, mdns.FogCastServiceType)
	if err != nil || len(ads) != 1 || len(ads[0].Addrs) != 1 || ads[0].Addrs[0] != want {
		t.Fatalf("native DNS-SD browse returned %+v: %v", ads, err)
	}
	ad := mdns.ParseFogCastTXT(ads[0].TXT)
	if !ad.DirectBindable() || ad.TargetID != admissionTargetID {
		t.Fatalf("unexpected advertisement: %+v", ad)
	}
	base := &url.URL{Scheme: "http", Host: netip.AddrPortFrom(ads[0].Addrs[0], ads[0].Port).String()}
	wire, err := network.NewHTTPClient(hostLink, network.TransportConfig{DialTimeout: time.Second, ResponseHeaderTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer wire.CloseIdleConnections()
	client := targetclient.NewClient(base, admissionBearer, wire).WithKitLease(targetclient.NewKitLease(base, admissionBearer, wire, "host", "test"))
	health, err := client.Health(ctx)
	if err != nil || !health.Ready || health.APIVersion != "v1" || health.TargetID != ad.TargetID || health.BootID != admissionBootID {
		t.Fatalf("authenticated health = %+v, %v", health, err)
	}
	for _, status := range []kitlease.Status{{State: "free"}, {State: "held", Owner: "local-console", Generation: "generation-2"}} {
		ownership.set(status)
		kit, err := client.AdoptEndpoint(ctx, base, false)
		if err != nil || kit.State != status.State || kit.Owner != status.Owner || kit.Generation != status.Generation || kit.Owned {
			t.Fatalf("observed lease = %+v, %v; want %+v", kit, err, status)
		}
	}
	for _, badBearer := range []string{"", "incorrect-test-credential"} {
		unauthorized := targetclient.NewClient(base, badBearer, wire)
		health, err := unauthorized.Health(ctx)
		if err != nil || health.TargetID != "" || health.BootID != "" {
			t.Fatalf("public health = %+v, %v", health, err)
		}
		if _, err := unauthorized.AdoptEndpoint(ctx, base, false); err == nil {
			t.Fatal("unauthenticated host obtained lease status")
		}
		if _, err := unauthorized.Status(ctx); err == nil {
			t.Fatal("unauthenticated host obtained target status")
		}
	}
}

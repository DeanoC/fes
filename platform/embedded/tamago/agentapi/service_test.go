// SPDX-License-Identifier: GPL-3.0-or-later

package agentapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/netip"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/fes/platform/embedded/tamago/agentapi"
	"github.com/DeanoC/fes/platform/embedded/tamago/mdns"
	"github.com/DeanoC/fes/platform/embedded/tamago/network"
)

func setupTestLinkPair(t *testing.T) (linkSrv, linkCli *network.Link, macSrv string, macCli [6]byte, cleanup func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())

	sToC := make(chan []byte, 256)
	cToS := make(chan []byte, 256)
	macSrv = "02:aa:00:00:01:01"
	macCli = [6]byte{0x02, 0xaa, 0x00, 0x00, 0x01, 0x02}

	var err error
	linkSrv, err = network.NewLink(nil, wireDev{in: cToS, out: sToC}, "192.0.2.10/24", macSrv, "")
	if err != nil {
		t.Fatal(err)
	}
	linkCli, err = network.NewLink(nil, wireDev{in: sToC, out: cToS}, "192.0.2.20/24", "02:aa:00:00:01:02", "")
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _ = linkSrv.Start(ctx) }()
	go func() { defer wg.Done(); _ = linkCli.Start(ctx) }()
	return linkSrv, linkCli, macSrv, macCli, func() {
		cancel()
		wg.Wait()
	}
}

func TestServiceHTTPAndMDNSDiscoveryOverLneto(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	linkSrv, linkCli, macSrv, macCli, cleanup := setupTestLinkPair(t)
	defer cleanup()

	const targetID = "11111111-2222-4333-8444-555555555555"
	ctrl := agentapi.NewStateController(agentapi.StateControllerConfig{
		Board:   "test",
		Address: linkSrv.Prefix().String(),
	})
	svc, err := agentapi.NewService(agentapi.ServiceConfig{
		Endpoint:             agentapi.NewLinkEndpoint(linkSrv, macSrv),
		Port:                 8080,
		TargetID:             targetID,
		AllowUnauthenticated: true,
		AdvertiseMDNS:        true,
		MDNSInstance:         "FES-Test",
		MDNSHost:             "fes-test.local",
		Controller:           ctrl,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	if got := svc.Addr(); got != netip.MustParseAddrPort("192.0.2.10:8080") {
		t.Fatalf("Addr() = %v, want 192.0.2.10:8080", got)
	}
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer stopCancel()
		_ = svc.Stop(stopCtx)
	}()

	browseCtx, cancelBrowse := context.WithTimeout(ctx, 4*time.Second)
	discovered, err := mdns.Browse(browseCtx, linkCli, mdns.BrowseConfig{
		LocalMAC:    macCli,
		LocalIP:     netip.MustParseAddr("192.0.2.20"),
		StopOnFirst: true,
	}, mdns.FogCastServiceType)
	cancelBrowse()
	if err != nil {
		t.Fatalf("mdns.Browse: %v", err)
	}
	if len(discovered) != 1 {
		t.Fatalf("len(discovered) = %d, want 1 (%+v)", len(discovered), discovered)
	}
	gotSvc := discovered[0]
	wantIP := netip.MustParseAddr("192.0.2.10")
	if gotSvc.Instance != "FES-Test" || gotSvc.Port != 8080 || !slices.Contains(gotSvc.Addrs, wantIP) {
		t.Fatalf("unexpected discovered service: %+v", gotSvc)
	}
	info := mdns.ParseFogCastTXT(gotSvc.TXT)
	if !info.DirectBindable() || info.TargetID != targetID {
		t.Fatalf("unexpected FogCast TXT info: %+v (raw=%v)", info, gotSvc.TXT)
	}

	httpClient, err := network.NewHTTPClient(linkCli, network.TransportConfig{
		DialTimeout:           3 * time.Second,
		ResponseHeaderTimeout: 3 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewHTTPClient: %v", err)
	}
	defer httpClient.CloseIdleConnections()

	base := "http://" + netip.AddrPortFrom(gotSvc.Addrs[0], gotSvc.Port).String()
	healthResp, err := httpClient.Get(base + "/v1/health")
	if err != nil {
		t.Fatalf("GET /v1/health: %v", err)
	}
	var health protocol.Health
	err = json.NewDecoder(healthResp.Body).Decode(&health)
	_ = healthResp.Body.Close()
	if err != nil || healthResp.StatusCode != http.StatusOK || !health.Ready || health.TargetID != targetID {
		t.Fatalf("/v1/health = status:%d err:%v body:%+v", healthResp.StatusCode, err, health)
	}

	statusResp, err := httpClient.Get(base + "/v1/status")
	if err != nil {
		t.Fatalf("GET /v1/status: %v", err)
	}
	var st protocol.Status
	err = json.NewDecoder(statusResp.Body).Decode(&st)
	_ = statusResp.Body.Close()
	if err != nil || statusResp.StatusCode != http.StatusOK || st.State != protocol.StateIdle {
		t.Fatalf("/v1/status = status:%d err:%v body:%+v", statusResp.StatusCode, err, st)
	}

	diagResp, err := httpClient.Get(base + "/v1/diagnostics")
	if err != nil {
		t.Fatalf("GET /v1/diagnostics: %v", err)
	}
	var diag agentapi.DiagnosticsSnapshot
	err = json.NewDecoder(diagResp.Body).Decode(&diag)
	_ = diagResp.Body.Close()
	if err != nil || diagResp.StatusCode != http.StatusOK || !diag.HTTP.Listening || !diag.MDNS.Enabled || diag.MDNS.Announcements < 1 {
		t.Fatalf("/v1/diagnostics = status:%d err:%v body:%+v", diagResp.StatusCode, err, diag)
	}

	stopCtx, stopCancel := context.WithTimeout(ctx, 2*time.Second)
	defer stopCancel()
	if err := svc.Stop(stopCtx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := svc.Stop(stopCtx); err != nil {
		t.Fatalf("second Stop: %v", err)
	}
	if got := svc.Stats().MDNS.Goodbyes; got < 1 {
		t.Fatalf("MDNS.Goodbyes = %d, want >= 1", got)
	}
	if err := svc.Start(ctx); !errors.Is(err, agentapi.ErrServiceStopped) {
		t.Fatalf("Start after Stop = %v, want ErrServiceStopped", err)
	}
}

func TestServiceValidationAndAuth(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	linkSrv, linkCli, macSrv, _, cleanup := setupTestLinkPair(t)
	defer cleanup()

	ep := agentapi.NewLinkEndpoint(linkSrv, macSrv)
	ctrl := agentapi.NewStateController(agentapi.StateControllerConfig{Board: "test", Address: "192.0.2.10/24"})

	badConfigs := []agentapi.ServiceConfig{
		{Port: 8080, Controller: ctrl, AllowUnauthenticated: true},
		{Endpoint: ep, Port: 0, Controller: ctrl, AllowUnauthenticated: true},
		{Endpoint: ep, Port: 8080, AllowUnauthenticated: true},
		{Endpoint: agentapi.NewLinkEndpoint(linkSrv, "not-a-mac"), Port: 8080, Controller: ctrl, AllowUnauthenticated: true},
		{Endpoint: ep, Port: 8080, Controller: ctrl},
		{Endpoint: ep, Port: 8080, Controller: ctrl, AllowUnauthenticated: true, AdvertiseMDNS: true, TargetID: "bad-id"},
	}
	for i, cfg := range badConfigs {
		if _, err := agentapi.NewService(cfg); err == nil {
			t.Fatalf("case %d: expected validation error", i)
		}
	}

	svc, err := agentapi.NewService(agentapi.ServiceConfig{
		Endpoint:   ep,
		Port:       8081,
		Token:      "secret-token",
		Controller: ctrl,
	})
	if err != nil {
		t.Fatalf("NewService(auth): %v", err)
	}
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("Start(auth): %v", err)
	}
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer stopCancel()
		_ = svc.Stop(stopCtx)
	}()

	if err := svc.Start(ctx); !errors.Is(err, agentapi.ErrServiceStarted) {
		t.Fatalf("second Start = %v, want ErrServiceStarted", err)
	}

	httpClient, err := network.NewHTTPClient(linkCli, network.TransportConfig{
		DialTimeout:           3 * time.Second,
		ResponseHeaderTimeout: 3 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewHTTPClient: %v", err)
	}
	defer httpClient.CloseIdleConnections()

	unauthResp, err := httpClient.Get("http://192.0.2.10:8081/v1/status")
	if err != nil {
		t.Fatalf("unauthenticated GET /v1/status: %v", err)
	}
	_ = unauthResp.Body.Close()
	if unauthResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want 401", unauthResp.StatusCode)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://192.0.2.10:8081/v1/status", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer secret-token")
	authResp, err := httpClient.Do(req)
	if err != nil {
		t.Fatalf("authenticated GET /v1/status: %v", err)
	}
	_ = authResp.Body.Close()
	if authResp.StatusCode != http.StatusOK {
		t.Fatalf("authenticated status = %d, want 200", authResp.StatusCode)
	}
}

// SPDX-License-Identifier: GPL-3.0-or-later

package agentapi_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/fes/platform/embedded/tamago/agentapi"
	"github.com/DeanoC/fes/platform/embedded/tamago/block"
	"github.com/DeanoC/fes/platform/embedded/tamago/fat"
	"github.com/DeanoC/fes/platform/embedded/tamago/mdns"
	"github.com/DeanoC/fes/platform/embedded/tamago/network"
	"github.com/DeanoC/fes/platform/embedded/tamago/ota"
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
		{Endpoint: ep, Port: 8080, Controller: ctrl, AllowUnauthenticated: true, AdvertiseMDNS: true, TargetID: "11111111-2222-4333-8444-555555555555", TLSConfig: &tls.Config{}},
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

func TestServiceDefaultMDNSNamesDerivedFromTargetID(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	link1, link2, mac1, mac2, cleanup := setupTestLinkPair(t)
	defer cleanup()

	const (
		id1 = "11111111-2222-4333-8444-555555555555"
		id2 = "aaaa1111-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	)

	svc1, err := agentapi.NewService(agentapi.ServiceConfig{
		Endpoint:             agentapi.NewLinkEndpoint(link1, mac1),
		Port:                 8080,
		TargetID:             id1,
		AllowUnauthenticated: true,
		AdvertiseMDNS:        true,
		Controller:           agentapi.NewStateController(agentapi.StateControllerConfig{Board: "test-1", Address: link1.Prefix().String()}),
	})
	if err != nil {
		t.Fatalf("NewService(1): %v", err)
	}
	svc2, err := agentapi.NewService(agentapi.ServiceConfig{
		Endpoint:             agentapi.NewLinkEndpoint(link2, "02:aa:00:00:01:02"),
		Port:                 8080,
		TargetID:             id2,
		AllowUnauthenticated: true,
		AdvertiseMDNS:        true,
		Controller:           agentapi.NewStateController(agentapi.StateControllerConfig{Board: "test-2", Address: link2.Prefix().String()}),
	})
	if err != nil {
		t.Fatalf("NewService(2): %v", err)
	}

	if err := svc1.Start(ctx); err != nil {
		t.Fatalf("Start(1): %v", err)
	}
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer stopCancel()
		_ = svc1.Stop(stopCtx)
	}()
	if err := svc2.Start(ctx); err != nil {
		t.Fatalf("Start(2): %v", err)
	}
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer stopCancel()
		_ = svc2.Stop(stopCtx)
	}()

	browseCtx1, cancelBrowse1 := context.WithTimeout(ctx, 4*time.Second)
	gotFrom1, err := mdns.Browse(browseCtx1, link2, mdns.BrowseConfig{
		LocalMAC:    mac2,
		LocalIP:     netip.MustParseAddr("192.0.2.20"),
		StopOnFirst: true,
	}, mdns.FogCastServiceType)
	cancelBrowse1()
	if err != nil {
		t.Fatalf("mdns.Browse(from link1): %v", err)
	}
	browseCtx2, cancelBrowse2 := context.WithTimeout(ctx, 4*time.Second)
	gotFrom2, err := mdns.Browse(browseCtx2, link1, mdns.BrowseConfig{
		LocalMAC:    [6]byte{0x02, 0xaa, 0x00, 0x00, 0x01, 0x01},
		LocalIP:     netip.MustParseAddr("192.0.2.10"),
		StopOnFirst: true,
	}, mdns.FogCastServiceType)
	cancelBrowse2()
	if err != nil {
		t.Fatalf("mdns.Browse(from link2): %v", err)
	}

	if len(gotFrom1) != 1 || gotFrom1[0].Instance != "FES-TamaGo-111111112222" || gotFrom1[0].Host != "fes-tamago-111111112222.local" || mdns.ParseFogCastTXT(gotFrom1[0].TXT).TargetID != id1 {
		t.Fatalf("unexpected default mDNS service for %s: %+v", id1, gotFrom1)
	}
	if len(gotFrom2) != 1 || gotFrom2[0].Instance != "FES-TamaGo-aaaa1111bbbb" || gotFrom2[0].Host != "fes-tamago-aaaa1111bbbb.local" || mdns.ParseFogCastTXT(gotFrom2[0].TXT).TargetID != id2 {
		t.Fatalf("unexpected default mDNS service for %s: %+v", id2, gotFrom2)
	}
}

type memFATDisk struct {
	mu      sync.Mutex
	sectors uint64
	data    map[uint64][block.SectorSize]byte
}

func newMemFATVolume(t *testing.T) *fat.Volume {
	t.Helper()
	const (
		reserved   = uint64(32)
		fats       = uint64(2)
		clusters   = uint64(65536)
		fatSectors = uint64(516)
		spc        = uint64(1)
	)
	total := reserved + fats*fatSectors + clusters*spc
	d := &memFATDisk{
		sectors: total,
		data:    make(map[uint64][block.SectorSize]byte),
	}
	var boot [block.SectorSize]byte
	binary.LittleEndian.PutUint16(boot[11:], block.SectorSize)
	boot[13] = byte(spc)
	binary.LittleEndian.PutUint16(boot[14:], uint16(reserved))
	boot[16] = byte(fats)
	binary.LittleEndian.PutUint32(boot[32:], uint32(total))
	binary.LittleEndian.PutUint32(boot[36:], uint32(fatSectors))
	binary.LittleEndian.PutUint32(boot[44:], 2)
	binary.LittleEndian.PutUint16(boot[48:], 1)
	boot[510], boot[511] = 0x55, 0xaa
	d.data[0] = boot

	var fsInfo [block.SectorSize]byte
	binary.LittleEndian.PutUint32(fsInfo[0:], 0x41615252)
	binary.LittleEndian.PutUint32(fsInfo[484:], 0x61417272)
	binary.LittleEndian.PutUint32(fsInfo[488:], 0xffffffff)
	binary.LittleEndian.PutUint32(fsInfo[492:], 0xffffffff)
	binary.LittleEndian.PutUint32(fsInfo[508:], 0xaa550000)
	d.data[1] = fsInfo

	var fat0 [block.SectorSize]byte
	binary.LittleEndian.PutUint32(fat0[0:], 0x0ffffff8)
	binary.LittleEndian.PutUint32(fat0[4:], 0x0fffffff)
	binary.LittleEndian.PutUint32(fat0[8:], 0x0fffffff)
	d.data[reserved] = fat0
	d.data[reserved+fatSectors] = fat0

	vol, err := fat.Mount(d)
	if err != nil {
		t.Fatalf("fat.Mount: %v", err)
	}
	return vol
}

func (d *memFATDisk) SectorCount() uint64 { return d.sectors }

func (d *memFATDisk) ReadSectors(lba uint64, dst []byte) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	count := uint64(len(dst) / block.SectorSize)
	for i := uint64(0); i < count; i++ {
		sec := d.data[lba+i]
		copy(dst[i*block.SectorSize:(i+1)*block.SectorSize], sec[:])
	}
	return nil
}

func (d *memFATDisk) WriteSectors(lba uint64, src []byte) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	count := uint64(len(src) / block.SectorSize)
	for i := uint64(0); i < count; i++ {
		var sec [block.SectorSize]byte
		copy(sec[:], src[i*block.SectorSize:(i+1)*block.SectorSize])
		d.data[lba+i] = sec
	}
	return nil
}

func (d *memFATDisk) Flush() error { return nil }

func TestServiceOTAHTTPLifecycleOverLneto(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	linkSrv, linkCli, macSrv, _, cleanup := setupTestLinkPair(t)
	defer cleanup()

	vol := newMemFATVolume(t)
	mgr, err := ota.Open(ctx, vol, ota.Config{Board: "de25-nano", MaxImageBytes: 64 << 10})
	if err != nil {
		t.Fatal(err)
	}
	v1 := bytes.Repeat([]byte("SLOT-A-V1"), 256)
	if _, err := mgr.SeedActiveSlot(ctx, ota.StageRequest{Version: "1.0.0", Target: "de25-nano", Payload: v1}); err != nil {
		t.Fatal(err)
	}

	const secret = "ota-bearer-secret"
	svc, err := agentapi.NewService(agentapi.ServiceConfig{
		Endpoint:       agentapi.NewLinkEndpoint(linkSrv, macSrv),
		Port:           8080,
		TargetID:       "33333333-4444-4555-8666-777777777777",
		Token:          secret,
		Controller:     agentapi.NewStateController(agentapi.StateControllerConfig{Board: "de25-nano", Address: linkSrv.Prefix().String()}),
		OTA:            mgr,
		MaxUploadBytes: 8192,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer stopCancel()
		_ = svc.Stop(stopCtx)
	}()

	tr, err := network.NewHTTPTransport(linkCli, network.TransportConfig{})
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: tr, Timeout: 5 * time.Second}
	baseURL := "http://" + svc.Addr().String()

	doReq := func(method, path string, body []byte, hdrs map[string]string, tok string) (*http.Response, []byte) {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, method, baseURL+path, bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
		for k, v := range hdrs {
			req.Header.Set(k, v)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		defer resp.Body.Close()
		var buf bytes.Buffer
		if _, err := buf.ReadFrom(resp.Body); err != nil {
			t.Fatal(err)
		}
		return resp, buf.Bytes()
	}

	// Unauthenticated OTA routes fail closed with 401.
	if resp, _ := doReq(http.MethodGet, "/v1/ota", nil, nil, ""); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauth GET /v1/ota status=%d, want 401", resp.StatusCode)
	}
	if resp, _ := doReq(http.MethodPost, "/v1/ota/stage", []byte("abc"), map[string]string{"X-OTA-Version": "2.0.0"}, "wrong"); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad-token POST /v1/ota/stage status=%d, want 401", resp.StatusCode)
	}

	// Diagnostics reports OTA state cleanly.
	resp, diagBody := doReq(http.MethodGet, "/v1/diagnostics", nil, nil, secret)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/v1/diagnostics status=%d body=%s", resp.StatusCode, diagBody)
	}
	var diag agentapi.DiagnosticsSnapshot
	if err := json.Unmarshal(diagBody, &diag); err != nil || diag.OTA == nil || diag.OTA.ActiveSlot != ota.SlotA {
		t.Fatalf("unexpected diagnostics OTA snapshot: %+v (%v)", diag, err)
	}

	// Stage validation failures: missing version (400), oversized upload (413), wrong target (400), digest mismatch (400).
	if resp, _ := doReq(http.MethodPost, "/v1/ota/stage", []byte("x"), nil, secret); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing version status=%d, want 400", resp.StatusCode)
	}
	if resp, _ := doReq(http.MethodPost, "/v1/ota/stage", make([]byte, 9000), map[string]string{"X-OTA-Version": "2.0"}, secret); resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized payload status=%d, want 413", resp.StatusCode)
	}
	v2 := bytes.Repeat([]byte("SLOT-B-V2"), 256)
	v2SHA := fmt.Sprintf("%x", sha256.Sum256(v2))
	if resp, _ := doReq(http.MethodPost, "/v1/ota/stage", v2, map[string]string{
		"X-OTA-Version": "2.0.0",
		"X-OTA-Target":  "qemu",
		"X-OTA-SHA256":  v2SHA,
	}, secret); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("wrong target status=%d, want 400", resp.StatusCode)
	}

	// Stage -> Cancel -> Re-stage -> Activate -> Conflict on stage during Pending -> SelectBoot -> Confirm -> Rollback.
	if resp, body := doReq(http.MethodPost, "/v1/ota/stage", v2, map[string]string{
		"X-OTA-Version":        "2.0.0",
		"X-OTA-Target":         "de25-nano",
		"X-OTA-SHA256":         v2SHA,
		"X-OTA-Rollback-Epoch": "2",
	}, secret); resp.StatusCode != http.StatusOK {
		t.Fatalf("stage v2 status=%d body=%s", resp.StatusCode, body)
	}
	if resp, body := doReq(http.MethodPost, "/v1/ota/cancel", nil, nil, secret); resp.StatusCode != http.StatusOK {
		t.Fatalf("cancel status=%d body=%s", resp.StatusCode, body)
	}
	if resp, body := doReq(http.MethodPost, "/v1/ota/stage", v2, map[string]string{
		"X-OTA-Version":        "2.0.0",
		"X-OTA-Target":         "de25-nano",
		"X-OTA-SHA256":         v2SHA,
		"X-OTA-Rollback-Epoch": "2",
	}, secret); resp.StatusCode != http.StatusOK {
		t.Fatalf("restage v2 status=%d body=%s", resp.StatusCode, body)
	}
	if resp, body := doReq(http.MethodPost, "/v1/ota/activate", nil, nil, secret); resp.StatusCode != http.StatusOK {
		t.Fatalf("activate status=%d body=%s", resp.StatusCode, body)
	}
	if resp, _ := doReq(http.MethodPost, "/v1/ota/stage", v2, map[string]string{"X-OTA-Version": "3.0"}, secret); resp.StatusCode != http.StatusConflict {
		t.Fatalf("stage during PhasePending status=%d, want 409", resp.StatusCode)
	}

	dec, err := mgr.SelectBoot(ctx)
	if err != nil || dec.Slot != ota.SlotB || !dec.IsTrial {
		t.Fatalf("SelectBoot: dec=%+v err=%v", dec, err)
	}
	if resp, body := doReq(http.MethodPost, "/v1/ota/confirm", nil, nil, secret); resp.StatusCode != http.StatusOK {
		t.Fatalf("confirm status=%d body=%s", resp.StatusCode, body)
	}
	var stAfter ota.State
	resp, stateBody := doReq(http.MethodGet, "/v1/ota", nil, nil, secret)
	if resp.StatusCode != http.StatusOK || json.Unmarshal(stateBody, &stAfter) != nil {
		t.Fatalf("GET /v1/ota after confirm failed: %d %s", resp.StatusCode, stateBody)
	}
	if stAfter.ActiveSlot != ota.SlotB || stAfter.Phase != ota.PhaseIdle || stAfter.SlotB == nil || !stAfter.SlotB.Healthy {
		t.Fatalf("unexpected state after HTTP confirm: %+v", stAfter)
	}

	if resp, body := doReq(http.MethodPost, "/v1/ota/rollback", []byte(`{"reason":"http rollback test"}`), nil, secret); resp.StatusCode != http.StatusOK {
		t.Fatalf("rollback status=%d body=%s", resp.StatusCode, body)
	} else {
		var rb ota.State
		if err := json.Unmarshal(body, &rb); err != nil || rb.ActiveSlot != ota.SlotA {
			t.Fatalf("unexpected rollback state: %s (%v)", body, err)
		}
	}
}

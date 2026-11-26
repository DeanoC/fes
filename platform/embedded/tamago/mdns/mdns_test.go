// SPDX-License-Identifier: GPL-3.0-or-later

package mdns

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"net/netip"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/network"
)

type wireEndpoint struct {
	hub    *wireHub
	id     int
	mu     sync.Mutex
	nextID uint64
	taps   map[uint64]func([]byte)
	paused atomic.Bool
}

type wireHub struct {
	mu        sync.Mutex
	endpoints []*wireEndpoint
}

func newWireHub() *wireHub { return &wireHub{} }

func (h *wireHub) newEndpoint() *wireEndpoint {
	h.mu.Lock()
	defer h.mu.Unlock()
	ep := &wireEndpoint{
		hub:  h,
		id:   len(h.endpoints) + 1,
		taps: make(map[uint64]func([]byte)),
	}
	h.endpoints = append(h.endpoints, ep)
	return ep
}

func (e *wireEndpoint) SendRawFrame(frame []byte) error {
	if e.paused.Load() {
		return network.ErrPaused
	}
	cp := bytes.Clone(frame)
	e.hub.mu.Lock()
	peers := append([]*wireEndpoint(nil), e.hub.endpoints...)
	e.hub.mu.Unlock()
	for _, peer := range peers {
		if peer == e {
			continue
		}
		peer.deliver(cp)
	}
	return nil
}

func (e *wireEndpoint) deliver(frame []byte) {
	e.mu.Lock()
	cbs := make([]func([]byte), 0, len(e.taps))
	for _, fn := range e.taps {
		cbs = append(cbs, fn)
	}
	e.mu.Unlock()
	for _, fn := range cbs {
		fn(frame)
	}
}

func (e *wireEndpoint) AddTap(fn func([]byte)) func() {
	e.mu.Lock()
	e.nextID++
	id := e.nextID
	e.taps[id] = fn
	e.mu.Unlock()
	return func() {
		e.mu.Lock()
		delete(e.taps, id)
		e.mu.Unlock()
	}
}

func (e *wireEndpoint) Err() error { return nil }

func TestMDNSWireEncodeDecodeAndRejections(t *testing.T) {
	msg := dnsMessage{
		ID:       0,
		Response: true,
		Answers: []dnsRecord{{
			Name:  "_fogcast._tcp.local",
			Type:  TypePTR,
			Class: ClassIN,
			TTL:   120,
			PTR:   "de25-nano._fogcast._tcp.local",
		}},
		Additional: []dnsRecord{
			{
				Name:       "de25-nano._fogcast._tcp.local",
				Type:       TypeSRV,
				Class:      ClassIN,
				CacheFlush: true,
				TTL:        120,
				SRVPort:    8080,
				SRVTarget:  "fes-de25.local",
			},
			{
				Name:       "de25-nano._fogcast._tcp.local",
				Type:       TypeTXT,
				Class:      ClassIN,
				CacheFlush: true,
				TTL:        120,
				TXT:        []string{"protocol=1", "target_id=12345678-1234-4234-8234-123456789abc"},
			},
			{
				Name:       "fes-de25.local",
				Type:       TypeA,
				Class:      ClassIN,
				CacheFlush: true,
				TTL:        120,
				A:          netip.MustParseAddr("192.0.2.50"),
			},
		},
	}
	payload, err := encodeMDNSMessage(msg)
	if err != nil {
		t.Fatal(err)
	}
	mac := [6]byte{0x02, 0x00, 0x00, 0x00, 0x00, 0x50}
	ip := [4]byte{192, 0, 2, 50}
	frame := buildMDNSFrame(mac, MulticastMAC(), ip, multicastIPv4Bytes, Port, Port, 42, payload)

	// RFC 6762 §11 requires IPv4 TTL=255.
	if ttl := frame[ethHeaderLen+8]; ttl != mdnsIPv4TTL {
		t.Fatalf("IPv4 TTL = %d, want %d", ttl, mdnsIPv4TTL)
	}
	if binary.BigEndian.Uint16(frame[ethHeaderLen+ipv4MinLen+2:ethHeaderLen+ipv4MinLen+4]) != Port {
		t.Fatal("unexpected mDNS destination port")
	}

	parsed, ok := parseMDNSFrame(frame, [6]byte{0x02, 0x00, 0x00, 0x00, 0x00, 0x99}, [4]byte{192, 0, 2, 99}, false)
	if !ok {
		t.Fatal("parseMDNSFrame rejected valid mDNS packet")
	}
	if !parsed.msg.Response || len(parsed.msg.Answers) != 1 || len(parsed.msg.Additional) != 3 {
		t.Fatalf("unexpected decoded message: %+v", parsed.msg)
	}
	if parsed.msg.Additional[0].CacheFlush != true || parsed.msg.Answers[0].CacheFlush != false {
		t.Fatal("unexpected cache-flush flags")
	}

	// Non-255 IPv4 TTL must be discarded per RFC 6762 §11.
	badTTLFrame := bytes.Clone(frame)
	badTTLFrame[ethHeaderLen+8] = 64
	binary.BigEndian.PutUint16(badTTLFrame[ethHeaderLen+10:ethHeaderLen+12], 0)
	binary.BigEndian.PutUint16(badTTLFrame[ethHeaderLen+10:ethHeaderLen+12], internetChecksum(badTTLFrame[ethHeaderLen:ethHeaderLen+ipv4MinLen]))
	if _, ok := parseMDNSFrame(badTTLFrame, [6]byte{0x02, 0x00, 0x00, 0x00, 0x00, 0x99}, [4]byte{192, 0, 2, 99}, false); ok {
		t.Fatal("parseMDNSFrame must discard packet with IPv4 TTL != 255")
	}

	// Compression pointer loop must fail cleanly.
	loop := make([]byte, dnsHeaderLen+4)
	loop[5] = 1
	loop[dnsHeaderLen] = 0xc0
	loop[dnsHeaderLen+1] = byte(dnsHeaderLen)
	if _, err := decodeMDNSMessage(loop); err == nil {
		t.Fatal("expected compression loop error")
	}
}

func TestFogCastTXTAndServiceValidation(t *testing.T) {
	id := "11111111-2222-4333-8444-555555555555"
	if !ValidID(id) || ValidID("NOT-A-UUID") || ValidID("11111111-2222-4333-8444-55555555555G") {
		t.Fatal("ValidID mismatch")
	}

	svc, err := NewFogCastService("DE25 Nano", "fes-de25", netip.MustParseAddr("192.0.2.50"), 8080, FogCastInfo{
		TargetID:   id,
		NodeID:     id,
		Mesh:       FogCastMeshProtocol,
		Cap:        "execute,display_sink",
		TTLSeconds: 90,
	})
	if err != nil {
		t.Fatal(err)
	}
	if svc.Host != "fes-de25.local" || svc.TTL != 90 {
		t.Fatalf("unexpected normalized service: %+v", svc)
	}
	info := ParseFogCastTXT(svc.TXT)
	if !info.DirectBindable() || info.TargetID != id || info.NodeID != id || info.Mesh != FogCastMeshProtocol || info.TTLSeconds != 90 {
		t.Fatalf("unexpected parsed FogCast info: %+v", info)
	}
	if len(svc.TXT) == 0 || svc.TXT[0] != "protocol=1" {
		t.Fatalf("expected protocol=1 first TXT entry, got %v", svc.TXT)
	}

	if _, err := NewFogCastService("bad", "fes-de25", netip.MustParseAddr("192.0.2.50"), 8080, FogCastInfo{TargetID: "bad"}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig for invalid target_id, got %v", err)
	}
	otherID := "22222222-3333-4444-8555-666666666666"
	if _, err := NewFogCastService("mismatch", "fes-de25", netip.MustParseAddr("192.0.2.50"), 8080, FogCastInfo{TargetID: id, NodeID: otherID}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig when node_id != target_id, got %v", err)
	}
}

func TestResponderAndBrowserDiscoveryGoodbyeAndPause(t *testing.T) {
	hub := newWireHub()
	serverEP := hub.newEndpoint()
	clientEP := hub.newEndpoint()

	id := "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	svc, err := NewFogCastService("DE25 Nano", "fes-de25.local", netip.MustParseAddr("192.0.2.50"), 8080, FogCastInfo{
		TargetID:   id,
		NodeID:     id,
		Mesh:       FogCastMeshProtocol,
		TTLSeconds: 120,
	})
	if err != nil {
		t.Fatal(err)
	}

	serverMAC := [6]byte{0x02, 0xaa, 0xbb, 0xcc, 0xdd, 0x01}
	clientMAC := [6]byte{0x02, 0xaa, 0xbb, 0xcc, 0xdd, 0x02}
	clientIP := netip.MustParseAddr("192.0.2.99")

	resp, err := NewResponder(ResponderConfig{
		Port:             serverEP,
		LocalMAC:         serverMAC,
		Service:          svc,
		AnnounceCount:    2,
		AnnounceInterval: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}

	runCtx, runCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer runCancel()
	if err := resp.Start(runCtx); err != nil {
		t.Fatal(err)
	}
	if err := resp.Start(runCtx); !errors.Is(err, ErrResponderStarted) {
		t.Fatalf("second Start = %v, want ErrResponderStarted", err)
	}

	// Active browse for _fogcast._tcp with StopOnFirst.
	browseCtx, browseCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer browseCancel()
	svcs, err := Browse(browseCtx, clientEP, BrowseConfig{
		LocalMAC:      clientMAC,
		LocalIP:       clientIP,
		QueryInterval: 50 * time.Millisecond,
		StopOnFirst:   true,
	}, FogCastServiceType)
	if err != nil {
		t.Fatal(err)
	}
	if len(svcs) != 1 || svcs[0].Instance != "DE25 Nano" || svcs[0].Port != 8080 || len(svcs[0].Addrs) != 1 || svcs[0].Addrs[0] != netip.MustParseAddr("192.0.2.50") {
		t.Fatalf("unexpected Browse result: %+v", svcs)
	}
	if info := ParseFogCastTXT(svcs[0].TXT); !info.DirectBindable() || info.TargetID != id {
		t.Fatalf("unexpected TXT metadata: %+v", info)
	}

	// ResolveHost for fes-de25.local.
	addrs, err := ResolveHost(browseCtx, clientEP, BrowseConfig{
		LocalMAC:        clientMAC,
		LocalIP:         clientIP,
		QueryInterval:   50 * time.Millisecond,
		UnicastResponse: true,
	}, "fes-de25.local")
	if err != nil {
		t.Fatal(err)
	}
	if len(addrs) != 1 || addrs[0] != netip.MustParseAddr("192.0.2.50") {
		t.Fatalf("ResolveHost = %v", addrs)
	}

	// Pause drop handling: pausing serverEP drops replies cleanly without failing responder.
	serverEP.paused.Store(true)
	pauseCtx, pauseCancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	_, _ = ResolveHost(pauseCtx, clientEP, BrowseConfig{
		LocalMAC:      clientMAC,
		LocalIP:       clientIP,
		QueryInterval: 25 * time.Millisecond,
	}, "fes-de25.local")
	pauseCancel()
	serverEP.paused.Store(false)

	statsBeforeStop := resp.Stats()
	if statsBeforeStop.QueriesReceived == 0 || statsBeforeStop.UnicastReplies == 0 || statsBeforeStop.PausedDrops == 0 {
		t.Fatalf("unexpected responder stats: %+v", statsBeforeStop)
	}

	// Observe goodbye TTL=0 frame emitted on Stop().
	goodbyeSeen := make(chan bool, 1)
	rmTap := clientEP.AddTap(func(frame []byte) {
		pkt, ok := parseMDNSFrame(frame, clientMAC, clientIP.As4(), true)
		if ok && pkt.msg.Response && len(pkt.msg.Answers) > 0 && pkt.msg.Answers[0].TTL == 0 {
			select {
			case goodbyeSeen <- true:
			default:
			}
		}
	})
	defer rmTap()

	resp.Stop()
	resp.Stop() // idempotent

	select {
	case <-goodbyeSeen:
	case <-time.After(time.Second):
		t.Fatal("expected TTL=0 goodbye packet on Stop")
	}

	if err := resp.Start(runCtx); !errors.Is(err, ErrResponderStopped) {
		t.Fatalf("Start after Stop = %v, want ErrResponderStopped", err)
	}
	if err := resp.AnnounceNow(); err != nil {
		t.Fatal(err)
	}
	if s := resp.Stats(); s.Running || s.Goodbyes != 1 {
		t.Fatalf("unexpected final responder stats: %+v", s)
	}
}

func TestBrowseExpiresStaleRecords(t *testing.T) {
	hub := newWireHub()
	serverEP := hub.newEndpoint()
	clientEP := hub.newEndpoint()

	id := "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	svc, err := NewFogCastService("ShortTTL", "fes-short.local", netip.MustParseAddr("192.0.2.55"), 8080, FogCastInfo{
		TargetID:   id,
		TTLSeconds: 1,
	})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := NewResponder(ResponderConfig{
		Port:             serverEP,
		LocalMAC:         [6]byte{0x02, 0xaa, 0xbb, 0xcc, 0xdd, 0x11},
		Service:          svc,
		AnnounceCount:    1,
		AnnounceInterval: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	rCtx, rCancel := context.WithCancel(context.Background())
	defer rCancel()
	if err := resp.Start(rCtx); err != nil {
		t.Fatal(err)
	}
	defer resp.Stop()

	var clockMu sync.Mutex
	fakeNow := time.Date(2026, time.November, 26, 12, 0, 0, 0, time.UTC)
	nowFn := func() time.Time {
		clockMu.Lock()
		defer clockMu.Unlock()
		return fakeNow
	}

	// Stop the responder before advancing fakeNow by >1s so no refresh arrives;
	// Browse with StopOnFirst=false must drop expired records at deadline.
	bCtx, bCancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer bCancel()
	go func() {
		time.Sleep(25 * time.Millisecond)
		rCancel()
		clockMu.Lock()
		fakeNow = fakeNow.Add(2 * time.Second)
		clockMu.Unlock()
	}()

	got, err := Browse(bCtx, clientEP, BrowseConfig{
		LocalMAC:      [6]byte{0x02, 0xaa, 0xbb, 0xcc, 0xdd, 0x22},
		LocalIP:       netip.MustParseAddr("192.0.2.99"),
		QueryInterval: 10 * time.Millisecond,
		StopOnFirst:   false,
		Now:           nowFn,
	}, FogCastServiceType)
	if !errors.Is(err, context.DeadlineExceeded) || len(got) != 0 {
		t.Fatalf("expected expired record to be pruned on Browse completion: got=%+v err=%v", got, err)
	}
}

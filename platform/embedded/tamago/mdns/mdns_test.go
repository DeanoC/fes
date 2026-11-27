// SPDX-License-Identifier: GPL-3.0-or-later

package mdns

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"net/netip"
	"strings"
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
	sent   [][]byte
	sentAt []time.Time
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
	e.mu.Lock()
	e.sent = append(e.sent, bytes.Clone(cp))
	e.sentAt = append(e.sentAt, time.Now())
	e.mu.Unlock()
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

func (e *wireEndpoint) sentFrames() ([][]byte, []time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	frames := make([][]byte, len(e.sent))
	for i := range e.sent {
		frames[i] = bytes.Clone(e.sent[i])
	}
	return frames, append([]time.Time(nil), e.sentAt...)
}

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

	probePayload, err := encodeMDNSMessage(dnsMessage{
		Questions: []dnsQuestion{{Name: "fes-de25.local", Type: TypeANY, Class: ClassIN}},
		Authorities: []dnsRecord{{
			Name: "fes-de25.local", Type: TypeA, Class: ClassIN,
			TTL: 120, A: netip.MustParseAddr("192.0.2.50"),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	probe, err := decodeMDNSMessage(probePayload)
	if err != nil || len(probe.Authorities) != 1 || probe.Authorities[0].A != netip.MustParseAddr("192.0.2.50") {
		t.Fatalf("authority section round trip = %+v, %v", probe, err)
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

	// Stop the responder after its first announcement, then advance fakeNow
	// beyond the one-second TTL so Browse must prune the received records.
	bCtx, bCancel := context.WithCancel(context.Background())
	defer bCancel()
	var announced atomic.Bool
	go func() {
		deadline := time.NewTimer(3 * time.Second)
		defer deadline.Stop()
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for resp.Stats().Announcements == 0 {
			select {
			case <-deadline.C:
				bCancel()
				return
			case <-ticker.C:
			}
		}
		announced.Store(true)
		rCancel()
		clockMu.Lock()
		fakeNow = fakeNow.Add(2 * time.Second)
		clockMu.Unlock()
		bCancel()
	}()

	got, err := Browse(bCtx, clientEP, BrowseConfig{
		LocalMAC:      [6]byte{0x02, 0xaa, 0xbb, 0xcc, 0xdd, 0x22},
		LocalIP:       netip.MustParseAddr("192.0.2.99"),
		QueryInterval: 10 * time.Millisecond,
		StopOnFirst:   false,
		Now:           nowFn,
	}, FogCastServiceType)
	if !announced.Load() || !errors.Is(err, context.Canceled) || len(got) != 0 {
		t.Fatalf("expected expired record to be pruned on Browse completion: got=%+v err=%v", got, err)
	}
}

func TestResponderProbesBeforeAnnouncing(t *testing.T) {
	hub := newWireHub()
	serverEP := hub.newEndpoint()
	clientEP := hub.newEndpoint()
	serverMAC := [6]byte{0x02, 0xaa, 0xbb, 0xcc, 0xdd, 0x31}
	clientMAC := [6]byte{0x02, 0xaa, 0xbb, 0xcc, 0xdd, 0x32}
	clientIP := netip.MustParseAddr("192.0.2.99")
	resp, err := NewResponder(ResponderConfig{
		Port: serverEP, LocalMAC: serverMAC,
		Service:       Service{Instance: "Probe", ServiceType: "_test._tcp", Host: "probe-host.local", Port: 8080, IP: netip.MustParseAddr("192.0.2.50"), TXT: []string{"id=probe"}},
		AnnounceCount: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Stop()

	messages := make(chan dnsMessage, 8)
	removeTap := clientEP.AddTap(func(frame []byte) {
		pkt, ok := parseMDNSFrame(frame, clientMAC, clientIP.As4(), true)
		if ok {
			select {
			case messages <- pkt.msg:
			default:
			}
		}
	})
	defer removeTap()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := resp.Start(ctx); err != nil {
		t.Fatal(err)
	}

	probes := 0
	for {
		select {
		case msg := <-messages:
			if msg.Response {
				if probes != probeAttempts {
					t.Fatalf("startup announcement followed %d probes, want %d", probes, probeAttempts)
				}
				if len(msg.Answers) == 0 || msg.Answers[0].Type != TypePTR {
					t.Fatalf("unexpected startup announcement: %+v", msg)
				}
				return
			}
			if len(msg.Authorities) == 0 {
				continue
			}
			probes++
			if len(msg.Questions) != 2 || len(msg.Authorities) != 3 {
				t.Fatalf("probe shape: questions=%+v authorities=%+v", msg.Questions, msg.Authorities)
			}
			for _, rr := range msg.Authorities {
				if rr.CacheFlush {
					t.Fatalf("probe authority record has cache-flush bit: %+v", rr)
				}
			}
		case <-time.After(3 * time.Second):
			t.Fatal("timed out waiting for probes and startup announcement")
		}
	}
}

func TestResponderProbeTieBreakAndConflictDefense(t *testing.T) {
	newResponder := func(t *testing.T, onError func(error)) *Responder {
		t.Helper()
		hub := newWireHub()
		resp, err := NewResponder(ResponderConfig{
			Port:          hub.newEndpoint(),
			LocalMAC:      [6]byte{0x02, 0xaa, 0xbb, 0xcc, 0xdd, 0x41},
			Service:       Service{Instance: "Conflict", ServiceType: "_test._tcp", Host: "conflict-host.local", Port: 8080, IP: netip.MustParseAddr("192.0.2.50"), TXT: []string{"id=one"}},
			AnnounceCount: 1,
			OnError:       onError,
		})
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}

	probeErr := make(chan error, 1)
	probing := newResponder(t, func(err error) { probeErr <- err })
	remoteProposal := probing.probeRecords()
	remoteProposal[0].A = netip.MustParseAddr("192.0.2.51")
	if conflict, localWins := compareProbeRecords(probing.probeRecords(), remoteProposal); !conflict || localWins {
		t.Fatalf("higher peer proposal comparison = conflict %v, localWins %v", conflict, localWins)
	}
	probing.nameMu.Lock()
	probing.probing = true
	probing.nameMu.Unlock()
	probing.handleProbeConflict(remoteProposal)
	select {
	case err := <-probeErr:
		t.Fatalf("unexpected probe conflict error on losing simultaneous probe: %v", err)
	default:
	}
	select {
	case <-probing.tieBreakCh:
	default:
		t.Fatal("responder did not signal tie-break retry for a losing probe")
	}
	probing.nameMu.Lock()
	probeConflicted := probing.conflicted
	probing.nameMu.Unlock()
	if probeConflicted {
		t.Fatal("responder terminally conflicted instead of backing off for a losing simultaneous probe")
	}

	conflictErr := make(chan error, 1)
	claimed := newResponder(t, func(err error) { conflictErr <- err })
	claimed.running.Store(true)
	claimed.nameMu.Lock()
	claimed.claimed = true
	claimed.nameMu.Unlock()
	conflictingPayload, err := encodeMDNSMessage(dnsMessage{Response: true, Answers: []dnsRecord{{
		Name: claimed.hostFQDN, Type: TypeA, Class: ClassIN, TTL: 120, A: netip.MustParseAddr("192.0.2.51"),
	}}})
	if err != nil {
		t.Fatal(err)
	}
	conflictingFrame := buildMDNSFrame(
		[6]byte{0x02, 0xaa, 0xbb, 0xcc, 0xdd, 0x49}, multicastMACBytes,
		netip.MustParseAddr("192.0.2.51").As4(), multicastIPv4Bytes, Port, Port, 1, conflictingPayload,
	)
	claimed.handleFrame(conflictingFrame)
	if stats := claimed.Stats(); stats.Announcements != 1 || stats.Goodbyes != 0 {
		t.Fatalf("first conflict should defend once: %+v", stats)
	}
	claimed.handleFrame(conflictingFrame)
	if err := <-conflictErr; !errors.Is(err, ErrResponderNameConflict) {
		t.Fatalf("repeated conflict error = %v", err)
	}
	if stats := claimed.Stats(); stats.Announcements != 1 || stats.Goodbyes != 1 {
		t.Fatalf("repeated conflict should relinquish after one defense: %+v", stats)
	}
}

func TestResponderLegacyUnicastAndKnownAnswerSuppression(t *testing.T) {
	hub := newWireHub()
	serverEP := hub.newEndpoint()
	clientEP := hub.newEndpoint()
	serverMAC := [6]byte{0x02, 0xaa, 0xbb, 0xcc, 0xdd, 0x51}
	clientMAC := [6]byte{0x02, 0xaa, 0xbb, 0xcc, 0xdd, 0x52}
	clientIP := netip.MustParseAddr("192.0.2.99")
	service := Service{Instance: "Legacy", ServiceType: "_test._tcp", Host: "legacy-host.local", Port: 8080, IP: netip.MustParseAddr("192.0.2.50"), TXT: []string{"id=legacy"}}
	resp, err := NewResponder(ResponderConfig{Port: serverEP, LocalMAC: serverMAC, Service: service, AnnounceCount: 1})
	if err != nil {
		t.Fatal(err)
	}
	resp.running.Store(true)
	resp.nameMu.Lock()
	resp.claimed = true
	resp.nameMu.Unlock()
	removeResponderTap := serverEP.AddTap(resp.handleFrame)
	defer removeResponderTap()

	responses := make(chan dnsMessage, 4)
	removeClientTap := clientEP.AddTap(func(frame []byte) {
		pkt, ok := parseMDNSFrame(frame, clientMAC, clientIP.As4(), true)
		if ok && pkt.msg.Response {
			select {
			case responses <- pkt.msg:
			default:
			}
		}
	})
	defer removeClientTap()

	svc, _, _, _, err := service.normalized()
	if err != nil {
		t.Fatal(err)
	}
	question := dnsQuestion{Name: svc.ServiceType + "." + svc.Domain, Type: TypePTR, Class: ClassIN}
	query, err := encodeMDNSMessage(dnsMessage{ID: 0x4242, Questions: []dnsQuestion{question}})
	if err != nil {
		t.Fatal(err)
	}
	frame := buildMDNSFrame(clientMAC, multicastMACBytes, clientIP.As4(), multicastIPv4Bytes, 49152, Port, 1, query)
	if err := clientEP.SendRawFrame(frame); err != nil {
		t.Fatal(err)
	}
	var reply dnsMessage
	select {
	case reply = <-responses:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for legacy unicast response")
	}
	if reply.ID != 0x4242 || len(reply.Questions) != 1 || reply.Questions[0].Name != question.Name || reply.Questions[0].Type != question.Type {
		t.Fatalf("legacy unicast response did not echo ID and question: %+v", reply)
	}
	allRecords := append(append([]dnsRecord(nil), reply.Answers...), reply.Additional...)
	if len(allRecords) == 0 {
		t.Fatalf("legacy response contains no records: %+v", reply)
	}
	for _, rr := range allRecords {
		if rr.CacheFlush || rr.TTL > legacyUnicastMaxTTL {
			t.Fatalf("legacy record retained cache-flush or excessive TTL: %+v", rr)
		}
	}

	before := resp.Stats().ResponsesSent
	knownQuery, err := encodeMDNSMessage(dnsMessage{
		Questions: []dnsQuestion{question},
		Answers:   []dnsRecord{{Name: question.Name, Type: TypePTR, Class: ClassIN, TTL: 100, PTR: svc.Instance + "." + question.Name}},
	})
	if err != nil {
		t.Fatal(err)
	}
	frame = buildMDNSFrame(clientMAC, multicastMACBytes, clientIP.As4(), multicastIPv4Bytes, Port, Port, 2, knownQuery)
	if err := clientEP.SendRawFrame(frame); err != nil {
		t.Fatal(err)
	}
	if after := resp.Stats().ResponsesSent; after != before {
		t.Fatalf("known-answer query triggered response: before=%d after=%d", before, after)
	}
}

func TestResponderPromotesAdditionalForLaterQuestion(t *testing.T) {
	hub := newWireHub()
	serverEP := hub.newEndpoint()
	clientEP := hub.newEndpoint()
	serverMAC := [6]byte{0x02, 0xaa, 0xbb, 0xcc, 0xdd, 0x53}
	clientMAC := [6]byte{0x02, 0xaa, 0xbb, 0xcc, 0xdd, 0x54}
	clientIP := netip.MustParseAddr("192.0.2.99")
	service := Service{Instance: "Multi", ServiceType: "_test._tcp", Host: "multi-host.local", Port: 8080, IP: netip.MustParseAddr("192.0.2.50"), TXT: []string{"id=multi"}}
	resp, err := NewResponder(ResponderConfig{Port: serverEP, LocalMAC: serverMAC, Service: service, AnnounceCount: 1})
	if err != nil {
		t.Fatal(err)
	}
	resp.running.Store(true)
	resp.nameMu.Lock()
	resp.claimed = true
	resp.nameMu.Unlock()
	removeResponderTap := serverEP.AddTap(resp.handleFrame)
	defer removeResponderTap()

	responses := make(chan dnsMessage, 1)
	removeClientTap := clientEP.AddTap(func(frame []byte) {
		pkt, ok := parseMDNSFrame(frame, clientMAC, clientIP.As4(), true)
		if ok && pkt.msg.Response {
			select {
			case responses <- pkt.msg:
			default:
			}
		}
	})
	defer removeClientTap()

	svc, _, _, _, err := service.normalized()
	if err != nil {
		t.Fatal(err)
	}
	query, err := encodeMDNSMessage(dnsMessage{Questions: []dnsQuestion{
		{Name: svc.ServiceType + "." + svc.Domain, Type: TypePTR, Class: ClassIN},
		{Name: svc.Instance + "." + svc.ServiceType + "." + svc.Domain, Type: TypeSRV, Class: ClassIN},
	}})
	if err != nil {
		t.Fatal(err)
	}
	frame := buildMDNSFrame(clientMAC, multicastMACBytes, clientIP.As4(), multicastIPv4Bytes, Port, Port, 3, query)
	if err := clientEP.SendRawFrame(frame); err != nil {
		t.Fatal(err)
	}
	var reply dnsMessage
	select {
	case reply = <-responses:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for multi-question response")
	}

	countSRV := func(records []dnsRecord) int {
		count := 0
		for _, rr := range records {
			if rr.Type == TypeSRV {
				count++
			}
		}
		return count
	}
	if countSRV(reply.Answers) != 1 || countSRV(reply.Additional) != 0 {
		t.Fatalf("SRV requested by later question must appear once as an Answer: answers=%+v additional=%+v", reply.Answers, reply.Additional)
	}
}

func TestBrowseTTLZeroGoodbyeGraceAndRescue(t *testing.T) {
	tests := []struct {
		name       string
		rescue     bool
		advance    time.Duration
		wantActive bool
	}{
		{name: "retained during grace", advance: 500 * time.Millisecond, wantActive: true},
		{name: "positive records rescue goodbye", rescue: true, advance: 1100 * time.Millisecond, wantActive: true},
		{name: "expires after grace", advance: 1100 * time.Millisecond},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hub := newWireHub()
			clientEP := hub.newEndpoint()
			clientMAC := [6]byte{0x02, 0xaa, 0xbb, 0xcc, 0xdd, 0x55}
			clientIP := netip.MustParseAddr("192.0.2.99")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			var clockMu sync.Mutex
			fakeNow := time.Date(2026, time.November, 27, 12, 0, 0, 0, time.UTC)
			nowFn := func() time.Time {
				clockMu.Lock()
				defer clockMu.Unlock()
				return fakeNow
			}

			type browseResult struct {
				services []DiscoveredService
				err      error
			}
			done := make(chan browseResult, 1)
			go func() {
				services, err := Browse(ctx, clientEP, BrowseConfig{
					LocalMAC: clientMAC,
					LocalIP:  clientIP,
					Now:      nowFn,
				}, "_test._tcp")
				done <- browseResult{services: services, err: err}
			}()

			deadline := time.Now().Add(time.Second)
			for {
				frames, _ := clientEP.sentFrames()
				if len(frames) > 0 {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("Browse did not send its initial query")
				}
				time.Sleep(5 * time.Millisecond)
			}

			serverMAC := [6]byte{0x02, 0xaa, 0xbb, 0xcc, 0xdd, 0x56}
			serverIP := netip.MustParseAddr("192.0.2.50").As4()
			instance := "Test._test._tcp.local"
			host := "test-host.local"
			addr := netip.MustParseAddr("192.0.2.10")
			deliver := func(id uint16, msg dnsMessage) {
				t.Helper()
				payload, err := encodeMDNSMessage(msg)
				if err != nil {
					t.Fatal(err)
				}
				clientEP.deliver(buildMDNSFrame(serverMAC, multicastMACBytes, serverIP, multicastIPv4Bytes, Port, Port, id, payload))
			}
			live := dnsMessage{Response: true,
				Answers: []dnsRecord{{Name: "_test._tcp.local", Type: TypePTR, Class: ClassIN, TTL: 120, PTR: instance}},
				Additional: []dnsRecord{
					{Name: instance, Type: TypeSRV, Class: ClassIN, TTL: 120, SRVPort: 8080, SRVTarget: host},
					{Name: instance, Type: TypeTXT, Class: ClassIN, TTL: 120, TXT: []string{"id=test"}},
					{Name: host, Type: TypeA, Class: ClassIN, TTL: 120, A: addr},
				},
			}
			deliver(1, live)

			goodbye := dnsMessage{Response: true, Answers: []dnsRecord{
				{Name: "_test._tcp.local", Type: TypePTR, Class: ClassIN, TTL: 0, PTR: instance},
				{Name: instance, Type: TypeSRV, Class: ClassIN, CacheFlush: true, TTL: 0, SRVPort: 8080, SRVTarget: host},
				{Name: instance, Type: TypeTXT, Class: ClassIN, CacheFlush: true, TTL: 0, TXT: []string{"id=test"}},
				{Name: host, Type: TypeA, Class: ClassIN, CacheFlush: true, TTL: 0, A: addr},
			}}
			deliver(2, goodbye)

			if tt.rescue {
				clockMu.Lock()
				fakeNow = fakeNow.Add(500 * time.Millisecond)
				clockMu.Unlock()
				deliver(3, live)
			}
			clockMu.Lock()
			fakeNow = fakeNow.Add(tt.advance)
			clockMu.Unlock()
			cancel()
			result := <-done
			if tt.wantActive {
				if result.err != nil || len(result.services) != 1 || len(result.services[0].Addrs) != 1 || result.services[0].Addrs[0] != addr {
					t.Fatalf("Browse result during/after goodbye grace = %+v, err=%v", result.services, result.err)
				}
			} else if len(result.services) != 0 || !errors.Is(result.err, context.Canceled) {
				t.Fatalf("Browse result after goodbye grace = %+v, err=%v; want no service and context cancellation", result.services, result.err)
			}
		})
	}
}

func TestBrowseRetransmitsWithBackoffAndKnownAnswers(t *testing.T) {
	hub := newWireHub()
	clientEP := hub.newEndpoint()
	clientMAC := [6]byte{0x02, 0xaa, 0xbb, 0xcc, 0xdd, 0x61}
	clientIP := netip.MustParseAddr("192.0.2.99")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := Browse(ctx, clientEP, BrowseConfig{LocalMAC: clientMAC, LocalIP: clientIP}, "_test._tcp")
		done <- err
	}()
	finished := false
	defer func() {
		cancel()
		if !finished {
			<-done
		}
	}()

	var responseSent bool
	var frames [][]byte
	var sentAt []time.Time
	deadline := time.Now().Add(5 * time.Second)
	for len(frames) < 3 && time.Now().Before(deadline) {
		frames, sentAt = clientEP.sentFrames()
		if len(frames) > 0 && !responseSent {
			payload, err := encodeMDNSMessage(dnsMessage{
				Response: true,
				Answers:  []dnsRecord{{Name: "_test._tcp.local", Type: TypePTR, Class: ClassIN, TTL: 120, PTR: "Test._test._tcp.local"}},
			})
			if err != nil {
				t.Fatal(err)
			}
			serverMAC := [6]byte{0x02, 0xaa, 0xbb, 0xcc, 0xdd, 0x62}
			serverIP := netip.MustParseAddr("192.0.2.50").As4()
			clientEP.deliver(buildMDNSFrame(serverMAC, multicastMACBytes, serverIP, multicastIPv4Bytes, Port, Port, 3, payload))
			responseSent = true
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(frames) < 3 {
		t.Fatalf("got %d browse queries in 5s, want 3", len(frames))
	}
	if sentAt[1].Sub(sentAt[0]) < time.Second || sentAt[2].Sub(sentAt[1]) < 2*time.Second {
		t.Fatalf("browse query intervals = %v and %v, want at least 1s then 2s", sentAt[1].Sub(sentAt[0]), sentAt[2].Sub(sentAt[1]))
	}
	for i := 1; i < 3; i++ {
		pkt, ok := parseMDNSFrame(frames[i], clientMAC, clientIP.As4(), true)
		if !ok || len(pkt.msg.Answers) != 1 || pkt.msg.Answers[0].Type != TypePTR || pkt.msg.Answers[0].PTR != "Test._test._tcp.local" {
			t.Fatalf("retransmit %d omitted cached PTR known answer: %+v (ok=%v)", i, pkt.msg, ok)
		}
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Browse after cancellation = %v, want context.Canceled", err)
	}
	finished = true
}

func TestBrowseCacheFlushExpiresOldARecordsAfterGrace(t *testing.T) {
	hub := newWireHub()
	clientEP := hub.newEndpoint()
	clientMAC := [6]byte{0x02, 0xaa, 0xbb, 0xcc, 0xdd, 0x71}
	clientIP := netip.MustParseAddr("192.0.2.99")
	ctx, cancel := context.WithTimeout(context.Background(), 2500*time.Millisecond)
	defer cancel()
	type browseResult struct {
		services []DiscoveredService
		err      error
	}
	done := make(chan browseResult, 1)
	go func() {
		services, err := Browse(ctx, clientEP, BrowseConfig{LocalMAC: clientMAC, LocalIP: clientIP}, "_test._tcp")
		done <- browseResult{services: services, err: err}
	}()

	deadline := time.Now().Add(time.Second)
	for {
		frames, _ := clientEP.sentFrames()
		if len(frames) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Browse did not send its initial query")
		}
		time.Sleep(5 * time.Millisecond)
	}
	serverMAC := [6]byte{0x02, 0xaa, 0xbb, 0xcc, 0xdd, 0x72}
	serverIP := netip.MustParseAddr("192.0.2.50").As4()
	deliver := func(id uint16, msg dnsMessage) {
		t.Helper()
		payload, err := encodeMDNSMessage(msg)
		if err != nil {
			t.Fatal(err)
		}
		clientEP.deliver(buildMDNSFrame(serverMAC, multicastMACBytes, serverIP, multicastIPv4Bytes, Port, Port, id, payload))
	}
	instance := "Test._test._tcp.local"
	oldAddr := netip.MustParseAddr("192.0.2.10")
	deliver(1, dnsMessage{
		Response: true,
		Answers:  []dnsRecord{{Name: "_test._tcp.local", Type: TypePTR, Class: ClassIN, TTL: 120, PTR: instance}},
		Additional: []dnsRecord{
			{Name: instance, Type: TypeSRV, Class: ClassIN, TTL: 120, SRVPort: 8080, SRVTarget: "test-host.local"},
			{Name: "test-host.local", Type: TypeA, Class: ClassIN, TTL: 120, A: oldAddr},
		},
	})
	time.Sleep(1100 * time.Millisecond)
	newAddrs := []netip.Addr{netip.MustParseAddr("192.0.2.20"), netip.MustParseAddr("192.0.2.21")}
	deliver(2, dnsMessage{Response: true, Answers: []dnsRecord{
		{Name: "test-host.local", Type: TypeA, Class: ClassIN, CacheFlush: true, TTL: 120, A: newAddrs[0]},
		{Name: "test-host.local", Type: TypeA, Class: ClassIN, CacheFlush: true, TTL: 120, A: newAddrs[1]},
	}})

	result := <-done
	if result.err != nil || len(result.services) != 1 {
		t.Fatalf("Browse result = %+v, err=%v", result.services, result.err)
	}
	got := result.services[0].Addrs
	if len(got) != 2 || got[0] != newAddrs[0] || got[1] != newAddrs[1] {
		t.Fatalf("cache-flush browse addresses = %v, want %v (old %s expired)", got, newAddrs, oldAddr)
	}
}

func TestCacheFlushDoesNotExpireRecentlyReceivedARecords(t *testing.T) {
	base := time.Date(2026, time.November, 26, 12, 0, 0, 0, time.UTC)
	cache := map[string]map[netip.Addr]cachedAddress{}
	oldAddr := netip.MustParseAddr("192.0.2.10")
	newAddr := netip.MustParseAddr("192.0.2.20")
	applyARecordPacket(cache, []dnsRecord{{Name: "flush-host.local", Type: TypeA, Class: ClassIN, TTL: 120, A: oldAddr}}, base)
	applyARecordPacket(cache, []dnsRecord{{Name: "flush-host.local", Type: TypeA, Class: ClassIN, CacheFlush: true, TTL: 120, A: newAddr}}, base.Add(500*time.Millisecond))
	if got := cache["flush-host.local"][oldAddr].expires; !got.Equal(base.Add(120 * time.Second)) {
		t.Fatalf("recent A record expiry = %v, want unchanged %v", got, base.Add(120*time.Second))
	}

	flushTime := base.Add(1600 * time.Millisecond)
	freshAddr := netip.MustParseAddr("192.0.2.21")
	applyARecordPacket(cache, []dnsRecord{{Name: "flush-host.local", Type: TypeA, Class: ClassIN, CacheFlush: true, TTL: 120, A: freshAddr}}, flushTime)
	if got := cache["flush-host.local"][oldAddr].expires; !got.Equal(flushTime.Add(cacheFlushGrace)) {
		t.Fatalf("older A record expiry = %v, want one-second flush grace ending %v", got, flushTime.Add(cacheFlushGrace))
	}
	if got := cache["flush-host.local"][newAddr].expires; !got.Equal(flushTime.Add(cacheFlushGrace)) {
		t.Fatalf("older A record expiry = %v, want one-second flush grace ending %v", got, flushTime.Add(cacheFlushGrace))
	}
	if got := cache["flush-host.local"][freshAddr].expires; !got.Equal(flushTime.Add(120 * time.Second)) {
		t.Fatalf("new cache-flush A record expiry = %v, want %v", got, flushTime.Add(120*time.Second))
	}
}

func waitForCondition(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal(msg)
}

func TestResponderPreFirstProbeResponseIgnored(t *testing.T) {
	hub := newWireHub()
	ep := hub.newEndpoint()
	peer := hub.newEndpoint()
	errCh := make(chan error, 4)
	resp, err := NewResponder(ResponderConfig{
		Port:          ep,
		LocalMAC:      [6]byte{0x02, 0xaa, 0xbb, 0xcc, 0xdd, 0x61},
		Service:       Service{Instance: "PreProbe", ServiceType: "_test._tcp", Host: "preprobe-host.local", Port: 8080, IP: netip.MustParseAddr("192.0.2.61")},
		AnnounceCount: 1,
		OnError:       func(err error) { errCh <- err },
	})
	if err != nil {
		t.Fatal(err)
	}
	resp.initialProbeDelay = 120 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := resp.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer resp.Stop()

	// Inject an authoritative response before the first probe is sent.
	// RFC 6762 §8.1 requires ignoring responses received prior to probing.
	time.Sleep(30 * time.Millisecond)
	payload, err := encodeMDNSMessage(dnsMessage{
		Response: true,
		Answers: []dnsRecord{
			{Name: "preprobe-host.local", Type: TypeA, Class: ClassIN, CacheFlush: true, TTL: 120, A: netip.MustParseAddr("192.0.2.99")},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	frame := buildMDNSFrame([6]byte{0x02, 0xaa, 0xbb, 0xcc, 0xdd, 0x62}, MulticastMAC(), [4]byte{192, 0, 2, 99}, multicastIPv4Bytes, Port, Port, 1, payload)
	if err := peer.SendRawFrame(frame); err != nil {
		t.Fatal(err)
	}

	waitForCondition(t, 2*time.Second, func() bool {
		resp.nameMu.Lock()
		defer resp.nameMu.Unlock()
		return resp.claimed
	}, "responder did not claim after ignoring pre-first-probe response")

	select {
	case err := <-errCh:
		t.Fatalf("unexpected responder error: %v", err)
	default:
	}
}

func TestResponderSimultaneousProbeTieBreakRetries(t *testing.T) {
	hub := newWireHub()
	ep := hub.newEndpoint()
	peer := hub.newEndpoint()
	errCh := make(chan error, 4)
	resp, err := NewResponder(ResponderConfig{
		Port:          ep,
		LocalMAC:      [6]byte{0x02, 0xaa, 0xbb, 0xcc, 0xdd, 0x71},
		Service:       Service{Instance: "TieBreak", ServiceType: "_test._tcp", Host: "tiebreak-host.local", Port: 8080, IP: netip.MustParseAddr("192.0.2.71")},
		AnnounceCount: 1,
		OnError:       func(err error) { errCh <- err },
	})
	if err != nil {
		t.Fatal(err)
	}
	resp.initialProbeDelay = 5 * time.Millisecond
	resp.tieBreakDelay = 40 * time.Millisecond

	var injected atomic.Bool
	var probeCount atomic.Int32
	unsub := peer.AddTap(func(raw []byte) {
		pkt, ok := parseMDNSFrame(raw, [6]byte{}, [4]byte{}, true)
		if !ok || pkt.msg.Response || len(pkt.msg.Questions) == 0 {
			return
		}
		probeCount.Add(1)
		if !injected.CompareAndSwap(false, true) {
			return
		}
		// Higher IP proposal wins the tie-break; peer then goes quiet so retry succeeds.
		proposal := resp.probeRecords()
		proposal[0].A = netip.MustParseAddr("192.0.2.250")
		payload, err := encodeMDNSMessage(dnsMessage{
			Questions:   pkt.msg.Questions,
			Authorities: proposal,
		})
		if err != nil {
			return
		}
		frame := buildMDNSFrame([6]byte{0x02, 0xaa, 0xbb, 0xcc, 0xdd, 0x72}, MulticastMAC(), [4]byte{192, 0, 2, 250}, multicastIPv4Bytes, Port, Port, 2, payload)
		go func() { _ = peer.SendRawFrame(frame) }()
	})
	defer unsub()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := resp.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer resp.Stop()

	waitForCondition(t, 2*time.Second, func() bool {
		resp.nameMu.Lock()
		defer resp.nameMu.Unlock()
		return resp.claimed
	}, "responder did not claim after tie-break backoff and probe restart")

	if got := probeCount.Load(); got < int32(probeAttempts+1) {
		t.Fatalf("probe count after tie-break retry = %d, want at least %d", got, probeAttempts+1)
	}
	select {
	case err := <-errCh:
		t.Fatalf("unexpected responder error: %v", err)
	default:
	}
}

func TestResponderProbeResponseAnyRecordConflict(t *testing.T) {
	hub := newWireHub()
	ep := hub.newEndpoint()
	peer := hub.newEndpoint()
	errCh := make(chan error, 4)
	resp, err := NewResponder(ResponderConfig{
		Port:          ep,
		LocalMAC:      [6]byte{0x02, 0xaa, 0xbb, 0xcc, 0xdd, 0x81},
		Service:       Service{Instance: "AnyRR", ServiceType: "_test._tcp", Host: "anyrr-host.local", Port: 8080, IP: netip.MustParseAddr("192.0.2.81")},
		AnnounceCount: 1,
		OnError:       func(err error) { errCh <- err },
	})
	if err != nil {
		t.Fatal(err)
	}
	resp.initialProbeDelay = 5 * time.Millisecond

	var injected atomic.Bool
	unsub := peer.AddTap(func(raw []byte) {
		pkt, ok := parseMDNSFrame(raw, [6]byte{}, [4]byte{}, true)
		if !ok || pkt.msg.Response || len(pkt.msg.Questions) == 0 {
			return
		}
		if !injected.CompareAndSwap(false, true) {
			return
		}
		// Answer with an IPv6 AAAA record (type 28) for the probed host name.
		aaaa := make([]byte, 16)
		aaaa[0] = 0xfe
		aaaa[1] = 0x80
		aaaa[15] = 0x01
		payload, err := encodeMDNSMessage(dnsMessage{
			Response: true,
			Answers: []dnsRecord{
				{Name: "anyrr-host.local", Type: 28, Class: ClassIN, CacheFlush: true, TTL: 120, RawRData: aaaa},
			},
		})
		if err != nil {
			return
		}
		frame := buildMDNSFrame([6]byte{0x02, 0xaa, 0xbb, 0xcc, 0xdd, 0x82}, MulticastMAC(), [4]byte{192, 0, 2, 82}, multicastIPv4Bytes, Port, Port, 3, payload)
		go func() { _ = peer.SendRawFrame(frame) }()
	})
	defer unsub()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := resp.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer resp.Stop()

	select {
	case err := <-errCh:
		if !errors.Is(err, ErrResponderNameConflict) {
			t.Fatalf("error = %v, want ErrResponderNameConflict", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for ErrResponderNameConflict on AAAA response during probing")
	}

	resp.nameMu.Lock()
	conflicted := resp.conflicted
	claimed := resp.claimed
	resp.nameMu.Unlock()
	if !conflicted || claimed {
		t.Fatalf("state after AAAA conflict = conflicted:%v claimed:%v, want true/false", conflicted, claimed)
	}
}

func TestResponderProbeAndAnnouncePausedRecovery(t *testing.T) {
	hub := newWireHub()
	ep := hub.newEndpoint()
	ep.paused.Store(true)
	errCh := make(chan error, 4)
	resp, err := NewResponder(ResponderConfig{
		Port:          ep,
		LocalMAC:      [6]byte{0x02, 0xaa, 0xbb, 0xcc, 0xdd, 0x91},
		Service:       Service{Instance: "Paused", ServiceType: "_test._tcp", Host: "paused-host.local", Port: 8080, IP: netip.MustParseAddr("192.0.2.91")},
		AnnounceCount: 2,
		OnError:       func(err error) { errCh <- err },
	})
	if err != nil {
		t.Fatal(err)
	}
	resp.initialProbeDelay = 5 * time.Millisecond

	peer := hub.newEndpoint()
	var announcements atomic.Int32
	unsub := peer.AddTap(func(raw []byte) {
		pkt, ok := parseMDNSFrame(raw, [6]byte{}, [4]byte{}, true)
		if !ok || !pkt.msg.Response || len(pkt.msg.Answers) == 0 {
			return
		}
		if announcements.Add(1) == 1 {
			ep.paused.Store(true)
		}
	})
	defer unsub()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := resp.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer resp.Stop()

	waitForCondition(t, 2*time.Second, func() bool {
		return resp.Stats().PausedDrops > 0
	}, "responder did not record paused drop while probing")

	if !resp.running.Load() {
		t.Fatal("responder exited on ErrPaused during probing")
	}

	probeDrops := resp.Stats().PausedDrops
	ep.paused.Store(false)

	waitForCondition(t, 2*time.Second, func() bool {
		return announcements.Load() == 1
	}, "responder did not recover from paused probing to send first announcement")

	// Wait for the second announcement attempt while paused to confirm pausedDrops
	// increments exactly once for that failed announcement.
	waitForCondition(t, 2*time.Second, func() bool {
		return resp.Stats().PausedDrops > probeDrops
	}, "responder did not record paused drop for second announcement")

	if got, want := resp.Stats().PausedDrops, probeDrops+1; got != want {
		t.Fatalf("PausedDrops after paused announcement = %d, want %d (single increment)", got, want)
	}

	select {
	case err := <-errCh:
		t.Fatalf("unexpected responder error: %v", err)
	default:
	}
}

func TestCompareProbeRecordsCrossRRTypeTieBreak(t *testing.T) {
	local := []dnsRecord{
		{Name: "tie.local", Type: TypeA, Class: ClassIN, TTL: 120, A: netip.MustParseAddr("192.0.2.10")},
	}
	// Remote proposes an AAAA record (type 28 > TypeA=1) for the same host name.
	aaaa := make([]byte, 16)
	aaaa[0] = 0xfe
	aaaa[1] = 0x80
	aaaa[15] = 0x01
	remoteAAAA := []dnsRecord{
		{Name: "tie.local", Type: 28, Class: ClassIN, TTL: 120, RawRData: aaaa},
	}
	if conflict, localWins := compareProbeRecords(local, remoteAAAA); !conflict || localWins {
		t.Fatalf("local A vs remote AAAA = conflict:%v localWins:%v, want true/false", conflict, localWins)
	}
	// When remote proposes both the matching A and an extra AAAA record, remote's longer set wins.
	remoteSuperset := []dnsRecord{
		local[0],
		remoteAAAA[0],
	}
	if conflict, localWins := compareProbeRecords(local, remoteSuperset); !conflict || localWins {
		t.Fatalf("local A vs remote {A,AAAA} = conflict:%v localWins:%v, want true/false", conflict, localWins)
	}
	// Reverse direction: local {A,AAAA} vs remote {A} -> local wins.
	if conflict, localWins := compareProbeRecords(remoteSuperset, local); !conflict || !localWins {
		t.Fatalf("local {A,AAAA} vs remote A = conflict:%v localWins:%v, want true/true", conflict, localWins)
	}
	// Identical record sets do not conflict.
	if conflict, _ := compareProbeRecords(local, local); conflict {
		t.Fatal("identical probe record sets reported a conflict")
	}
}

func TestCompareProbeRecordsPerNameArbitration(t *testing.T) {
	// Split tie-break outcomes must be reported independently per normalized name:
	// winning on a-host.local must not mask losing on z-svc._fogcast._tcp.local.
	localWinEarlyLoseLate := []dnsRecord{
		{Name: "a-host.local", Type: TypeA, Class: ClassIN, TTL: 120, A: netip.MustParseAddr("192.0.2.20")},
		{Name: "z-svc._fogcast._tcp.local", Type: TypeSRV, Class: ClassIN, TTL: 120, SRVPort: 8000, SRVTarget: "a-host.local"},
	}
	remoteLoseEarlyWinLate := []dnsRecord{
		{Name: "a-host.local", Type: TypeA, Class: ClassIN, TTL: 120, A: netip.MustParseAddr("192.0.2.10")},
		{Name: "z-svc._fogcast._tcp.local", Type: TypeSRV, Class: ClassIN, TTL: 120, SRVPort: 9000, SRVTarget: "b-host.local"},
	}

	outcomes := compareProbeRecordSets(localWinEarlyLoseLate, remoteLoseEarlyWinLate)
	if outcomes["a-host.local"] <= 0 || outcomes["z-svc._fogcast._tcp.local"] >= 0 {
		t.Fatalf("split outcomes = %+v, want a-host.local > 0 and z-svc._fogcast._tcp.local < 0", outcomes)
	}
	revOutcomes := compareProbeRecordSets(remoteLoseEarlyWinLate, localWinEarlyLoseLate)
	if revOutcomes["a-host.local"] >= 0 || revOutcomes["z-svc._fogcast._tcp.local"] <= 0 {
		t.Fatalf("reverse split outcomes = %+v, want a-host.local < 0 and z-svc._fogcast._tcp.local > 0", revOutcomes)
	}

	// Win on one name + identical record on the second name -> local wins overall.
	remoteSameLate := []dnsRecord{
		{Name: "a-host.local", Type: TypeA, Class: ClassIN, TTL: 120, A: netip.MustParseAddr("192.0.2.10")},
		{Name: "z-svc._fogcast._tcp.local", Type: TypeSRV, Class: ClassIN, TTL: 120, SRVPort: 8000, SRVTarget: "a-host.local"},
	}
	if conflict, localWins := compareProbeRecords(localWinEarlyLoseLate, remoteSameLate); !conflict || !localWins {
		t.Fatalf("win earlier + identical later = conflict:%v localWins:%v, want true/true", conflict, localWins)
	}

	// Identical sets yield 0 for each name and no conflict.
	eq := compareProbeRecordSets(localWinEarlyLoseLate, localWinEarlyLoseLate)
	if eq["a-host.local"] != 0 || eq["z-svc._fogcast._tcp.local"] != 0 {
		t.Fatalf("identical outcomes = %+v, want 0/0", eq)
	}
}

func TestResponderSplitProbeTieBreakConvergesWithoutLivelock(t *testing.T) {
	t.Run("simultaneous-split-claim-winner-and-conflict-loser", func(t *testing.T) {
		hub := newWireHub()
		epA := hub.newEndpoint()
		epB := hub.newEndpoint()
		errA := make(chan error, 4)
		errB := make(chan error, 4)

		// respA has higher IP (wins shared-host.local A) and lower SRV port (loses SplitSvc SRV).
		respA, err := NewResponder(ResponderConfig{
			Port:          epA,
			LocalMAC:      [6]byte{0x02, 0xaa, 0xbb, 0xcc, 0xee, 0x01},
			Service:       Service{Instance: "SplitSvc", ServiceType: "_test._tcp", Host: "shared-host.local", Port: 7000, IP: netip.MustParseAddr("192.0.2.20")},
			AnnounceCount: 1,
			OnError:       func(e error) { errA <- e },
		})
		if err != nil {
			t.Fatal(err)
		}
		respA.initialProbeDelay = 15 * time.Millisecond
		respA.tieBreakDelay = 800 * time.Millisecond

		// respB has lower IP (loses shared-host.local A) and higher SRV port (wins SplitSvc SRV).
		respB, err := NewResponder(ResponderConfig{
			Port:          epB,
			LocalMAC:      [6]byte{0x02, 0xaa, 0xbb, 0xcc, 0xee, 0x02},
			Service:       Service{Instance: "SplitSvc", ServiceType: "_test._tcp", Host: "shared-host.local", Port: 9000, IP: netip.MustParseAddr("192.0.2.10")},
			AnnounceCount: 1,
			OnError:       func(e error) { errB <- e },
		})
		if err != nil {
			t.Fatal(err)
		}
		respB.initialProbeDelay = 15 * time.Millisecond
		respB.tieBreakDelay = 800 * time.Millisecond

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		if err := respA.Start(ctx); err != nil {
			t.Fatal(err)
		}
		defer respA.Stop()
		if err := respB.Start(ctx); err != nil {
			t.Fatal(err)
		}
		defer respB.Stop()

		// Both must claim the name they won without backing off, and then resolve
		// cleanly within 3s without livelocking (either one relinquishes and the
		// survivor claims both names, or both relinquish on each other's defense).
		waitForCondition(t, 2*time.Second, func() bool {
			hostA, _ := respA.claimedNames()
			_, instB := respB.claimedNames()
			return hostA && instB
		}, "split tie-break responders did not independently claim the names they won")

		var settledA, confA, claimA bool
		var settledB, confB, claimB bool
		updateOutcome := func(r *Responder, errCh <-chan error, settled, conf, claim *bool, name string) {
			if *settled {
				return
			}
			select {
			case e := <-errCh:
				if !errors.Is(e, ErrResponderNameConflict) {
					t.Fatalf("%s unexpected error: %v", name, e)
				}
				*settled = true
				*conf = true
				return
			default:
			}
			if r.isClaimed() {
				*settled = true
				*claim = true
			}
		}

		waitForCondition(t, 3*time.Second, func() bool {
			updateOutcome(respA, errA, &settledA, &confA, &claimA, "respA")
			updateOutcome(respB, errB, &settledB, &confB, &claimB, "respB")
			return settledA && settledB
		}, "split tie-break responders did not converge out of probing")
		if claimA && claimB {
			t.Fatal("both responders claimed the same service after split tie-break")
		}
		if !confA && !confB {
			t.Fatalf("expected at least one responder conflict, got A(conf=%v claim=%v) B(conf=%v claim=%v)", confA, claimA, confB, claimB)
		}
	})

	t.Run("one-name-tie-break-loss-preserves-other-name", func(t *testing.T) {
		hub := newWireHub()
		ep := hub.newEndpoint()
		peer := hub.newEndpoint()
		errCh := make(chan error, 4)

		resp, err := NewResponder(ResponderConfig{
			Port:          ep,
			LocalMAC:      [6]byte{0x02, 0xaa, 0xbb, 0xcc, 0xee, 0x11},
			Service:       Service{Instance: "SplitSingle", ServiceType: "_test._tcp", Host: "split-single.local", Port: 7000, IP: netip.MustParseAddr("192.0.2.20")},
			AnnounceCount: 1,
			OnError:       func(e error) { errCh <- e },
		})
		if err != nil {
			t.Fatal(err)
		}
		resp.initialProbeDelay = 10 * time.Millisecond
		resp.tieBreakDelay = 120 * time.Millisecond

		var injected atomic.Bool
		var hostProbes atomic.Int32
		var instOnlyProbes atomic.Int32
		unsub := peer.AddTap(func(raw []byte) {
			pkt, ok := parseMDNSFrame(raw, [6]byte{}, [4]byte{}, true)
			if !ok || pkt.msg.Response || len(pkt.msg.Authorities) == 0 {
				return
			}
			hasHost, hasInst := false, false
			for _, q := range pkt.msg.Questions {
				switch {
				case strings.EqualFold(strings.TrimSuffix(q.Name, "."), "split-single.local"):
					hasHost = true
				case strings.EqualFold(strings.TrimSuffix(q.Name, "."), "splitsingle._test._tcp.local"):
					hasInst = true
				}
			}
			if hasHost {
				hostProbes.Add(1)
			}
			if hasInst && !hasHost {
				instOnlyProbes.Add(1)
			}
			if !injected.CompareAndSwap(false, true) {
				return
			}
			// Lose on instance SRV (port 9000 > 7000) while winning on host A (192.0.2.10 < 192.0.2.20).
			payload, err := encodeMDNSMessage(dnsMessage{
				Questions: pkt.msg.Questions,
				Authorities: []dnsRecord{
					{Name: "split-single.local", Type: TypeA, Class: ClassIN, TTL: 120, A: netip.MustParseAddr("192.0.2.10")},
					{Name: "splitsingle._test._tcp.local", Type: TypeSRV, Class: ClassIN, TTL: 120, SRVPort: 9000, SRVTarget: "split-single.local"},
				},
			})
			if err != nil {
				return
			}
			frame := buildMDNSFrame([6]byte{0x02, 0xaa, 0xbb, 0xcc, 0xee, 0x12}, MulticastMAC(), [4]byte{192, 0, 2, 10}, multicastIPv4Bytes, Port, Port, 4, payload)
			go func() { _ = peer.SendRawFrame(frame) }()
		})
		defer unsub()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		if err := resp.Start(ctx); err != nil {
			t.Fatal(err)
		}
		defer resp.Stop()

		waitForCondition(t, 2*time.Second, func() bool {
			return resp.isClaimed() && resp.Stats().Announcements >= 1
		}, "responder did not finish claiming and announcing after single-name tie-break backoff")

		if got := hostProbes.Load(); got != int32(probeAttempts) {
			t.Fatalf("host probe count = %d, want %d (must not restart on instance tie-break loss)", got, probeAttempts)
		}
		if got := instOnlyProbes.Load(); got < int32(probeAttempts) {
			t.Fatalf("instance-only retry probes = %d, want at least %d", got, probeAttempts)
		}
		select {
		case e := <-errCh:
			t.Fatalf("unexpected responder error: %v", e)
		default:
		}
	})
}

func TestSRVPriorityAndWeightWireAndTieBreak(t *testing.T) {
	srvHighPrio := dnsRecord{
		Name:        "svc._fogcast._tcp.local",
		Type:        TypeSRV,
		Class:       ClassIN,
		TTL:         120,
		SRVPriority: 10,
		SRVWeight:   20,
		SRVPort:     8080,
		SRVTarget:   "host.local",
	}
	encoded, err := encodeMDNSMessage(dnsMessage{
		Authorities: []dnsRecord{srvHighPrio},
	})
	if err != nil {
		t.Fatalf("encodeMDNSMessage(high prio): %v", err)
	}
	decoded, err := decodeMDNSMessage(encoded)
	if err != nil {
		t.Fatalf("decodeMDNSMessage(high prio): %v", err)
	}
	if len(decoded.Authorities) != 1 {
		t.Fatalf("len(Authorities) = %d, want 1", len(decoded.Authorities))
	}
	got := decoded.Authorities[0]
	if got.SRVPriority != 10 || got.SRVWeight != 20 || got.SRVPort != 8080 || got.SRVTarget != "host.local" {
		t.Fatalf("decoded SRV = {prio:%d weight:%d port:%d target:%q}, want {10 20 8080 %q}",
			got.SRVPriority, got.SRVWeight, got.SRVPort, got.SRVTarget, "host.local")
	}

	decodeOneSRV := func(prio, weight uint16) []dnsRecord {
		raw, err := encodeMDNSMessage(dnsMessage{
			Authorities: []dnsRecord{{
				Name:        "svc._fogcast._tcp.local",
				Type:        TypeSRV,
				Class:       ClassIN,
				TTL:         120,
				SRVPriority: prio,
				SRVWeight:   weight,
				SRVPort:     8080,
				SRVTarget:   "host.local",
			}},
		})
		if err != nil {
			t.Fatalf("encodeMDNSMessage: %v", err)
		}
		msg, err := decodeMDNSMessage(raw)
		if err != nil {
			t.Fatalf("decodeMDNSMessage: %v", err)
		}
		return msg.Authorities
	}

	prio0 := decodeOneSRV(0, 0)
	prio10 := decodeOneSRV(10, 0)
	if conflict, localWins := compareProbeRecords(prio0, prio10); !conflict || localWins {
		t.Fatalf("prio 0 vs prio 10 = conflict:%v localWins:%v, want true/false", conflict, localWins)
	}
	if conflict, localWins := compareProbeRecords(prio10, prio0); !conflict || !localWins {
		t.Fatalf("prio 10 vs prio 0 = conflict:%v localWins:%v, want true/true", conflict, localWins)
	}

	weight0 := decodeOneSRV(10, 0)
	weight20 := decodeOneSRV(10, 20)
	if conflict, localWins := compareProbeRecords(weight0, weight20); !conflict || localWins {
		t.Fatalf("weight 0 vs weight 20 = conflict:%v localWins:%v, want true/false", conflict, localWins)
	}
	if conflict, localWins := compareProbeRecords(weight20, weight0); !conflict || !localWins {
		t.Fatalf("weight 20 vs weight 0 = conflict:%v localWins:%v, want true/true", conflict, localWins)
	}
	if conflict, _ := compareProbeRecords(weight20, weight20); conflict {
		t.Fatal("identical SRV priority/weight/port/target reported a conflict")
	}
}

func TestPartialClaimConflictAndStopEmitGoodbye(t *testing.T) {
	t.Run("conflict-while-partially-claimed", func(t *testing.T) {
		hub := newWireHub()
		ep := hub.newEndpoint()
		peer := hub.newEndpoint()
		errCh := make(chan error, 4)

		resp, err := NewResponder(ResponderConfig{
			Port:          ep,
			LocalMAC:      [6]byte{0x02, 0xaa, 0xbb, 0xcc, 0xef, 0x01},
			Service:       Service{Instance: "Partial", ServiceType: "_test._tcp", Host: "partial-host.local", Port: 8080, IP: netip.MustParseAddr("192.0.2.150")},
			AnnounceCount: 1,
			OnError:       func(e error) { errCh <- e },
		})
		if err != nil {
			t.Fatal(err)
		}

		var goodbyes []dnsMessage
		var mu sync.Mutex
		unsub := peer.AddTap(func(raw []byte) {
			pkt, ok := parseMDNSFrame(raw, [6]byte{}, [4]byte{}, true)
			if !ok || !pkt.msg.Response {
				return
			}
			mu.Lock()
			goodbyes = append(goodbyes, pkt.msg)
			mu.Unlock()
		})
		defer unsub()

		resp.running.Store(true)
		resp.nameMu.Lock()
		resp.hostProbe.claimed = true
		resp.instanceProbe.probing = true
		resp.nameMu.Unlock()

		conflictPayload, err := encodeMDNSMessage(dnsMessage{
			Response: true,
			Answers: []dnsRecord{
				{Name: resp.instanceFQDN, Type: TypeSRV, Class: ClassIN, CacheFlush: true, TTL: 120, SRVPort: 9090, SRVTarget: "other-host.local"},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		frame := buildMDNSFrame([6]byte{0x02, 0xaa, 0xbb, 0xcc, 0xef, 0x02}, MulticastMAC(), [4]byte{192, 0, 2, 151}, multicastIPv4Bytes, Port, Port, 1, conflictPayload)
		resp.handleFrame(frame)

		select {
		case e := <-errCh:
			if !errors.Is(e, ErrResponderNameConflict) {
				t.Fatalf("error = %v, want ErrResponderNameConflict", e)
			}
		default:
			t.Fatal("expected ErrResponderNameConflict on probing instance conflict")
		}
		if got := resp.Stats().Goodbyes; got != 1 {
			t.Fatalf("Goodbyes = %d, want 1", got)
		}

		mu.Lock()
		defer mu.Unlock()
		if len(goodbyes) != 1 || len(goodbyes[0].Answers) != 1 {
			t.Fatalf("captured goodbye messages = %+v, want 1 message with 1 A answer", goodbyes)
		}
		rr := goodbyes[0].Answers[0]
		if rr.Type != TypeA || rr.TTL != 0 || !strings.EqualFold(strings.TrimSuffix(rr.Name, "."), "partial-host.local") {
			t.Fatalf("unexpected partial goodbye record: %+v", rr)
		}
	})

	t.Run("stop-while-partially-claimed", func(t *testing.T) {
		hub := newWireHub()
		ep := hub.newEndpoint()
		peer := hub.newEndpoint()
		errCh := make(chan error, 4)

		resp, err := NewResponder(ResponderConfig{
			Port:          ep,
			LocalMAC:      [6]byte{0x02, 0xaa, 0xbb, 0xcc, 0xef, 0x11},
			Service:       Service{Instance: "PartialStop", ServiceType: "_test._tcp", Host: "partial-stop.local", Port: 7000, IP: netip.MustParseAddr("192.0.2.160")},
			AnnounceCount: 1,
			OnError:       func(e error) { errCh <- e },
		})
		if err != nil {
			t.Fatal(err)
		}
		resp.initialProbeDelay = 10 * time.Millisecond
		resp.tieBreakDelay = 3 * time.Second

		var injected atomic.Bool
		var responses []dnsMessage
		var mu sync.Mutex
		unsub := peer.AddTap(func(raw []byte) {
			pkt, ok := parseMDNSFrame(raw, [6]byte{}, [4]byte{}, true)
			if !ok {
				return
			}
			if pkt.msg.Response {
				mu.Lock()
				responses = append(responses, pkt.msg)
				mu.Unlock()
				return
			}
			if len(pkt.msg.Authorities) == 0 || !injected.CompareAndSwap(false, true) {
				return
			}
			// Beat instanceFQDN (9000 > 7000) while losing hostFQDN (192.0.2.10 < 192.0.2.160).
			payload, err := encodeMDNSMessage(dnsMessage{
				Questions: pkt.msg.Questions,
				Authorities: []dnsRecord{
					{Name: "partial-stop.local", Type: TypeA, Class: ClassIN, TTL: 120, A: netip.MustParseAddr("192.0.2.10")},
					{Name: "partialstop._test._tcp.local", Type: TypeSRV, Class: ClassIN, TTL: 120, SRVPort: 9000, SRVTarget: "partial-stop.local"},
				},
			})
			if err != nil {
				return
			}
			frame := buildMDNSFrame([6]byte{0x02, 0xaa, 0xbb, 0xcc, 0xef, 0x12}, MulticastMAC(), [4]byte{192, 0, 2, 10}, multicastIPv4Bytes, Port, Port, 2, payload)
			go func() { _ = peer.SendRawFrame(frame) }()
		})
		defer unsub()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		if err := resp.Start(ctx); err != nil {
			t.Fatal(err)
		}

		waitForCondition(t, 2*time.Second, func() bool {
			hostClaimed, instClaimed := resp.claimedNames()
			return hostClaimed && !instClaimed
		}, "responder did not reach partial-claim state (host only)")

		resp.Stop()

		if got := resp.Stats().Goodbyes; got != 1 {
			t.Fatalf("Goodbyes on partial Stop = %d, want 1", got)
		}
		mu.Lock()
		defer mu.Unlock()
		if len(responses) != 1 || len(responses[0].Answers) != 1 {
			t.Fatalf("captured responses = %+v, want 1 partial-A goodbye", responses)
		}
		rr := responses[0].Answers[0]
		if rr.Type != TypeA || rr.TTL != 0 || !strings.EqualFold(strings.TrimSuffix(rr.Name, "."), "partial-stop.local") {
			t.Fatalf("unexpected partial Stop goodbye record: %+v", rr)
		}
		select {
		case e := <-errCh:
			t.Fatalf("unexpected responder error: %v", e)
		default:
		}
	})
}

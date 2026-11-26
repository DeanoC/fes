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
	if err := <-probeErr; !errors.Is(err, ErrResponderNameConflict) {
		t.Fatalf("probe conflict error = %v", err)
	}
	probing.nameMu.Lock()
	probeRelinquished := probing.conflicted
	probing.nameMu.Unlock()
	if !probeRelinquished {
		t.Fatal("responder did not relinquish a losing probe")
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

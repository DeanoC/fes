// SPDX-License-Identifier: GPL-3.0-or-later

package network

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	gnet "github.com/usbarmory/go-net"
)

type fakeRawPort struct {
	mu      sync.Mutex
	nextID  uint64
	taps    map[uint64]func([]byte)
	onSend  func(frame []byte, p *fakeRawPort)
	sendErr error
	portErr error
}

func newFakeRawPort() *fakeRawPort {
	return &fakeRawPort{taps: make(map[uint64]func([]byte))}
}

func (p *fakeRawPort) SendRawFrame(frame []byte) error {
	cp := append([]byte(nil), frame...)
	p.mu.Lock()
	if p.sendErr != nil {
		err := p.sendErr
		p.mu.Unlock()
		return err
	}
	hook := p.onSend
	p.mu.Unlock()
	if hook != nil {
		hook(cp, p)
	}
	return nil
}

func (p *fakeRawPort) AddTap(fn func([]byte)) func() {
	p.mu.Lock()
	p.nextID++
	id := p.nextID
	p.taps[id] = fn
	p.mu.Unlock()
	return func() {
		p.mu.Lock()
		delete(p.taps, id)
		p.mu.Unlock()
	}
}

func (p *fakeRawPort) inject(frame []byte) {
	cp := append([]byte(nil), frame...)
	p.mu.Lock()
	cbs := make([]func([]byte), 0, len(p.taps))
	for _, fn := range p.taps {
		cbs = append(cbs, fn)
	}
	p.mu.Unlock()
	for _, fn := range cbs {
		fn(cp)
	}
}

func (p *fakeRawPort) Err() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.portErr
}

func makeARPReplyFrame(dstMAC, senderMAC [6]byte, senderIP, targetIP [4]byte) []byte {
	frame := make([]byte, 60)
	copy(frame[0:6], dstMAC[:])
	copy(frame[6:12], senderMAC[:])
	binary.BigEndian.PutUint16(frame[12:14], ethTypeARP)
	arp := frame[ethHeaderLen : ethHeaderLen+arpHeaderLen]
	binary.BigEndian.PutUint16(arp[0:2], 1)
	binary.BigEndian.PutUint16(arp[2:4], ethTypeIPv4)
	arp[4] = 6
	arp[5] = 4
	binary.BigEndian.PutUint16(arp[6:8], arpOpReply)
	copy(arp[8:14], senderMAC[:])
	copy(arp[14:18], senderIP[:])
	copy(arp[18:24], dstMAC[:])
	copy(arp[24:28], targetIP[:])
	return frame
}

func makeDNSResponsePayload(id uint16, flags uint16, qname string, cnames [][2]string, aRecs []struct {
	name string
	ip   [4]byte
}, usePointerForFirstA bool) []byte {
	var b bytes.Buffer
	var hdr [12]byte
	binary.BigEndian.PutUint16(hdr[0:2], id)
	binary.BigEndian.PutUint16(hdr[2:4], flags)
	binary.BigEndian.PutUint16(hdr[4:6], 1)
	binary.BigEndian.PutUint16(hdr[6:8], uint16(len(cnames)+len(aRecs)))
	b.Write(hdr[:])

	qWire, _ := encodeDNSName(qname)
	b.Write(qWire)
	b.Write([]byte{0, dnsTypeA, 0, dnsClassIN})

	for _, cn := range cnames {
		nWire, _ := encodeDNSName(cn[0])
		tWire, _ := encodeDNSName(cn[1])
		b.Write(nWire)
		var rr [10]byte
		binary.BigEndian.PutUint16(rr[0:2], dnsTypeCNAME)
		binary.BigEndian.PutUint16(rr[2:4], dnsClassIN)
		binary.BigEndian.PutUint32(rr[4:8], 60)
		binary.BigEndian.PutUint16(rr[8:10], uint16(len(tWire)))
		b.Write(rr[:])
		b.Write(tWire)
	}
	for i, a := range aRecs {
		if i == 0 && usePointerForFirstA {
			// Compression pointer to offset 12 (QNAME).
			b.Write([]byte{0xc0, 0x0c})
		} else {
			nWire, _ := encodeDNSName(a.name)
			b.Write(nWire)
		}
		var rr [10]byte
		binary.BigEndian.PutUint16(rr[0:2], dnsTypeA)
		binary.BigEndian.PutUint16(rr[2:4], dnsClassIN)
		binary.BigEndian.PutUint32(rr[4:8], 60)
		binary.BigEndian.PutUint16(rr[8:10], 4)
		b.Write(rr[:])
		b.Write(a.ip[:])
	}
	return b.Bytes()
}

func TestDNSWireEncodingAndResponseParsing(t *testing.T) {
	q, err := EncodeDNSQuery(0x1234, "FogCast.Local.")
	if err != nil {
		t.Fatalf("EncodeDNSQuery: %v", err)
	}
	if binary.BigEndian.Uint16(q[0:2]) != 0x1234 || binary.BigEndian.Uint16(q[2:4]) != 0x0100 {
		t.Fatalf("unexpected query header: %x", q[:12])
	}
	for _, bad := range []string{"", ".", "..", "bad host", "a..b", strings.Repeat("a", 64) + ".com", strings.Repeat("a.", 130) + "com"} {
		if _, err := EncodeDNSQuery(1, bad); !errors.Is(err, ErrDNSInvalidConfig) {
			t.Errorf("EncodeDNSQuery(%q) err=%v, want ErrDNSInvalidConfig", bad, err)
		}
	}

	// Compression pointer + multiple A records.
	resp := makeDNSResponsePayload(0x2222, 0x8180, "fogcast.local", nil, []struct {
		name string
		ip   [4]byte
	}{
		{"fogcast.local", [4]byte{192, 168, 1, 10}},
		{"fogcast.local", [4]byte{192, 168, 1, 11}},
	}, true)
	addrs, err := ParseDNSResponse(resp, 0x2222, "FOGCAST.LOCAL.")
	if err != nil || len(addrs) != 2 || addrs[0].String() != "192.168.1.10" || addrs[1].String() != "192.168.1.11" {
		t.Fatalf("ParseDNSResponse multi-A = (%v, %v)", addrs, err)
	}

	// CNAME chain resolution.
	cnameResp := makeDNSResponsePayload(0x3333, 0x8180, "app.example.test",
		[][2]string{{"app.example.test", "edge.example.test"}},
		[]struct {
			name string
			ip   [4]byte
		}{{"edge.example.test", [4]byte{10, 20, 30, 40}}}, false)
	addrs, err = ParseDNSResponse(cnameResp, 0x3333, "app.example.test")
	if err != nil || len(addrs) != 1 || addrs[0].String() != "10.20.30.40" {
		t.Fatalf("ParseDNSResponse CNAME = (%v, %v)", addrs, err)
	}

	// Error RCODEs and truncation.
	nx := makeDNSResponsePayload(0x4444, 0x8183, "missing.test", nil, nil, false)
	if _, err := ParseDNSResponse(nx, 0x4444, "missing.test"); !errors.Is(err, ErrDNSNotFound) {
		t.Fatalf("NXDOMAIN err=%v", err)
	}
	sf := makeDNSResponsePayload(0x4444, 0x8182, "fail.test", nil, nil, false)
	if _, err := ParseDNSResponse(sf, 0x4444, "fail.test"); !errors.Is(err, ErrDNSServerError) {
		t.Fatalf("SERVFAIL err=%v", err)
	}
	tc := makeDNSResponsePayload(0x4444, 0x8380, "trunc.test", nil, nil, false)
	if _, err := ParseDNSResponse(tc, 0x4444, "trunc.test"); !errors.Is(err, ErrDNSTruncated) {
		t.Fatalf("TC err=%v", err)
	}
	empty := makeDNSResponsePayload(0x4444, 0x8180, "empty.test", nil, nil, false)
	if _, err := ParseDNSResponse(empty, 0x4444, "empty.test"); !errors.Is(err, ErrDNSNoRecords) {
		t.Fatalf("no-records err=%v", err)
	}

	// Self-referential compression pointer loop must be rejected cleanly without hanging.
	loopMsg := make([]byte, 20)
	binary.BigEndian.PutUint16(loopMsg[0:2], 0x5555)
	binary.BigEndian.PutUint16(loopMsg[2:4], 0x8180)
	binary.BigEndian.PutUint16(loopMsg[4:6], 1)
	loopMsg[12], loopMsg[13] = 0xc0, 0x0c // points to itself!
	if _, err := ParseDNSResponse(loopMsg, 0x5555, "loop.test"); err == nil {
		t.Fatal("compression pointer loop was accepted")
	}
	if _, err := ParseDNSResponse(resp, 0x9999, "fogcast.local"); err == nil {
		t.Fatal("mismatched ID accepted")
	}
	if _, err := ParseDNSResponse(resp, 0x2222, "other.local"); err == nil {
		t.Fatal("mismatched question accepted")
	}
	if _, err := ParseDNSResponse(q, 0x1234, "fogcast.local"); err == nil {
		t.Fatal("query packet accepted as response")
	}
	if _, err := ParseDNSResponse(resp[:8], 0x2222, "fogcast.local"); err == nil {
		t.Fatal("short packet accepted")
	}
}

func TestResolveAEndToEndARPAndServerFailover(t *testing.T) {
	clientMAC := [6]byte{0x52, 0x54, 0x00, 0x12, 0x34, 0x56}
	gwMAC := [6]byte{0x52, 0x55, 0x0a, 0x00, 0x02, 0x02}
	clientIP := [4]byte{10, 0, 2, 15}
	gwIP := [4]byte{10, 0, 2, 2}
	badDNS := [4]byte{10, 0, 2, 3}
	goodDNS := [4]byte{8, 8, 8, 8} // off-subnet -> must ARP for gateway 10.0.2.2!

	port := newFakeRawPort()
	var mu sync.Mutex
	dnsAttempts := 0
	port.onSend = func(frame []byte, p *fakeRawPort) {
		ethType := binary.BigEndian.Uint16(frame[12:14])
		if ethType == ethTypeARP {
			arp := frame[ethHeaderLen : ethHeaderLen+arpHeaderLen]
			var target [4]byte
			copy(target[:], arp[24:28])
			if target == gwIP {
				go p.inject(makeARPReplyFrame(clientMAC, gwMAC, gwIP, clientIP))
			}
			// Drop ARP for badDNS (10.0.2.3) so primary server times out and fails over!
			return
		}
		if ethType == ethTypeIPv4 {
			ip := frame[ethHeaderLen : ethHeaderLen+ipv4MinLen]
			var dstIP [4]byte
			copy(dstIP[:], ip[16:20])
			if dstIP != goodDNS {
				return
			}
			udp := frame[ethHeaderLen+ipv4MinLen:]
			srcPort := binary.BigEndian.Uint16(udp[0:2])
			dnsPayload := udp[udpHeaderLen:]
			id := binary.BigEndian.Uint16(dnsPayload[0:2])

			mu.Lock()
			dnsAttempts++
			attempt := dnsAttempts
			mu.Unlock()
			if attempt == 1 {
				// Drop first query to exercise retransmission, plus inject wrong-ID noise.
				noise := makeDNSResponsePayload(id^1, 0x8180, "fes.example", nil, []struct {
					name string
					ip   [4]byte
				}{{"fes.example", [4]byte{1, 2, 3, 4}}}, false)
				go p.inject(buildUDPv4Frame(gwMAC, clientMAC, goodDNS, clientIP, dnsPort, srcPort, 1, noise))
				return
			}
			reply := makeDNSResponsePayload(id, 0x8180, "fes.example", nil, []struct {
				name string
				ip   [4]byte
			}{{"fes.example", [4]byte{198, 51, 100, 7}}}, true)
			go p.inject(buildUDPv4Frame(gwMAC, clientMAC, goodDNS, clientIP, dnsPort, srcPort, 2, reply))
		}
	}

	// IPv4 literal short-circuits without hitting the wire.
	lit, err := ResolveA(context.Background(), port, DNSConfig{}, "127.0.0.1")
	if err != nil || len(lit) != 1 || lit[0].String() != "127.0.0.1" {
		t.Fatalf("literal ResolveA = (%v, %v)", lit, err)
	}
	if _, err := ResolveA(context.Background(), port, DNSConfig{}, "::1"); !errors.Is(err, ErrDNSInvalidConfig) {
		t.Fatalf("IPv6 literal err=%v, want ErrDNSInvalidConfig", err)
	}

	// Full resolution: primary DNS (10.0.2.3) times out on ARP -> fails over to 8.8.8.8 via ARP(10.0.2.2) + retransmit.
	addrs, err := ResolveA(context.Background(), port, DNSConfig{
		LocalMAC:      "52:54:00:12:34:56",
		LocalPrefix:   "10.0.2.15/24",
		Gateway:       "10.0.2.2",
		Servers:       []string{netip.AddrFrom4(badDNS).String(), netip.AddrFrom4(goodDNS).String()},
		Timeout:       200 * time.Millisecond,
		RetryInterval: 5 * time.Millisecond,
	}, "fes.example")
	if err != nil {
		t.Fatalf("ResolveA failover: %v", err)
	}
	if len(addrs) != 1 || addrs[0].String() != "198.51.100.7" {
		t.Fatalf("ResolveA addrs=%v, want [198.51.100.7]", addrs)
	}
	if dnsAttempts < 2 {
		t.Fatalf("expected DNS retransmission, got attempts=%d", dnsAttempts)
	}
}

func TestResolveAValidationAndLinkRawTapAndPause(t *testing.T) {
	port := newFakeRawPort()
	if _, err := ResolveA(nil, port, DNSConfig{}, "a.test"); !errors.Is(err, ErrDNSInvalidConfig) {
		t.Fatalf("nil ctx err=%v", err)
	}
	if _, err := ResolveA(context.Background(), nil, DNSConfig{}, "a.test"); !errors.Is(err, ErrDNSInvalidConfig) {
		t.Fatalf("nil port err=%v", err)
	}
	for _, cfg := range []DNSConfig{
		{LocalMAC: "bad", LocalPrefix: "10.0.2.15/24", Servers: []string{"10.0.2.3"}},
		{LocalMAC: "52:54:00:12:34:56", LocalPrefix: "bad", Servers: []string{"10.0.2.3"}},
		{LocalMAC: "52:54:00:12:34:56", LocalPrefix: "10.0.2.15/24", Gateway: "10.9.9.1", Servers: []string{"10.0.2.3"}},
		{LocalMAC: "52:54:00:12:34:56", LocalPrefix: "10.0.2.15/24"},
		{LocalMAC: "52:54:00:12:34:56", LocalPrefix: "10.0.2.15/24", Servers: []string{"not-ip"}},
		{LocalMAC: "52:54:00:12:34:56", LocalPrefix: "10.0.2.15/24", Servers: []string{"10.0.2.3"}, NextHopMAC: "bad"},
		{LocalMAC: "52:54:00:12:34:56", LocalPrefix: "10.0.2.15/24", Servers: []string{"10.0.2.3"}, Timeout: -time.Second},
		{LocalMAC: "52:54:00:12:34:56", LocalPrefix: "10.0.2.15/24", Servers: []string{"8.8.8.8"}},
	} {
		if _, err := ResolveA(context.Background(), port, cfg, "a.test"); !errors.Is(err, ErrDNSInvalidConfig) {
			t.Errorf("cfg %+v err=%v, want ErrDNSInvalidConfig", cfg, err)
		}
	}

	// Verify Link.SendRawFrame respects ErrPaused and Link.AddTap delivers inbound copies.
	dev := newFakeDHCPDevice("52:54:00:12:34:56", false)
	link := &Link{stack: &fakeStack{}, device: dev, state: &linkState{}}
	if err := link.SendRawFrame(make([]byte, 10)); err == nil {
		t.Fatal("short raw frame accepted")
	}
	if err := link.SendRawFrame(make([]byte, 60)); err != nil {
		t.Fatalf("SendRawFrame active: %v", err)
	}
	link.state.paused.Store(true)
	if err := link.SendRawFrame(make([]byte, 60)); !errors.Is(err, ErrPaused) {
		t.Fatalf("SendRawFrame paused err=%v, want ErrPaused", err)
	}
	link.state.paused.Store(false)

	gotTap := make(chan []byte, 1)
	remove := link.AddTap(func(frame []byte) { gotTap <- frame })
	dev.enqueueRX(bytes.Repeat([]byte{0xab}, 60))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- link.Start(ctx) }()
	select {
	case f := <-gotTap:
		if len(f) != 60 || f[0] != 0xab {
			t.Fatalf("unexpected tap frame len=%d b0=%x", len(f), f[0])
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for Link.AddTap delivery")
	}
	remove()
	remove() // idempotent
	cancel()
	<-done
}

type fakeStack struct{ gnet.Stack }

func (*fakeStack) RecvInboundPacket([]byte) error { return nil }

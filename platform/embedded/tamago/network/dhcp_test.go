// SPDX-License-Identifier: GPL-3.0-or-later

package network

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeDHCPDevice struct {
	mu      sync.Mutex
	mac     string
	rxQueue [][]byte
	txLog   [][]byte
	onTx    func(frame []byte, dev *fakeDHCPDevice)
	txErr   error
	rxErr   error
	devErr  error
	ready   chan struct{}
}

func newFakeDHCPDevice(mac string, withIRQ bool) *fakeDHCPDevice {
	d := &fakeDHCPDevice{mac: mac}
	if withIRQ {
		d.ready = make(chan struct{}, 1)
	}
	return d
}

func (d *fakeDHCPDevice) enqueueRX(frame []byte) {
	cp := append([]byte(nil), frame...)
	d.mu.Lock()
	d.rxQueue = append(d.rxQueue, cp)
	ch := d.ready
	d.mu.Unlock()
	if ch != nil {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func (d *fakeDHCPDevice) RXReady() <-chan struct{} { return d.ready }

func (d *fakeDHCPDevice) Receive(dst []byte) (int, error) {
	d.mu.Lock()
	if d.rxErr != nil {
		err := d.rxErr
		d.mu.Unlock()
		return 0, err
	}
	if len(d.rxQueue) == 0 {
		d.mu.Unlock()
		return 0, nil
	}
	frame := d.rxQueue[0]
	d.rxQueue = d.rxQueue[1:]
	d.mu.Unlock()
	n := copy(dst, frame)
	return n, nil
}

func (d *fakeDHCPDevice) Transmit(frame []byte) error {
	cp := append([]byte(nil), frame...)
	d.mu.Lock()
	if d.txErr != nil {
		err := d.txErr
		d.mu.Unlock()
		return err
	}
	d.txLog = append(d.txLog, cp)
	hook := d.onTx
	d.mu.Unlock()
	if hook != nil {
		hook(cp, d)
	}
	return nil
}

func (d *fakeDHCPDevice) Err() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.devErr
}

func (d *fakeDHCPDevice) MAC() string       { return d.mac }
func (d *fakeDHCPDevice) Statistics() Stats { return Stats{} }

func extractClientDHCP(t *testing.T, frame []byte) (msgType byte, xid uint32, mac [6]byte, reqIP, srvID [4]byte, hostname string) {
	t.Helper()
	if len(frame) < ethHeaderLen+ipv4MinLen+udpHeaderLen+dhcpMinPayload {
		t.Fatalf("short client frame: %d", len(frame))
	}
	ip := frame[ethHeaderLen : ethHeaderLen+ipv4MinLen]
	if internetChecksum(ip) != 0 {
		t.Fatalf("invalid client IPv4 header checksum")
	}
	udpLen := int(binary.BigEndian.Uint16(frame[ethHeaderLen+ipv4MinLen+4 : ethHeaderLen+ipv4MinLen+6]))
	udp := frame[ethHeaderLen+ipv4MinLen : ethHeaderLen+ipv4MinLen+udpLen]
	if udpChecksum(ip[12:16], ip[16:20], udp) != 0 {
		t.Fatalf("invalid client UDP checksum")
	}
	bootp := udp[udpHeaderLen:]
	if bootp[0] != bootpRequest || bootp[1] != 1 || bootp[2] != 6 {
		t.Fatalf("unexpected bootp header: op=%d htype=%d hlen=%d", bootp[0], bootp[1], bootp[2])
	}
	xid = binary.BigEndian.Uint32(bootp[4:8])
	copy(mac[:], bootp[28:34])
	opts := bootp[dhcpMinPayload:]
	for i := 0; i < len(opts); {
		code := opts[i]
		if code == dhcpOptPad {
			i++
			continue
		}
		if code == dhcpOptEnd {
			break
		}
		l := int(opts[i+1])
		val := opts[i+2 : i+2+l]
		switch code {
		case dhcpOptMsgType:
			msgType = val[0]
		case dhcpOptRequestedIP:
			copy(reqIP[:], val)
		case dhcpOptServerID:
			copy(srvID[:], val)
		case dhcpOptHostname:
			hostname = string(val)
		}
		i += 2 + l
	}
	return
}

func makeServerReplyFrame(dstMAC, clientMAC [6]byte, srcIP, yiaddr [4]byte, xid uint32, opts []byte, zeroUDPChecksum bool) []byte {
	dhcpLen := dhcpMinPayload + len(opts)
	udpLen := udpHeaderLen + dhcpLen
	ipLen := ipv4MinLen + udpLen
	frame := make([]byte, ethHeaderLen+ipLen)

	copy(frame[0:6], dstMAC[:])
	copy(frame[6:12], []byte{0x52, 0x55, 0x0a, 0x00, 0x02, 0x02})
	binary.BigEndian.PutUint16(frame[12:14], ethTypeIPv4)

	ip := frame[ethHeaderLen : ethHeaderLen+ipv4MinLen]
	ip[0] = 0x45
	binary.BigEndian.PutUint16(ip[2:4], uint16(ipLen))
	ip[8] = 64
	ip[9] = ipProtoUDP
	copy(ip[12:16], srcIP[:])
	copy(ip[16:20], []byte{255, 255, 255, 255})
	binary.BigEndian.PutUint16(ip[10:12], internetChecksum(ip))

	udp := frame[ethHeaderLen+ipv4MinLen:]
	binary.BigEndian.PutUint16(udp[0:2], dhcpServerPort)
	binary.BigEndian.PutUint16(udp[2:4], dhcpClientPort)
	binary.BigEndian.PutUint16(udp[4:6], uint16(udpLen))

	bootp := udp[udpHeaderLen:]
	bootp[0] = bootpReply
	bootp[1] = 1
	bootp[2] = 6
	binary.BigEndian.PutUint32(bootp[4:8], xid)
	copy(bootp[16:20], yiaddr[:])
	copy(bootp[28:34], clientMAC[:])
	copy(bootp[bootpFixedLen:dhcpMinPayload], dhcpMagicCookie[:])
	copy(bootp[dhcpMinPayload:], opts)

	if !zeroUDPChecksum {
		csum := udpChecksum(ip[12:16], ip[16:20], udp)
		if csum == 0 {
			csum = 0xffff
		}
		binary.BigEndian.PutUint16(udp[6:8], csum)
	}
	return frame
}

func makeDHCPOptions(msgType byte, mask, router, dns, serverID *[4]byte, leaseSecs uint32) []byte {
	var b bytes.Buffer
	b.Write([]byte{dhcpOptMsgType, 1, msgType})
	if mask != nil {
		b.Write([]byte{dhcpOptSubnetMask, 4})
		b.Write(mask[:])
	}
	if router != nil {
		b.Write([]byte{dhcpOptRouter, 4})
		b.Write(router[:])
	}
	if dns != nil {
		b.Write([]byte{dhcpOptDNS, 4})
		b.Write(dns[:])
	}
	if serverID != nil {
		b.Write([]byte{dhcpOptServerID, 4})
		b.Write(serverID[:])
	}
	if leaseSecs > 0 {
		var t [4]byte
		binary.BigEndian.PutUint32(t[:], leaseSecs)
		b.Write([]byte{dhcpOptLeaseTime, 4})
		b.Write(t[:])
	}
	b.WriteByte(dhcpOptPad)
	b.WriteByte(dhcpOptEnd)
	return b.Bytes()
}

func TestAcquireDHCPHandshakeAndIRQWait(t *testing.T) {
	macStr := "52:54:00:12:34:56"
	hw, _ := net.ParseMAC(macStr)
	var clientMAC [6]byte
	copy(clientMAC[:], hw)

	serverIP := [4]byte{10, 0, 2, 2}
	offeredIP := [4]byte{10, 0, 2, 15}
	mask := [4]byte{255, 255, 255, 0}
	router := [4]byte{10, 0, 2, 2}
	dns := [4]byte{10, 0, 2, 3}

	for _, withIRQ := range []bool{false, true} {
		dev := newFakeDHCPDevice(macStr, withIRQ)
		dev.onTx = func(frame []byte, d *fakeDHCPDevice) {
			msgType, xid, mac, reqIP, srvID, hostname := extractClientDHCP(t, frame)
			if mac != clientMAC {
				t.Errorf("tx mac=%x, want %x", mac, clientMAC)
			}
			if hostname != "fes-test" {
				t.Errorf("tx hostname=%q, want fes-test", hostname)
			}
			switch msgType {
			case dhcpMsgDiscover:
				// Enqueue a stray unrelated frame (wrong XID) followed by valid OFFER.
				bad := makeServerReplyFrame(broadcastMAC, clientMAC, serverIP, offeredIP, xid^1,
					makeDHCPOptions(dhcpMsgOffer, &mask, &router, &dns, &serverIP, 86400), false)
				good := makeServerReplyFrame(broadcastMAC, clientMAC, serverIP, offeredIP, xid,
					makeDHCPOptions(dhcpMsgOffer, &mask, &router, &dns, &serverIP, 86400), false)
				go func() {
					time.Sleep(time.Millisecond)
					d.enqueueRX(bad)
					d.enqueueRX(good)
				}()
			case dhcpMsgRequest:
				if reqIP != offeredIP || srvID != serverIP {
					t.Errorf("request mismatch reqIP=%v srvID=%v", reqIP, srvID)
				}
				// Use zero UDP checksum (valid per RFC 768) and unicast dst MAC.
				ack := makeServerReplyFrame(clientMAC, clientMAC, serverIP, offeredIP, xid,
					makeDHCPOptions(dhcpMsgAck, &mask, &router, &dns, &serverIP, 86400), true)
				go func() {
					time.Sleep(time.Millisecond)
					d.enqueueRX(ack)
				}()
			default:
				t.Errorf("unexpected client DHCP message type %d", msgType)
			}
		}

		lease, err := AcquireDHCP(context.Background(), dev, DHCPConfig{
			Hostname:        "fes-test",
			InitialInterval: 5 * time.Millisecond,
			MaxInterval:     20 * time.Millisecond,
			Timeout:         time.Second,
		})
		if err != nil {
			t.Fatalf("withIRQ=%t AcquireDHCP: %v", withIRQ, err)
		}
		if lease.Address != "10.0.2.15/24" || lease.Gateway != "10.0.2.2" || lease.Server != "10.0.2.2" ||
			len(lease.DNS) != 1 || lease.DNS[0] != "10.0.2.3" || lease.LeaseDuration != 24*time.Hour {
			t.Fatalf("withIRQ=%t unexpected lease: %+v", withIRQ, lease)
		}
	}
}

func TestAcquireDHCPRetransmitNAKAndMissingRouterRejection(t *testing.T) {
	macStr := "52:54:00:aa:bb:cc"
	hw, _ := net.ParseMAC(macStr)
	var clientMAC [6]byte
	copy(clientMAC[:], hw)

	serverIP := [4]byte{192, 168, 1, 1}
	routerIP := [4]byte{192, 168, 1, 254}
	firstIP := [4]byte{192, 168, 1, 50}
	secondIP := [4]byte{192, 168, 1, 51}
	mask := [4]byte{255, 255, 255, 0}

	var mu sync.Mutex
	discovers := 0
	requests := 0

	dev := newFakeDHCPDevice(macStr, true)
	dev.onTx = func(frame []byte, d *fakeDHCPDevice) {
		msgType, xid, _, _, _, _ := extractClientDHCP(t, frame)
		mu.Lock()
		defer mu.Unlock()
		switch msgType {
		case dhcpMsgDiscover:
			discovers++
			if discovers == 1 {
				// First offer omits option 3 router -> must be rejected, forcing retransmit.
				d.enqueueRX(makeServerReplyFrame(broadcastMAC, clientMAC, serverIP, firstIP, xid,
					makeDHCPOptions(dhcpMsgOffer, &mask, nil, nil, &serverIP, 3600), false))
				return
			}
			ip := firstIP
			if discovers >= 3 {
				ip = secondIP
			}
			offer := makeServerReplyFrame(broadcastMAC, clientMAC, serverIP, ip, xid,
				makeDHCPOptions(dhcpMsgOffer, &mask, &routerIP, nil, &serverIP, 3600), false)
			d.enqueueRX(offer)
		case dhcpMsgRequest:
			requests++
			if requests == 1 {
				// Send NAK from another server first (must be ignored), then NAK from selected server (must restart).
				otherSrv := [4]byte{192, 168, 1, 99}
				d.enqueueRX(makeServerReplyFrame(broadcastMAC, clientMAC, otherSrv, firstIP, xid,
					makeDHCPOptions(dhcpMsgNak, nil, nil, nil, &otherSrv, 0), false))
				d.enqueueRX(makeServerReplyFrame(broadcastMAC, clientMAC, serverIP, firstIP, xid,
					makeDHCPOptions(dhcpMsgNak, nil, nil, nil, &serverIP, 0), false))
				return
			}
			// Also enqueue mismatched-yiaddr ACK (must be ignored) before valid ACK.
			d.enqueueRX(makeServerReplyFrame(broadcastMAC, clientMAC, serverIP, firstIP, xid,
				makeDHCPOptions(dhcpMsgAck, &mask, &routerIP, nil, &serverIP, 3600), false))
			d.enqueueRX(makeServerReplyFrame(broadcastMAC, clientMAC, serverIP, secondIP, xid,
				makeDHCPOptions(dhcpMsgAck, &mask, &routerIP, nil, &serverIP, 3600), false))
		}
	}

	lease, err := AcquireDHCP(context.Background(), dev, DHCPConfig{
		InitialInterval: 3 * time.Millisecond,
		MaxInterval:     6 * time.Millisecond,
		Timeout:         time.Second,
	})
	if err != nil {
		t.Fatalf("AcquireDHCP after retransmit+NAK: %v", err)
	}
	if lease.Address != "192.168.1.51/24" || lease.Gateway != "192.168.1.254" || lease.Server != "192.168.1.1" {
		t.Fatalf("unexpected lease: %+v", lease)
	}
}

func TestAcquireDHCPContinuousNoiseDoesNotStarveRetransmit(t *testing.T) {
	macStr := "52:54:00:de:ad:01"
	hw, _ := net.ParseMAC(macStr)
	var clientMAC [6]byte
	copy(clientMAC[:], hw)

	serverIP := [4]byte{10, 0, 2, 2}
	offeredIP := [4]byte{10, 0, 2, 15}
	mask := [4]byte{255, 255, 255, 0}

	// Custom noisy device that always returns a stray non-DHCP Ethernet frame when queue is empty.
	base := newFakeDHCPDevice(macStr, false)
	stray := make([]byte, 64)
	copy(stray[0:6], broadcastMAC[:])
	binary.BigEndian.PutUint16(stray[12:14], 0x0806) // ARP

	var mu sync.Mutex
	discovers := 0
	base.onTx = func(frame []byte, d *fakeDHCPDevice) {
		msgType, xid, _, _, _, _ := extractClientDHCP(t, frame)
		mu.Lock()
		defer mu.Unlock()
		switch msgType {
		case dhcpMsgDiscover:
			discovers++
			if discovers >= 2 {
				d.enqueueRX(makeServerReplyFrame(broadcastMAC, clientMAC, serverIP, offeredIP, xid,
					makeDHCPOptions(dhcpMsgOffer, &mask, &serverIP, nil, &serverIP, 600), false))
			}
		case dhcpMsgRequest:
			d.enqueueRX(makeServerReplyFrame(broadcastMAC, clientMAC, serverIP, offeredIP, xid,
				makeDHCPOptions(dhcpMsgAck, &mask, &serverIP, nil, &serverIP, 600), false))
		}
	}

	noisy := &noisyDHCPDevice{fakeDHCPDevice: base, stray: stray}
	lease, err := AcquireDHCP(context.Background(), noisy, DHCPConfig{
		InitialInterval: 4 * time.Millisecond,
		MaxInterval:     8 * time.Millisecond,
		Timeout:         time.Second,
	})
	if err != nil {
		t.Fatalf("AcquireDHCP under continuous noise: %v", err)
	}
	if lease.Address != "10.0.2.15/24" || discovers < 2 {
		t.Fatalf("lease=%+v discovers=%d", lease, discovers)
	}
}

type noisyDHCPDevice struct {
	*fakeDHCPDevice
	stray []byte
}

func (n *noisyDHCPDevice) Receive(dst []byte) (int, error) {
	got, err := n.fakeDHCPDevice.Receive(dst)
	if err != nil || got > 0 {
		return got, err
	}
	return copy(dst, n.stray), nil
}

func TestAcquireDHCPRequestExhaustionRestartsDiscover(t *testing.T) {
	macStr := "52:54:00:01:02:03"
	hw, _ := net.ParseMAC(macStr)
	var clientMAC [6]byte
	copy(clientMAC[:], hw)

	serverIP := [4]byte{10, 10, 0, 1}
	offeredIP := [4]byte{10, 10, 0, 20}
	mask := [4]byte{255, 255, 255, 0}

	var mu sync.Mutex
	discovers := 0
	requests := 0

	dev := newFakeDHCPDevice(macStr, false)
	dev.onTx = func(frame []byte, d *fakeDHCPDevice) {
		msgType, xid, _, _, _, _ := extractClientDHCP(t, frame)
		mu.Lock()
		defer mu.Unlock()
		switch msgType {
		case dhcpMsgDiscover:
			discovers++
			d.enqueueRX(makeServerReplyFrame(broadcastMAC, clientMAC, serverIP, offeredIP, xid,
				makeDHCPOptions(dhcpMsgOffer, &mask, &serverIP, nil, &serverIP, 600), false))
		case dhcpMsgRequest:
			requests++
			// Ignore the first maxDHCPRequestRetries requests so client restarts DISCOVER.
			if requests <= maxDHCPRequestRetries {
				return
			}
			d.enqueueRX(makeServerReplyFrame(broadcastMAC, clientMAC, serverIP, offeredIP, xid,
				makeDHCPOptions(dhcpMsgAck, &mask, &serverIP, nil, &serverIP, 600), false))
		}
	}

	lease, err := AcquireDHCP(context.Background(), dev, DHCPConfig{
		InitialInterval: 2 * time.Millisecond,
		MaxInterval:     2 * time.Millisecond,
		Timeout:         time.Second,
	})
	if err != nil {
		t.Fatalf("AcquireDHCP after request retry restart: %v", err)
	}
	if lease.Address != "10.10.0.20/24" || discovers < 2 {
		t.Fatalf("lease=%+v discovers=%d", lease, discovers)
	}
}

func TestAcquireDHCPValidationAndErrors(t *testing.T) {
	dev := newFakeDHCPDevice("52:54:00:12:34:56", false)

	if _, err := AcquireDHCP(nil, dev, DHCPConfig{}); !errors.Is(err, ErrDHCPInvalidConfig) {
		t.Fatalf("nil ctx err=%v", err)
	}
	if _, err := AcquireDHCP(context.Background(), nil, DHCPConfig{}); !errors.Is(err, ErrDHCPInvalidConfig) {
		t.Fatalf("nil dev err=%v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := AcquireDHCP(canceled, dev, DHCPConfig{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled ctx err=%v", err)
	}
	if _, err := AcquireDHCP(context.Background(), newFakeDHCPDevice("01:00:00:00:00:01", false), DHCPConfig{}); !errors.Is(err, ErrDHCPInvalidConfig) {
		t.Fatalf("multicast MAC err=%v", err)
	}
	if _, err := AcquireDHCP(context.Background(), newFakeDHCPDevice("00:00:00:00:00:00", false), DHCPConfig{}); !errors.Is(err, ErrDHCPInvalidConfig) {
		t.Fatalf("zero MAC err=%v", err)
	}
	if err := ValidateDHCPConfig(DHCPConfig{}); err != nil {
		t.Fatalf("ValidateDHCPConfig(zero): %v", err)
	}
	for _, badCfg := range []DHCPConfig{
		{InitialInterval: -time.Millisecond},
		{MaxInterval: -time.Millisecond},
		{Timeout: -time.Millisecond},
		{Hostname: strings.Repeat("a", 256)},
		{Hostname: "bad\nhost"},
	} {
		if err := ValidateDHCPConfig(badCfg); !errors.Is(err, ErrDHCPInvalidConfig) {
			t.Fatalf("ValidateDHCPConfig(%+v) err=%v, want ErrDHCPInvalidConfig", badCfg, err)
		}
		if _, err := AcquireDHCP(context.Background(), dev, badCfg); !errors.Is(err, ErrDHCPInvalidConfig) {
			t.Fatalf("cfg %+v err=%v, want ErrDHCPInvalidConfig", badCfg, err)
		}
	}

	// Timeout when no server replies.
	if _, err := AcquireDHCP(context.Background(), dev, DHCPConfig{
		InitialInterval: 2 * time.Millisecond,
		MaxInterval:     4 * time.Millisecond,
		Timeout:         12 * time.Millisecond,
		XID:             0x12345678,
	}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("silence err=%v, want DeadlineExceeded", err)
	}

	// Device TX / RX / Err errors propagate immediately.
	txFail := newFakeDHCPDevice("52:54:00:12:34:56", false)
	txFail.txErr = errors.New("tx fault")
	if _, err := AcquireDHCP(context.Background(), txFail, DHCPConfig{Timeout: time.Second}); err == nil {
		t.Fatal("expected tx fault")
	}
	rxFail := newFakeDHCPDevice("52:54:00:12:34:56", false)
	rxFail.rxErr = errors.New("rx fault")
	if _, err := AcquireDHCP(context.Background(), rxFail, DHCPConfig{Timeout: time.Second}); err == nil {
		t.Fatal("expected rx fault")
	}
	devFail := newFakeDHCPDevice("52:54:00:12:34:56", false)
	devFail.devErr = errors.New("device fault")
	if _, err := AcquireDHCP(context.Background(), devFail, DHCPConfig{Timeout: time.Second}); err == nil {
		t.Fatal("expected device fault")
	}
}

func TestParseDHCPFrameAndLeaseValidationRejections(t *testing.T) {
	mac := [6]byte{0x52, 0x54, 0x00, 0x12, 0x34, 0x56}
	serverIP := [4]byte{10, 0, 2, 2}
	yiaddr := [4]byte{10, 0, 2, 15}
	mask := [4]byte{255, 255, 255, 0}
	xid := uint32(0xdeadbeef)

	good := makeServerReplyFrame(broadcastMAC, mac, serverIP, yiaddr, xid,
		makeDHCPOptions(dhcpMsgOffer, &mask, &serverIP, nil, &serverIP, 600), false)
	if _, ok := parseDHCPFrame(good, mac, xid); !ok {
		t.Fatal("valid frame rejected")
	}

	// Mutate frame fields and confirm every malformed variant is rejected.
	mutations := []struct {
		name string
		mut  func([]byte) []byte
	}{
		{"short", func(b []byte) []byte { return b[:100] }},
		{"wrong-dst-mac", func(b []byte) []byte { b[0] = 0x02; return b }},
		{"wrong-ethertype", func(b []byte) []byte { b[12] = 0x08; b[13] = 0x06; return b }},
		{"wrong-ip-ver", func(b []byte) []byte { b[14] = 0x65; return b }},
		{"fragmented", func(b []byte) []byte {
			b[20] = 0x20 // MF flag
			b[24], b[25] = 0, 0
			binary.BigEndian.PutUint16(b[24:26], internetChecksum(b[14:34]))
			return b
		}},
		{"wrong-proto", func(b []byte) []byte {
			b[23] = 6 // TCP
			b[24], b[25] = 0, 0
			binary.BigEndian.PutUint16(b[24:26], internetChecksum(b[14:34]))
			return b
		}},
		{"bad-ip-checksum", func(b []byte) []byte { b[24] ^= 0xff; return b }},
		{"bad-udp-checksum", func(b []byte) []byte { b[40] ^= 0xff; return b }},
		{"wrong-ports", func(b []byte) []byte { b[35] = 99; b[40], b[41] = 0, 0; return b }},
		{"wrong-op", func(b []byte) []byte { b[42] = bootpRequest; b[40], b[41] = 0, 0; return b }},
		{"wrong-chaddr", func(b []byte) []byte { b[42+28] ^= 1; b[40], b[41] = 0, 0; return b }},
		{"bad-cookie", func(b []byte) []byte { b[42+bootpFixedLen] ^= 1; b[40], b[41] = 0, 0; return b }},
	}
	for _, tc := range mutations {
		cp := append([]byte(nil), good...)
		if _, ok := parseDHCPFrame(tc.mut(cp), mac, xid); ok {
			t.Errorf("mutation %q unexpectedly accepted", tc.name)
		}
	}

	// Non-contiguous subnet mask, missing server ID, out-of-subnet gateway rejected.
	if _, ok := maskPrefixBits([4]byte{255, 0, 255, 0}); ok {
		t.Error("non-contiguous mask accepted")
	}
	if _, ok := maskPrefixBits([4]byte{255, 255, 255, 255}); ok {
		t.Error("/32 mask accepted")
	}
	badLease := dhcpParsedReply{
		msgType:   dhcpMsgOffer,
		yiaddr:    [4]byte{10, 0, 2, 15},
		mask:      [4]byte{255, 255, 255, 0},
		hasMask:   true,
		routers:   [][4]byte{{10, 0, 99, 1}},
		serverID:  [4]byte{10, 0, 99, 2},
		hasServer: true,
	}
	if _, ok := buildValidatedLease(badLease); ok {
		t.Error("out-of-subnet router and server accepted")
	}
	// Odd-length UDP checksum coverage.
	odd := []byte{1, 2, 3}
	_ = internetChecksum(odd)
	_ = udpChecksum(serverIP[:], yiaddr[:], odd)
}

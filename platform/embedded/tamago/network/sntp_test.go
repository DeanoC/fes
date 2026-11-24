// SPDX-License-Identifier: GPL-3.0-or-later

package network

import (
	"context"
	"encoding/binary"
	"errors"
	"net/netip"
	"sync/atomic"
	"testing"
	"time"
)

func makeSNTPServerPacket(li, vn, mode, stratum uint8, originate uint64, rxTime, txTime time.Time) []byte {
	pkt := make([]byte, sntpPacketLen)
	pkt[0] = (li << 6) | (vn << 3) | (mode & 0x07)
	pkt[1] = stratum
	binary.BigEndian.PutUint64(pkt[24:32], originate)
	if !rxTime.IsZero() {
		binary.BigEndian.PutUint64(pkt[32:40], encodeNTPTimestamp(rxTime))
	}
	if !txTime.IsZero() {
		binary.BigEndian.PutUint64(pkt[40:48], encodeNTPTimestamp(txTime))
	}
	return pkt
}

func TestSNTPEncodeParseAndEraTimestamps(t *testing.T) {
	const nonce = uint64(0x1122334455667788)
	req := EncodeSNTPRequest(nonce)
	if len(req) != sntpPacketLen {
		t.Fatalf("req len=%d, want %d", len(req), sntpPacketLen)
	}
	if req[0] != 0x23 {
		t.Fatalf("req[0]=%#x, want 0x23 (LI=0, VN=4, Mode=3)", req[0])
	}
	if got := binary.BigEndian.Uint64(req[40:48]); got != nonce {
		t.Fatalf("req nonce=%#x, want %#x", got, nonce)
	}

	// Era 0 (2026) and Era 1 (2040, post-2036 rollover) round-trip cleanly.
	for _, want := range []time.Time{
		time.Date(2026, 11, 24, 12, 34, 56, 500_000_000, time.UTC),
		time.Date(2040, 5, 1, 0, 15, 30, 250_000_000, time.UTC),
	} {
		rx := want.Add(-2 * time.Millisecond)
		pkt := makeSNTPServerPacket(0, 4, 4, 1, nonce, rx, want)
		reply, err := ParseSNTPResponse(pkt, nonce)
		if err != nil {
			t.Fatalf("ParseSNTPResponse(%v): %v", want, err)
		}
		if reply.Stratum != 1 || reply.Version != 4 {
			t.Fatalf("reply=%+v", reply)
		}
		if diff := reply.TransmitTime.Sub(want); diff < -2*time.Microsecond || diff > 2*time.Microsecond {
			t.Fatalf("TransmitTime=%v, want %v (diff=%v)", reply.TransmitTime, want, diff)
		}
		if diff := reply.ReceiveTime.Sub(rx); diff < -2*time.Microsecond || diff > 2*time.Microsecond {
			t.Fatalf("ReceiveTime=%v, want %v (diff=%v)", reply.ReceiveTime, rx, diff)
		}
	}

	// Nonce mismatch is rejected BEFORE KoD or LI=3 so stray packets cannot fail an exchange.
	strayKoD := makeSNTPServerPacket(3, 4, 4, 0, nonce^1, time.Time{}, time.Now())
	if _, err := ParseSNTPResponse(strayKoD, nonce); !errors.Is(err, ErrSNTPInvalidReply) {
		t.Fatalf("stray KoD err=%v, want ErrSNTPInvalidReply", err)
	}
	kod := makeSNTPServerPacket(0, 4, 4, 0, nonce, time.Time{}, time.Now())
	if _, err := ParseSNTPResponse(kod, nonce); !errors.Is(err, ErrSNTPKissOfDeath) {
		t.Fatalf("KoD err=%v, want ErrSNTPKissOfDeath", err)
	}
	unsync := makeSNTPServerPacket(3, 4, 4, 2, nonce, time.Time{}, time.Now())
	if _, err := ParseSNTPResponse(unsync, nonce); !errors.Is(err, ErrSNTPUnsynchronized) {
		t.Fatalf("unsync err=%v, want ErrSNTPUnsynchronized", err)
	}
	zeroTx := makeSNTPServerPacket(0, 4, 4, 1, nonce, time.Time{}, time.Time{})
	if _, err := ParseSNTPResponse(zeroTx, nonce); !errors.Is(err, ErrSNTPInvalidReply) {
		t.Fatalf("zeroTx err=%v, want ErrSNTPInvalidReply", err)
	}
}

func TestQuerySNTPOverARPCustomPortAndFailover(t *testing.T) {
	clientMACStr := "52:54:00:12:34:56"
	serverMACStr := "52:55:0a:00:02:02"
	clientMAC, _ := parseUnicastMAC(clientMACStr)
	serverMAC, _ := parseUnicastMAC(serverMACStr)
	clientIP := [4]byte{10, 0, 2, 15}
	primaryIP := [4]byte{10, 0, 2, 1}
	secondaryIP := [4]byte{10, 0, 2, 2}
	const customPort = uint16(12345)

	wantUTC := time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)
	var arpSeen atomic.Int32

	port := newFakeRenewPort()
	port.onSend = func(frame []byte, p *fakeRenewPort) {
		ethType := binary.BigEndian.Uint16(frame[12:14])
		if ethType == ethTypeARP {
			arpSeen.Add(1)
			var targetIP [4]byte
			copy(targetIP[:], frame[ethHeaderLen+24:ethHeaderLen+28])
			go p.inject(makeARPReplyFrame(clientMAC, serverMAC, targetIP, clientIP))
			return
		}
		if ethType != ethTypeIPv4 {
			return
		}
		var dstIP [4]byte
		copy(dstIP[:], frame[ethHeaderLen+16:ethHeaderLen+20])
		udpOff := ethHeaderLen + ipv4MinLen
		srcPort := binary.BigEndian.Uint16(frame[udpOff : udpOff+2])
		dstPort := binary.BigEndian.Uint16(frame[udpOff+2 : udpOff+4])
		if dstPort != customPort {
			return
		}
		payload := frame[udpOff+udpHeaderLen:]
		if len(payload) < sntpPacketLen {
			return
		}
		nonce := binary.BigEndian.Uint64(payload[40:48])
		if dstIP == primaryIP {
			// Primary returns KoD -> QuerySNTP must fail over to secondary.
			kod := makeSNTPServerPacket(0, 4, 4, 0, nonce, time.Time{}, time.Time{})
			go p.inject(buildUDPv4Frame(serverMAC, clientMAC, primaryIP, clientIP, customPort, srcPort, 1, kod))
			return
		}
		if dstIP == secondaryIP {
			// First send a stray reply with wrong nonce (must be ignored), then valid reply.
			stray := makeSNTPServerPacket(3, 4, 4, 0, nonce^0xff, time.Time{}, wantUTC)
			reply := makeSNTPServerPacket(0, 4, 4, 1, nonce, wantUTC.Add(-time.Millisecond), wantUTC)
			go func() {
				p.inject(buildUDPv4Frame(serverMAC, clientMAC, secondaryIP, clientIP, customPort, srcPort, 2, stray))
				time.Sleep(10 * time.Millisecond)
				p.inject(buildUDPv4Frame(serverMAC, clientMAC, secondaryIP, clientIP, customPort, srcPort, 3, reply))
			}()
		}
	}

	res, err := QuerySNTP(context.Background(), port, SNTPConfig{
		LocalMAC:      clientMACStr,
		LocalPrefix:   "10.0.2.15/24",
		Gateway:       "10.0.2.2",
		Servers:       []string{"10.0.2.1", "10.0.2.2"},
		ServerPort:    customPort,
		RetryInterval: 50 * time.Millisecond,
		Timeout:       2 * time.Second,
	})
	if err != nil {
		t.Fatalf("QuerySNTP: %v", err)
	}
	if arpSeen.Load() == 0 {
		t.Fatal("expected ARP resolution before SNTP query")
	}
	if res.Server != "10.0.2.2" || res.Stratum != 1 {
		t.Fatalf("res=%+v, want server=10.0.2.2 stratum=1", res)
	}
	// Midpoint adjustment adds ~half RTT (~5ms) to wantUTC.
	if diff := res.Time.Sub(wantUTC); diff < 0 || diff > 250*time.Millisecond {
		t.Fatalf("res.Time=%v, want near %v (diff=%v)", res.Time, wantUTC, diff)
	}
}

func TestResolveACustomServerPort(t *testing.T) {
	clientMACStr := "52:54:00:12:34:56"
	serverMACStr := "52:55:0a:00:02:02"
	clientMAC, _ := parseUnicastMAC(clientMACStr)
	serverMAC, _ := parseUnicastMAC(serverMACStr)
	clientIP := [4]byte{10, 0, 2, 15}
	serverIP := [4]byte{10, 0, 2, 2}
	const customPort = uint16(15353)

	port := newFakeRenewPort()
	port.onSend = func(frame []byte, p *fakeRenewPort) {
		if binary.BigEndian.Uint16(frame[12:14]) != ethTypeIPv4 {
			return
		}
		udpOff := ethHeaderLen + ipv4MinLen
		srcPort := binary.BigEndian.Uint16(frame[udpOff : udpOff+2])
		dstPort := binary.BigEndian.Uint16(frame[udpOff+2 : udpOff+4])
		if dstPort != customPort {
			return
		}
		q := frame[udpOff+udpHeaderLen:]
		id := binary.BigEndian.Uint16(q[0:2])
		resp := buildFakeDNSResponse(id, "fes-host.local", [4]byte{10, 0, 2, 2})
		go p.inject(buildUDPv4Frame(serverMAC, clientMAC, serverIP, clientIP, customPort, srcPort, 1, resp))
	}

	addrs, err := ResolveA(context.Background(), port, DNSConfig{
		LocalMAC:      clientMACStr,
		LocalPrefix:   "10.0.2.15/24",
		Gateway:       "10.0.2.2",
		Servers:       []string{"10.0.2.2"},
		ServerPort:    customPort,
		NextHopMAC:    serverMACStr,
		RetryInterval: 20 * time.Millisecond,
		Timeout:       time.Second,
	}, "fes-host.local")
	if err != nil {
		t.Fatalf("ResolveA custom port: %v", err)
	}
	if len(addrs) != 1 || addrs[0] != netip.MustParseAddr("10.0.2.2") {
		t.Fatalf("addrs=%v, want [10.0.2.2]", addrs)
	}
}

func TestInterfaceLeaseRenewOnceSynchronizesAndExcludesRenewer(t *testing.T) {
	clientMACStr := "52:54:00:12:34:56"
	clientMAC, _ := parseUnicastMAC(clientMACStr)
	serverIP := [4]byte{10, 0, 2, 2}
	clientIP := [4]byte{10, 0, 2, 15}
	mask := [4]byte{255, 255, 255, 0}

	var state interfaceLease
	state.init(DHCPLease{
		Address:       "10.0.2.15/24",
		Gateway:       "10.0.2.2",
		Server:        "10.0.2.2",
		LeaseDuration: time.Minute,
	}, time.Now(), true)

	port := newFakeRenewPort()
	port.onSend = func(frame []byte, p *fakeRenewPort) {
		xid := binary.BigEndian.Uint32(frame[ethHeaderLen+ipv4MinLen+udpHeaderLen+4 : ethHeaderLen+ipv4MinLen+udpHeaderLen+8])
		go p.inject(makeServerReplyFrame(broadcastMAC, clientMAC, serverIP, clientIP, xid,
			makeDHCPOptions(dhcpMsgAck, &mask, &serverIP, nil, &serverIP, 7200), false))
	}

	got, err := state.renewOnce(context.Background(), port, clientMACStr, false, RenewConfig{RetryInterval: 10 * time.Millisecond}, nil, nil)
	if err != nil {
		t.Fatalf("renewOnce: %v", err)
	}
	if got.LeaseDuration != 2*time.Hour {
		t.Fatalf("got.LeaseDuration=%v, want 2h", got.LeaseDuration)
	}
	stored, ok := state.get()
	if !ok || stored.LeaseDuration != 2*time.Hour {
		t.Fatalf("stored lease=(%+v, %t), want 2h", stored, ok)
	}

	// While a background LeaseRenewer is running, renewOnce fails closed with ErrRenewerActive.
	ren, err := state.newRenewer(port, clientMACStr, RenewConfig{RetryInterval: 10 * time.Millisecond}, nil, nil)
	if err != nil {
		t.Fatalf("newRenewer: %v", err)
	}
	if err := ren.Start(context.Background()); err != nil {
		t.Fatalf("ren.Start: %v", err)
	}
	if _, err := state.renewOnce(context.Background(), port, clientMACStr, false, RenewConfig{RetryInterval: 10 * time.Millisecond}, nil, nil); !errors.Is(err, ErrRenewerActive) {
		ren.Stop()
		t.Fatalf("renewOnce while active renewer err=%v, want ErrRenewerActive", err)
	}
	ren.Stop()

	// NAK during renewOnce triggers onTerminal immediately.
	var terminalErr error
	nakPort := newFakeRenewPort()
	nakPort.onSend = func(frame []byte, p *fakeRenewPort) {
		xid := binary.BigEndian.Uint32(frame[ethHeaderLen+ipv4MinLen+udpHeaderLen+4 : ethHeaderLen+ipv4MinLen+udpHeaderLen+8])
		go p.inject(makeServerReplyFrame(broadcastMAC, clientMAC, serverIP, clientIP, xid,
			makeDHCPOptions(dhcpMsgNak, nil, nil, nil, &serverIP, 0), false))
	}
	if _, err := state.renewOnce(context.Background(), nakPort, clientMACStr, false, RenewConfig{RetryInterval: 10 * time.Millisecond}, nil, func(e error) {
		terminalErr = e
	}); !errors.Is(err, ErrLeaseNAK) {
		t.Fatalf("renewOnce NAK err=%v, want ErrLeaseNAK", err)
	}
	if !errors.Is(terminalErr, ErrLeaseNAK) {
		t.Fatalf("terminalErr=%v, want ErrLeaseNAK", terminalErr)
	}
}

func buildFakeDNSResponse(id uint16, name string, ip [4]byte) []byte {
	q, err := EncodeDNSQuery(id, name)
	if err != nil {
		panic(err)
	}
	resp := append([]byte(nil), q...)
	binary.BigEndian.PutUint16(resp[2:4], 0x8180) // QR=1, RD=1, RA=1, RCODE=0
	binary.BigEndian.PutUint16(resp[6:8], 1)      // ANCOUNT=1
	// Pointer to question name at offset 12, Type A, Class IN, TTL 60, RDLEN 4, IPv4.
	ans := []byte{
		0xc0, 0x0c,
		0x00, 0x01,
		0x00, 0x01,
		0x00, 0x00, 0x00, 0x3c,
		0x00, 0x04,
		ip[0], ip[1], ip[2], ip[3],
	}
	return append(resp, ans...)
}

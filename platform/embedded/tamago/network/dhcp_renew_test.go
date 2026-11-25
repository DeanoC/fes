// SPDX-License-Identifier: GPL-3.0-or-later

package network

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeRenewPort struct {
	mu      sync.Mutex
	nextID  uint64
	taps    map[uint64]func([]byte)
	onSend  func(frame []byte, p *fakeRenewPort)
	sendErr error
	portErr error
}

func newFakeRenewPort() *fakeRenewPort {
	return &fakeRenewPort{taps: make(map[uint64]func([]byte))}
}

func (p *fakeRenewPort) SendRawFrame(frame []byte) error {
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

func (p *fakeRenewPort) AddTap(fn func([]byte)) func() {
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

func (p *fakeRenewPort) inject(frame []byte) {
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

func (p *fakeRenewPort) Err() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.portErr
}

func TestRenewDHCPOnceUnicastRebindNAKAndChanged(t *testing.T) {
	clientMACStr := "52:54:00:12:34:56"
	serverMACStr := "52:55:0a:00:02:02"
	clientMAC, _ := parseUnicastMAC(clientMACStr)
	serverMAC, _ := parseUnicastMAC(serverMACStr)
	serverIP := [4]byte{10, 0, 2, 2}
	clientIP := [4]byte{10, 0, 2, 15}
	mask := [4]byte{255, 255, 255, 0}
	dns := [4]byte{10, 0, 2, 3}

	initial := DHCPLease{
		Address:       "10.0.2.15/24",
		Gateway:       "10.0.2.2",
		Server:        "10.0.2.2",
		LeaseDuration: time.Hour,
	}

	// 1. Unicast T1 renew with ciaddr set and server MAC targeted.
	port := newFakeRenewPort()
	port.onSend = func(frame []byte, p *fakeRenewPort) {
		var dstMAC [6]byte
		copy(dstMAC[:], frame[0:6])
		if dstMAC != serverMAC {
			t.Errorf("unicast renew dstMAC=%x, want %x", dstMAC, serverMAC)
		}
		bootp := frame[ethHeaderLen+ipv4MinLen+udpHeaderLen:]
		var ciaddr [4]byte
		copy(ciaddr[:], bootp[12:16])
		if ciaddr != clientIP {
			t.Errorf("renew ciaddr=%v, want %v", ciaddr, clientIP)
		}
		xid := binary.BigEndian.Uint32(bootp[4:8])
		ack := makeServerReplyFrame(clientMAC, clientMAC, serverIP, clientIP, xid,
			makeDHCPOptions(dhcpMsgAck, &mask, &serverIP, &dns, &serverIP, 7200), false)
		go p.inject(ack)
	}

	renewed, err := RenewDHCPOnce(context.Background(), port, clientMACStr, initial, false, RenewConfig{
		ServerMAC:     serverMACStr,
		RetryInterval: 5 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("RenewDHCPOnce unicast: %v", err)
	}
	if renewed.LeaseDuration != 2*time.Hour || len(renewed.DNS) != 1 || renewed.DNS[0] != "10.0.2.3" {
		t.Fatalf("unexpected renewed lease: %+v", renewed)
	}

	// 2. Broadcast T2 rebind accepts ACK from another server with broadcast dstMAC.
	otherSrv := [4]byte{10, 0, 2, 4}
	port2 := newFakeRenewPort()
	port2.onSend = func(frame []byte, p *fakeRenewPort) {
		var dstMAC [6]byte
		copy(dstMAC[:], frame[0:6])
		if dstMAC != broadcastMAC {
			t.Errorf("rebind dstMAC=%x, want broadcast", dstMAC)
		}
		xid := binary.BigEndian.Uint32(frame[ethHeaderLen+ipv4MinLen+udpHeaderLen+4 : ethHeaderLen+ipv4MinLen+udpHeaderLen+8])
		ack := makeServerReplyFrame(broadcastMAC, clientMAC, otherSrv, clientIP, xid,
			makeDHCPOptions(dhcpMsgAck, &mask, &serverIP, nil, &otherSrv, 3600), false)
		go p.inject(ack)
	}
	rebound, err := RenewDHCPOnce(context.Background(), port2, clientMACStr, initial, true, RenewConfig{
		ServerMAC:     serverMACStr,
		RetryInterval: 5 * time.Millisecond,
	})
	if err != nil || rebound.Server != "10.0.2.4" {
		t.Fatalf("RenewDHCPOnce rebind=(%+v, %v)", rebound, err)
	}

	// 3. DHCPNAK returns ErrLeaseNAK immediately (ignoring stray NAK from another server in T1).
	port3 := newFakeRenewPort()
	port3.onSend = func(frame []byte, p *fakeRenewPort) {
		xid := binary.BigEndian.Uint32(frame[ethHeaderLen+ipv4MinLen+udpHeaderLen+4 : ethHeaderLen+ipv4MinLen+udpHeaderLen+8])
		go func() {
			p.inject(makeServerReplyFrame(broadcastMAC, clientMAC, otherSrv, clientIP, xid,
				makeDHCPOptions(dhcpMsgNak, nil, nil, nil, &otherSrv, 0), false))
			p.inject(makeServerReplyFrame(broadcastMAC, clientMAC, serverIP, clientIP, xid,
				makeDHCPOptions(dhcpMsgNak, nil, nil, nil, &serverIP, 0), false))
		}()
	}
	if _, err := RenewDHCPOnce(context.Background(), port3, clientMACStr, initial, false, RenewConfig{RetryInterval: 5 * time.Millisecond}); !errors.Is(err, ErrLeaseNAK) {
		t.Fatalf("NAK err=%v, want ErrLeaseNAK", err)
	}

	// 4. ACK changing client IP returns ErrLeaseChanged.
	changedIP := [4]byte{10, 0, 2, 99}
	port4 := newFakeRenewPort()
	port4.onSend = func(frame []byte, p *fakeRenewPort) {
		xid := binary.BigEndian.Uint32(frame[ethHeaderLen+ipv4MinLen+udpHeaderLen+4 : ethHeaderLen+ipv4MinLen+udpHeaderLen+8])
		go p.inject(makeServerReplyFrame(broadcastMAC, clientMAC, serverIP, changedIP, xid,
			makeDHCPOptions(dhcpMsgAck, &mask, &serverIP, nil, &serverIP, 3600), false))
	}
	if _, err := RenewDHCPOnce(context.Background(), port4, clientMACStr, initial, false, RenewConfig{RetryInterval: 5 * time.Millisecond}); !errors.Is(err, ErrLeaseChanged) {
		t.Fatalf("changed IP err=%v, want ErrLeaseChanged", err)
	}

	// 5. Renewal ACK omitting unchanged subnet mask, router, DNS, and server ID preserves them from current.
	initialWithDNS := initial
	initialWithDNS.DNS = []string{"10.0.2.3"}
	port5 := newFakeRenewPort()
	port5.onSend = func(frame []byte, p *fakeRenewPort) {
		xid := binary.BigEndian.Uint32(frame[ethHeaderLen+ipv4MinLen+udpHeaderLen+4 : ethHeaderLen+ipv4MinLen+udpHeaderLen+8])
		// ACK carries only msgType=ACK and leaseTime=1800 (no mask, router, DNS, or server ID).
		go p.inject(makeServerReplyFrame(clientMAC, clientMAC, serverIP, clientIP, xid,
			makeDHCPOptions(dhcpMsgAck, nil, nil, nil, nil, 1800), false))
	}
	minimal, err := RenewDHCPOnce(context.Background(), port5, clientMACStr, initialWithDNS, false, RenewConfig{
		ServerMAC:     serverMACStr,
		RetryInterval: 5 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("minimal ACK RenewDHCPOnce: %v", err)
	}
	if minimal.Address != initialWithDNS.Address || minimal.Gateway != initialWithDNS.Gateway ||
		minimal.Server != initialWithDNS.Server || minimal.LeaseDuration != 30*time.Minute ||
		len(minimal.DNS) != 1 || minimal.DNS[0] != "10.0.2.3" {
		t.Fatalf("minimal ACK lease=%+v", minimal)
	}
}

func TestLeaseRenewerT1T2LifecycleAndStopDrain(t *testing.T) {
	clientMACStr := "52:54:00:12:34:56"
	serverMACStr := "52:55:0a:00:02:02"
	clientMAC, _ := parseUnicastMAC(clientMACStr)
	serverIP := [4]byte{10, 0, 2, 2}
	clientIP := [4]byte{10, 0, 2, 15}
	mask := [4]byte{255, 255, 255, 0}

	// Validation & opt-in Stop-before-Start checks.
	port := newFakeRenewPort()
	goodLease := DHCPLease{
		Address:       "10.0.2.15/24",
		Gateway:       "10.0.2.2",
		Server:        "10.0.2.2",
		LeaseDuration: 40 * time.Millisecond,
	}
	if _, err := NewLeaseRenewer(nil, clientMACStr, goodLease, RenewConfig{}); !errors.Is(err, ErrDHCPInvalidConfig) {
		t.Fatalf("nil port err=%v", err)
	}
	if _, err := NewLeaseRenewer(port, "bad", goodLease, RenewConfig{}); !errors.Is(err, ErrDHCPInvalidConfig) {
		t.Fatalf("bad mac err=%v", err)
	}
	if _, err := NewLeaseRenewer(port, clientMACStr, DHCPLease{Address: "10.0.2.15/24", Gateway: "10.0.2.2", Server: "10.0.2.2"}, RenewConfig{}); !errors.Is(err, ErrDHCPInvalidConfig) {
		t.Fatalf("zero lease err=%v", err)
	}
	for _, badCfg := range []RenewConfig{
		{RetryInterval: -time.Millisecond},
		{Hostname: strings.Repeat("x", 256)},
		{Hostname: "a\nb"},
		{ServerMAC: "bad"},
	} {
		if _, err := NewLeaseRenewer(port, clientMACStr, goodLease, badCfg); !errors.Is(err, ErrDHCPInvalidConfig) {
			t.Errorf("badCfg %+v err=%v", badCfg, err)
		}
	}

	idle, err := NewLeaseRenewer(port, clientMACStr, goodLease, RenewConfig{})
	if err != nil {
		t.Fatalf("NewLeaseRenewer: %v", err)
	}
	idle.Stop()
	if err := idle.Start(context.Background()); !errors.Is(err, ErrRenewerStopped) {
		t.Fatalf("Start after Stop err=%v, want ErrRenewerStopped", err)
	}

	// T1 silence falls through to T2 broadcast rebind which succeeds!
	var renewedCount atomic.Uint32
	renewedCh := make(chan DHCPLease, 4)
	port2 := newFakeRenewPort()
	port2.onSend = func(frame []byte, p *fakeRenewPort) {
		var dstMAC [6]byte
		copy(dstMAC[:], frame[0:6])
		if dstMAC != broadcastMAC {
			// Ignore unicast T1 renew so renewer must transition to T2 broadcast rebind.
			return
		}
		xid := binary.BigEndian.Uint32(frame[ethHeaderLen+ipv4MinLen+udpHeaderLen+4 : ethHeaderLen+ipv4MinLen+udpHeaderLen+8])
		go p.inject(makeServerReplyFrame(broadcastMAC, clientMAC, serverIP, clientIP, xid,
			makeDHCPOptions(dhcpMsgAck, &mask, &serverIP, nil, &serverIP, 3600), false))
	}

	ren, err := NewLeaseRenewer(port2, clientMACStr, goodLease, RenewConfig{
		ServerMAC:     serverMACStr,
		RetryInterval: 3 * time.Millisecond,
		OnRenew: func(l DHCPLease) {
			renewedCount.Add(1)
			select {
			case renewedCh <- l:
			default:
			}
		},
	})
	if err != nil {
		t.Fatalf("NewLeaseRenewer T2: %v", err)
	}
	if err := ren.Start(nil); !errors.Is(err, ErrDHCPInvalidConfig) {
		t.Fatalf("Start(nil) err=%v", err)
	}
	if err := ren.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := ren.Start(context.Background()); !errors.Is(err, ErrRenewerStarted) {
		t.Fatalf("double Start err=%v, want ErrRenewerStarted", err)
	}
	select {
	case got := <-renewedCh:
		if got.LeaseDuration != time.Hour {
			t.Fatalf("rebound LeaseDuration=%v, want 1h", got.LeaseDuration)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for T2 rebind renewal")
	}
	ren.Stop()
	if ren.Running() || ren.Renewals() < 1 || ren.Lease().LeaseDuration != time.Hour {
		t.Fatalf("post-stop state running=%t renewals=%d lease=%+v", ren.Running(), ren.Renewals(), ren.Lease())
	}

	// Expiry when both T1 and T2 receive no replies records ErrLeaseExpired and calls OnError.
	errCh := make(chan error, 1)
	silent := newFakeRenewPort()
	expRen, err := NewLeaseRenewer(silent, clientMACStr, DHCPLease{
		Address:       "10.0.2.15/24",
		Gateway:       "10.0.2.2",
		Server:        "10.0.2.2",
		LeaseDuration: 24 * time.Millisecond,
	}, RenewConfig{
		RetryInterval: 3 * time.Millisecond,
		OnError:       func(e error) { errCh <- e },
	})
	if err != nil {
		t.Fatalf("NewLeaseRenewer expire: %v", err)
	}
	if err := expRen.Start(context.Background()); err != nil {
		t.Fatalf("Start expire: %v", err)
	}
	select {
	case gotErr := <-errCh:
		if !errors.Is(gotErr, ErrLeaseExpired) {
			t.Fatalf("OnError=%v, want ErrLeaseExpired", gotErr)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for ErrLeaseExpired")
	}
	expRen.Stop()
	if !errors.Is(expRen.Err(), ErrLeaseExpired) {
		t.Fatalf("Err=%v, want ErrLeaseExpired", expRen.Err())
	}

	// Preserving original AcquiredAt in the past triggers T1 immediately without extending expiry,
	// and propagates updated lease + timestamp to onUpdate before invoking OnRenew.
	pastRenewed := make(chan DHCPLease, 1)
	var updatedMu sync.Mutex
	var updatedLease DHCPLease
	var updatedAt time.Time
	var sawUpdateInOnRenew atomic.Bool
	portPast := newFakeRenewPort()
	portPast.onSend = func(frame []byte, p *fakeRenewPort) {
		xid := binary.BigEndian.Uint32(frame[ethHeaderLen+ipv4MinLen+udpHeaderLen+4 : ethHeaderLen+ipv4MinLen+udpHeaderLen+8])
		go p.inject(makeServerReplyFrame(clientMAC, clientMAC, serverIP, clientIP, xid,
			makeDHCPOptions(dhcpMsgAck, nil, nil, nil, &serverIP, 3600), false))
	}
	pastRen, err := NewLeaseRenewer(portPast, clientMACStr, DHCPLease{
		Address:       "10.0.2.15/24",
		Gateway:       "10.0.2.2",
		Server:        "10.0.2.2",
		LeaseDuration: time.Hour,
	}, RenewConfig{
		AcquiredAt:    time.Now().Add(-40 * time.Minute), // already past 30m T1!
		RetryInterval: 5 * time.Millisecond,
		OnRenew: func(l DHCPLease) {
			updatedMu.Lock()
			if updatedLease.LeaseDuration == time.Hour && !updatedAt.IsZero() {
				sawUpdateInOnRenew.Store(true)
			}
			updatedMu.Unlock()
			pastRenewed <- l
		},
	})
	if err != nil {
		t.Fatalf("NewLeaseRenewer past: %v", err)
	}
	pastRen.onUpdate = func(next DHCPLease, at time.Time) {
		updatedMu.Lock()
		updatedLease = next
		updatedAt = at
		updatedMu.Unlock()
	}
	if err := pastRen.Start(context.Background()); err != nil {
		t.Fatalf("Start past: %v", err)
	}
	select {
	case <-pastRenewed:
	case <-time.After(time.Second):
		t.Fatal("past AcquiredAt did not trigger immediate T1 renewal")
	}
	if !sawUpdateInOnRenew.Load() {
		t.Fatal("onUpdate did not run before OnRenew")
	}
	pastRen.Stop()
}

func TestRenewDHCPOnceOverRealLink(t *testing.T) {
	clientMACStr := "52:54:00:12:34:56"
	serverMACStr := "52:55:0a:00:02:02"
	clientMAC, _ := parseUnicastMAC(clientMACStr)
	serverIP := [4]byte{10, 0, 2, 2}
	clientIP := [4]byte{10, 0, 2, 15}
	mask := [4]byte{255, 255, 255, 0}

	dev := newFakeDHCPDevice(clientMACStr, false)
	dev.onTx = func(frame []byte, d *fakeDHCPDevice) {
		xid := binary.BigEndian.Uint32(frame[ethHeaderLen+ipv4MinLen+udpHeaderLen+4 : ethHeaderLen+ipv4MinLen+udpHeaderLen+8])
		d.enqueueRX(makeServerReplyFrame(clientMAC, clientMAC, serverIP, clientIP, xid,
			makeDHCPOptions(dhcpMsgAck, &mask, &serverIP, nil, &serverIP, 3600), false))
	}
	link := &Link{stack: &fakeStack{}, device: dev, state: &linkState{}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- link.Start(ctx) }()
	defer func() {
		cancel()
		<-done
	}()

	initial := DHCPLease{
		Address:       "10.0.2.15/24",
		Gateway:       "10.0.2.2",
		Server:        "10.0.2.2",
		LeaseDuration: time.Minute,
	}
	renewed, err := RenewDHCPOnce(context.Background(), link, clientMACStr, initial, false, RenewConfig{
		ServerMAC:     serverMACStr,
		RetryInterval: 5 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("RenewDHCPOnce over *Link: %v", err)
	}
	if renewed.LeaseDuration != time.Hour || renewed.Address != initial.Address {
		t.Fatalf("renewed=%+v", renewed)
	}
}

func TestLeaseRenewerClearsStaleServerMACAfterRebindServerChange(t *testing.T) {
	clientMACStr := "52:54:00:12:34:56"
	oldServerMACStr := "52:55:0a:00:02:02"
	clientMAC, _ := parseUnicastMAC(clientMACStr)
	oldServerMAC, _ := parseUnicastMAC(oldServerMACStr)
	newServerIP := [4]byte{10, 0, 2, 9}
	clientIP := [4]byte{10, 0, 2, 15}
	mask := [4]byte{255, 255, 255, 0}
	gw := [4]byte{10, 0, 2, 2}

	var mu sync.Mutex
	rebound := false
	sentToOldMACAfterRebind := false
	renewedCh := make(chan DHCPLease, 4)

	port := newFakeRenewPort()
	port.onSend = func(frame []byte, p *fakeRenewPort) {
		var dstMAC [6]byte
		copy(dstMAC[:], frame[0:6])
		xid := binary.BigEndian.Uint32(frame[ethHeaderLen+ipv4MinLen+udpHeaderLen+4 : ethHeaderLen+ipv4MinLen+udpHeaderLen+8])
		mu.Lock()
		alreadyRebound := rebound
		if alreadyRebound && dstMAC == oldServerMAC {
			sentToOldMACAfterRebind = true
		}
		if !alreadyRebound {
			if dstMAC != broadcastMAC {
				// Ignore T1 unicast to old server so renewer falls through to T2 rebind.
				mu.Unlock()
				return
			}
			rebound = true
			mu.Unlock()
			// New server answers T2 with a short 40ms lease so a second cycle runs immediately.
			go p.inject(makeServerReplyFrame(broadcastMAC, clientMAC, newServerIP, clientIP, xid,
				makeDHCPOptions(dhcpMsgAck, &mask, &gw, nil, &newServerIP, 1), false))
			return
		}
		mu.Unlock()
		// Next T1 must broadcast (not send to oldServerMAC) and receive ACK from newServerIP.
		if dstMAC == broadcastMAC {
			go p.inject(makeServerReplyFrame(broadcastMAC, clientMAC, newServerIP, clientIP, xid,
				makeDHCPOptions(dhcpMsgAck, &mask, &gw, nil, &newServerIP, 3600), false))
		}
	}

	ren, err := NewLeaseRenewer(port, clientMACStr, DHCPLease{
		Address:       "10.0.2.15/24",
		Gateway:       "10.0.2.2",
		Server:        "10.0.2.2",
		LeaseDuration: 30 * time.Millisecond,
	}, RenewConfig{
		ServerMAC:     oldServerMACStr,
		RetryInterval: 3 * time.Millisecond,
		OnRenew: func(l DHCPLease) {
			select {
			case renewedCh <- l:
			default:
			}
		},
	})
	if err != nil {
		t.Fatalf("NewLeaseRenewer: %v", err)
	}
	if err := ren.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// First renewal (T2 rebind to 10.0.2.9 with 1s lease), then second renewal (T1 at ~500ms).
	for i := 0; i < 2; i++ {
		select {
		case <-renewedCh:
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for renewal %d", i+1)
		}
	}
	ren.Stop()
	mu.Lock()
	stale := sentToOldMACAfterRebind
	mu.Unlock()
	if stale {
		t.Fatal("renewer sent T1 request to stale old ServerMAC after rebinding to a new server")
	}
}

func TestLeaseExpiryHaltsActiveLinkAndServerT1T2AndPacing(t *testing.T) {
	// 1. Lease expiry halts an active *Link immediately (RFC 2131 §4.4.5) even with no OnError hook.
	clientMACStr := "52:54:00:12:34:56"
	dev := newFakeDHCPDevice(clientMACStr, false)
	link := &Link{stack: &fakeStack{}, device: dev, state: &linkState{}}
	ctx, cancel := context.WithCancel(context.Background())
	linkDone := make(chan error, 1)
	go func() { linkDone <- link.Start(ctx) }()
	defer func() {
		cancel()
		<-linkDone
	}()

	ren, err := NewLeaseRenewer(link, clientMACStr, DHCPLease{
		Address:       "10.0.2.15/24",
		Gateway:       "10.0.2.2",
		Server:        "10.0.2.2",
		LeaseDuration: 24 * time.Millisecond,
	}, RenewConfig{
		RetryInterval: 3 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewLeaseRenewer: %v", err)
	}
	if err := ren.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	deadline := time.Now().Add(time.Second)
	for ren.Running() && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	ren.Stop()
	if !errors.Is(ren.Err(), ErrLeaseExpired) {
		t.Fatalf("ren.Err()=%v, want ErrLeaseExpired", ren.Err())
	}
	if !errors.Is(link.Err(), ErrLeaseExpired) {
		t.Fatalf("link.Err()=%v, want ErrLeaseExpired (active Link was not halted on expiry)", link.Err())
	}
	if err := link.SendRawFrame(make([]byte, 60)); !errors.Is(err, ErrLeaseExpired) {
		t.Fatalf("SendRawFrame after expiry err=%v, want ErrLeaseExpired", err)
	}

	// 2. Server-supplied T1 (option 58) and T2 (option 59) are parsed and honored by leaseTimers.
	clientMAC, _ := parseUnicastMAC(clientMACStr)
	serverIP := [4]byte{10, 0, 2, 2}
	clientIP := [4]byte{10, 0, 2, 15}
	mask := [4]byte{255, 255, 255, 0}
	opts := makeDHCPOptions(dhcpMsgAck, &mask, &serverIP, nil, &serverIP, 3600)
	// Strip trailing End (255), append option 58 (600s) and option 59 (1200s) and End.
	opts = opts[:len(opts)-1]
	opts = append(opts, dhcpOptRenewalTime, 4, 0, 0, 0x02, 0x58) // 600s
	opts = append(opts, dhcpOptRebindTime, 4, 0, 0, 0x04, 0xb0)  // 1200s
	opts = append(opts, dhcpOptEnd)
	frame := makeServerReplyFrame(clientMAC, clientMAC, serverIP, clientIP, 0x11223344, opts, false)
	parsed, ok := parseDHCPFrame(frame, clientMAC, 0x11223344)
	if !ok {
		t.Fatal("failed to parse ACK with option 58/59")
	}
	lease, ok := buildValidatedLease(parsed)
	if !ok || lease.RenewalTime != 600*time.Second || lease.RebindTime != 1200*time.Second {
		t.Fatalf("option 58/59 lease=%+v ok=%t", lease, ok)
	}
	t1, t2, renew := leaseTimers(lease)
	if !renew || t1 != 600*time.Second || t2 != 1200*time.Second {
		t.Fatalf("leaseTimers=(%v, %v, %t), want (10m, 20m, true)", t1, t2, renew)
	}
	// Earlier server-defined T2 clamps default T1 so rebind is never skipped.
	earlyT2 := DHCPLease{LeaseDuration: time.Hour, RebindTime: 15 * time.Minute}
	t1, t2, renew = leaseTimers(earlyT2)
	if !renew || t1 != 15*time.Minute || t2 != 15*time.Minute {
		t.Fatalf("early T2 leaseTimers=(%v, %v, %t), want (15m, 15m, true)", t1, t2, renew)
	}

	// 3. RFC 2131 §4.4.5 remaining-time retry pacing.
	if got := nextRenewWait(context.Background(), defaultRenewMinInterval); got != defaultRenewMinInterval {
		t.Fatalf("no-deadline nextRenewWait=%v, want %v", got, defaultRenewMinInterval)
	}
	longCtx, longCancel := context.WithTimeout(context.Background(), 9*time.Hour)
	defer longCancel()
	if got := nextRenewWait(longCtx, defaultRenewMinInterval); got < 4*time.Hour || got > 5*time.Hour {
		t.Fatalf("9h remaining nextRenewWait=%v, want ~4.5h", got)
	}
	midCtx, midCancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer midCancel()
	if got := nextRenewWait(midCtx, defaultRenewMinInterval); got != defaultRenewMinInterval {
		t.Fatalf("90s remaining nextRenewWait=%v, want 60s floor", got)
	}
	shortCtx, shortCancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer shortCancel()
	if got := nextRenewWait(shortCtx, defaultRenewMinInterval); got <= 0 || got > 20*time.Second {
		t.Fatalf("20s remaining nextRenewWait=%v, want (0, 20s]", got)
	}
}

func TestSingleActiveRenewerEnforcement(t *testing.T) {
	clientMACStr := "52:54:00:12:34:56"
	clientMAC, _ := parseUnicastMAC(clientMACStr)
	serverIP := [4]byte{10, 0, 2, 2}
	clientIP := [4]byte{10, 0, 2, 15}
	mask := [4]byte{255, 255, 255, 0}

	dev := newFakeDHCPDevice(clientMACStr, false)
	link := &Link{stack: &fakeStack{}, device: dev, state: &linkState{}}
	initial := DHCPLease{
		Address:       "10.0.2.15/24",
		Gateway:       "10.0.2.2",
		Server:        "10.0.2.2",
		LeaseDuration: 40 * time.Millisecond,
	}

	// 1. Pre-start rejections (discarded idle handle, nil or cancelled context) never leak *Link ownership.
	cancelledCtx, cancelNow := context.WithCancel(context.Background())
	cancelNow()
	idleDirect, err := NewLeaseRenewer(link, clientMACStr, initial, RenewConfig{RetryInterval: 5 * time.Millisecond})
	if err != nil {
		t.Fatalf("idleDirect: %v", err)
	}
	if err := idleDirect.Start(nil); !errors.Is(err, ErrDHCPInvalidConfig) {
		t.Fatalf("Start(nil) err=%v, want ErrDHCPInvalidConfig", err)
	}
	if err := idleDirect.Start(cancelledCtx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Start(cancelled) err=%v, want context.Canceled", err)
	}

	// 2. Direct *Link ownership allows only one started renewer at a time; stopping a
	//    rejected handle must not release the active renewer's ownership.
	direct1, err := NewLeaseRenewer(link, clientMACStr, initial, RenewConfig{RetryInterval: 5 * time.Millisecond})
	if err != nil {
		t.Fatalf("direct1: %v", err)
	}
	direct2, err := NewLeaseRenewer(link, clientMACStr, initial, RenewConfig{RetryInterval: 5 * time.Millisecond})
	if err != nil {
		t.Fatalf("direct2: %v", err)
	}
	if err := direct1.Start(context.Background()); err != nil {
		t.Fatalf("direct1.Start: %v", err)
	}
	if err := direct2.Start(context.Background()); !errors.Is(err, ErrRenewerActive) {
		t.Fatalf("concurrent direct2.Start err=%v, want ErrRenewerActive", err)
	}
	direct2.Stop()
	if err := idleDirect.Start(context.Background()); !errors.Is(err, ErrRenewerActive) {
		t.Fatalf("idleDirect.Start while direct1 still active err=%v, want ErrRenewerActive", err)
	}
	direct1.Stop()

	// 3. interfaceLease enforces one active renewer, synchronizes renewed lease/AcquiredAt
	//    even to pre-created handles started later, and releases cleanly on Stop() or ctx cancel.
	var state interfaceLease
	if _, ok := state.get(); ok {
		t.Fatal("uninitialized interfaceLease reported hasLease=true")
	}
	if _, err := state.newRenewer(link, clientMACStr, RenewConfig{}, nil, nil); !errors.Is(err, ErrDHCPInvalidConfig) {
		t.Fatalf("newRenewer without lease err=%v, want ErrDHCPInvalidConfig", err)
	}

	state.init(initial, time.Now(), true)
	port := newFakeRenewPort()
	newServerIP := [4]byte{10, 0, 2, 9}
	renewedCh := make(chan DHCPLease, 2)
	port.onSend = func(frame []byte, p *fakeRenewPort) {
		var dstMAC [6]byte
		copy(dstMAC[:], frame[0:6])
		if dstMAC != broadcastMAC {
			return
		}
		xid := binary.BigEndian.Uint32(frame[ethHeaderLen+ipv4MinLen+udpHeaderLen+4 : ethHeaderLen+ipv4MinLen+udpHeaderLen+8])
		go p.inject(makeServerReplyFrame(broadcastMAC, clientMAC, newServerIP, clientIP, xid,
			makeDHCPOptions(dhcpMsgAck, &mask, &serverIP, nil, &newServerIP, 1800), false))
	}

	// Pre-start rejection on interfaceLease must not leak ownership.
	badStart, err := state.newRenewer(port, clientMACStr, RenewConfig{RetryInterval: 5 * time.Millisecond}, nil, nil)
	if err != nil {
		t.Fatalf("badStart: %v", err)
	}
	if err := badStart.Start(cancelledCtx); !errors.Is(err, context.Canceled) {
		t.Fatalf("badStart.Start(cancelled) err=%v, want context.Canceled", err)
	}

	ren1, err := state.newRenewer(port, clientMACStr, RenewConfig{
		ServerMAC:     "52:55:0a:00:02:02",
		RetryInterval: 5 * time.Millisecond,
		OnRenew: func(l DHCPLease) {
			select {
			case renewedCh <- l:
			default:
			}
		},
	}, nil, nil)
	if err != nil {
		t.Fatalf("state.newRenewer(1): %v", err)
	}
	// Construct ren2 before ren1 starts with the old server's MAC; ren2 must be rejected
	// while ren1 is active, and once ren1 rebinds to 10.0.2.9 and stops, ren2 must refresh
	// to the 1800s lease and clear its stale ServerMAC on Start.
	ren2, err := state.newRenewer(link, clientMACStr, RenewConfig{
		ServerMAC:     "52:55:0a:00:02:02",
		RetryInterval: 5 * time.Millisecond,
	}, nil, nil)
	if err != nil {
		t.Fatalf("state.newRenewer(2): %v", err)
	}
	if err := ren1.Start(context.Background()); err != nil {
		t.Fatalf("ren1.Start: %v", err)
	}
	if err := ren2.Start(context.Background()); !errors.Is(err, ErrRenewerActive) {
		t.Fatalf("concurrent ren2.Start while ren1 active err=%v, want ErrRenewerActive", err)
	}
	select {
	case <-renewedCh:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for ren1 renewal")
	}
	ren1.Stop()

	gotLease, ok := state.get()
	if !ok || gotLease.LeaseDuration != 1800*time.Second || gotLease.Server != "10.0.2.9" {
		t.Fatalf("state.get()=(%+v, %t), want 1800s lease from 10.0.2.9", gotLease, ok)
	}
	ctx2, cancel2 := context.WithCancel(context.Background())
	if err := ren2.Start(ctx2); err != nil {
		t.Fatalf("ren2.Start after ren1.Stop: %v", err)
	}
	if ren2.Lease().LeaseDuration != 1800*time.Second || ren2.Lease().Server != "10.0.2.9" {
		t.Fatalf("ren2 lease after Start=%+v, want refreshed 1800s lease from 10.0.2.9", ren2.Lease())
	}
	if ren2.cfg.ServerMAC != "" {
		t.Fatalf("ren2.cfg.ServerMAC=%q, want cleared after server change", ren2.cfg.ServerMAC)
	}
	// Cancelling ren2's context releases both interfaceLease and *Link ownership on loop exit.
	cancel2()
	deadline := time.Now().Add(time.Second)
	for ren2.Running() && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	ren3, err := state.newRenewer(link, clientMACStr, RenewConfig{RetryInterval: 5 * time.Millisecond}, nil, nil)
	if err != nil {
		t.Fatalf("state.newRenewer(3) after ctx cancel: %v", err)
	}
	if err := ren3.Start(context.Background()); err != nil {
		t.Fatalf("ren3.Start after ren2 ctx cancel: %v", err)
	}
	ren3.Stop()
}

func TestRenewalAnchorsLeaseAtRequestSendTime(t *testing.T) {
	clientMACStr := "52:54:00:12:34:56"
	serverMACStr := "52:55:0a:00:02:02"
	clientMAC, _ := parseUnicastMAC(clientMACStr)
	serverIP := [4]byte{10, 0, 2, 2}
	clientIP := [4]byte{10, 0, 2, 15}
	mask := [4]byte{255, 255, 255, 0}

	// Test both T1 (unicast) and T2 (broadcast rebind) with a delayed DHCPACK.
	for _, tc := range []struct {
		name          string
		ignoreUnicast bool
	}{
		{name: "T1", ignoreUnicast: false},
		{name: "T2", ignoreUnicast: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			port := newFakeRenewPort()
			const ackDelay = 60 * time.Millisecond
			var mu sync.Mutex
			var firstReqAt time.Time
			var replied bool
			port.onSend = func(frame []byte, p *fakeRenewPort) {
				var dstMAC [6]byte
				copy(dstMAC[:], frame[0:6])
				if tc.ignoreUnicast && dstMAC != broadcastMAC {
					return
				}
				xid := binary.BigEndian.Uint32(frame[ethHeaderLen+ipv4MinLen+udpHeaderLen+4 : ethHeaderLen+ipv4MinLen+udpHeaderLen+8])
				mu.Lock()
				if replied {
					mu.Unlock()
					return
				}
				replied = true
				firstReqAt = time.Now()
				mu.Unlock()
				go func() {
					time.Sleep(ackDelay)
					p.inject(makeServerReplyFrame(broadcastMAC, clientMAC, serverIP, clientIP, xid,
						makeDHCPOptions(dhcpMsgAck, &mask, &serverIP, nil, &serverIP, 3600), false))
				}()
			}

			var state interfaceLease
			state.init(DHCPLease{
				Address:       "10.0.2.15/24",
				Gateway:       "10.0.2.2",
				Server:        "10.0.2.2",
				LeaseDuration: 400 * time.Millisecond,
				RenewalTime:   20 * time.Millisecond,
				RebindTime:    160 * time.Millisecond,
			}, time.Now(), true)

			renewedAtCh := make(chan time.Time, 1)
			ren, err := state.newRenewer(port, clientMACStr, RenewConfig{
				ServerMAC:     serverMACStr,
				RetryInterval: 200 * time.Millisecond,
				OnRenew: func(DHCPLease) {
					select {
					case renewedAtCh <- time.Now():
					default:
					}
				},
			}, nil, nil)
			if err != nil {
				t.Fatalf("newRenewer: %v", err)
			}
			if err := ren.Start(context.Background()); err != nil {
				t.Fatalf("Start: %v", err)
			}
			var ackProcessedAt time.Time
			select {
			case ackProcessedAt = <-renewedAtCh:
			case <-time.After(2 * time.Second):
				ren.Stop()
				t.Fatal("timed out waiting for delayed renewal")
			}
			ren.Stop()

			mu.Lock()
			reqAt := firstReqAt
			mu.Unlock()
			state.mu.RLock()
			gotAcquiredAt := state.acquiredAt
			state.mu.RUnlock()

			if gotAcquiredAt.IsZero() {
				t.Fatal("state.acquiredAt is zero")
			}
			if gotAcquiredAt.After(reqAt.Add(15 * time.Millisecond)) {
				t.Fatalf("acquiredAt=%v was not anchored at DHCPREQUEST send time %v (ackProcessedAt=%v)",
					gotAcquiredAt, reqAt, ackProcessedAt)
			}
			if lag := ackProcessedAt.Sub(gotAcquiredAt); lag < ackDelay/2 {
				t.Fatalf("ackProcessedAt - acquiredAt = %v, want >= %v (anchored at request time, not ACK arrival)",
					lag, ackDelay/2)
			}
		})
	}
}

func TestT1SendErrorFailsWithoutPrematureT2Rebind(t *testing.T) {
	port := newFakeRenewPort()
	sendBoom := errors.New("tx ring full")
	var sendCalls atomic.Int32
	port.sendErr = nil
	// Fail on first SendRawFrame without setting port.Err() and count total attempts.
	customPort := &countingErrPort{base: port, err: sendBoom, calls: &sendCalls}
	errCh := make(chan error, 1)
	ren, err := NewLeaseRenewer(customPort, "52:54:00:12:34:56", DHCPLease{
		Address:       "10.0.2.15/24",
		Gateway:       "10.0.2.2",
		Server:        "10.0.2.2",
		LeaseDuration: time.Hour,
		RenewalTime:   10 * time.Millisecond,
		RebindTime:    30 * time.Minute,
	}, RenewConfig{
		ServerMAC:     "52:55:0a:00:02:02",
		RetryInterval: 5 * time.Millisecond,
		OnError: func(err error) {
			select {
			case errCh <- err:
			default:
			}
		},
	})
	if err != nil {
		t.Fatalf("NewLeaseRenewer: %v", err)
	}
	if err := ren.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	select {
	case gotErr := <-errCh:
		if !errors.Is(gotErr, sendBoom) {
			t.Fatalf("OnError=%v, want %v", gotErr, sendBoom)
		}
	case <-time.After(time.Second):
		ren.Stop()
		t.Fatal("timed out waiting for T1 send error")
	}
	ren.Stop()
	if got := sendCalls.Load(); got != 1 {
		t.Fatalf("SendRawFrame calls=%d, want 1 (must not advance to T2 rebind on T1 send error)", got)
	}
}

type countingErrPort struct {
	base  *fakeRenewPort
	err   error
	calls *atomic.Int32
}

func (p *countingErrPort) SendRawFrame([]byte) error {
	p.calls.Add(1)
	return p.err
}
func (p *countingErrPort) AddTap(fn func([]byte)) func() { return p.base.AddTap(fn) }
func (p *countingErrPort) Err() error                    { return nil }

func TestPausedSendRetriesQuicklyBeforeBackoff(t *testing.T) {
	clientMACStr := "52:54:00:12:34:56"
	clientMAC, _ := parseUnicastMAC(clientMACStr)
	serverIP := [4]byte{10, 0, 2, 2}
	clientIP := [4]byte{10, 0, 2, 15}
	mask := [4]byte{255, 255, 255, 0}

	port := newFakeRenewPort()
	var attempts atomic.Int32
	pausedPort := &pausedOncePort{
		base:     port,
		attempts: &attempts,
		onSuccess: func(frame []byte) {
			xid := binary.BigEndian.Uint32(frame[ethHeaderLen+ipv4MinLen+udpHeaderLen+4 : ethHeaderLen+ipv4MinLen+udpHeaderLen+8])
			go port.inject(makeServerReplyFrame(broadcastMAC, clientMAC, serverIP, clientIP, xid,
				makeDHCPOptions(dhcpMsgAck, &mask, &serverIP, nil, &serverIP, 3600), false))
		},
	}

	// Simulate a 24h lease's T1->T2 window (9h remaining) with default 60s RetryInterval.
	// A single ErrPaused must retry quickly (~250ms), NOT back off by ~4.5h.
	ctx, cancel := context.WithTimeout(context.Background(), 9*time.Hour)
	defer cancel()
	start := time.Now()
	got, err := RenewDHCPOnce(ctx, pausedPort, clientMACStr, DHCPLease{
		Address:       "10.0.2.15/24",
		Gateway:       "10.0.2.2",
		Server:        "10.0.2.2",
		LeaseDuration: 24 * time.Hour,
	}, false, RenewConfig{})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("RenewDHCPOnce after pause: %v", err)
	}
	if got.LeaseDuration != time.Hour {
		t.Fatalf("LeaseDuration=%v, want 1h", got.LeaseDuration)
	}
	if attempts.Load() < 2 {
		t.Fatalf("attempts=%d, want >= 2", attempts.Load())
	}
	if elapsed > 2*time.Second {
		t.Fatalf("paused retry took %v, want < 2s (should not apply RFC unanswered-request backoff)", elapsed)
	}
}

type pausedOncePort struct {
	base      *fakeRenewPort
	attempts  *atomic.Int32
	onSuccess func(frame []byte)
}

func (p *pausedOncePort) SendRawFrame(frame []byte) error {
	if p.attempts.Add(1) == 1 {
		return ErrPaused
	}
	if p.onSuccess != nil {
		p.onSuccess(append([]byte(nil), frame...))
	}
	return nil
}
func (p *pausedOncePort) AddTap(fn func([]byte)) func() { return p.base.AddTap(fn) }
func (p *pausedOncePort) Err() error                    { return nil }

func TestLateACKAfterDeadlineRejected(t *testing.T) {
	clientMACStr := "52:54:00:12:34:56"
	clientMAC, _ := parseUnicastMAC(clientMACStr)
	serverIP := [4]byte{10, 0, 2, 2}
	clientIP := [4]byte{10, 0, 2, 15}
	mask := [4]byte{255, 255, 255, 0}

	// Repeat several times so Go's pseudo-random select cannot mask a bug where
	// both replyCh and ctx.Done() are ready simultaneously after the deadline.
	for i := 0; i < 10; i++ {
		port := newFakeRenewPort()
		port.onSend = func(frame []byte, p *fakeRenewPort) {
			xid := binary.BigEndian.Uint32(frame[ethHeaderLen+ipv4MinLen+udpHeaderLen+4 : ethHeaderLen+ipv4MinLen+udpHeaderLen+8])
			time.Sleep(15 * time.Millisecond)
			p.inject(makeServerReplyFrame(broadcastMAC, clientMAC, serverIP, clientIP, xid,
				makeDHCPOptions(dhcpMsgAck, &mask, &serverIP, nil, &serverIP, 3600), false))
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
		_, err := RenewDHCPOnce(ctx, port, clientMACStr, DHCPLease{
			Address:       "10.0.2.15/24",
			Gateway:       "10.0.2.2",
			Server:        "10.0.2.2",
			LeaseDuration: time.Minute,
		}, true, RenewConfig{RetryInterval: 5 * time.Millisecond})
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("iter %d: late ACK after deadline err=%v, want context.DeadlineExceeded", i, err)
		}
	}
}

// RFC 2131 §3.3: lease time 0xffffffff means an infinite lease, which is never
// renewed. T2 used to be (LeaseDuration*7)/8, which overflows int64 for leases
// over ~1.32e9 s; T1 then clamped to 1 ms and the renewer re-sent DHCPREQUEST
// about every millisecond after each ACK.
func TestLeaseRenewerInfiniteLeaseNeverRenews(t *testing.T) {
	clientMACStr := "52:54:00:12:34:56"
	clientMAC, _ := parseUnicastMAC(clientMACStr)
	serverIP := [4]byte{10, 0, 2, 2}
	clientIP := [4]byte{10, 0, 2, 15}
	mask := [4]byte{255, 255, 255, 0}
	infinite := time.Duration(0xffffffff) * time.Second

	// An infinite lease has no T1/T2, even when the server also sends options 58/59.
	for _, l := range []DHCPLease{
		{LeaseDuration: infinite},
		{LeaseDuration: infinite, RenewalTime: time.Hour, RebindTime: 2 * time.Hour},
	} {
		if t1, t2, renew := leaseTimers(l); renew {
			t.Fatalf("leaseTimers(%+v)=(%v, %v, true), want renew=false for an infinite lease", l, t1, t2)
		}
	}
	// Finite leases either side of the old d*7 int64 overflow (1317624577 s was the
	// first to wrap) and up to the largest finite lease keep the 1/2 and 7/8 defaults.
	for _, secs := range []uint32{3600, 1317624576, 1317624577, 0xfffffffe} {
		t1, t2, renew := leaseTimers(DHCPLease{LeaseDuration: time.Duration(secs) * time.Second})
		wantT1 := time.Duration(secs) * 500 * time.Millisecond
		wantT2 := time.Duration(secs) * 875 * time.Millisecond
		if !renew || t1 != wantT1 || t2 != wantT2 {
			t.Fatalf("leaseTimers(%ds)=(%v, %v, %t), want (%v, %v, true)", secs, t1, t2, renew, wantT1, wantT2)
		}
	}

	// The fake server answers every DHCPREQUEST with an infinite-lease DHCPACK.
	newAckingPort := func(sends *atomic.Int32) *fakeRenewPort {
		port := newFakeRenewPort()
		port.onSend = func(frame []byte, p *fakeRenewPort) {
			sends.Add(1)
			xid := binary.BigEndian.Uint32(frame[ethHeaderLen+ipv4MinLen+udpHeaderLen+4 : ethHeaderLen+ipv4MinLen+udpHeaderLen+8])
			go p.inject(makeServerReplyFrame(broadcastMAC, clientMAC, serverIP, clientIP, xid,
				makeDHCPOptions(dhcpMsgAck, &mask, &serverIP, nil, &serverIP, 0xffffffff), false))
		}
		return port
	}

	t.Run("RenewalACK", func(t *testing.T) {
		// A T1 renewal ACK granting an infinite lease is applied once and never re-requested.
		var sends atomic.Int32
		sendsAtRenew := make(chan int32, 1)
		ren, err := NewLeaseRenewer(newAckingPort(&sends), clientMACStr, DHCPLease{
			Address:       "10.0.2.15/24",
			Gateway:       "10.0.2.2",
			Server:        "10.0.2.2",
			LeaseDuration: time.Hour,
		}, RenewConfig{
			AcquiredAt:    time.Now().Add(-40 * time.Minute), // past T1 (30m), before T2 (52.5m)
			RetryInterval: 5 * time.Millisecond,
			OnRenew: func(DHCPLease) {
				select {
				case sendsAtRenew <- sends.Load():
				default:
				}
			},
		})
		if err != nil {
			t.Fatalf("NewLeaseRenewer: %v", err)
		}
		if err := ren.Start(context.Background()); err != nil {
			t.Fatalf("Start: %v", err)
		}
		defer ren.Stop()
		var atRenew int32
		select {
		case atRenew = <-sendsAtRenew:
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for the renewal granting an infinite lease")
		}
		time.Sleep(100 * time.Millisecond)
		if got := sends.Load(); got != atRenew {
			t.Fatalf("DHCPREQUEST sends grew %d -> %d after an infinite-lease ACK, want no renewal", atRenew, got)
		}
		if got := ren.Lease().LeaseDuration; got != infinite {
			t.Fatalf("renewed LeaseDuration=%v, want infinite %v", got, infinite)
		}
		if !ren.Running() || ren.Renewals() != 1 || ren.Err() != nil {
			t.Fatalf("infinite lease running=%t renewals=%d err=%v, want running with 1 renewal and no error",
				ren.Running(), ren.Renewals(), ren.Err())
		}
		ren.Stop()
		if ren.Running() || ren.Err() != nil {
			t.Fatalf("after Stop running=%t err=%v, want stopped with no error", ren.Running(), ren.Err())
		}
	})

	t.Run("InitialLease", func(t *testing.T) {
		// A renewer started on an infinite lease sends nothing and idles until its
		// context is cancelled.
		var sends atomic.Int32
		ren, err := NewLeaseRenewer(newAckingPort(&sends), clientMACStr, DHCPLease{
			Address:       "10.0.2.15/24",
			Gateway:       "10.0.2.2",
			Server:        "10.0.2.2",
			LeaseDuration: infinite,
		}, RenewConfig{RetryInterval: 5 * time.Millisecond})
		if err != nil {
			t.Fatalf("NewLeaseRenewer: %v", err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		if err := ren.Start(ctx); err != nil {
			t.Fatalf("Start: %v", err)
		}
		defer ren.Stop()
		time.Sleep(50 * time.Millisecond)
		if got := sends.Load(); got != 0 || !ren.Running() || ren.Renewals() != 0 {
			t.Fatalf("infinite initial lease sends=%d running=%t renewals=%d, want 0 sends while running idle",
				got, ren.Running(), ren.Renewals())
		}
		cancel()
		deadline := time.Now().Add(time.Second)
		for ren.Running() && time.Now().Before(deadline) {
			time.Sleep(2 * time.Millisecond)
		}
		if ren.Running() || ren.Err() != nil || sends.Load() != 0 {
			t.Fatalf("after cancel running=%t err=%v sends=%d, want stopped, no error, 0 sends",
				ren.Running(), ren.Err(), sends.Load())
		}
	})
}

func TestLeaseRenewerNotifyLinkUpVerification(t *testing.T) {
	clientMACStr := "52:54:00:12:34:56"
	clientMAC, _ := parseUnicastMAC(clientMACStr)
	serverIP := [4]byte{10, 0, 2, 2}
	clientIP := [4]byte{10, 0, 2, 15}
	mask := [4]byte{255, 255, 255, 0}

	baseLease := DHCPLease{
		Address:       "10.0.2.15/24",
		Gateway:       "10.0.2.2",
		Server:        "10.0.2.2",
		LeaseDuration: time.Hour,
	}

	t.Run("NilAndNegativeConfig", func(t *testing.T) {
		var nilRen *LeaseRenewer
		nilRen.NotifyLinkUp() // must not panic
		for _, bad := range []RenewConfig{
			{LinkUpDebounce: -time.Millisecond},
			{LinkUpProbeTimeout: -time.Millisecond},
		} {
			if _, err := NewLeaseRenewer(newFakeRenewPort(), clientMACStr, baseLease, bad); !errors.Is(err, ErrDHCPInvalidConfig) {
				t.Fatalf("bad config %+v err=%v want ErrDHCPInvalidConfig", bad, err)
			}
		}
	})

	t.Run("ImmediateBroadcastVerifyRefreshesAndDebounces", func(t *testing.T) {
		var sends atomic.Int32
		renewedCh := make(chan DHCPLease, 4)
		port := newFakeRenewPort()
		port.onSend = func(frame []byte, p *fakeRenewPort) {
			sends.Add(1)
			var dstMAC [6]byte
			copy(dstMAC[:], frame[0:6])
			if dstMAC != broadcastMAC {
				t.Errorf("link-up verify dstMAC=%x, want broadcast", dstMAC)
			}
			if req, _, err := parseDHCPClientRequest(frame); err != nil || req != initRebootRequest(clientIP) {
				t.Errorf("link-up verify request=%+v err=%v, want INIT-REBOOT %+v", req, err, initRebootRequest(clientIP))
			}
			xid := binary.BigEndian.Uint32(frame[ethHeaderLen+ipv4MinLen+udpHeaderLen+4 : ethHeaderLen+ipv4MinLen+udpHeaderLen+8])
			go p.inject(makeServerReplyFrame(broadcastMAC, clientMAC, serverIP, clientIP, xid,
				makeDHCPOptions(dhcpMsgAck, &mask, &serverIP, nil, &serverIP, 7200), false))
		}
		ren, err := NewLeaseRenewer(port, clientMACStr, baseLease, RenewConfig{
			RetryInterval:      5 * time.Millisecond,
			LinkUpDebounce:     120 * time.Millisecond,
			LinkUpProbeTimeout: 200 * time.Millisecond,
			OnRenew: func(l DHCPLease) {
				renewedCh <- l
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := ren.Start(context.Background()); err != nil {
			t.Fatal(err)
		}
		defer ren.Stop()

		// Rapid burst collapses cleanly under debounce.
		ren.NotifyLinkUp()
		ren.NotifyLinkUp()
		ren.NotifyLinkUp()
		select {
		case got := <-renewedCh:
			if got.LeaseDuration != 2*time.Hour {
				t.Fatalf("renewed lease=%+v", got)
			}
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for link-up lease refresh")
		}
		if sends.Load() != 1 || ren.Renewals() != 1 {
			t.Fatalf("burst caused sends=%d renewals=%d, want 1/1", sends.Load(), ren.Renewals())
		}

		// After debounce expires, another NotifyLinkUp verifies again.
		time.Sleep(150 * time.Millisecond)
		ren.NotifyLinkUp()
		select {
		case <-renewedCh:
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for post-debounce link-up refresh")
		}
		if ren.Renewals() != 2 {
			t.Fatalf("renewals=%d want 2", ren.Renewals())
		}
	})

	t.Run("TimeoutPreservesValidLease", func(t *testing.T) {
		var sends atomic.Int32
		errCh := make(chan error, 1)
		port := newFakeRenewPort()
		port.onSend = func([]byte, *fakeRenewPort) {
			sends.Add(1) // Drop silently so probe times out cleanly.
		}
		ren, err := NewLeaseRenewer(port, clientMACStr, baseLease, RenewConfig{
			RetryInterval:      10 * time.Millisecond,
			LinkUpDebounce:     10 * time.Millisecond,
			LinkUpProbeTimeout: 40 * time.Millisecond,
			OnError:            func(e error) { errCh <- e },
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := ren.Start(context.Background()); err != nil {
			t.Fatal(err)
		}
		defer ren.Stop()

		ren.NotifyLinkUp()
		deadline := time.Now().Add(time.Second)
		for sends.Load() == 0 && time.Now().Before(deadline) {
			time.Sleep(2 * time.Millisecond)
		}
		if sends.Load() == 0 {
			t.Fatal("link-up probe did not transmit")
		}
		time.Sleep(80 * time.Millisecond)
		select {
		case e := <-errCh:
			t.Fatalf("unexpected error on silent link-up probe: %v", e)
		default:
		}
		if !ren.Running() || ren.Err() != nil {
			t.Fatalf("renewer died after silent link-up probe: running=%t err=%v", ren.Running(), ren.Err())
		}
	})

	t.Run("NAKHaltsImmediatelyBeforeT1", func(t *testing.T) {
		errCh := make(chan error, 1)
		port := newFakeRenewPort()
		port.onSend = func(frame []byte, p *fakeRenewPort) {
			xid := binary.BigEndian.Uint32(frame[ethHeaderLen+ipv4MinLen+udpHeaderLen+4 : ethHeaderLen+ipv4MinLen+udpHeaderLen+8])
			go p.inject(makeServerReplyFrame(broadcastMAC, clientMAC, serverIP, clientIP, xid,
				makeDHCPOptions(dhcpMsgNak, nil, nil, nil, &serverIP, 0), false))
		}
		ren, err := NewLeaseRenewer(port, clientMACStr, baseLease, RenewConfig{
			RetryInterval:      5 * time.Millisecond,
			LinkUpDebounce:     10 * time.Millisecond,
			LinkUpProbeTimeout: 200 * time.Millisecond,
			OnError:            func(e error) { errCh <- e },
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := ren.Start(context.Background()); err != nil {
			t.Fatal(err)
		}
		defer ren.Stop()

		ren.NotifyLinkUp()
		select {
		case e := <-errCh:
			if !errors.Is(e, ErrLeaseNAK) {
				t.Fatalf("err=%v want ErrLeaseNAK", e)
			}
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for immediate NAK halt")
		}
	})

	t.Run("MovedKitHaltsOnNewNetworkNAK", func(t *testing.T) {
		// The kit holds 10.0.2.15/24 from 10.0.2.2, and its link returns on
		// 192.168.7.0/24. That network's server (192.168.7.1) is modelled on
		// RFC 2131 §4.3.2: it NAKs an INIT-REBOOT request (ciaddr 0, option 50)
		// for an address on the wrong network, and stays silent for a request
		// with ciaddr set, which it holds no lease for. A REBINDING-form probe
		// gets no answer here and keeps the foreign address until T1/T2.
		newNet := netip.MustParsePrefix("192.168.7.0/24")
		newServerIP := [4]byte{192, 168, 7, 1}
		sent := make(chan dhcpClientRequest, 8)
		dev := newFakeDHCPDevice(clientMACStr, false)
		dev.onTx = func(frame []byte, d *fakeDHCPDevice) {
			req, xid, err := parseDHCPClientRequest(frame)
			if err != nil {
				t.Errorf("link-up probe frame: %v", err)
				return
			}
			select {
			case sent <- req:
			default:
			}
			if req.Ciaddr != ([4]byte{}) || !req.HasRequestedIP || req.HasServerID {
				return // not INIT-REBOOT: nothing shows the client moved
			}
			if newNet.Contains(netip.AddrFrom4(req.RequestedIP)) {
				return // right network; not exercised
			}
			// Wrong network. giaddr is 0, so the NAK is broadcast.
			d.enqueueRX(makeServerReplyFrame(broadcastMAC, clientMAC, newServerIP, [4]byte{}, xid,
				makeDHCPOptions(dhcpMsgNak, nil, nil, nil, &newServerIP, 0), false))
		}
		link := &Link{stack: &fakeStack{}, device: dev, state: &linkState{}}
		linkCtx, linkCancel := context.WithCancel(context.Background())
		linkDone := make(chan error, 1)
		go func() { linkDone <- link.Start(linkCtx) }()
		defer func() {
			linkCancel()
			<-linkDone
		}()

		errCh := make(chan error, 1)
		ren, err := NewLeaseRenewer(link, clientMACStr, baseLease, RenewConfig{
			RetryInterval:      5 * time.Millisecond,
			LinkUpDebounce:     10 * time.Millisecond,
			LinkUpProbeTimeout: 2 * time.Second,
			OnError: func(e error) {
				select {
				case errCh <- e:
				default:
				}
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := ren.Start(context.Background()); err != nil {
			t.Fatal(err)
		}
		defer ren.Stop()

		ren.NotifyLinkUp()
		select {
		case e := <-errCh:
			if !errors.Is(e, ErrLeaseNAK) {
				t.Fatalf("err=%v want ErrLeaseNAK", e)
			}
		case <-time.After(time.Second):
			t.Fatal("no DHCPNAK for the link-up probe: the moved kit keeps 10.0.2.15 until T1/T2")
		}
		if !errors.Is(link.Err(), ErrLeaseNAK) {
			t.Fatalf("link.Err()=%v, want ErrLeaseNAK (link not halted)", link.Err())
		}
		select {
		case got := <-sent:
			if want := initRebootRequest(clientIP); got != want {
				t.Fatalf("link-up probe=%+v, want INIT-REBOOT %+v", got, want)
			}
		default:
			t.Fatal("no link-up probe was sent")
		}
	})
}

// dhcpClientRequest holds the fields of a client DHCPREQUEST that tell the
// RFC 2131 §4.3.2 RENEWING, REBINDING and INIT-REBOOT forms apart.
type dhcpClientRequest struct {
	DstMAC         [6]byte
	SrcIP, DstIP   [4]byte
	Ciaddr         [4]byte
	BroadcastFlag  bool
	HasRequestedIP bool    // option 50 present
	RequestedIP    [4]byte // option 50 value
	HasServerID    bool    // option 54 present
}

// initRebootRequest is the RFC 2131 §4.3.2 INIT-REBOOT form for leaseIP:
// broadcast from 0.0.0.0, ciaddr 0, leaseIP in option 50, no option 54.
func initRebootRequest(leaseIP [4]byte) dhcpClientRequest {
	return dhcpClientRequest{
		DstMAC:         broadcastMAC,
		DstIP:          [4]byte{255, 255, 255, 255},
		BroadcastFlag:  true,
		HasRequestedIP: true,
		RequestedIP:    leaseIP,
	}
}

// parseDHCPClientRequest decodes a client DHCPREQUEST frame and its xid. It
// returns errors rather than calling t.Fatal, so fake-port callbacks that run
// on the renewer goroutine can use it.
func parseDHCPClientRequest(frame []byte) (dhcpClientRequest, uint32, error) {
	var req dhcpClientRequest
	if len(frame) < ethHeaderLen+ipv4MinLen+udpHeaderLen+dhcpMinPayload {
		return req, 0, fmt.Errorf("short frame: %d bytes", len(frame))
	}
	copy(req.DstMAC[:], frame[0:6])
	ip := frame[ethHeaderLen : ethHeaderLen+ipv4MinLen]
	if internetChecksum(ip) != 0 {
		return req, 0, errors.New("bad IPv4 header checksum")
	}
	copy(req.SrcIP[:], ip[12:16])
	copy(req.DstIP[:], ip[16:20])
	udp := frame[ethHeaderLen+ipv4MinLen:]
	udpLen := int(binary.BigEndian.Uint16(udp[4:6]))
	if udpLen < udpHeaderLen+dhcpMinPayload || udpLen > len(udp) {
		return req, 0, fmt.Errorf("bad UDP length %d", udpLen)
	}
	udp = udp[:udpLen]
	if udpChecksum(ip[12:16], ip[16:20], udp) != 0 {
		return req, 0, errors.New("bad UDP checksum")
	}
	bootp := udp[udpHeaderLen:]
	if bootp[0] != bootpRequest || [4]byte(bootp[bootpFixedLen:dhcpMinPayload]) != dhcpMagicCookie {
		return req, 0, errors.New("not a BOOTP request carrying the DHCP magic cookie")
	}
	xid := binary.BigEndian.Uint32(bootp[4:8])
	req.BroadcastFlag = binary.BigEndian.Uint16(bootp[10:12])&dhcpFlagBroadcast != 0
	copy(req.Ciaddr[:], bootp[12:16])
	var msgType byte
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
		if i+2 > len(opts) || i+2+int(opts[i+1]) > len(opts) {
			return req, 0, fmt.Errorf("truncated option %d", code)
		}
		val := opts[i+2 : i+2+int(opts[i+1])]
		switch code {
		case dhcpOptMsgType:
			if len(val) == 1 {
				msgType = val[0]
			}
		case dhcpOptRequestedIP:
			if len(val) != 4 {
				return req, 0, fmt.Errorf("option 50 length %d", len(val))
			}
			req.HasRequestedIP = true
			copy(req.RequestedIP[:], val)
		case dhcpOptServerID:
			req.HasServerID = true
		}
		i += 2 + len(val)
	}
	if msgType != dhcpMsgRequest {
		return req, 0, fmt.Errorf("DHCP message type %d, want DHCPREQUEST", msgType)
	}
	return req, xid, nil
}

// RFC 2131 §4.3.2: RENEWING and REBINDING requests carry the lease address in
// ciaddr and the IPv4 source with no option 50. An INIT-REBOOT request has
// both zero, the lease address in option 50 and no server identifier.
func TestDHCPRequestWireFormPerState(t *testing.T) {
	clientMACStr := "52:54:00:12:34:56"
	serverMACStr := "52:55:0a:00:02:02"
	clientMAC, _ := parseUnicastMAC(clientMACStr)
	serverMAC, _ := parseUnicastMAC(serverMACStr)
	serverIP := [4]byte{10, 0, 2, 2}
	leaseIP := [4]byte{10, 0, 2, 15}
	mask := [4]byte{255, 255, 255, 0}
	limited := [4]byte{255, 255, 255, 255}
	current := DHCPLease{
		Address:       "10.0.2.15/24",
		Gateway:       "10.0.2.2",
		Server:        "10.0.2.2",
		LeaseDuration: 30 * time.Minute,
	}

	for _, tc := range []struct {
		name  string
		state dhcpRequestState
		want  dhcpClientRequest
	}{
		{"RENEWING", requestRenewing,
			dhcpClientRequest{DstMAC: serverMAC, SrcIP: leaseIP, DstIP: serverIP, Ciaddr: leaseIP}},
		{"REBINDING", requestRebinding,
			dhcpClientRequest{DstMAC: broadcastMAC, SrcIP: leaseIP, DstIP: limited, Ciaddr: leaseIP, BroadcastFlag: true}},
		{"INIT-REBOOT", requestInitReboot, initRebootRequest(leaseIP)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sent := make(chan []byte, 1)
			port := newFakeRenewPort()
			port.onSend = func(frame []byte, p *fakeRenewPort) {
				select {
				case sent <- frame:
				default:
				}
				if _, xid, err := parseDHCPClientRequest(frame); err == nil {
					go p.inject(makeServerReplyFrame(broadcastMAC, clientMAC, serverIP, leaseIP, xid,
						makeDHCPOptions(dhcpMsgAck, &mask, &serverIP, nil, &serverIP, 3600), false))
				}
			}
			// ServerMAC is set in every case: only RENEWING may unicast to it.
			lease, _, err := renewDHCPOnceWithTime(context.Background(), port, clientMACStr, current, tc.state,
				RenewConfig{ServerMAC: serverMACStr, RetryInterval: 5 * time.Millisecond})
			if err != nil || lease.Address != current.Address || lease.LeaseDuration != time.Hour {
				t.Fatalf("renewDHCPOnceWithTime=(%+v, %v), want the ACKed 1h lease", lease, err)
			}
			got, _, err := parseDHCPClientRequest(<-sent)
			if err != nil {
				t.Fatalf("request frame: %v", err)
			}
			if got != tc.want {
				t.Fatalf("request form=%+v, want %+v", got, tc.want)
			}
		})
	}
}

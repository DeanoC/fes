// SPDX-License-Identifier: GPL-3.0-or-later

package network

import (
	"context"
	"encoding/binary"
	"errors"
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

	// Preserving original AcquiredAt in the past triggers T1 immediately without extending expiry.
	pastRenewed := make(chan DHCPLease, 1)
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
		OnRenew:       func(l DHCPLease) { pastRenewed <- l },
	})
	if err != nil {
		t.Fatalf("NewLeaseRenewer past: %v", err)
	}
	if err := pastRen.Start(context.Background()); err != nil {
		t.Fatalf("Start past: %v", err)
	}
	select {
	case <-pastRenewed:
	case <-time.After(time.Second):
		t.Fatal("past AcquiredAt did not trigger immediate T1 renewal")
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

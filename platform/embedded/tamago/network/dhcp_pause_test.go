// SPDX-License-Identifier: GPL-3.0-or-later

package network

import (
	"context"
	"encoding/binary"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Use the actual Interface receive worker, renewal timer and DHCP packets. No
// address of the retired incarnation may escape after the hardware gate opens.
func TestLeaseExpiryWithContainedC64AndFreshAddress(t *testing.T) {
	for _, mode := range []string{"timer-expires-during-play", "fresh", "unexpired", "expired-during-PHY", "no-DHCP", "publish-failure", "failed-drain", "missing-grant", "foreign-stop", "authoritative-NAK", "changed-ACK", "raw-device-failure"} {
		t.Run(mode, func(t *testing.T) {
			lifetime, cancel := context.WithCancel(t.Context())
			defer cancel()
			dev := newFakeDHCPDevice("02:11:22:33:44:55", true)
			server, mask, newIP := [4]byte{192, 0, 2, 1}, [4]byte{255, 255, 255, 0}, [4]byte{192, 0, 2, 88}
			config, _ := staticConfig("192.0.2.7/24", "192.0.2.1")
			var current atomic.Pointer[Interface]
			duration := 45 * time.Second
			if mode == "timer-expires-during-play" {
				duration = 250 * time.Millisecond
			}
			old, err := newInterface(lifetime, config, dev, DHCPLease{Address: config.Address, Gateway: config.Gateway, Server: config.Gateway, LeaseDuration: duration}, time.Now(), true, &current)
			if err != nil {
				t.Fatal(err)
			}
			current.Store(old)
			var stops, starts, unavailable, withdraws, publications atomic.Int32
			expire := func() {
				old.leaseState.mu.Lock()
				old.leaseState.acquiredAt = time.Now().Add(-2 * time.Minute)
				old.leaseState.mu.Unlock()
			}
			c, err := old.BindControl(DMAControl{Stop: func(context.Context) error {
				if stops.Add(1) == 2 && mode == "failed-drain" {
					return errors.New("unproven drain")
				}
				return nil
			}, Start: func(context.Context) error {
				starts.Add(1)
				if mode == "expired-during-PHY" {
					expire()
				}
				return nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			if err = c.Claim(); err != nil {
				t.Fatal(err)
			}
			var fresh *Interface
			defer func() {
				cancel()
				if fresh != nil {
					<-fresh.receiverDone
				}
				<-old.receiverDone
			}()
			if err = old.RecoverDHCPAfterPause(c, lifetime, DHCPConfig{Timeout: 100 * time.Millisecond, InitialInterval: 10 * time.Millisecond},
				func() { withdraws.Add(1) }, func(next *Interface) error {
					publications.Add(1)
					if next.Address() != "192.0.2.88/24" || old.Err() == nil {
						return errors.New("wrong network incarnation")
					}
					if mode == "publish-failure" {
						return errors.New("cannot publish")
					}
					fresh = next
					return nil
				}, func(error) { unavailable.Add(1) }); err != nil {
				t.Fatal(err)
			}
			if mode == "authoritative-NAK" || mode == "changed-ACK" {
				// Send a real renewing client an authoritative answer while it
				// still owns the address. RenewLeaseOnce must close the old
				// link, yet the local launcher still needs its physical stop.
				dev.mu.Lock()
				dev.onTx = func(packet []byte, d *fakeDHCPDevice) {
					if len(packet) < ethHeaderLen+ipv4MinLen+udpHeaderLen+dhcpMinPayload || packet[23] != 17 || binary.BigEndian.Uint16(packet[36:38]) != 67 {
						return
					}
					_, xid, mac, _, _, _ := extractClientDHCP(t, packet)
					msg := byte(dhcpMsgAck)
					if mode == "authoritative-NAK" {
						msg = dhcpMsgNak
					}
					d.enqueueRX(makeServerReplyFrame(broadcastMAC, mac, server, newIP, xid, makeDHCPOptions(msg, &mask, &server, nil, &server, 120), false))
				}
				dev.mu.Unlock()
				renewCtx, stop := context.WithTimeout(lifetime, 2*time.Second)
				_, err := old.RenewLeaseOnce(renewCtx, false, RenewConfig{RetryInterval: 10 * time.Millisecond})
				stop()
				want := ErrLeaseChanged
				if mode == "authoritative-NAK" {
					want = ErrLeaseNAK
				}
				if !errors.Is(err, want) || !errors.Is(old.Err(), want) {
					t.Fatalf("retire current lease: %v, %v, want %v", err, old.Err(), want)
				}
			}
			if err = c.PauseNetwork(lifetime); err != nil {
				t.Fatal(err)
			}
			var started, scope, returned atomic.Bool
			var messagesMu sync.Mutex
			var messages []byte
			dev.mu.Lock()
			dev.onTx = func(frame []byte, d *fakeDHCPDevice) {
				if !started.Load() {
					return
				}
				if len(frame) >= 34 && binary.BigEndian.Uint16(frame[12:14]) == 0x0800 && frame[26] == 192 && frame[29] == 7 {
					t.Error("sent with expired IPv4 address")
				}
				if len(frame) < ethHeaderLen+ipv4MinLen+udpHeaderLen+dhcpMinPayload || frame[23] != 17 || binary.BigEndian.Uint16(frame[36:38]) != 67 {
					return
				}
				if !scope.Load() {
					t.Error("DHCP before protected scope confirmed")
				}
				mt, xid, mac, _, _, _ := extractClientDHCP(t, frame)
				messagesMu.Lock()
				messages = append(messages, mt)
				messagesMu.Unlock()
				if mode == "no-DHCP" || mode == "failed-drain" || mode == "foreign-stop" {
					return
				}
				reply := byte(dhcpMsgOffer)
				if mt == dhcpMsgRequest {
					reply = dhcpMsgAck
				} else if mt != dhcpMsgDiscover {
					t.Errorf("unexpected DHCP type %d", mt)
					return
				}
				d.enqueueRX(makeServerReplyFrame(broadcastMAC, mac, server, newIP, xid, makeDHCPOptions(reply, &mask, &server, nil, &server, 120), false))
			}
			dev.mu.Unlock()
			started.Store(true)
			if mode == "timer-expires-during-play" {
				ren, err := old.NewLeaseRenewer(RenewConfig{})
				if err != nil {
					t.Fatal(err)
				}
				if err := ren.Start(lifetime); err != nil {
					t.Fatal(err)
				}
				defer ren.Stop()
				select {
				case <-old.Done():
					if err := old.Err(); !errors.Is(err, ErrLeaseExpired) {
						t.Fatalf("lease timer: %v", err)
					}
				case <-time.After(3 * time.Second):
					t.Fatal("lease stayed alive during extended C64 session")
				}
			} else if mode != "unexpired" && mode != "expired-during-PHY" && mode != "authoritative-NAK" && mode != "changed-ACK" {
				expire()
				// The normal DHCP terminal callback stores ErrLeaseExpired
				// before ending its receive worker.
				old.bridge.fail(ErrLeaseExpired)
				old.stop(ErrLeaseExpired)
			}
			if err = c.PauseNetwork(lifetime); err != nil {
				t.Fatalf("local C64 Stop after pause: %v", err)
			}
			if mode == "raw-device-failure" {
				dev.mu.Lock()
				dev.txErr = ErrLeaseNAK // A device error can even use a DHCP sentinel.
				dev.mu.Unlock()
			}
			err = c.ResumeNetworkWithScope(lifetime, func() error {
				if starts.Load() != 1 || stops.Load() != 1 || !scope.CompareAndSwap(false, true) {
					t.Fatal("scope grant out of order")
				}
				if mode == "missing-grant" {
					return errors.New("current C64 scope could not be proven")
				}
				return nil
			}, func() error {
				if stops.Load() != 2 || !scope.CompareAndSwap(true, false) {
					t.Fatal("scope retirement out of order")
				}
				if mode == "foreign-stop" {
					return errors.New("stop receipt from another grant")
				}
				returned.Store(true)
				return nil
			})
			if mode == "failed-drain" || mode == "foreign-stop" || mode == "missing-grant" || mode == "raw-device-failure" {
				if err == nil || c.PauseNetwork(lifetime) == nil {
					t.Fatal("hardware stop failure allowed release")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			messagesMu.Lock()
			sent := append([]byte(nil), messages...)
			messagesMu.Unlock()
			if mode == "unexpired" {
				if len(sent) != 0 || starts.Load() != 1 || stops.Load() != 1 || withdraws.Load() != 0 || current.Load() != old {
					t.Fatal("healthy fast path changed")
				}
				return
			}
			if old.bridge.Available() == nil {
				t.Fatal("old link remained usable")
			}
			if mode == "no-DHCP" || mode == "publish-failure" {
				if !c.state.paused || stops.Load() != 2 || unavailable.Load() != 1 || current.Load() != old || !returned.Load() {
					t.Fatal("lost physical containment or promoted failed DHCP")
				}
				if err := c.PauseNetwork(lifetime); err != nil {
					t.Fatalf("local Park after IP failure: %v", err)
				}
				return
			}
			if len(sent) != 2 || sent[0] != dhcpMsgDiscover || sent[1] != dhcpMsgRequest || publications.Load() != 1 ||
				current.Load() != fresh || c.state.link != fresh.bridge || fresh.bridge.Available() != nil || c.state.paused || stops.Load() != 1 {
				t.Fatalf("fresh address not verified, DHCP packets %v", sent)
			}
		})
	}
}

func TestUnusableDHCPLeaseCannotStartARP(t *testing.T) {
	cfg, _ := staticConfig("192.0.2.4/24", "192.0.2.1")
	for _, lease := range []struct {
		name     string
		at       time.Time
		duration time.Duration
	}{
		{"no option 51", time.Now(), 0}, {"delayed ACK", time.Now().Add(-time.Minute), time.Second},
		{"missing request time", time.Time{}, time.Hour},
	} {
		t.Run(lease.name, func(t *testing.T) {
			d := newFakeDHCPDevice("02:11:22:33:44:55", true)
			_, err := newInterface(t.Context(), cfg, d, DHCPLease{Address: cfg.Address, Gateway: cfg.Gateway, Server: cfg.Gateway, LeaseDuration: lease.duration}, lease.at, true, new(atomic.Pointer[Interface]))
			if !errors.Is(err, ErrLeaseExpired) {
				t.Fatalf("unusable lease opened: %v", err)
			}
			d.mu.Lock()
			defer d.mu.Unlock()
			if len(d.txLog) > 0 {
				t.Fatal("unusable lease generated ARP/IP frames")
			}
		})
	}
	// Configure can immediately schedule gateway ARP. A clock transition
	// between initial admission and the first TX must still shut the wire.
	var checks atomic.Int32
	d := newFakeDHCPDevice("02:11:22:33:44:55", true)
	l, err := newLink(nil, d, cfg.Address, d.MAC(), cfg.Gateway, func() bool { return checks.Add(1) > 1 })
	if err != nil {
		t.Fatal(err)
	}
	defer l.engine.stop()
	if err := l.SendRawFrame(make([]byte, 60)); !errors.Is(err, ErrLeaseExpired) {
		t.Fatal(err)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.txLog) > 0 {
		t.Fatal("expired between admission and first frame")
	}
}

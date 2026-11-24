// SPDX-License-Identifier: GPL-3.0-or-later

package xgmac

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/network"
)

func TestOpen100BaseTXFullDuplexOptIn(t *testing.T) {
	// 1. Default strict config rejects a 100BASE-TX full-duplex link.
	h := newHardware()
	h.phy[10] = 0     // partner does not advertise 1000BASE-T
	h.phy[4] = 1 << 8 // local advertises 100BASE-TX FD
	h.phy[5] = 1 << 8 // partner advertises 100BASE-TX FD
	if _, err := Open(h, h.mem, configuration()); err == nil {
		t.Fatal("default strict config accepted 100BASE-TX link without Allow100Full")
	}

	// 2. Allow100Full admits 100BASE-TX FD (both with Extended Status and 100M-only PHY),
	//    programs XGMAC_CONFIG_SS_100_MII (4<<29), invokes OnSpeedChange, and passes traffic.
	for _, tc := range []struct {
		name      string
		clearExt  bool
		wantSpeed LinkSpeed
	}{
		{name: "gigabit-phy-on-100m-switch", clearExt: false, wantSpeed: LinkSpeed100Full},
		{name: "100m-only-phy-no-ext-status", clearExt: true, wantSpeed: LinkSpeed100Full},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hw := newHardware()
			hw.phy[10] = 0
			hw.phy[4] = 1 << 8
			hw.phy[5] = 1 << 8
			if tc.clearExt {
				hw.phy[1] &^= 1 << 8
			}
			var gotSpeeds []LinkSpeed
			cfg := configuration()
			cfg.Allow100Full = true
			cfg.OnSpeedChange = func(s LinkSpeed) error {
				gotSpeeds = append(gotSpeeds, s)
				return nil
			}
			d, err := Open(hw, hw.mem, cfg)
			if err != nil {
				t.Fatalf("Open 100BASE-TX FD: %v", err)
			}
			if got := hw.regs[txConfig] & txSpeedMask; got != txSpeed100MII {
				t.Fatalf("txConfig SS=%#x, want %#x (100M MII)", got, txSpeed100MII)
			}
			up, sp := d.LinkStatus()
			if !up || sp != tc.wantSpeed {
				t.Fatalf("LinkStatus()=(%t, %d), want (true, %d)", up, sp, tc.wantSpeed)
			}
			if len(gotSpeeds) != 1 || gotSpeeds[0] != tc.wantSpeed {
				t.Fatalf("OnSpeedChange=%v, want [%d]", gotSpeeds, tc.wantSpeed)
			}
			frame := bytes.Repeat([]byte{0x42}, 64)
			hw.rxQueue = append(hw.rxQueue, frame)
			dst := make([]byte, 1600)
			n, err := d.Receive(dst)
			if err != nil || !bytes.Equal(dst[:n], frame) {
				t.Fatalf("Receive n=%d err=%v", n, err)
			}
			if err := d.Transmit(frame); err != nil {
				t.Fatalf("Transmit: %v", err)
			}
		})
	}

	// 3. Half-duplex 100M, 10M full-duplex, and 1000BASE-T MS fault remain rejected even with Allow100Full.
	for _, tc := range []struct {
		name string
		prep func(*hardware)
	}{
		{name: "100m-half", prep: func(hw *hardware) { hw.phy[10] = 0; hw.phy[4] = 1 << 7; hw.phy[5] = 1 << 7 }},
		{name: "10m-full", prep: func(hw *hardware) { hw.phy[10] = 0; hw.phy[4] = 1 << 6; hw.phy[5] = 1 << 6 }},
		{name: "1000m-ms-fault", prep: func(hw *hardware) { hw.phy[10] = 1<<15 | 1<<11; hw.phy[4] = 1 << 8; hw.phy[5] = 1 << 8 }},
		{name: "1000m-half-overrides-100m-full", prep: func(hw *hardware) {
			hw.phy[9] = 1 << 8
			hw.phy[10] = 1 << 10
			hw.phy[4] = 1 << 8
			hw.phy[5] = 1 << 8
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hw := newHardware()
			tc.prep(hw)
			cfg := configuration()
			cfg.Allow100Full = true
			if _, err := Open(hw, hw.mem, cfg); err == nil {
				t.Fatalf("%s unexpectedly admitted", tc.name)
			}
		})
	}

	// 4. OnSpeedChange error fails closed before starting DMA.
	hwErr := newHardware()
	cfgErr := configuration()
	cfgErr.Allow100Full = true
	boom := errors.New("clock switch failed")
	cfgErr.OnSpeedChange = func(LinkSpeed) error { return boom }
	if _, err := Open(hwErr, hwErr.mem, cfgErr); !errors.Is(err, boom) {
		t.Fatalf("Open OnSpeedChange error=%v, want %v", err, boom)
	}
	if hwErr.regs[txControl]&1 != 0 || hwErr.regs[rxControl]&1 != 0 {
		t.Fatal("DMA started after OnSpeedChange error")
	}
}

func TestLinkFlapRecoveryAndSpeedDowngradeUpgrade(t *testing.T) {
	h := newHardware()
	h.phy[4] = 1 << 8
	h.phy[5] = 1 << 8

	type event struct {
		up    bool
		speed LinkSpeed
	}
	var events []event
	var speedChanges []LinkSpeed
	var dev *Device

	cfg := configuration()
	cfg.Allow100Full = true
	cfg.RecoverLinkLoss = true
	cfg.OnSpeedChange = func(s LinkSpeed) error {
		speedChanges = append(speedChanges, s)
		return nil
	}
	cfg.OnLinkChange = func(up bool, s LinkSpeed) {
		// Verify callback runs outside d.mu and observes updated atomic status.
		if dev != nil {
			gotUp, gotSp := dev.LinkStatus()
			if gotUp != up || gotSp != s {
				t.Errorf("OnLinkChange callback saw (%t,%d), want (%t,%d)", gotUp, gotSp, up, s)
			}
		}
		events = append(events, event{up: up, speed: s})
	}

	var err error
	dev, err = Open(h, h.mem, cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if up, sp := dev.LinkStatus(); !up || sp != LinkSpeed1000Full {
		t.Fatalf("initial LinkStatus=(%t,%d), want (true,1000)", up, sp)
	}

	// 1. Unplug Ethernet cable -> transient link down, non-fatal, drops TX cleanly without DMA fault.
	h.phy[1] &^= 1 << 2
	h.now = h.now.Add(time.Second)

	dst := make([]byte, 1600)
	if n, err := dev.Receive(dst); err != nil || n != 0 {
		t.Fatalf("Receive on link down: n=%d err=%v, want (0, nil)", n, err)
	}
	if dev.Err() != nil {
		t.Fatalf("device retired on recoverable link down: %v", dev.Err())
	}
	if up, sp := dev.LinkStatus(); up || sp != LinkSpeedDown {
		t.Fatalf("link-down LinkStatus=(%t,%d), want (false,0)", up, sp)
	}
	txBefore := len(h.frames)
	if err := dev.Transmit(bytes.Repeat([]byte{0x11}, 64)); err != nil {
		t.Fatalf("Transmit on link down returned error: %v", err)
	}
	if len(h.frames) != txBefore || dev.Statistics().Dropped != 1 {
		t.Fatalf("Transmit while down sent=%d dropped=%d", len(h.frames)-txBefore, dev.Statistics().Dropped)
	}

	// 2. Replug into 100BASE-TX FD switch -> recovers and switches XGMAC SS to 100M.
	h.phy[1] |= 1 << 2
	h.phy[10] = 0
	h.now = h.now.Add(500 * time.Millisecond)

	frame100 := bytes.Repeat([]byte{0x22}, 80)
	h.rxQueue = append(h.rxQueue, frame100)
	n, err := dev.Receive(dst)
	if err != nil || !bytes.Equal(dst[:n], frame100) {
		t.Fatalf("Receive after 100M recovery: n=%d err=%v", n, err)
	}
	if got := h.regs[txConfig] & txSpeedMask; got != txSpeed100MII {
		t.Fatalf("txConfig SS after 100M recovery=%#x, want %#x", got, txSpeed100MII)
	}
	if err := dev.Transmit(frame100); err != nil {
		t.Fatalf("Transmit after 100M recovery: %v", err)
	}

	// 3. Flap again and plug back into 1000BASE-T FD switch -> upgrades back to 1000M.
	h.phy[1] &^= 1 << 2
	h.now = h.now.Add(time.Second)
	if _, err := dev.Receive(dst); err != nil {
		t.Fatalf("second link-down Receive: %v", err)
	}
	h.phy[1] |= 1 << 2
	h.phy[10] = 1 << 11
	h.now = h.now.Add(500 * time.Millisecond)
	frame1000 := bytes.Repeat([]byte{0x33}, 96)
	if err := dev.Transmit(frame1000); err != nil {
		t.Fatalf("Transmit after 1000M recovery: %v", err)
	}
	if got := h.regs[txConfig] & txSpeedMask; got != txSpeed1000GMII {
		t.Fatalf("txConfig SS after 1000M recovery=%#x, want %#x", got, txSpeed1000GMII)
	}

	counters := dev.LinkCounters()
	if counters.Flaps != 2 || counters.Recoveries != 2 {
		t.Fatalf("LinkCounters=%+v, want {Flaps:2 Recoveries:2}", counters)
	}
	wantEvents := []event{
		{up: false, speed: LinkSpeedDown},
		{up: true, speed: LinkSpeed100Full},
		{up: false, speed: LinkSpeedDown},
		{up: true, speed: LinkSpeed1000Full},
	}
	if len(events) != len(wantEvents) {
		t.Fatalf("events=%+v, want %+v", events, wantEvents)
	}
	for i, w := range wantEvents {
		if events[i] != w {
			t.Fatalf("events[%d]=%+v, want %+v", i, events[i], w)
		}
	}
	wantSpeeds := []LinkSpeed{LinkSpeed1000Full, LinkSpeed100Full, LinkSpeed1000Full}
	if len(speedChanges) != len(wantSpeeds) {
		t.Fatalf("speedChanges=%v, want %v", speedChanges, wantSpeeds)
	}
	for i, w := range wantSpeeds {
		if speedChanges[i] != w {
			t.Fatalf("speedChanges[%d]=%d, want %d", i, speedChanges[i], w)
		}
	}
}

func TestRecoverLinkLossStillFailsClosedOnHardwareAndMDIOFaults(t *testing.T) {
	for _, mode := range []string{"mdio", "loopback", "bus", "stopped", "unsupported-100m", "unsupported-half"} {
		t.Run(mode, func(t *testing.T) {
			h := newHardware()
			cfg := configuration()
			cfg.RecoverLinkLoss = true
			if mode == "unsupported-half" {
				cfg.Allow100Full = true
			}
			d, err := Open(h, h.mem, cfg)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			switch mode {
			case "mdio":
				h.mode = "mdio"
				h.now = h.now.Add(time.Second)
			case "loopback":
				h.phy[0] |= 1 << 14
				h.now = h.now.Add(time.Second)
			case "bus":
				h.regs[channelStatus] |= 1 << 12
			case "stopped":
				h.regs[rxControl] = 0
			case "unsupported-100m":
				// Completed autonegotiation at 100M without Allow100Full must fail closed.
				h.phy[10] = 0
				h.phy[4] = 1 << 8
				h.phy[5] = 1 << 8
				h.now = h.now.Add(time.Second)
			case "unsupported-half":
				// Completed autonegotiation at 100M half-duplex must fail closed even with Allow100Full.
				h.phy[10] = 0
				h.phy[4] = 1 << 7
				h.phy[5] = 1 << 7
				h.now = h.now.Add(time.Second)
			}
			if _, err := d.Receive(make([]byte, 1600)); err == nil {
				t.Fatalf("mode %s did not fail closed under RecoverLinkLoss", mode)
			}
			if d.Err() == nil {
				t.Fatalf("mode %s left d.Err() == nil", mode)
			}
			if up, sp := d.LinkStatus(); up || sp != LinkSpeedDown {
				t.Fatalf("mode %s failed LinkStatus=(%t,%d), want (false,0)", mode, up, sp)
			}
		})
	}

	// Open with RecoverLinkLoss must also fail closed on completed unsupported negotiation.
	hwBad := newHardware()
	hwBad.phy[10] = 0
	hwBad.phy[4] = 1 << 8
	hwBad.phy[5] = 1 << 8
	cfgBad := configuration()
	cfgBad.RecoverLinkLoss = true
	if _, err := Open(hwBad, hwBad.mem, cfgBad); err == nil {
		t.Fatal("Open accepted completed 100M link without Allow100Full under RecoverLinkLoss")
	}
}

func TestRecoverLinkLossOpenAndControlCycleWhileUnplugged(t *testing.T) {
	h := newHardware()
	h.phy[1] &^= 1 << 2 // unplugged at boot
	h.phy[4] = 1 << 8
	h.phy[5] = 1 << 8

	cfg := configuration()
	cfg.Allow100Full = true
	cfg.RecoverLinkLoss = true
	var stopCallbackUp bool
	cfg.OnLinkChange = func(up bool, _ LinkSpeed) {
		if up {
			stopCallbackUp = true
		}
	}
	d, err := Open(h, h.mem, cfg)
	if err != nil {
		t.Fatalf("Open while unplugged with RecoverLinkLoss: %v", err)
	}
	if up, sp := d.LinkStatus(); up || sp != LinkSpeedDown {
		t.Fatalf("LinkStatus=(%t,%d), want (false,0)", up, sp)
	}

	// BindControl Stop -> Start succeeds even while Ethernet cable remains unplugged,
	// and a cable returning right before Stop must not emit a contradictory up=true callback.
	ctl, err := d.BindControl(network.DMAControl{
		Stop: func(context.Context) error {
			h.regs[txControl] = 0
			h.regs[rxControl] = 0
			h.regs[txConfig] = 0
			h.regs[rxConfig] = 0
			return nil
		},
		Start: func(context.Context) error { return nil },
	})
	if err != nil {
		t.Fatalf("BindControl: %v", err)
	}
	h.phy[1] |= 1 << 2
	h.now = h.now.Add(500 * time.Millisecond)
	if err := ctl.Stop(context.Background()); err != nil {
		t.Fatalf("ctl.Stop while cable returned: %v", err)
	}
	if stopCallbackUp {
		t.Fatal("BindControl.Stop emitted up=true recovery notification while pausing device")
	}
	h.phy[1] &^= 1 << 2
	if err := ctl.Start(context.Background()); err != nil {
		t.Fatalf("ctl.Start while unplugged: %v", err)
	}
	if up, _ := d.LinkStatus(); up {
		t.Fatal("expected link still down after ctl.Start while unplugged")
	}

	// Plug in a 100BASE-TX FD cable after resume -> recovers and transmits.
	h.phy[1] |= 1 << 2
	h.phy[10] = 0
	h.now = h.now.Add(500 * time.Millisecond)
	if err := d.Transmit(bytes.Repeat([]byte{0x55}, 64)); err != nil {
		t.Fatalf("Transmit after plug-in: %v", err)
	}
	if up, sp := d.LinkStatus(); !up || sp != LinkSpeed100Full {
		t.Fatalf("LinkStatus after plug-in=(%t,%d), want (true,100)", up, sp)
	}
	if got := h.regs[txConfig] & txSpeedMask; got != txSpeed100MII {
		t.Fatalf("txConfig SS=%#x, want %#x", got, txSpeed100MII)
	}
}

type linkChange struct {
	up    bool
	speed LinkSpeed
}

// TestControlRestartReportsLinkChangeAcrossPause covers BindControl.Start
// re-probing the PHY: a link that differs from the one consumers observed before
// Stop is reported exactly once, outside d.mu, and later polls compare against
// it; Recoveries never exceeds Flaps.
func TestControlRestartReportsLinkChangeAcrossPause(t *testing.T) {
	const carrier = uint16(1 << 2)
	unplug := func(h *hardware) { h.phy[1] &^= carrier }
	plug := func(h *hardware) { h.phy[1] |= carrier }
	for _, tc := range []struct {
		name     string
		bootDown bool
		// script drives the device; poll forces a link probe, cycle runs Stop,
		// the paused change, then Start.
		script      func(h *hardware, poll func(), cycle func(paused func()))
		wantChanges []linkChange
		wantStats   LinkStats
		wantSS      uint32
	}{
		{
			name: "down-before-stop-up-after-start",
			script: func(h *hardware, poll func(), cycle func(func())) {
				unplug(h)
				poll()
				plug(h)
				// Stop's own probe sees the carrier return but must defer it.
				h.now = h.now.Add(time.Second)
				cycle(func() {})
				poll()
			},
			wantChanges: []linkChange{{false, LinkSpeedDown}, {true, LinkSpeed1000Full}},
			wantStats:   LinkStats{Flaps: 1, Recoveries: 1},
			wantSS:      txSpeed1000GMII,
		},
		{
			name: "up-before-stop-down-after-start",
			script: func(h *hardware, poll func(), cycle func(func())) {
				cycle(func() { unplug(h) })
				poll()
				plug(h)
				poll()
			},
			wantChanges: []linkChange{{false, LinkSpeedDown}, {true, LinkSpeed1000Full}},
			wantStats:   LinkStats{Flaps: 1, Recoveries: 1},
			wantSS:      txSpeed1000GMII,
		},
		{
			name: "speed-change-across-pause",
			script: func(h *hardware, poll func(), cycle func(func())) {
				cycle(func() { h.phy[10] = 0 })
				poll()
			},
			wantChanges: []linkChange{{true, LinkSpeed100Full}},
			wantSS:      txSpeed100MII,
		},
		{
			name: "unchanged-across-pause",
			script: func(h *hardware, poll func(), cycle func(func())) {
				cycle(func() {})
				poll()
			},
			wantSS: txSpeed1000GMII,
		},
		{
			name:     "first-link-up-after-open-down-is-no-recovery",
			bootDown: true,
			script: func(h *hardware, poll func(), cycle func(func())) {
				plug(h)
				poll()
			},
			wantChanges: []linkChange{{true, LinkSpeed1000Full}},
			wantSS:      txSpeed1000GMII,
		},
		{
			name:     "open-down-up-after-start-then-flap",
			bootDown: true,
			script: func(h *hardware, poll func(), cycle func(func())) {
				cycle(func() { plug(h) })
				unplug(h)
				poll()
				plug(h)
				poll()
			},
			wantChanges: []linkChange{{true, LinkSpeed1000Full}, {false, LinkSpeedDown}, {true, LinkSpeed1000Full}},
			wantStats:   LinkStats{Flaps: 1, Recoveries: 1},
			wantSS:      txSpeed1000GMII,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHardware()
			h.phy[4] = 1 << 8
			h.phy[5] = 1 << 8
			if tc.bootDown {
				unplug(h)
			}
			var dev *Device
			var changes []linkChange
			cfg := configuration()
			cfg.Allow100Full = true
			cfg.RecoverLinkLoss = true
			cfg.OnLinkChange = func(up bool, speed LinkSpeed) {
				if !dev.mu.TryLock() {
					t.Error("OnLinkChange ran under the device mutex")
				} else {
					dev.mu.Unlock()
				}
				if gotUp, gotSpeed := dev.LinkStatus(); gotUp != up || gotSpeed != speed {
					t.Errorf("OnLinkChange(%t,%d) while LinkStatus=(%t,%d)", up, speed, gotUp, gotSpeed)
				}
				changes = append(changes, linkChange{up, speed})
			}
			var err error
			if dev, err = Open(h, h.mem, cfg); err != nil {
				t.Fatalf("Open: %v", err)
			}
			ctl, err := dev.BindControl(network.DMAControl{
				Stop: func(context.Context) error {
					h.regs[txControl], h.regs[rxControl], h.regs[txConfig], h.regs[rxConfig] = 0, 0, 0, 0
					return nil
				},
				Start: func(context.Context) error { return nil },
			})
			if err != nil {
				t.Fatalf("BindControl: %v", err)
			}
			poll := func() {
				t.Helper()
				h.now = h.now.Add(time.Second)
				if _, err := dev.Receive(make([]byte, maxFrame)); err != nil {
					t.Fatalf("Receive: %v", err)
				}
			}
			cycle := func(paused func()) {
				t.Helper()
				before := len(changes)
				if err := ctl.Stop(context.Background()); err != nil {
					t.Fatalf("Stop: %v", err)
				}
				if len(changes) != before {
					t.Fatalf("Stop reported %+v; a pause is no link change", changes[before:])
				}
				if up, speed := dev.LinkStatus(); up || speed != LinkSpeedDown {
					t.Fatalf("paused LinkStatus=(%t,%d), want (false,0)", up, speed)
				}
				paused()
				if err := ctl.Start(context.Background()); err != nil {
					t.Fatalf("Start: %v", err)
				}
			}
			tc.script(h, poll, cycle)

			if len(changes) != len(tc.wantChanges) {
				t.Fatalf("OnLinkChange=%+v, want %+v", changes, tc.wantChanges)
			}
			for i, want := range tc.wantChanges {
				if changes[i] != want {
					t.Fatalf("OnLinkChange[%d]=%+v, want %+v (all %+v)", i, changes[i], want, changes)
				}
			}
			if got := dev.LinkCounters(); got != tc.wantStats {
				t.Fatalf("LinkCounters=%+v, want %+v", got, tc.wantStats)
			}
			if dev.Err() != nil {
				t.Fatalf("device retired: %v", dev.Err())
			}
			if got := h.regs[txConfig] & txSpeedMask; got != tc.wantSS {
				t.Fatalf("txConfig SS=%#x, want %#x", got, tc.wantSS)
			}
			// Every scenario ends with the link up: traffic flows at that speed.
			sent := len(h.frames)
			if err := dev.Transmit(bytes.Repeat([]byte{0x66}, 64)); err != nil || len(h.frames) != sent+1 {
				t.Fatalf("Transmit after restart: err=%v sent=%d", err, len(h.frames)-sent)
			}
		})
	}
}

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
	for _, mode := range []string{"mdio", "loopback", "bus", "stopped"} {
		t.Run(mode, func(t *testing.T) {
			h := newHardware()
			cfg := configuration()
			cfg.RecoverLinkLoss = true
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
}

func TestRecoverLinkLossOpenAndControlCycleWhileUnplugged(t *testing.T) {
	h := newHardware()
	h.phy[1] &^= 1 << 2 // unplugged at boot
	h.phy[4] = 1 << 8
	h.phy[5] = 1 << 8

	cfg := configuration()
	cfg.Allow100Full = true
	cfg.RecoverLinkLoss = true
	d, err := Open(h, h.mem, cfg)
	if err != nil {
		t.Fatalf("Open while unplugged with RecoverLinkLoss: %v", err)
	}
	if up, sp := d.LinkStatus(); up || sp != LinkSpeedDown {
		t.Fatalf("LinkStatus=(%t,%d), want (false,0)", up, sp)
	}

	// BindControl Stop -> Start succeeds even while Ethernet cable remains unplugged.
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
	if err := ctl.Stop(context.Background()); err != nil {
		t.Fatalf("ctl.Stop while unplugged: %v", err)
	}
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

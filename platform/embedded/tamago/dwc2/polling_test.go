// SPDX-License-Identifier: GPL-3.0-or-later
package dwc2

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/input"
	"github.com/DeanoC/fes/platform/embedded/tamago/usbcontrol"
)

func TestOwnedPollingDrainsPacketBeforeLongCoreTransition(t *testing.T) {
	hw := newController()
	first := []byte{0, 0, 4, 0, 0, 0, 0, 0}
	second := []byte{0, 0, 5, 0, 0, 0, 0, 0}
	hw.reports = [][]byte{first, second}
	fw := &ownedFirmware{t: t, controller: hw}
	ctx := context.Background()
	client, err := usbcontrol.New(ctx, fw, 17)
	if err != nil {
		t.Fatal(err)
	}
	var host *Host
	source, err := usbcontrol.Open(ctx, client, func() (input.USBReports, error) {
		var e error
		host, e = Open(hw, Config{PHYClockHz: 60000000})
		return host, e
	})
	if err != nil {
		t.Fatal(err)
	}
	before, valid := client.LastReceipt()
	if !valid {
		t.Fatal("no lease")
	}
	if data, err := source.PollKeyboard(); err != nil || data != nil || !host.pending.active {
		t.Fatal("no pending packet", data, err)
	}
	if err := source.PausePolling(ctx); err != nil {
		t.Fatal(err)
	}
	if host.pending.active || host.healthStage != 0 || hw.regs[hcchar]&channelEnable != 0 || host.toggle != 2 {
		t.Fatal("packet not drained", host.pending, host.toggle)
	}
	accesses, starts := hw.accesses, len(hw.starts)
	hw.tick = hw.tick.Add(100 * time.Second)
	for range 10 {
		if data, err := source.PollKeyboard(); err != nil || data != nil {
			t.Fatal(data, err)
		}
	}
	if hw.accesses != accesses || len(hw.starts) != starts {
		t.Fatal("paused host accessed hardware")
	}
	if err := source.ResumePolling(ctx); err != nil {
		t.Fatal(err)
	}
	if hw.accesses != accesses {
		t.Fatal("resume accessed hardware")
	}
	var got []byte
	for range 1000 {
		got, err = source.PollKeyboard()
		if err != nil {
			t.Fatal(err)
		}
		if got != nil {
			break
		}
	}
	if !bytes.Equal(got, second) {
		t.Fatal("drained report replayed or PID lost", got)
	}
	after, valid := client.LastReceipt()
	if !valid || after != before || fw.confirms != 1 || fw.stops != 0 {
		t.Fatal("pause changed secure ownership", after, before)
	}
	if err := source.Close(ctx); err != nil || fw.stops != 1 {
		t.Fatal("owned stop", err)
	}
	accesses = hw.accesses
	if err := source.ResumePolling(ctx); err == nil || hw.accesses != accesses {
		t.Fatal("closed source resumed", err)
	}
}

func TestPollingDrainFailureRetiresWithoutRestart(t *testing.T) {
	for _, mode := range []string{"stuck", "halt", "pid", "disconnect"} {
		t.Run(mode, func(t *testing.T) {
			hw := newController()
			host := open(t, hw)
			hw.reports = [][]byte{{0, 0, 4, 0, 0, 0, 0, 0}}
			hw.mode = mode
			if mode == "disconnect" {
				hw.regs[hprt] &^= 1
			}
			// Some errors are visible before arming; otherwise Pause must observe them.
			_, err := host.PollKeyboard()
			if err == nil {
				err = host.PausePolling(context.Background())
			}
			if err == nil {
				t.Fatal("ambiguous packet admitted pause")
			}
			accesses := hw.accesses
			if err := host.ResumePolling(context.Background()); err == nil {
				t.Fatal("failed host resumed")
			}
			if _, err := host.PollKeyboard(); err == nil || hw.accesses != accesses {
				t.Fatal("failed host touched hardware", err)
			}
		})
	}
}

func TestGamepadPollingPauseRetainsTypedDescriptorAndLease(t *testing.T) {
	hw := gamepadController(t)
	hw.reports = [][]byte{{128, 128, 0, 0}, {255, 0, 1, 0}}
	fw := &ownedFirmware{t: t, controller: hw, gamepad: true}
	ctx := context.Background()
	client, err := usbcontrol.New(ctx, fw, 17)
	if err != nil {
		t.Fatal(err)
	}
	var host *GamepadHost
	source, err := usbcontrol.OpenGamepad(ctx, client, func() (input.USBGamepadReports, error) {
		var e error
		host, e = OpenGamepad(hw, Config{PHYClockHz: 60000000})
		return host, e
	})
	if err != nil {
		t.Fatal(err)
	}
	descriptor := host.ReportDescriptor()
	if _, err := source.PollGamepad(); err != nil {
		t.Fatal(err)
	}
	if err := source.PausePolling(ctx); err != nil {
		t.Fatal(err)
	}
	hw.tick = hw.tick.Add(100 * time.Second)
	if err := source.ResumePolling(ctx); err != nil {
		t.Fatal(err)
	}
	var data []byte
	for range 1000 {
		data, err = source.PollGamepad()
		if err != nil {
			t.Fatal(err)
		}
		if data != nil {
			break
		}
	}
	if !bytes.Equal(data, []byte{255, 0, 1, 0}) || !bytes.Equal(descriptor, host.ReportDescriptor()) || fw.stops != 0 || fw.confirms != 1 {
		t.Fatal("gamepad ownership or report changed", data)
	}
	if err := source.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestPollingPauseCompletesSplitThenChecksHubAfterLongGap(t *testing.T) {
	key := keyboardNode(2)
	key.device.reports = [][]byte{{0, 0, 4, 0, 0, 0, 0, 0}, {0, 0, 5, 0, 0, 0, 0, 0}}
	bus := newHubBus(hubNode(0, key))
	host, err := Open(bus, Config{PHYClockHz: 60000000})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := host.PollKeyboard(); err != nil || !host.pending.active {
		t.Fatal("split not pending", err)
	}
	if err := host.PausePolling(context.Background()); err != nil {
		t.Fatal(err)
	}
	if host.toggle != 2 || host.pending.active || bus.completes == 0 {
		t.Fatal("split was abandoned")
	}
	bus.regs.tick = bus.regs.tick.Add(100 * time.Second)
	if err := host.ResumePolling(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := report(t, host); !bytes.Equal(got, []byte{0, 0, 5, 0, 0, 0, 0, 0}) {
		t.Fatal(got)
	}
}

func TestIdlePollingPauseRejectsUntrackedHardwareActivity(t *testing.T) {
	for _, fault := range []string{"channel", "other-channel", "rx", "dma"} {
		t.Run(fault, func(t *testing.T) {
			hw := newController()
			host := open(t, hw)
			switch fault {
			case "channel":
				hw.regs[hcchar] = channelEnable
			case "other-channel":
				hw.regs[hcchar+0x20] = channelEnable
			case "rx":
				hw.rx = []uint32{2 << 17}
			case "dma":
				hw.regs[gahb] |= 1 << 5
			}
			if err := host.PausePolling(context.Background()); err == nil {
				t.Fatal("untracked activity paused")
			}
			accesses := hw.accesses
			if err := host.ResumePolling(context.Background()); err == nil || hw.accesses != accesses {
				t.Fatal("failed idle proof resumed", err)
			}
		})
	}
}

func TestOwnedPollingDrainFailureRetainsStopReservation(t *testing.T) {
	hw := newController()
	fw := &ownedFirmware{t: t, controller: hw}
	ctx := context.Background()
	client, err := usbcontrol.New(ctx, fw, 17)
	if err != nil {
		t.Fatal(err)
	}
	source, err := usbcontrol.Open(ctx, client, func() (input.USBReports, error) { return Open(hw, Config{PHYClockHz: 60000000}) })
	if err != nil {
		t.Fatal(err)
	}
	hw.mode = "stuck"
	hw.frozen = true
	if _, err := source.PollKeyboard(); err != nil {
		t.Fatal(err)
	}
	if err := source.PausePolling(ctx); err == nil {
		t.Fatal("frozen drain accepted")
	}
	accesses := hw.accesses
	if _, err := source.PollKeyboard(); err == nil || hw.accesses != accesses {
		t.Fatal("failed source polled", err)
	}
	if err := source.ResumePolling(ctx); err == nil || hw.accesses != accesses {
		t.Fatal("failed source resumed", err)
	}
	receipt, valid := client.LastReceipt()
	if !valid || !receipt.Active || !receipt.Confirmed || fw.stops != 0 {
		t.Fatal("drain failure released USB", receipt)
	}
	if err := source.Close(ctx); err == nil || fw.stops != 1 {
		t.Fatal("known failed source did not attempt one owned stop", err)
	}
	source.Close(ctx)
	if fw.stops != 1 {
		t.Fatal("failed source stop repeated")
	}
}

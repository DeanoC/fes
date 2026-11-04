// SPDX-License-Identifier: GPL-3.0-or-later
package dwc2

import (
	"bytes"
	"context"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/input"
	"github.com/DeanoC/fes/platform/embedded/tamago/usbcontrol"
)

func pendingIN(t *testing.T) (*Host, *controller, *rxObserved) {
	t.Helper()
	c := newController()
	h := open(t, c)
	o := &rxObserved{controller: c}
	h.io = o
	flags := uint32(8 | 1<<15 | 1<<17 | 1<<20 | 1<<22)
	c.regs[hcchar] = flags | channelEnable
	c.regs[hcint] = 0
	h.pending = packet{active: true, in: true, pid: 2, limit: 8, char: flags, deadline: c.Now().Add(time.Second)}
	return h, c, o
}

func TestDirectINToggleNotificationWaitsForRealDataWithoutRearming(t *testing.T) {
	for _, metadata := range []bool{false, true} {
		cname := "interrupt-only"
		if metadata {
			cname = "RX-and-interrupt"
		}
		t.Run(cname, func(t *testing.T) {
			h, c, o := pendingIN(t)
			if metadata {
				// Non-data status may retain arbitrary BCNT/DPID; no FIFO
				// bytes exist for it. The accepted packet still needs its own PID.
				c.rx = []uint32{5<<17 | 2047<<4 | 3<<15}
			}
			c.regs[hcint] = dataToggle
			deadline, starts, toggle := h.pending.deadline, len(c.starts), h.toggle
			done, data, nak, err := h.step()
			if err != nil || done || nak || len(data) != 0 || o.fifos != 0 {
				t.Fatal(done, data, nak, err)
			}
			if c.regs[hcint] != 0 || c.regs[hcchar]&channelEnable == 0 || len(c.starts) != starts ||
				h.pending.pid != 2 || h.toggle != toggle || h.pending.deadline != deadline || !h.pending.active || h.pending.received {
				t.Fatal("notification completed/rearmed/retimed the packet")
			}
			want := []byte{0, 0, 4, 0, 0, 0, 0, 0}
			c.active.pid = 2
			c.complete(want, true)
			for n := 0; n < 3; n++ {
				done, data, nak, err = h.step()
				if err != nil || nak {
					t.Fatal(err, nak)
				}
				if done {
					break
				}
			}
			if !done || !bytes.Equal(data, want) || h.pending.active || c.regs[hcchar]&channelEnable != 0 || len(c.starts) != starts {
				t.Fatal(done, data)
			}
		})
	}
}

func TestToggleNotificationCannotSupplyDataOrRelaxOtherFailures(t *testing.T) {
	for _, mode := range []string{"completion-without-data", "wrong-PID", "halt", "stall", "OUT", "split", "other-channel", "deadline", "frozen-poll-bound"} {
		t.Run(mode, func(t *testing.T) {
			h, c, o := pendingIN(t)
			c.rx = []uint32{5 << 17}
			c.regs[hcint] = dataToggle
			switch mode {
			case "completion-without-data":
				c.regs[hcint] |= 1
				c.regs[hcchar] = 0
			case "halt":
				c.regs[hcint] |= 2
				c.regs[hcchar] = 0
			case "stall":
				c.regs[hcint] |= 1 << 3
			case "OUT":
				h.pending.in = false
			case "split":
				h.pending.split = true
			case "other-channel":
				c.rx[0] |= 1
			case "deadline":
				h.pending.deadline = c.Now().Add(time.Millisecond)
			case "frozen-poll-bound":
				c.frozen = true
				h.pending.polls = 999999
			}
			done, data, nak, err := h.step()
			if mode == "wrong-PID" {
				if err != nil {
					t.Fatal(err)
				}
				c.active.pid = 0
				c.complete(make([]byte, 8), true)
				done, data, nak, err = h.step()
			}
			if mode == "deadline" || mode == "frozen-poll-bound" {
				for n := 0; err == nil && n < 30; n++ {
					done, data, nak, err = h.step()
				}
				if err == nil || !strings.Contains(err.Error(), "packet timeout") || !strings.Contains(err.Error(), "first toggle observation") {
					t.Fatal(err)
				}
			}
			if err == nil || done || nak || len(data) != 0 || o.fifos != 0 {
				t.Fatal("bad transfer escaped", done, data, nak, err)
			}
			h.fail(err)
			accesses := c.accesses
			if _, err := h.PollKeyboard(); err == nil || c.accesses != accesses {
				t.Fatal("faulted host resumed")
			}
		})
	}
}

func TestToggleQueueWorkIsBoundedAndDoesNotExtendDeadline(t *testing.T) {
	h, c, o := pendingIN(t)
	for n := 0; n < 20; n++ {
		c.rx = append(c.rx, 5<<17)
	}
	c.regs[hcint] = dataToggle
	deadline := h.pending.deadline
	before := c.accesses
	done, data, nak, err := h.step()
	if err != nil || done || nak || len(data) != 0 || len(c.rx) != 12 || o.pops != 8 || o.fifos != 0 ||
		c.accesses-before > 100 || h.pending.deadline != deadline {
		t.Fatal(done, data, nak, err, c.accesses-before)
	}
}

func TestRXOnlyAndCoalescedToggleEventsStillRequireCorrectPayload(t *testing.T) {
	for _, coalesced := range []bool{false, true} {
		h, c, o := pendingIN(t)
		c.rx = []uint32{5 << 17}
		c.regs[hcint] = 0
		if !coalesced {
			if done, data, nak, err := h.step(); err != nil || done || nak || len(data) != 0 || o.fifos != 0 {
				t.Fatal(done, data, nak, err)
			}
		}
		want := []byte{0, 0, 4, 0, 0, 0, 0, 0}
		c.active.pid = 2
		c.complete(want, true)
		if coalesced {
			c.regs[hcint] |= dataToggle
		}
		var done bool
		var data []byte
		for n := 0; n < 3; n++ {
			var err error
			var nak bool
			done, data, nak, err = h.step()
			if err != nil || nak {
				t.Fatal(err, nak)
			}
			if done {
				break
			}
		}
		if !done || !bytes.Equal(data, want) || c.regs[hcchar]&channelEnable != 0 {
			t.Fatal(done, data)
		}
	}
}

// This model delays the real IN packet until the notification is acknowledged.
// Clearing that notification does not start another host transaction.
type notifyingController struct {
	*controller
	waiting, acknowledged bool
	injected              int
}

func (c *notifyingController) Read32(reg uint32) uint32 {
	if reg == gint && c.acknowledged {
		c.acknowledged, c.waiting = false, false
		c.controller.receive()
	}
	return c.controller.Read32(reg)
}
func (c *notifyingController) Write32(reg, value uint32) {
	c.controller.Write32(reg, value)
	if reg == hcint && value == dataToggle && c.waiting {
		c.acknowledged = true
	}
}
func TestCapturedDescriptorControlTransferSurvivesNotificationWithoutSetupReplay(t *testing.T) {
	newPad := func() *controller {
		c := gamepadController(t)
		d, err := os.ReadFile("../../../../tests/fixtures/nano-usb-gamepad/joystick-report.bin")
		if err != nil {
			t.Fatal(err)
		}
		c.reportDescriptor = d
		c.configuration[25], c.configuration[26] = byte(len(d)), byte(len(d)>>8)
		return c
	}
	control := newPad()
	if _, err := OpenGamepad(control, Config{PHYClockHz: 60000000}); err != nil {
		t.Fatal(err)
	}
	c := &notifyingController{controller: newPad()}
	c.beforeIN = func(hw *controller) bool {
		if c.injected == 0 && hw.request != nil && hw.request.value == 0x2200 && hw.request.offset == 8 {
			c.injected++
			c.waiting = true
			hw.rx = append(hw.rx, 5<<17|8<<4)
			hw.regs[hcint] = dataToggle
			return true
		}
		return false
	}
	fw := &ownedFirmware{t: t, controller: c.controller, gamepad: true}
	client, err := usbcontrol.New(context.Background(), fw, 17)
	if err != nil {
		t.Fatal(err)
	}
	var h *GamepadHost
	source, err := usbcontrol.OpenGamepad(context.Background(), client, func() (input.USBGamepadReports, error) {
		var err error
		h, err = OpenGamepad(c, Config{PHYClockHz: 60000000})
		return h, err
	})
	if err != nil || c.injected != 1 || !bytes.Equal(h.ReportDescriptor(), c.reportDescriptor) {
		t.Fatal(err, c.injected)
	}
	if !reflect.DeepEqual(c.starts, control.starts) || !reflect.DeepEqual(c.requests, control.requests) {
		t.Fatal("notification replayed/changed a USB transaction")
	}
	if fw.confirms != 1 {
		t.Fatal("missing confirmed owner")
	}
	if err := source.Close(context.Background()); err != nil || fw.stops != 1 {
		t.Fatal(err, fw)
	}
	accesses := c.accesses
	if _, err := source.PollGamepad(); err == nil || c.accesses != accesses {
		t.Fatal("closed owner polled hardware", err)
	}
}

type setupFaultController struct{ *controller }

func (c setupFaultController) Write32(reg, value uint32) {
	c.controller.Write32(reg, value)
	if reg == fifo && len(c.active.setup) == 8 {
		c.regs[hcint] = 1 << 3
	}
}
func TestControlFailuresRetainSetupDataAndStatusContext(t *testing.T) {
	for _, phase := range []string{"setup", "data", "status"} {
		t.Run(phase, func(t *testing.T) {
			c := gamepadController(t)
			var io Registers = c
			if phase == "setup" {
				io = setupFaultController{c}
			}
			c.beforeIN = func(hw *controller) bool {
				if phase == "data" && hw.request.value == 0x2200 && hw.request.offset == 8 {
					hw.mode = "pid"
					hw.receive()
					return true
				}
				if phase == "status" && hw.request.request == 9 {
					hw.regs[hcint] = 1 << 3
					return true
				}
				return false
			}
			h, err := OpenGamepad(io, Config{PHYClockHz: 60000000})
			if err == nil || h == nil {
				t.Fatal(err)
			}
			fields := []string{"dwc2: control address=", "phase=" + phase, "endpoint=0", "split=false"}
			switch phase {
			case "setup":
				fields = append(fields, "request=0x06 value=0x0100", "length=8", "offset=0")
			case "data":
				fields = append(fields, "request=0x06 value=0x2200", "offset=8", "invalid RX size/PID/ownership", "nonperiodic=", "periodic=", "frame=")
			case "status":
				fields = append(fields, "request=0x09 value=0x0001", "length=0", "offset=0")
			}
			for _, field := range fields {
				if !strings.Contains(err.Error(), field) {
					t.Fatalf("missing %q: %v", field, err)
				}
			}
			accesses := c.accesses
			if _, err := h.PollGamepad(); err == nil || c.accesses != accesses {
				t.Fatal("failed control resumed", err)
			}
		})
	}
}

func TestToggleInterruptCannotHideOUTSplitOrOtherFatalErrors(t *testing.T) {
	for _, mode := range []string{"OUT", "split", "babble"} {
		h, c, _ := pendingIN(t)
		c.regs[hcint] = dataToggle
		switch mode {
		case "OUT":
			h.pending.in = false
		case "split":
			h.pending.split = true
		case "babble":
			c.regs[hcint] |= 1 << 8
		}
		if done, data, nak, err := h.step(); err == nil || done || nak || len(data) != 0 {
			t.Fatal(mode, done, data, nak, err)
		}
	}
}

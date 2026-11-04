// SPDX-License-Identifier: GPL-3.0-or-later
package dwc2

import (
	"context"
	"testing"

	g "github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol/generated"
	"github.com/DeanoC/fes/platform/embedded/tamago/input"
	"github.com/DeanoC/fes/platform/embedded/tamago/usbcontrol"
)

// The Go client's C wire tests independently execute the real dispatcher and
// board checks. This test joins that client to the actual DWC2 host driver and
// its descriptor/FIFO/channel model, using a modeled firmware reply source.
type ownedFirmware struct {
	expectedAddress uint32
	gamepad         bool

	t                 *testing.T
	controller        *controller
	generation        uint64
	active, confirmed bool
	confirms, stops   int
}

func (f *ownedFirmware) Invoke(a [7]uint64) ([4]uint64, error) {
	if a[0] != uint64(g.FesEmbeddedUsbCall) || a[3] != uint64(g.FesEmbeddedUsbCookie) || a[5] != 0 || a[6] != 0 {
		f.t.Fatal("wrong USB wire")
	}
	if a[1] != uint64(g.FesEmbeddedUsbQuery) && (a[2] != 17 || a[4] != f.generation) {
		f.t.Fatal("stale wire owner")
	}
	switch uint32(a[1]) {
	case g.FesEmbeddedUsbQuery:
	case g.FesEmbeddedUsbAcquire:
		f.active = true
		f.generation++
	case g.FesEmbeddedUsbConfirm:
		want := f.expectedAddress
		if want == 0 {
			want = 1
		}
		ready := f.controller.protocol
		if f.gamepad {
			ready = f.controller.reportFetched && !f.controller.protocol
		}
		if !f.controller.configured || !ready || f.controller.address != want {
			f.t.Fatal("confirmation before actual driver enumeration")
		}
		f.confirmed = true
		f.generation++
		f.confirms++
	case g.FesEmbeddedUsbStop:
		f.active = false
		f.confirmed = false
		f.generation++
		f.stops++
	default:
		f.t.Fatal("unknown wire operation")
	}
	flags := g.FesEmbeddedUsbSupported | g.FesEmbeddedUsbDataHeld | g.FesEmbeddedUsbClosedOnly
	if f.active {
		flags |= g.FesEmbeddedUsbActive
	} else {
		flags |= g.FesEmbeddedUsbContained
	}
	if f.confirmed {
		flags |= g.FesEmbeddedUsbConfirmed
	}
	return [4]uint64{0, uint64(g.FesEmbeddedUsbTag)<<32 | uint64(flags), 17, f.generation}, nil
}
func TestOwnedSourceUsesActualDWC2EnumerationAndRevokesPolling(t *testing.T) {
	hw := newController()
	hw.reports = [][]byte{{0, 0, 4, 0, 0, 0, 0, 0}}
	firmware := &ownedFirmware{t: t, controller: hw}
	client, err := usbcontrol.New(context.Background(), firmware, 17)
	if err != nil {
		t.Fatal(err)
	}
	source, err := usbcontrol.Open(context.Background(), client, func() (input.USBReports, error) { return Open(hw, Config{PHYClockHz: 60000000}) })
	if err != nil || firmware.confirms != 1 {
		t.Fatal(err)
	}
	received := false
	for range 100 {
		report, err := source.PollKeyboard()
		if err != nil {
			t.Fatal(err)
		}
		if report != nil {
			if len(report) != 8 || report[2] != 4 {
				t.Fatal(report)
			}
			received = true
			break
		}
	}
	if !received {
		t.Fatal("no real driver report")
	}
	if err := source.Close(context.Background()); err != nil || firmware.stops != 1 {
		t.Fatal(err)
	}
	accesses := hw.accesses
	if _, err := source.PollKeyboard(); err == nil || hw.accesses != accesses {
		t.Fatal("closed source touched controller")
	}
}
func TestFailedDWC2EnumerationRetainsDriverAndAbortsLease(t *testing.T) {
	hw := newController()
	hw.device[4] = 9 // Hubs cannot operate at low speed.
	firmware := &ownedFirmware{t: t, controller: hw}
	client, err := usbcontrol.New(context.Background(), firmware, 17)
	if err != nil {
		t.Fatal(err)
	}
	var partial *Host
	source, err := usbcontrol.Open(context.Background(), client, func() (input.USBReports, error) {
		var e error
		partial, e = Open(hw, Config{PHYClockHz: 60000000})
		return partial, e
	})
	if err == nil || source == nil || partial == nil || firmware.confirms != 0 || firmware.stops != 1 {
		t.Fatal("partial driver ownership lost", err)
	}
	accesses := hw.accesses
	if _, err := source.PollKeyboard(); err == nil || hw.accesses != accesses {
		t.Fatal("failed source retried driver")
	}
	if _, err := partial.PollKeyboard(); err == nil || hw.accesses != accesses {
		t.Fatal("failed driver retried hardware")
	}
}

func TestPIOOwnershipDriftRetiresBeforeAnotherTransfer(t *testing.T) {
	for _, pending := range []bool{false, true} {
		t.Run(map[bool]string{false: "idle", true: "pending"}[pending], func(t *testing.T) {
			hw := newController()
			host := open(t, hw)
			if pending {
				if _, err := host.PollKeyboard(); err != nil {
					t.Fatal(err)
				}
			}
			starts := len(hw.starts)
			channel := hw.regs[hcchar]
			hw.regs[gahb] |= 1 << 5
			if _, err := host.PollKeyboard(); err == nil {
				t.Fatal("foreign DMA state accepted")
			}
			if len(hw.starts) != starts || hw.regs[hcchar] != channel || hw.regs[gahb]&(1<<5) == 0 {
				t.Fatal("driver mutated a foreign DMA channel")
			}
			accesses := hw.accesses
			if _, err := host.PollKeyboard(); err == nil || hw.accesses != accesses {
				t.Fatal("retired driver retried")
			}
		})
	}
}

func TestOwnedSourceAdmitsHubOnlyAfterActualKeyboardConfiguration(t *testing.T) {
	key := keyboardNode(2)
	key.device.reports = [][]byte{{0, 0, 9, 0, 0, 0, 0, 0}}
	bus := newHubBus(hubNode(0, key))
	model := &ownedFirmware{t: t, controller: bus.regs, expectedAddress: 2}
	cpu := ownedHubCaller{model: model, key: key}
	client, err := usbcontrol.New(context.Background(), cpu, 17)
	if err != nil {
		t.Fatal(err)
	}
	source, err := usbcontrol.Open(context.Background(), client, func() (input.USBReports, error) { return Open(bus, Config{PHYClockHz: 60000000}) })
	if err != nil || model.confirms != 1 || !key.device.configured {
		t.Fatal(err)
	}
	found := false
	for i := 0; i < 100; i++ {
		r, e := source.PollKeyboard()
		if e != nil {
			t.Fatal(e)
		}
		if r != nil {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("confirmed hub source produced no report")
	}
	if err = source.Close(context.Background()); err != nil || model.stops != 1 {
		t.Fatal(err)
	}
	before := bus.regs.accesses
	if _, err = source.PollKeyboard(); err == nil || bus.regs.accesses != before {
		t.Fatal("closed hub source touched controller")
	}
}

type ownedHubCaller struct {
	model *ownedFirmware
	key   *node
}

func (c ownedHubCaller) Invoke(a [7]uint64) ([4]uint64, error) {
	// The independent firmware model observes actual device configuration state.
	c.model.controller.configured = c.key.device.configured
	c.model.controller.protocol = c.key.device.protocol
	c.model.controller.address = c.key.device.address
	return c.model.Invoke(a)
}

// SPDX-License-Identifier: GPL-3.0-or-later
package usbcontrol

import (
	"context"
	"errors"
	"testing"

	"github.com/DeanoC/fes/platform/embedded/tamago/gic"
	"github.com/DeanoC/fes/platform/embedded/tamago/usb"
)

type busTestHost struct {
	irqTestDriver
	services  int
	ctrlCalls int
	openPipes int
	serviceErr error
	devices   []usb.DeviceInfo
	events    []usb.HostEvent
}

func (b *busTestHost) Service() error {
	b.services++
	return b.serviceErr
}
func (b *busTestHost) NextEvent() (usb.HostEvent, bool) {
	if len(b.events) == 0 {
		return usb.HostEvent{}, false
	}
	ev := b.events[0]
	b.events = b.events[1:]
	return ev, true
}
func (b *busTestHost) Devices() []usb.DeviceInfo { return b.devices }
func (b *busTestHost) ControlTransfer(uint8, byte, byte, uint16, uint16, []byte) (int, error) {
	b.ctrlCalls++
	return 0, nil
}
func (b *busTestHost) OpenPipe(uint8, usb.EndpointDescriptor) (usb.Pipe, error) {
	b.openPipes++
	return nil, nil
}

func TestBusHostActualCFirmwareLifecycleIRQAndPause(t *testing.T) {
	var nilBus *BusHost
	if err := nilBus.Service(); !errors.Is(err, ErrRetired) {
		t.Fatalf("nil BusHost Service=%v", err)
	}
	if err := nilBus.EnableIRQ(nil, 125); !errors.Is(err, ErrRetired) {
		t.Fatalf("nil BusHost EnableIRQ=%v", err)
	}
	nilBus.DisableIRQ()
	if nilBus.Ready() != nil || nilBus.IRQCount() != 0 {
		t.Fatal("nil BusHost returned non-zero IRQ state")
	}

	p := newRPC(t)
	ctx := context.Background()
	c, err := New(ctx, p, 17)
	if err != nil {
		t.Fatal(err)
	}

	drv := &busTestHost{
		irqTestDriver: irqTestDriver{ready: make(chan struct{}, 1), count: 7},
		devices: []usb.DeviceInfo{
			{Address: 2, Speed: usb.SpeedHigh},
		},
		events: []usb.HostEvent{
			{Kind: usb.DeviceAttached, Device: usb.DeviceInfo{Address: 2, Speed: usb.SpeedHigh}},
		},
	}
	bus, err := OpenBus(ctx, c, func() (usb.BusHost, error) {
		v, ok := c.LastReceipt()
		if !ok || !v.Active || v.Confirmed {
			t.Fatal("driver opened before acquire", v)
		}
		p.model(t, 0)
		return drv, nil
	})
	if err != nil {
		t.Fatalf("OpenBus: %v", err)
	}

	// Verify GICv3 IRQ wiring uses guarded receipt predicate and exposes Ready/IRQCount.
	if err := bus.EnableIRQ(nil, 125); err != nil {
		t.Fatalf("EnableIRQ: %v", err)
	}
	if drv.guard == nil || !drv.guard() {
		t.Fatal("IRQ guard did not validate active confirmed lease")
	}
	if bus.Ready() != drv.ready || bus.IRQCount() != 7 {
		t.Fatalf("unexpected Ready/IRQCount: %v %d", bus.Ready(), bus.IRQCount())
	}

	// Verify BusHost operations while active.
	if err := bus.Service(); err != nil || drv.services != 1 {
		t.Fatalf("Service: err=%v services=%d", err, drv.services)
	}
	if ev, ok := bus.NextEvent(); !ok || ev.Kind != usb.DeviceAttached || ev.Device.Address != 2 {
		t.Fatalf("NextEvent: ok=%t ev=%+v", ok, ev)
	}
	if devs := bus.Devices(); len(devs) != 1 || devs[0].Address != 2 {
		t.Fatalf("Devices: %+v", devs)
	}
	if _, err := bus.ControlTransfer(2, 0x20, 0, 0, 0, nil); err != nil || drv.ctrlCalls != 1 {
		t.Fatalf("ControlTransfer: err=%v calls=%d", err, drv.ctrlCalls)
	}
	if _, err := bus.OpenPipe(2, usb.EndpointDescriptor{Address: 0x81, TransferType: usb.TransferInterrupt, MaxPacketSize: 16}); err != nil || drv.openPipes != 1 {
		t.Fatalf("OpenPipe: err=%v opens=%d", err, drv.openPipes)
	}

	// PausePolling blocks control/pipe mutations and makes Service a no-op.
	if err := bus.PausePolling(ctx); err != nil || !drv.paused {
		t.Fatalf("PausePolling: err=%v paused=%t", err, drv.paused)
	}
	if err := bus.Service(); err != nil || drv.services != 1 {
		t.Fatalf("Service while paused ran driver: err=%v services=%d", err, drv.services)
	}
	if _, err := bus.ControlTransfer(2, 0x20, 0, 0, 0, nil); !errors.Is(err, ErrTransition) {
		t.Fatalf("ControlTransfer while paused=%v, want ErrTransition", err)
	}
	if err := bus.ResumePolling(ctx); err != nil || drv.paused {
		t.Fatalf("ResumePolling: err=%v paused=%t", err, drv.paused)
	}

	// Close pauses polling, stops EL3 lease, and disables/retires IRQ.
	copied := *bus
	if err := copied.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !drv.paused || drv.enabled || drv.retires+drv.disables == 0 {
		t.Fatalf("Close did not drain/disable driver: paused=%t enabled=%t disables=%d retires=%d",
			drv.paused, drv.enabled, drv.disables, drv.retires)
	}
	if drv.guard() {
		t.Fatal("IRQ guard still true after Close")
	}
	if err := bus.Service(); !errors.Is(err, ErrRetired) {
		t.Fatalf("Service after Close=%v, want ErrRetired", err)
	}
	if p.metrics[2] != 0 {
		t.Fatalf("firmware metrics not stopped: %+v", p.metrics)
	}
}

func TestBusHostActualCFirmwareFaultRetiresLease(t *testing.T) {
	p := newRPC(t)
	ctx := context.Background()
	c, err := New(ctx, p, 17)
	if err != nil {
		t.Fatal(err)
	}
	boom := errors.New("controller fault")
	drv := &busTestHost{serviceErr: boom}
	bus, err := OpenBus(ctx, c, func() (usb.BusHost, error) {
		p.model(t, 0)
		return drv, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := bus.Service(); !errors.Is(err, boom) {
		t.Fatalf("Service err=%v, want %v", err, boom)
	}
	if err := bus.Service(); !errors.Is(err, ErrRetired) {
		t.Fatalf("post-fault Service err=%v, want ErrRetired", err)
	}
	if _, err := bus.OpenPipe(2, usb.EndpointDescriptor{}); !errors.Is(err, ErrRetired) {
		t.Fatalf("post-fault OpenPipe err=%v, want ErrRetired", err)
	}
	if err := bus.Close(ctx); !errors.Is(err, boom) {
		t.Fatalf("Close err=%v, want joined %v", err, boom)
	}
}

func TestBusHostNilFactoryAndUnsupportedIRQ(t *testing.T) {
	ctx := context.Background()
	tr := &irqMockTransport{owner: 17}
	c, err := New(ctx, tr, 17)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := OpenBus(ctx, c, nil); !errors.Is(err, ErrTransition) {
		t.Fatalf("nil factory err=%v", err)
	}
	// Plain usb.BusHost without guarded IRQ returns gic.ErrUnsupported.
	type plainBus struct{ usb.BusHost }
	b, err := OpenBus(ctx, c, func() (usb.BusHost, error) { return plainBus{}, nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := b.EnableIRQ(nil, 125); !errors.Is(err, gic.ErrUnsupported) {
		t.Fatalf("plain BusHost EnableIRQ=%v, want ErrUnsupported", err)
	}
	if err := b.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

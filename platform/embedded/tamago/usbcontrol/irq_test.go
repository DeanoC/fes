// SPDX-License-Identifier: GPL-3.0-or-later
package usbcontrol

import (
	"context"
	"errors"
	"testing"

	g "github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol/generated"
	"github.com/DeanoC/fes/platform/embedded/tamago/gic"
	"github.com/DeanoC/fes/platform/embedded/tamago/input"
)

type irqMockTransport struct {
	owner      uint64
	generation uint64
	active     bool
	confirmed  bool
}

func (m *irqMockTransport) Invoke(a [7]uint64) ([4]uint64, error) {
	switch uint32(a[1]) {
	case g.FesEmbeddedUsbQuery:
	case g.FesEmbeddedUsbAcquire:
		m.active = true
		m.generation++
	case g.FesEmbeddedUsbConfirm:
		m.confirmed = true
		m.generation++
	case g.FesEmbeddedUsbStop:
		m.active = false
		m.confirmed = false
		m.generation++
	default:
		return [4]uint64{}, errors.New("unexpected op")
	}
	flags := g.FesEmbeddedUsbSupported | g.FesEmbeddedUsbDataHeld | g.FesEmbeddedUsbClosedOnly
	if m.active {
		flags |= g.FesEmbeddedUsbActive
	} else {
		flags |= g.FesEmbeddedUsbContained
	}
	if m.confirmed {
		flags |= g.FesEmbeddedUsbConfirmed
	}
	return [4]uint64{0, uint64(g.FesEmbeddedUsbTag)<<32 | uint64(flags), m.owner, m.generation}, nil
}

type irqTestDriver struct {
	ready     chan struct{}
	enabled   bool
	disables  int
	paused    bool
	count     uint64
	enableErr error
}

func (d *irqTestDriver) PollKeyboard() ([]byte, error)       { return nil, nil }
func (d *irqTestDriver) PollGamepad() ([]byte, error)        { return nil, nil }
func (d *irqTestDriver) PausePolling(context.Context) error  { d.paused = true; return nil }
func (d *irqTestDriver) ResumePolling(context.Context) error { d.paused = false; return nil }
func (d *irqTestDriver) EnableIRQ(_ *gic.Controller, _ uint32) error {
	if d.enableErr != nil {
		return d.enableErr
	}
	d.enabled = true
	return nil
}
func (d *irqTestDriver) DisableIRQ() {
	d.enabled = false
	d.disables++
}
func (d *irqTestDriver) Ready() <-chan struct{} {
	if !d.enabled {
		return nil
	}
	return d.ready
}
func (d *irqTestDriver) IRQCount() uint64 { return d.count }

type plainKeyboardDriver struct{}

func (plainKeyboardDriver) PollKeyboard() ([]byte, error) { return nil, nil }

func TestSourceAndGamepadSourceIRQControl(t *testing.T) {
	var nilSrc *Source
	if err := nilSrc.EnableIRQ(nil, 125); !errors.Is(err, ErrRetired) {
		t.Fatalf("nil Source EnableIRQ=%v", err)
	}
	nilSrc.DisableIRQ()
	if nilSrc.Ready() != nil || nilSrc.IRQCount() != 0 {
		t.Fatal("nil Source returned non-zero IRQ state")
	}

	var nilPad *GamepadSource
	if err := nilPad.EnableIRQ(nil, 125); !errors.Is(err, ErrRetired) {
		t.Fatalf("nil GamepadSource EnableIRQ=%v", err)
	}
	nilPad.DisableIRQ()
	if nilPad.Ready() != nil || nilPad.IRQCount() != 0 {
		t.Fatal("nil GamepadSource returned non-zero IRQ state")
	}

	ctx := context.Background()

	// Driver without IRQ support returns gic.ErrUnsupported.
	trPlain := &irqMockTransport{owner: 17}
	cPlain, err := New(ctx, trPlain, 17)
	if err != nil {
		t.Fatal(err)
	}
	plainSrc, err := Open(ctx, cPlain, func() (input.USBReports, error) { return plainKeyboardDriver{}, nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := plainSrc.EnableIRQ(nil, 125); !errors.Is(err, gic.ErrUnsupported) {
		t.Fatalf("plain driver EnableIRQ=%v, want ErrUnsupported", err)
	}
	if plainSrc.Ready() != nil || plainSrc.IRQCount() != 0 {
		t.Fatal("plain driver reported IRQ state")
	}
	plainSrc.DisableIRQ()
	if err := plainSrc.Close(ctx); err != nil {
		t.Fatal(err)
	}

	// Keyboard Source with IRQ + Pause/Resume + Close teardown.
	trKey := &irqMockTransport{owner: 19}
	cKey, err := New(ctx, trKey, 19)
	if err != nil {
		t.Fatal(err)
	}
	kd := &irqTestDriver{ready: make(chan struct{}, 1), count: 7}
	kSrc, err := Open(ctx, cKey, func() (input.USBReports, error) { return kd, nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := kSrc.PausePolling(ctx); err != nil {
		t.Fatal(err)
	}
	if err := kSrc.EnableIRQ(nil, 125); !errors.Is(err, ErrTransition) {
		t.Fatalf("paused Source EnableIRQ=%v, want ErrTransition", err)
	}
	if err := kSrc.ResumePolling(ctx); err != nil {
		t.Fatal(err)
	}
	if err := kSrc.EnableIRQ(nil, 125); err != nil {
		t.Fatal(err)
	}
	if kSrc.Ready() == nil || kSrc.IRQCount() != 7 {
		t.Fatalf("Source IRQ state ready=%v count=%d", kSrc.Ready() != nil, kSrc.IRQCount())
	}
	if err := kSrc.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if kd.enabled || kd.disables != 1 || kSrc.Ready() != nil {
		t.Fatalf("Source Close did not disable IRQ: enabled=%t disables=%d", kd.enabled, kd.disables)
	}
	if err := kSrc.EnableIRQ(nil, 125); !errors.Is(err, ErrRetired) {
		t.Fatalf("closed Source EnableIRQ=%v, want ErrRetired", err)
	}

	// GamepadSource with IRQ + explicit DisableIRQ + Close.
	trPad := &irqMockTransport{owner: 23}
	cPad, err := New(ctx, trPad, 23)
	if err != nil {
		t.Fatal(err)
	}
	gd := &irqTestDriver{ready: make(chan struct{}, 1), count: 3}
	gSrc, err := OpenGamepad(ctx, cPad, func() (input.USBGamepadReports, error) { return gd, nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := gSrc.EnableIRQ(nil, 125); err != nil {
		t.Fatal(err)
	}
	if gSrc.Ready() == nil || gSrc.IRQCount() != 3 {
		t.Fatalf("GamepadSource IRQ state ready=%v count=%d", gSrc.Ready() != nil, gSrc.IRQCount())
	}
	gSrc.DisableIRQ()
	if gd.enabled || gSrc.Ready() != nil {
		t.Fatal("GamepadSource DisableIRQ did not clear state")
	}
	if err := gSrc.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

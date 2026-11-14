// SPDX-License-Identifier: GPL-3.0-or-later
package usbcontrol

import (
	"context"
	"errors"
	"testing"

	g "github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol/generated"
	"github.com/DeanoC/fes/platform/embedded/tamago/input"
	"github.com/DeanoC/fes/platform/embedded/tamago/sdcontrol"
)

func sharedCall(t *testing.T, p *rpc, fid, op, cookie uint32, owner, generation uint64) [4]uint64 {
	t.Helper()
	r, err := p.Invoke([7]uint64{uint64(fid), uint64(op), owner, uint64(cookie), generation})
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func sharedBridge(t *testing.T, p *rpc, op uint32, generation uint64) [4]uint64 {
	t.Helper()
	owner := uint64(17)
	if op == g.FesEmbeddedBridgeSnapshot {
		owner, generation = 0, 0
	}
	r := sharedCall(t, p, g.FesEmbeddedBridgeCall, op, g.FesEmbeddedBridgeCookie, owner, generation)
	if r[0] != 0 {
		t.Fatalf("bridge op %x: %x metrics=%x", op, r, p.metrics)
	}
	return r
}
func sharedOpen(t *testing.T, p *rpc) (*Client, *GamepadSource, *padReports) {
	t.Helper()
	c, err := NewSharedPIO(context.Background(), p, 17)
	if err != nil {
		t.Fatal(err)
	}
	d := &padReports{}
	s, err := OpenGamepad(context.Background(), c, func() (input.USBGamepadReports, error) { return d, nil })
	if err != nil {
		t.Fatal(err)
	}
	return c, s, d
}
func TestSharedActualDispatcherSDCoreReturnAndDynamicContainment(t *testing.T) {
	p := newSharedRPC(t)
	c, source, driver := sharedOpen(t, p)
	ctx := context.Background()
	sd, err := sdcontrol.NewClient(p)
	if err != nil {
		t.Fatal(err)
	}
	returned := uint64(0)
	for n := 0; n < 3; n++ {
		if n == 0 {
			err = sd.Resume()
		} else {
			err = sd.ResumeAfterReturn(17, returned)
		}
		if err != nil {
			t.Fatal(n, err)
		}
		p.model(t, 0) // native SD clock/reset preparation
		if err := sd.Prepare(); err != nil {
			t.Fatal(err)
		}
		if err := c.Observe(ctx); err != nil || p.metrics[2] != 1 {
			t.Fatal("USB observation stole SD lease", err, p.metrics)
		}
		if r, err := source.PollGamepad(); err != nil || len(r) != 4 {
			t.Fatal(r, err)
		}
		p.model(t, 1) // native SD disables its status interrupt enables
		if err := sd.Stop(); err != nil {
			t.Fatal(err)
		}
		last, ok := sd.LastReceipt()
		if !ok {
			t.Fatal("lost SD receipt")
		}
		core := sharedBridge(t, p, g.FesEmbeddedBridgeControl, last.Generation)
		if n == 2 {
			core = sharedBridge(t, p, g.FesEmbeddedBridgeRelease|8<<g.FesEmbeddedBridgeReleaseShift, core[3])
		}
		raw := sharedCall(t, p, g.FesEmbeddedUsbCall, g.FesEmbeddedUsbQuery, g.FesEmbeddedUsbCookie, 0, 0)
		if raw[0] != 0 || raw[3] != 2 || (uint32(raw[1])&g.FesEmbeddedUsbDataHeld != 0) != (n != 2) {
			t.Fatalf("incorrect live containment: %x", raw)
		}
		if err := c.Observe(ctx); err != nil {
			t.Fatal("core transition changed USB identity", err)
		}
		beforeIO := p.metrics[4]
		refused := sharedCall(t, p, g.FesEmbeddedUsbCall, g.FesEmbeddedUsbStop, g.FesEmbeddedUsbCookie, 17, 2)
		if refused[0] != uint64(g.FesEmbeddedUsbBusy) || p.metrics[4] != beforeIO || p.metrics[0] != 1 {
			t.Fatal("USB stopped before the owned core returned", refused, p.metrics)
		}
		if _, err := source.PollGamepad(); err != nil {
			t.Fatal("data-path observation revoked source", err)
		}
		op := g.FesEmbeddedBridgeClose
		if n != 2 {
			op |= g.FesEmbeddedBridgeRequestHeld
		}
		closed := sharedBridge(t, p, op, core[3])
		returned = closed[3]
		if err := c.Observe(ctx); err != nil {
			t.Fatal(err)
		}
	}
	// A DDR close alone does not supply SD's separate qualified return policy.
	if err := source.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if p.metrics[0] != 0 || p.metrics[2] != 0 || driver.polls != 6 {
		t.Fatal(p.metrics, driver.polls)
	}
}
func TestSharedUSBStopPreservesActiveSDLease(t *testing.T) {
	p := newSharedRPC(t)
	_, source, _ := sharedOpen(t, p)
	sd, err := sdcontrol.NewClient(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := sd.Resume(); err != nil {
		t.Fatal(err)
	}
	p.model(t, 0)
	if err := sd.Prepare(); err != nil {
		t.Fatal(err)
	}
	before, _ := sd.LastReceipt()
	if err := source.Close(context.Background()); err != nil || p.metrics[2] != 1 {
		t.Fatal(err, p.metrics)
	}
	after, ok := sd.LastReceipt()
	if !ok || before != after || !after.Active {
		t.Fatal("USB stop changed SD permission", before, after)
	}
	p.model(t, 1)
	if err := sd.Stop(); err != nil {
		t.Fatal(err)
	}
}
func TestSharedObservationDuringConfigurationDoesNotMutateUSB(t *testing.T) {
	p := newSharedRPC(t)
	c, source, _ := sharedOpen(t, p)
	before, _ := c.LastReceipt()
	p.model(t, 3)
	if err := c.Observe(context.Background()); err != nil {
		t.Fatal("independent USB observation blocked by configuration", err)
	}
	after, valid := c.LastReceipt()
	if !valid || after != before {
		t.Fatal("configuration changed USB identity", after)
	}
	r := sharedCall(t, p, g.FesEmbeddedUsbCall, g.FesEmbeddedUsbStop, g.FesEmbeddedUsbCookie, 17, 2)
	if r[0] != uint64(g.FesEmbeddedUsbBusy) || p.metrics[0] != 1 {
		t.Fatal("USB mutated during configuration", r, p.metrics)
	}
	if _, err := source.PollGamepad(); err != nil {
		t.Fatal(err)
	}
	p.model(t, 12)
	if err := source.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}
func TestSharedModeIsExplicitAndRequiresRealMasterBinding(t *testing.T) {
	ctx := context.Background()
	if _, err := NewSharedPIO(ctx, newRPC(t), 17); err == nil {
		t.Fatal("shared client accepted exclusive firmware")
	}
	p := newSharedRPC(t)
	if _, err := New(ctx, p, 17); err == nil || p.metrics[0] != 0 {
		t.Fatal("exclusive client accepted shared firmware", err)
	}
	if _, err := NewSharedPIO(ctx, newSharedRPC(t, "unbound"), 17); err == nil {
		t.Fatal("reset bits without master binding granted shared permission")
	}
	c, err := NewSharedPIO(ctx, p, 17)
	if err != nil {
		t.Fatal(err)
	}
	before := p.metrics[4]
	raw := sharedCall(t, p, g.FesEmbeddedUsbCall, g.FesEmbeddedUsbAcquire, g.FesEmbeddedUsbCookie, 18, 0)
	if raw[0] != uint64(g.FesEmbeddedUsbRejected) || p.metrics[4] != before {
		t.Fatal("foreign USB request sampled hardware", raw, p.metrics)
	}
	if err := c.Acquire(ctx); err != nil {
		t.Fatal(err)
	}
	before = p.metrics[4]
	raw = sharedCall(t, p, g.FesEmbeddedSdCall, g.FesEmbeddedSdQuery, g.FesEmbeddedSdCookie, 0, 0)
	if raw[0] != uint64(g.FesEmbeddedSdBusy) || p.metrics[4] == before {
		// The gate performs USB observation, but never enters the SD provider.
		t.Fatal("unconfirmed USB did not gate other calls", raw, p.metrics)
	}
	if err := c.Stop(ctx); err != nil {
		t.Fatal("known prepared abort failed", err)
	}
}
func TestSharedModeRequiresBoundContainmentHook(t *testing.T) {
	p := newSharedRPC(t, "missing-contained")
	if _, err := NewSharedPIO(context.Background(), p, 17); err == nil {
		t.Fatal("missing containment hook granted a shared USB lease")
	}
	if p.metrics[0] != 0 || p.metrics[1] != 0 || p.metrics[2] != 0 {
		t.Fatal("rejected binding retained an active device grant", p.metrics)
	}
	before := p.metrics[4]
	r := sharedCall(t, p, g.FesEmbeddedSdCall, g.FesEmbeddedSdQuery, g.FesEmbeddedSdCookie, 0, 0)
	if r[0] != uint64(g.FesEmbeddedSdError) || p.metrics[4] != before {
		t.Fatal("unbound containment reached hardware", r, p.metrics)
	}
}

func TestSharedActualDispatcherRetiresFaultsAndRetainsReservations(t *testing.T) {
	for _, fault := range []uint64{2, 4, 7, 8, 11} {
		t.Run(map[uint64]string{2: "DMA", 4: "stop-reset", 7: "PHY", 8: "foreign-master", 11: "observer-binding"}[fault], func(t *testing.T) {
			p := newSharedRPC(t)
			c, _, _ := sharedOpen(t, p)
			p.model(t, fault)
			var err error
			if fault == 4 {
				err = c.Stop(context.Background())
			} else {
				err = c.Observe(context.Background())
			}
			if !errors.Is(err, ErrRetired) || p.metrics[0] != 1 || p.metrics[2] != 0 {
				t.Fatal(err, p.metrics)
			}
			before := p.metrics[4]
			r := sharedCall(t, p, g.FesEmbeddedSdCall, g.FesEmbeddedSdQuery, g.FesEmbeddedSdCookie, 0, 0)
			if r[0] != uint64(g.FesEmbeddedSdError) || p.metrics[4] != before {
				t.Fatal("failed shared authority re-entered hardware", r, p.metrics)
			}
			calls := p.calls
			if err := c.Stop(context.Background()); !errors.Is(err, ErrRetired) || p.calls != calls {
				t.Fatal("uncertain client retried stop", err)
			}
		})
	}
}

func TestSharedCoreStopFailuresRetireInputWithoutUSBReset(t *testing.T) {
	for _, fault := range []uint64{6, 10} {
		t.Run(map[uint64]string{6: "producer-stop", 10: "late-producer-proof"}[fault], func(t *testing.T) {
			p := newSharedRPC(t)
			c, _, _ := sharedOpen(t, p)
			state := sharedBridge(t, p, g.FesEmbeddedBridgeSnapshot, 0)
			core := sharedBridge(t, p, g.FesEmbeddedBridgeControl, state[3])
			p.model(t, fault)
			writes := p.metrics[6]
			raw := sharedCall(t, p, g.FesEmbeddedBridgeCall, g.FesEmbeddedBridgeClose|g.FesEmbeddedBridgeRequestHeld, g.FesEmbeddedBridgeCookie, 17, core[3])
			if raw[0] != uint64(g.FesEmbeddedUsbError) || raw[2] != 0 || raw[3] != 0 || p.metrics[0] != 1 || p.metrics[6] != writes {
				t.Fatal("failed core stop published a return or reset USB", raw, p.metrics)
			}
			beforeIO := p.metrics[4]
			if err := c.Observe(context.Background()); !errors.Is(err, ErrRetired) || p.metrics[4] != beforeIO {
				t.Fatal("failed core authority still admitted USB", err, p.metrics)
			}
		})
	}
}

func TestSharedStartupFailureKeepsUSBReservationSeparateFromSD(t *testing.T) {
	for _, falseConfirmation := range []bool{false, true} {
		t.Run(map[bool]string{false: "driver-failure", true: "false-confirmation"}[falseConfirmation], func(t *testing.T) {
			p := newSharedRPC(t)
			ctx := context.Background()
			c, err := NewSharedPIO(ctx, p, 17)
			if err != nil {
				t.Fatal(err)
			}
			driverFailure := errors.New("gamepad startup failed")
			s, err := OpenGamepad(ctx, c, func() (input.USBGamepadReports, error) {
				if !falseConfirmation {
					return &padReports{}, driverFailure
				}
				p.model(t, 5) // corrupt the compiled callback after the reservation exists
				return &padReports{}, nil
			})
			wantActive := uint64(0)
			if falseConfirmation {
				wantActive = 1
			}
			if s == nil || !errors.Is(err, ErrRetired) || p.metrics[0] != wantActive || p.metrics[2] != 0 {
				t.Fatal("failed startup lost resources or claimed SD reservation", err, p.metrics)
			}
			if !falseConfirmation && !errors.Is(err, driverFailure) {
				t.Fatal(err)
			}
			if falseConfirmation {
				beforeIO := p.metrics[4]
				r := sharedCall(t, p, g.FesEmbeddedSdCall, g.FesEmbeddedSdQuery, g.FesEmbeddedSdCookie, 0, 0)
				if r[0] != uint64(g.FesEmbeddedSdError) || p.metrics[4] != beforeIO {
					t.Fatal("false confirmation permitted another master", r, p.metrics)
				}
			}
		})
	}
}

func TestSharedDecoderRequiresOneExplicitScopeAndNeverChangesMode(t *testing.T) {
	ctx := context.Background()
	p := newSharedRPC(t)
	for _, flags := range []uint32{
		g.FesEmbeddedUsbSupported | g.FesEmbeddedUsbSharedPio | g.FesEmbeddedUsbClosedOnly | g.FesEmbeddedUsbContained | g.FesEmbeddedUsbDataHeld,
		g.FesEmbeddedUsbSupported | g.FesEmbeddedUsbSharedPio | g.FesEmbeddedUsbContained,
		g.FesEmbeddedUsbSupported | g.FesEmbeddedUsbSharedPio | g.FesEmbeddedUsbContained | g.FesEmbeddedUsbDataHeld | 0x80,
	} {
		wrapped := callerFunc(func(a [7]uint64) ([4]uint64, error) {
			r, err := p.Invoke(a)
			r[1] = uint64(g.FesEmbeddedUsbTag)<<32 | uint64(flags)
			return r, err
		})
		if _, err := NewSharedPIO(ctx, wrapped, 17); err == nil {
			t.Fatal("bad initial shared scope admitted", flags)
		}
	}
	c, _, _ := sharedOpen(t, p)
	c.cpu = callerFunc(func(a [7]uint64) ([4]uint64, error) {
		r, err := p.Invoke(a)
		r[1] &^= uint64(g.FesEmbeddedUsbSharedPio)
		r[1] |= uint64(g.FesEmbeddedUsbClosedOnly)
		return r, err
	})
	if err := c.Observe(ctx); !errors.Is(err, ErrRetired) {
		t.Fatal("firmware mode changed under a shared source", err)
	}
}

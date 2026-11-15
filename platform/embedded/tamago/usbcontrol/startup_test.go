// SPDX-License-Identifier: GPL-3.0-or-later
package usbcontrol

import (
	"context"
	"errors"
	"testing"

	g "github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol/generated"
	"github.com/DeanoC/fes/platform/embedded/tamago/input"
)

// The existing RPC runs the actual C dispatcher, service and board policies.
// Its register model supplies neither physical USB nor electrical proof.
func TestStartupFailureKeepsVerifiedStopBeforeLocalRetirement(t *testing.T) {
	p := newRPC(t)
	c, err := New(context.Background(), p, 17)
	if err != nil {
		t.Fatal(err)
	}
	cause := errors.New("root connection wait exhausted")
	driver := &reports{}
	s, err := Open(context.Background(), c, func() (input.USBReports, error) { return driver, cause })
	var failure *StartupFailure
	if !errors.As(err, &failure) || !errors.Is(err, cause) || !errors.Is(err, ErrRetired) {
		t.Fatal(err)
	}
	stopped, known := failure.StopReceipt()
	if failure.Stage() != "driver" || !failure.StopAttempted() || !known || stopped.Owner != 17 || stopped.Generation != 2 || stopped.Active || stopped.Confirmed {
		t.Fatal(failure, stopped)
	}
	if _, valid := c.LastReceipt(); valid {
		t.Fatal("historical stop revived retired client")
	}
	stopped.Owner = 99
	if again, _ := failure.StopReceipt(); again.Owner != 17 {
		t.Fatal("receipt alias escaped")
	}
	calls := p.calls
	if _, err := s.PollKeyboard(); !errors.Is(err, cause) {
		t.Fatal(err)
	}
	if err := s.Close(context.Background()); !errors.Is(err, cause) {
		t.Fatal(err)
	}
	if err := c.Acquire(context.Background()); !errors.Is(err, ErrRetired) {
		t.Fatal(err)
	}
	if p.calls != calls || driver.polls != 0 {
		t.Fatal("failure observation authorized reuse")
	}
}

func TestStartupFailureLostReplyNeverClaimsContainment(t *testing.T) {
	for _, op := range []uint32{g.FesEmbeddedUsbAcquire, g.FesEmbeddedUsbConfirm, g.FesEmbeddedUsbStop} {
		t.Run(map[uint32]string{g.FesEmbeddedUsbAcquire: "acquire", g.FesEmbeddedUsbConfirm: "confirm", g.FesEmbeddedUsbStop: "stop"}[op], func(t *testing.T) {
			p := newRPC(t)
			lost := errors.New("response lost")
			cpu := callerFunc(func(a [7]uint64) ([4]uint64, error) {
				r, e := p.Invoke(a)
				if a[1] == uint64(op) {
					return r, lost
				}
				return r, e
			})
			c, err := New(context.Background(), cpu, 17)
			if err != nil {
				t.Fatal(err)
			}
			factories := 0
			s, err := Open(context.Background(), c, func() (input.USBReports, error) {
				factories++
				if op == g.FesEmbeddedUsbConfirm {
					p.model(t, 0)
					return &reports{}, nil
				}
				return &reports{}, errors.New("driver failed")
			})
			var failure *StartupFailure
			if s == nil || !errors.As(err, &failure) || !errors.Is(err, lost) {
				t.Fatal(err)
			}
			if stopped, known := failure.StopReceipt(); known || stopped != (Receipt{}) {
				t.Fatal("uncertain reply supplied stop proof", stopped)
			}
			wantFactories := 0
			if op != g.FesEmbeddedUsbAcquire {
				wantFactories = 1
			}
			if factories != wantFactories || failure.StopAttempted() != (op == g.FesEmbeddedUsbStop) {
				t.Fatal(failure, factories)
			}
			calls := p.calls
			_ = s.Close(context.Background())
			if p.calls != calls {
				t.Fatal("uncertain cleanup retried")
			}
		})
	}
}

func TestCanceledKnownAcquisitionRecordsStopWithoutDriver(t *testing.T) {
	p := newRPC(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cpu := callerFunc(func(a [7]uint64) ([4]uint64, error) {
		r, e := p.Invoke(a)
		if a[1] == uint64(g.FesEmbeddedUsbAcquire) {
			cancel()
		}
		return r, e
	})
	c, err := New(ctx, cpu, 17)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Open(ctx, c, func() (input.USBReports, error) { t.Fatal("canceled factory ran"); return nil, nil })
	var failure *StartupFailure
	if !errors.As(err, &failure) || !errors.Is(err, context.Canceled) || failure.Stage() != "acquire" {
		t.Fatal(err)
	}
	if _, known := failure.StopReceipt(); !known || !failure.StopAttempted() {
		t.Fatal(failure)
	}
}

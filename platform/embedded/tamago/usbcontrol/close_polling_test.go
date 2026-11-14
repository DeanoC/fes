// SPDX-License-Identifier: GPL-3.0-or-later
package usbcontrol

import (
	"context"
	"errors"
	"testing"

	g "github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol/generated"
	"github.com/DeanoC/fes/platform/embedded/tamago/input"
)

type drainingReports struct {
	reports
	pauses  int
	pending bool
	drain   func(context.Context) error
}

func (d *drainingReports) PausePolling(ctx context.Context) error {
	d.pauses++
	if d.drain != nil {
		if err := d.drain(ctx); err != nil {
			return err
		}
	}
	d.pending = false
	return nil
}
func (d *drainingReports) ResumePolling(context.Context) error {
	return errors.New("test driver must never resume during close")
}

func TestCloseDrainsWithActualSharedFirmware(t *testing.T) {
	for _, state := range []string{"pending", "paused", "canceled-before", "canceled-after-drain", "stale", "drain-failed", "firmware-failed"} {
		t.Run(state, func(t *testing.T) {
			p := newSharedRPC(t)
			d := &drainingReports{pending: true}
			stops := 0
			cpu := callerFunc(func(a [7]uint64) ([4]uint64, error) {
				if a[0] == uint64(g.FesEmbeddedUsbCall) && a[1] == uint64(g.FesEmbeddedUsbStop) {
					stops++
					if state != "stale" && state != "drain-failed" && (d.pauses != 1 || d.pending) {
						t.Fatal("Stop before driver drain")
					}
				}
				return p.Invoke(a)
			})
			c, err := NewSharedPIO(context.Background(), cpu, 17)
			if err != nil {
				t.Fatal(err)
			}
			s, err := Open(context.Background(), c, func() (input.USBReports, error) { return d, nil })
			if err != nil {
				t.Fatal(err)
			}
			if state == "paused" {
				if err := s.PausePolling(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			if state == "stale" {
				if err := c.Stop(context.Background()); err != nil {
					t.Fatal(err)
				}
				calls := p.calls
				if err := s.Close(context.Background()); !errors.Is(err, ErrRetired) || d.pauses != 0 || p.calls != calls {
					t.Fatal("stale source drained/stopped", err)
				}
				return
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cause := errors.New("unretired packet")
			if state == "canceled-before" {
				cancel()
			}
			if state == "canceled-after-drain" {
				d.drain = func(context.Context) error { cancel(); return nil }
			}
			if state == "drain-failed" {
				d.drain = func(context.Context) error { return cause }
			}
			if state == "firmware-failed" {
				d.drain = func(context.Context) error { p.model(t, 2); return nil }
			}
			err = s.Close(ctx)
			if state == "canceled-before" || state == "canceled-after-drain" {
				if !errors.Is(err, context.Canceled) || stops != 0 {
					t.Fatal("cancellation issued Stop", err, stops)
				}
				want := 0
				if state == "canceled-after-drain" {
					want = 1
				}
				if d.pauses != want {
					t.Fatal(d.pauses)
				}
				if err = s.Close(context.Background()); err != nil || d.pauses != 1 || stops != 1 {
					t.Fatal("known canceled close could not finish once", err)
				}
			} else if state == "drain-failed" {
				if !errors.Is(err, cause) || !errors.Is(err, ErrRetired) || stops != 1 || p.metrics[0] != 0 {
					t.Fatal("lost drain failure or owned cleanup", err, p.metrics)
				}
			} else if state == "firmware-failed" {
				if !errors.Is(err, ErrRetired) || stops != 1 || p.metrics[0] != 1 || p.metrics[7] != 1 {
					t.Fatal("foreign DMA released failed firmware lease", err, p.metrics)
				}
			} else if err != nil || stops != 1 || d.pauses != 1 || p.metrics[0] != 0 {
				t.Fatal("drained close failed", err, p.metrics)
			}
			calls, pauses := p.calls, d.pauses
			_ = s.Close(context.Background())
			if p.calls != calls || d.pauses != pauses {
				t.Fatal("close replayed drain/Stop")
			}
			if _, err := s.PollKeyboard(); err == nil || d.polls != 0 {
				t.Fatal("closed reports polled", err)
			}
		})
	}
}

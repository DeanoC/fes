// SPDX-License-Identifier: GPL-3.0-or-later
package usbcontrol

import (
	"context"
	"errors"
	"testing"

	"github.com/DeanoC/fes/platform/embedded/tamago/input"
)

type padReports struct {
	polls int
	err   error
}

func (p *padReports) PollGamepad() ([]byte, error) { p.polls++; return []byte{128, 128, 1, 0}, p.err }

func TestGamepadActualCFirmwareOwnershipAndReplacement(t *testing.T) {
	p := newRPC(t)
	ctx := context.Background()
	c, err := New(ctx, p, 17)
	if err != nil {
		t.Fatal(err)
	}
	open := func(driver *padReports) *GamepadSource {
		t.Helper()
		s, err := OpenGamepad(ctx, c, func() (input.USBGamepadReports, error) {
			v, ok := c.LastReceipt()
			if !ok || !v.Active || v.Confirmed {
				t.Fatal("driver before prepare", v)
			}
			p.model(t, 0)
			return driver, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	driver := &padReports{}
	s := open(driver)
	copy := *s
	if _, keyboard := any(s).(input.USBReports); keyboard {
		t.Fatal("gamepad source exposes keyboard reports")
	}
	if r, err := s.PollGamepad(); err != nil || len(r) != 4 || driver.polls != 1 {
		t.Fatal(r, err)
	}
	if err := copy.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PollGamepad(); !errors.Is(err, ErrRetired) || driver.polls != 1 {
		t.Fatal("copied source did not revoke original", err)
	}
	next := open(&padReports{})
	calls := p.calls
	if err := s.Close(ctx); err != nil || p.calls != calls {
		t.Fatal("old source stopped replacement", err)
	}
	if r, err := next.PollGamepad(); err != nil || len(r) != 4 {
		t.Fatal(r, err)
	}
	if err := next.Close(ctx); err != nil || p.metrics[2] != 0 {
		t.Fatal(err, p.metrics)
	}
}
func TestGamepadActualCFirmwareFailures(t *testing.T) {
	for _, mode := range []string{"startup", "poll", "stop"} {
		t.Run(mode, func(t *testing.T) {
			p := newRPC(t)
			ctx := context.Background()
			c, err := New(ctx, p, 17)
			if err != nil {
				t.Fatal(err)
			}
			failure := errors.New("gamepad failure")
			driver := &padReports{}
			s, err := OpenGamepad(ctx, c, func() (input.USBGamepadReports, error) {
				if mode == "startup" {
					return driver, failure
				}
				p.model(t, 0)
				return driver, nil
			})
			if mode == "startup" {
				if s == nil || !errors.Is(err, failure) || p.metrics[2] != 0 {
					t.Fatal(err, p.metrics)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if mode == "poll" {
					driver.err = failure
					if _, err := s.PollGamepad(); !errors.Is(err, failure) {
						t.Fatal(err)
					}
				} else {
					p.model(t, 2)
				}
				if err := s.Close(ctx); err == nil {
					t.Fatal("failure hidden")
				}
				if mode == "stop" && p.metrics[2] != 1 {
					t.Fatal("failed stop lost reservation", p.metrics)
				}
			}
			calls, polls := p.calls, driver.polls
			if _, err := s.PollGamepad(); err == nil || driver.polls != polls {
				t.Fatal("faulted driver polled", err)
			}
			if err := s.Close(ctx); err == nil || p.calls != calls {
				t.Fatal("faulted close retried", err)
			}
		})
	}
}

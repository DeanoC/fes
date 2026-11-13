// SPDX-License-Identifier: GPL-3.0-or-later
package usbcontrol

import (
	"context"
	"errors"
	"sync"
	"time"

	g "github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol/generated"
)

// reportLease binds an exclusive synchronous typed report driver to its lease.
// Retain the Source, Client and failed driver until reboot, including when Open
// returns an error. Driver factories must retain/return partial resources and
// must not start background polling or re-enter Client mutation methods.
type reportLease struct {
	operation sync.Mutex
	client    *Client
	driver    reportDriver
	receipt   Receipt
	closed    bool
	paused    bool
	fault     error
}

func openReports(ctx context.Context, c *Client, factory func() (reportDriver, error)) (*reportLease, error) {
	if c == nil || ctx == nil || factory == nil {
		return nil, ErrTransition
	}
	if !c.operation.TryLock() {
		return nil, ErrBusy
	}
	defer c.operation.Unlock()
	before, valid := c.LastReceipt()
	if !valid {
		return nil, ErrRetired
	}
	if before.Active {
		return nil, ErrTransition
	}
	s := &reportLease{client: c}
	failed := &StartupFailure{stage: "acquire"}
	err := c.transitionLocked(ctx, g.FesEmbeddedUsbAcquire)
	if err == nil {
		if err = ctx.Err(); err == nil {
			failed.stage = "driver"
			s.driver, err = factory()
		}
		if err == nil && s.driver == nil {
			err = errors.New("usbcontrol: driver returned no report source")
		}
	}
	if err == nil {
		failed.stage = "confirm"
		err = c.transitionLocked(ctx, g.FesEmbeddedUsbConfirm)
	}
	if err != nil {
		current, known := c.LastReceipt()
		if known && current.Active {
			failed.stopAttempted = true
			cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			stopErr := c.transitionLocked(cleanup, g.FesEmbeddedUsbStop)
			err = errors.Join(err, stopErr)
			cancel()
			if stopped, valid := c.LastReceipt(); stopErr == nil && valid && !stopped.Active && !stopped.Confirmed {
				failed.stopped, failed.stopVerified = stopped, true
			}
		}
		// If no wire mutation occurred, cancellation is recoverable at Client level.
		// Any attempted driver startup or acquired lease is single-use after failure.
		if s.driver != nil || current.Generation != before.Generation || !known {
			err = c.retire(err)
		}
		failed.cause = err
		err = failed
		s.closed, s.fault = true, err
		return s, err
	}
	s.receipt, _ = c.LastReceipt()
	return s, nil
}
func (s *reportLease) poll() ([]byte, error) {
	if s == nil {
		return nil, ErrRetired
	}
	if !s.operation.TryLock() {
		return nil, nil
	}
	defer s.operation.Unlock()
	if s.closed || s.driver == nil {
		return nil, errors.Join(ErrRetired, s.fault)
	}
	if s.fault != nil {
		return nil, errors.Join(ErrRetired, s.fault)
	}
	c := s.client
	if !c.operation.TryLock() {
		return nil, nil
	}
	defer c.operation.Unlock()
	current, valid := c.LastReceipt()
	if !valid || current != s.receipt || !current.Active || !current.Confirmed {
		return nil, ErrRetired
	}
	if s.paused {
		return nil, nil
	}
	report, err := s.driver.pollReport()
	if err != nil {
		s.fault = err
		return nil, err
	}
	return report, nil
}

// Close revokes local polling before one firmware stop. Copies of a pointer
// share closed state; an old source cannot stop a replacement generation.
func (s *reportLease) close(ctx context.Context) error {
	if s == nil {
		return ErrRetired
	}
	if !s.operation.TryLock() {
		return ErrBusy
	}
	defer s.operation.Unlock()
	if s.closed {
		return s.fault
	}
	if ctx == nil {
		return ErrTransition
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	c := s.client
	if !c.operation.TryLock() {
		return ErrBusy
	}
	defer c.operation.Unlock()
	current, valid := c.LastReceipt()
	if !valid || current != s.receipt {
		return ErrRetired
	}
	s.closed = true
	priorFault := s.fault
	stopErr := c.transitionLocked(ctx, g.FesEmbeddedUsbStop)
	after, known := c.LastReceipt()
	if known && after == current && ctx.Err() != nil {
		s.closed = false
		return errors.Join(priorFault, stopErr)
	}
	s.fault = errors.Join(priorFault, stopErr)
	if priorFault != nil {
		s.fault = c.retire(s.fault)
	}
	return s.fault
}

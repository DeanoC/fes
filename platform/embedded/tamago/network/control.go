// SPDX-License-Identifier: GPL-3.0-or-later
package network

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

var (
	ErrPaused  = errors.New("network: traffic paused")
	ErrControl = errors.New("network: exclusive platform DMA control required")
	ErrBusy    = errors.New("network: control operation active")
)

// DMAControl is supplied by the trusted platform for this exact device and
// pinned arena, never by a package. Stop must stop the driver and independently
// prove all DMA/transactions drained. Clearing start bits is insufficient.
// Start must revalidate ownership and safely restart that same device. Both
// callbacks must honor their contexts. Memory stays reserved until reboot.
type DMAControl struct {
	Stop  func(context.Context) error
	Start func(context.Context) error
	// ResumeTimeout bounds the complete trusted restart, including any PHY
	// training. A zero value keeps the five-second default. Stop always has
	// its separate five-second drain bound.
	ResumeTimeout time.Duration
	// Confirm is an optional platform-stage check after driver initialization.
	// The driver must consume it before exposing its final Stop/Start controls;
	// NewController rejects an unconsumed hook instead of reopening traffic.
	Confirm func(context.Context) error
}

// Controller joins a software traffic gate to explicit platform DMA controls.
// Each controller is permanently bound to one device and its pinned DMA arena.
// A paired DHCP launcher can replace an expired software endpoint on resume.
type Controller struct {
	state *linkController
}

type linkController struct {
	operation sync.Mutex
	link      *Link
	dma       DMAControl
	paused    bool
	assigned  atomic.Bool
	// I/O uncertainty is terminal independently of any expired software link.
	hardwareError error
	lease         *pauseLease
}

// Only the DHCP-selected local launcher installs recovery. A replaced link is
// permanently retired. The original physical controller and arena stay owned.
type pauseLease struct {
	expired     func() bool
	withdraw    func(context.Context) error
	acquire     func(context.Context) (*Link, error)
	unavailable func(error)
}

func invalidDHCPLease(err error) bool {
	// Only definitive responses from the renewing client retire an otherwise
	// healthy address. Driver and hardware authority are checked separately.
	return err == ErrLeaseExpired || err == ErrLeaseNAK || err == ErrLeaseChanged
}

func (c *linkController) leaseRetired() bool {
	return c.lease != nil && (invalidDHCPLease(c.link.Err()) || c.lease.expired())
}

func (c *linkController) checkLink() error {
	if c.hardwareError != nil {
		return c.hardwareError
	}
	if err := c.link.driverError(); err != nil {
		return err
	}
	if failure := c.link.state.failure.Load(); failure != nil {
		if c.lease != nil && invalidDHCPLease(failure.err) {
			return nil
		}
		return failure.err
	}
	return nil
}

// Claim transfers this active controller to one lifecycle owner, without IO.
// Copies share the claim. The caller must stop using the handle independently;
// claiming it supplies no platform or DMA authority of its own.
func (c *Controller) Claim() error {
	if c == nil || c.state == nil {
		return ErrControl
	}
	if !c.state.operation.TryLock() {
		return ErrBusy
	}
	defer c.state.operation.Unlock()
	if err := c.state.link.Available(); err != nil {
		return err
	}
	if !c.state.assigned.CompareAndSwap(false, true) {
		return ErrControl
	}
	return nil
}

// NewController binds once, without hardware IO. A missing control supplier
// cannot turn software cancellation into a DMA fence. Failed control retains
// this claim and the gate permanently; no replacement or retry is permitted.
func NewController(link *Link, dma DMAControl) (*Controller, error) {
	if link == nil || link.state == nil || link.device == nil || link.stack == nil || dma.Stop == nil || dma.Start == nil || dma.Confirm != nil || dma.ResumeTimeout < 0 {
		return nil, ErrControl
	}
	if err := link.Err(); err != nil {
		return nil, err
	}
	if !link.state.claimed.CompareAndSwap(false, true) {
		return nil, ErrControl
	}
	if dma.ResumeTimeout == 0 {
		dma.ResumeTimeout = 5 * time.Second
	}
	return &Controller{state: &linkController{link: link, dma: dma}}, nil
}

// Available rejects new sockets while paused. Err reports terminal failures
// separately: a healthy paused receive loop must remain alive for resumption.
func (l *Link) Available() error {
	if err := l.Err(); err != nil {
		return err
	}
	if l.state.paused.Load() {
		return ErrPaused
	}
	if expiry := l.state.expired; expiry != nil && expiry() {
		return ErrLeaseExpired
	}
	return nil
}

func (l *Link) enter() bool {
	if l.Available() != nil || !l.state.io.TryRLock() {
		return false
	}
	if l.Available() != nil {
		l.state.io.RUnlock()
		return false
	}
	return true
}

func (c *linkController) fail(err error) error {
	c.link.state.paused.Store(true)
	c.hardwareError = err
	c.link.fail(err)
	return err
}

// PauseNetwork closes the gate before waiting for every in-flight Receive and
// Transmit, then requests independent platform drain. Success is idempotent
// while already paused; unknown completion is terminal and never replayed.
func (c *Controller) PauseNetwork(parent context.Context) error {
	if c == nil || c.state == nil {
		return ErrControl
	}
	return c.state.pause(parent)
}

func (c *linkController) pause(parent context.Context) error {
	if parent == nil {
		return ErrControl
	}
	if err := parent.Err(); err != nil {
		return err
	}
	if !c.operation.TryLock() {
		return ErrBusy
	}
	defer c.operation.Unlock()
	if err := c.checkLink(); err != nil {
		return err
	}
	if c.paused {
		return nil
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	c.link.state.paused.Store(true)
	if err := c.link.waitForIO(ctx); err != nil {
		return c.fail(err)
	}
	if err := errors.Join(ctx.Err(), c.checkLink()); err != nil {
		return c.fail(err)
	}
	if err := c.dma.Stop(ctx); err != nil {
		return c.fail(err)
	}
	if err := errors.Join(ctx.Err(), c.checkLink()); err != nil {
		return c.fail(err)
	}
	c.paused = true
	return nil
}

// Only call after closing the packet gate. New readers cannot enter while we
// wait for any device Receive or Transmit already in progress.
func (l *Link) waitForIO(ctx context.Context) error {
	for !l.state.io.TryLock() {
		if err := ctx.Err(); err != nil {
			return err
		}
		runtime.Gosched()
	}
	l.state.io.Unlock()
	return ctx.Err()
}

// ResumeNetwork opens the gate only after the paired platform restart succeeds.
// Existing TCP operations retain their application deadlines during a pause;
// the stack's existing egress worker drains queued packets after resumption.
func (c *Controller) ResumeNetwork(parent context.Context) error {
	if c == nil || c.state == nil {
		return ErrControl
	}
	return c.state.resume(parent, nil, nil)
}

// ResumeNetworkWithScope confirms the caller's protected running-core scope
// after a trusted hardware restart, before the first packet can use the MAC.
// If DHCP subsequently fails and safely stops that MAC, verifyStopped must
// independently match that stop and the original scope before local use.
func (c *Controller) ResumeNetworkWithScope(parent context.Context, confirm, verifyStopped func() error) error {
	if c == nil || c.state == nil || confirm == nil || verifyStopped == nil {
		return ErrControl
	}
	return c.state.resume(parent, confirm, verifyStopped)
}

func (c *linkController) resume(parent context.Context, confirm, verifyStopped func() error) error {
	if parent == nil {
		return ErrControl
	}
	if err := parent.Err(); err != nil {
		return err
	}
	if !c.operation.TryLock() {
		return ErrBusy
	}
	defer c.operation.Unlock()
	if err := c.checkLink(); err != nil {
		return err
	}
	if !c.paused {
		return ErrControl
	}
	ctx, cancel := context.WithTimeout(parent, c.dma.ResumeTimeout)
	defer cancel()
	if err := c.dma.Start(ctx); err != nil {
		return c.fail(err)
	}
	if err := errors.Join(ctx.Err(), c.checkLink()); err != nil {
		return c.fail(err)
	}
	// A lease may expire during PHY training. Keep the original packet gate
	// shut until that boundary has completed and the lease is checked again.
	if confirm != nil {
		if err := confirm(); err != nil {
			return c.fail(err)
		}
	}
	if c.leaseRetired() {
		old := c.link
		old.fail(ErrLeaseExpired)
		acquire, done := context.WithTimeout(parent, defaultDHCPTimeout)
		err := c.lease.withdraw(acquire)
		var fresh *Link
		if err == nil {
			fresh, err = c.lease.acquire(acquire)
		}
		done()
		if err != nil {
			// The physical restart succeeded. Prove a new physical stop even
			// when DHCP's deadline expired before allowing local operation.
			drain, finish := context.WithTimeout(context.Background(), 5*time.Second)
			if fresh != nil {
				// A fresh stack may already have entered device I/O before
				// its publication failed. Its packet gate is now closed.
				if waitErr := fresh.waitForIO(drain); waitErr != nil {
					finish()
					return c.fail(errors.Join(err, waitErr))
				}
			}
			stopErr := c.dma.Stop(drain)
			if stopErr == nil {
				stopErr = drain.Err()
			}
			finish()
			if stopErr != nil {
				return c.fail(errors.Join(err, stopErr))
			}
			if reason := c.checkLink(); reason != nil {
				return c.fail(errors.Join(err, reason))
			}
			if verifyStopped != nil {
				if stoppedErr := verifyStopped(); stoppedErr != nil {
					return c.fail(errors.Join(err, stoppedErr))
				}
			}
			c.lease.unavailable(err)
			return nil
		}
		if fresh == nil || fresh == old || fresh.device != old.device || fresh.Err() != nil ||
			!fresh.state.claimed.CompareAndSwap(false, true) {
			return c.fail(errors.New("network: invalid replacement DHCP link"))
		}
		c.link = fresh
	}
	c.paused = false
	c.link.state.paused.Store(false)
	return nil
}

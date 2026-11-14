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
	// Confirm is an optional platform-stage check after driver initialization.
	// The driver must consume it before exposing its final Stop/Start controls;
	// NewController rejects an unconsumed hook instead of reopening traffic.
	Confirm func(context.Context) error
}

// Controller joins a software traffic gate to explicit platform DMA controls.
// It keeps the existing IP stack and socket provider; there is no reattachment,
// new descriptor allocation, implicit hardware implementation or recovery.
type Controller struct {
	state *linkController
}

type linkController struct {
	operation sync.Mutex
	link      *Link
	dma       DMAControl
	paused    bool
	assigned  atomic.Bool
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
	if link == nil || link.state == nil || link.device == nil || link.stack == nil || dma.Stop == nil || dma.Start == nil || dma.Confirm != nil {
		return nil, ErrControl
	}
	if err := link.Err(); err != nil {
		return nil, err
	}
	if !link.state.claimed.CompareAndSwap(false, true) {
		return nil, ErrControl
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
	c.link.fail(err)
	return c.link.Err()
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
	if err := c.link.Err(); err != nil {
		return err
	}
	if c.paused {
		return nil
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	c.link.state.paused.Store(true)
	for !c.link.state.io.TryLock() {
		if err := ctx.Err(); err != nil {
			return c.fail(err)
		}
		runtime.Gosched()
	}
	c.link.state.io.Unlock()
	if err := errors.Join(ctx.Err(), c.link.Err()); err != nil {
		return c.fail(err)
	}
	if err := c.dma.Stop(ctx); err != nil {
		return c.fail(err)
	}
	if err := errors.Join(ctx.Err(), c.link.Err()); err != nil {
		return c.fail(err)
	}
	c.paused = true
	return nil
}

// ResumeNetwork opens the gate only after the paired platform restart succeeds.
// Existing TCP operations retain their application deadlines during a pause;
// the stack's existing egress worker drains queued packets after resumption.
func (c *Controller) ResumeNetwork(parent context.Context) error {
	if c == nil || c.state == nil {
		return ErrControl
	}
	return c.state.resume(parent)
}

func (c *linkController) resume(parent context.Context) error {
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
	if err := c.link.Err(); err != nil {
		return err
	}
	if !c.paused {
		return ErrControl
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	if err := c.dma.Start(ctx); err != nil {
		return c.fail(err)
	}
	if err := errors.Join(ctx.Err(), c.link.Err()); err != nil {
		return c.fail(err)
	}
	c.paused = false
	c.link.state.paused.Store(false)
	return nil
}

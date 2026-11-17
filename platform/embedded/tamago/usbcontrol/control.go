// SPDX-License-Identifier: GPL-3.0-or-later

// Package usbcontrol owns the private firmware USB PIO lease. It never
// initializes secure ownership; shared mode requires explicit firmware proof.
package usbcontrol

import (
	"context"
	"errors"
	"fmt"
	"sync"

	g "github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol/generated"
)

type Caller interface {
	Invoke([7]uint64) ([4]uint64, error)
}

var (
	ErrNotReady   = errors.New("usbcontrol: secure USB provider unavailable")
	ErrRetired    = errors.New("usbcontrol: uncertain or closed USB ownership; retain resources")
	ErrTransition = errors.New("usbcontrol: invalid local transition")
	ErrBusy       = errors.New("usbcontrol: local operation in flight")
)

type Receipt struct {
	Owner, Generation uint64
	Active, Confirmed bool
}
type Client struct {
	operation sync.Mutex
	view      sync.RWMutex
	cpu       Caller
	last      Receipt
	retired   bool
	shared    bool
}

func decode(r [4]uint64, shared bool) (Receipt, bool, error) {
	tag := uint64(g.FesEmbeddedUsbTag) << 32
	if r[1]>>32 != uint64(g.FesEmbeddedUsbTag) {
		return Receipt{}, false, errors.New("usbcontrol: unknown firmware tag")
	}
	if r[0] != 0 {
		if r[1] != tag || r[2] != 0 || r[3] != 0 {
			return Receipt{}, false, errors.New("usbcontrol: failed reply contains state")
		}
		if r[0] == uint64(g.FesEmbeddedUsbNotReady) {
			return Receipt{}, false, ErrNotReady
		}
		return Receipt{}, false, fmt.Errorf("usbcontrol: firmware status %d", r[0])
	}
	flags := uint32(r[1])
	base := g.FesEmbeddedUsbSupported | g.FesEmbeddedUsbDataHeld | g.FesEmbeddedUsbClosedOnly
	normalized := flags
	if shared {
		base = g.FesEmbeddedUsbSupported | g.FesEmbeddedUsbSharedPio
		normalized &^= g.FesEmbeddedUsbDataHeld
	}
	if normalized != base|g.FesEmbeddedUsbContained && normalized != base|g.FesEmbeddedUsbActive && normalized != base|g.FesEmbeddedUsbActive|g.FesEmbeddedUsbConfirmed {
		return Receipt{}, false, errors.New("usbcontrol: invalid USB scope flags")
	}
	v := Receipt{r[2], r[3], flags&g.FesEmbeddedUsbActive != 0, flags&g.FesEmbeddedUsbConfirmed != 0}
	if v.Owner == 0 || v.Active && v.Generation == 0 || v.Confirmed && v.Generation < 2 {
		return Receipt{}, false, errors.New("usbcontrol: invalid owner/generation")
	}
	return v, flags&g.FesEmbeddedUsbDataHeld != 0, nil
}
func New(ctx context.Context, cpu Caller, bootOwner uint64) (*Client, error) {
	return newClient(ctx, cpu, bootOwner, false)
}

// NewSharedPIO requires an explicitly bound shared firmware provider. The
// initial controller and data paths must be held. Later DataHeld observations
// may change with the owned core; they never grant the caller bridge authority.
func NewSharedPIO(ctx context.Context, cpu Caller, bootOwner uint64) (*Client, error) {
	return newClient(ctx, cpu, bootOwner, true)
}
func newClient(ctx context.Context, cpu Caller, bootOwner uint64, shared bool) (*Client, error) {
	if ctx == nil || cpu == nil || bootOwner == 0 {
		return nil, ErrNotReady
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	raw, err := cpu.Invoke([7]uint64{uint64(g.FesEmbeddedUsbCall), uint64(g.FesEmbeddedUsbQuery), 0, uint64(g.FesEmbeddedUsbCookie)})
	if err != nil {
		return nil, err
	}
	v, held, err := decode(raw, shared)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Reconstructing a client cannot adopt a previous, active or returned lease.
	if v.Owner != bootOwner || v.Generation != 0 || v.Active || v.Confirmed || !held {
		return nil, ErrTransition
	}
	return &Client{cpu: cpu, last: v, shared: shared}, nil
}
func (c *Client) LastReceipt() (Receipt, bool) {
	if c == nil {
		return Receipt{}, false
	}
	c.view.RLock()
	defer c.view.RUnlock()
	return c.last, !c.retired && c.cpu != nil
}
func (c *Client) retire(err error) error {
	c.view.Lock()
	c.retired = true
	c.view.Unlock()
	return errors.Join(ErrRetired, err)
}
func (c *Client) transition(ctx context.Context, op uint32) error {
	if c == nil {
		return ErrRetired
	}
	if !c.operation.TryLock() {
		return ErrBusy
	}
	defer c.operation.Unlock()
	return c.transitionLocked(ctx, op)
}
func (c *Client) transitionLocked(ctx context.Context, op uint32) error {
	before, valid := c.LastReceipt()
	if !valid {
		return ErrRetired
	}
	if ctx == nil {
		return ErrTransition
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	switch op {
	case g.FesEmbeddedUsbAcquire:
		if before.Active {
			return ErrTransition
		}
	case g.FesEmbeddedUsbConfirm:
		if !before.Active || before.Confirmed {
			return ErrTransition
		}
	case g.FesEmbeddedUsbStop, g.FesEmbeddedUsbQuery:
	default:
		return ErrTransition
	}
	increment := op == g.FesEmbeddedUsbAcquire || op == g.FesEmbeddedUsbConfirm || op == g.FesEmbeddedUsbStop && before.Active
	if increment && before.Generation == ^uint64(0) {
		return c.retire(errors.New("USB generation exhausted"))
	}
	owner, generation := before.Owner, before.Generation
	if op == g.FesEmbeddedUsbQuery {
		owner, generation = 0, 0
	}
	raw, err := c.cpu.Invoke([7]uint64{uint64(g.FesEmbeddedUsbCall), uint64(op), owner, uint64(g.FesEmbeddedUsbCookie), generation})
	if err != nil {
		return c.retire(err)
	}
	after, _, err := decode(raw, c.shared)
	if err != nil {
		return c.retire(err)
	}
	expected := before
	switch op {
	case g.FesEmbeddedUsbAcquire:
		expected.Active = true
	case g.FesEmbeddedUsbConfirm:
		expected.Confirmed = true
	case g.FesEmbeddedUsbStop:
		expected.Active = false
		expected.Confirmed = false
	}
	if increment {
		expected.Generation++
	}
	if after != expected {
		return c.retire(errors.New("usbcontrol: transition receipt mismatch"))
	}
	c.view.Lock()
	c.last = after
	c.view.Unlock()
	// A complete response still records exact ownership if cancellation arrives
	// during the bounded call, permitting cleanup of that known lease.
	return ctx.Err()
}
func (c *Client) Acquire(ctx context.Context) error {
	return c.transition(ctx, g.FesEmbeddedUsbAcquire)
}
func (c *Client) Confirm(ctx context.Context) error {
	return c.transition(ctx, g.FesEmbeddedUsbConfirm)
}
func (c *Client) Stop(ctx context.Context) error    { return c.transition(ctx, g.FesEmbeddedUsbStop) }
func (c *Client) Observe(ctx context.Context) error { return c.transition(ctx, g.FesEmbeddedUsbQuery) }

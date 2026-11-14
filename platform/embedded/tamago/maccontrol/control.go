// SPDX-License-Identifier: GPL-3.0-or-later

// Package maccontrol consumes the fixed firmware MAC lease. It supplies no
// physical platform qualification, address selection or secure initializer.
package maccontrol

import (
	"context"
	"errors"
	"fmt"
	"sync"

	g "github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol/generated"
	"github.com/DeanoC/fes/platform/embedded/tamago/network"
)

type Caller interface {
	Invoke([7]uint64) ([4]uint64, error)
}

var (
	ErrNotReady   = errors.New("maccontrol: trusted MAC supplier unavailable")
	ErrRetired    = errors.New("maccontrol: uncertain MAC ownership; retain DMA memory")
	ErrTransition = errors.New("maccontrol: invalid local transition")
	ErrBusy       = errors.New("maccontrol: operation in flight")
)

// Receipt is the last complete reply, not a live hardware observation. This
// generation is private to the MAC and cannot replace a bridge/SD/USB receipt.
type Receipt struct {
	Owner, Generation uint64
	Active, Confirmed bool
}

// Client copies share ownership, retirement and the exclusive platform binding.
type Client struct{ state *client }
type client struct {
	operation sync.Mutex
	view      sync.RWMutex
	cpu       Caller
	last      Receipt
	retired   bool
	bound     bool
}

func decode(r [4]uint64) (Receipt, error) {
	tag := uint64(g.FesEmbeddedMacTag) << 32
	if r[1]>>32 != uint64(g.FesEmbeddedMacTag) {
		return Receipt{}, errors.New("maccontrol: unknown reply tag")
	}
	if r[0] != 0 {
		if r[1] != tag || r[2] != 0 || r[3] != 0 {
			return Receipt{}, errors.New("maccontrol: failed reply contains ownership")
		}
		if r[0] == uint64(g.FesEmbeddedMacNotReady) {
			return Receipt{}, ErrNotReady
		}
		return Receipt{}, fmt.Errorf("maccontrol: firmware status %d", r[0])
	}
	flags := uint32(r[1])
	base := g.FesEmbeddedMacSupported | g.FesEmbeddedMacPinnedWindow | g.FesEmbeddedMacPrivateGeneration
	if flags != base|g.FesEmbeddedMacContained && flags != base|g.FesEmbeddedMacActive && flags != base|g.FesEmbeddedMacActive|g.FesEmbeddedMacConfirmed {
		return Receipt{}, errors.New("maccontrol: invalid state flags")
	}
	v := Receipt{r[2], r[3], flags&g.FesEmbeddedMacActive != 0, flags&g.FesEmbeddedMacConfirmed != 0}
	if v.Owner == 0 || v.Active && v.Generation == 0 || v.Confirmed && v.Generation < 2 {
		return Receipt{}, errors.New("maccontrol: invalid owner/generation")
	}
	return v, nil
}

// New requires this trusted boot's fresh contained generation zero. The caller
// must separately bind the exact device/window to its qualified boot profile.
// A reconstructed client cannot adopt a prior active or returned lease.
func New(ctx context.Context, cpu Caller, bootOwner uint64) (*Client, error) {
	if ctx == nil || cpu == nil || bootOwner == 0 {
		return nil, ErrNotReady
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r, err := cpu.Invoke([7]uint64{uint64(g.FesEmbeddedMacCall), uint64(g.FesEmbeddedMacQuery), 0, uint64(g.FesEmbeddedMacCookie)})
	if err != nil {
		return nil, err
	}
	v, err := decode(r)
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if v != (Receipt{Owner: bootOwner}) {
		return nil, ErrTransition
	}
	return &Client{state: &client{cpu: cpu, last: v}}, nil
}

func (c *Client) LastReceipt() (Receipt, bool) {
	if c == nil || c.state == nil {
		return Receipt{}, false
	}
	s := c.state
	s.view.RLock()
	defer s.view.RUnlock()
	return s.last, !s.retired
}

func (s *client) retire(err error) error {
	s.view.Lock()
	s.retired = true
	s.view.Unlock()
	return errors.Join(ErrRetired, err)
}

func (c *Client) transition(ctx context.Context, op uint32, bound bool) error {
	if c == nil || c.state == nil {
		return ErrRetired
	}
	s := c.state
	if !s.operation.TryLock() {
		return ErrBusy
	}
	defer s.operation.Unlock()
	before, valid := c.LastReceipt()
	if !valid {
		return ErrRetired
	}
	if ctx == nil || s.bound != bound {
		return ErrTransition
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	expected := before
	needed := uint64(0)
	switch op {
	case g.FesEmbeddedMacAcquire:
		if before.Active {
			return ErrTransition
		}
		expected.Active = true
		needed = 3
	case g.FesEmbeddedMacConfirm:
		if !before.Active || before.Confirmed {
			return ErrTransition
		}
		expected.Confirmed = true
		needed = 2
	case g.FesEmbeddedMacStop:
		expected.Active, expected.Confirmed = false, false
		if before.Active {
			needed = 1
		}
	case g.FesEmbeddedMacQuery:
	default:
		return ErrTransition
	}
	if before.Generation > ^uint64(0)-needed {
		return s.retire(errors.New("MAC generation exhausted"))
	}
	if needed != 0 {
		expected.Generation++
	}
	owner, generation := before.Owner, before.Generation
	if op == g.FesEmbeddedMacQuery {
		owner, generation = 0, 0
	}
	raw, err := s.cpu.Invoke([7]uint64{uint64(g.FesEmbeddedMacCall), uint64(op), owner, uint64(g.FesEmbeddedMacCookie), generation})
	if err != nil {
		return s.retire(err)
	}
	after, err := decode(raw)
	if err != nil {
		return s.retire(err)
	}
	if after != expected {
		return s.retire(errors.New("maccontrol: transition receipt mismatch"))
	}
	s.view.Lock()
	s.last = after
	s.view.Unlock()
	// Record exact completed ownership even if cancellation arrived in the
	// bounded call. The driver/controller still retires the cancelled operation.
	return ctx.Err()
}

func (c *Client) Acquire(ctx context.Context) error {
	return c.transition(ctx, g.FesEmbeddedMacAcquire, false)
}
func (c *Client) Confirm(ctx context.Context) error {
	return c.transition(ctx, g.FesEmbeddedMacConfirm, false)
}
func (c *Client) Stop(ctx context.Context) error {
	return c.transition(ctx, g.FesEmbeddedMacStop, false)
}
func (c *Client) Observe(ctx context.Context) error {
	return c.transition(ctx, g.FesEmbeddedMacQuery, false)
}

// BindPlatform transfers one confirmed lease to the XGMAC driver's platform
// stages before packet workers start. Pass the result through Device.BindControl,
// never directly to NewController: Start grants stopped controller access and
// Confirm must follow descriptor initialization before traffic resumes.
func (c *Client) BindPlatform() (network.DMAControl, error) {
	if c == nil || c.state == nil {
		return network.DMAControl{}, ErrRetired
	}
	s := c.state
	if !s.operation.TryLock() {
		return network.DMAControl{}, ErrBusy
	}
	defer s.operation.Unlock()
	v, valid := c.LastReceipt()
	if !valid {
		return network.DMAControl{}, ErrRetired
	}
	if s.bound || !v.Active || !v.Confirmed {
		return network.DMAControl{}, ErrTransition
	}
	s.bound = true
	handle := Client{state: s}
	return network.DMAControl{
		Stop:    func(ctx context.Context) error { return handle.transition(ctx, g.FesEmbeddedMacStop, true) },
		Start:   func(ctx context.Context) error { return handle.transition(ctx, g.FesEmbeddedMacAcquire, true) },
		Confirm: func(ctx context.Context) error { return handle.transition(ctx, g.FesEmbeddedMacConfirm, true) },
	}, nil
}

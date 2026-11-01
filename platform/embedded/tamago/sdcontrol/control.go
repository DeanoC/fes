// SPDX-License-Identifier: GPL-3.0-or-later

// Package sdcontrol coordinates the experimental firmware SD lease. It grants
// no platform qualification and never initializes secure firmware ownership.
package sdcontrol

import (
	"errors"
	"fmt"
	"sync"

	g "github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol/generated"
)

type Caller interface {
	Invoke([7]uint64) ([4]uint64, error)
}

var (
	ErrNotReady   = errors.New("sdcontrol: trusted firmware SD ownership not initialized")
	ErrRetired    = errors.New("sdcontrol: uncertain ownership; unresolved lease retained")
	ErrTransition = errors.New("sdcontrol: invalid local lease transition")
)

// Receipt is the last successful firmware response, not a fresh observation.
// Owner and Generation share the bridge channel's namespace.
type Receipt struct {
	Owner, Generation uint64
	Active, Prepared  bool
}

type Client struct {
	mu      sync.Mutex
	cpu     Caller
	last    Receipt
	retired bool
}

func decode(r [4]uint64) (Receipt, error) {
	tag := uint64(g.FesEmbeddedSdTag) << 32
	if r[1]>>32 != uint64(g.FesEmbeddedSdTag) {
		return Receipt{}, errors.New("sdcontrol: unknown firmware tag")
	}
	if r[0] != 0 {
		if r[1] != tag || r[2] != 0 || r[3] != 0 {
			return Receipt{}, errors.New("sdcontrol: unsuccessful firmware reply contains state")
		}
		switch r[0] {
		case uint64(g.FesEmbeddedSdNotReady):
			return Receipt{}, ErrNotReady
		case uint64(g.FesEmbeddedSdBusy), uint64(g.FesEmbeddedSdRejected), uint64(g.FesEmbeddedSdError):
			return Receipt{}, fmt.Errorf("sdcontrol: firmware status %d", r[0])
		default:
			return Receipt{}, errors.New("sdcontrol: unknown firmware status")
		}
	}
	flags := uint32(r[1])
	base := g.FesEmbeddedSdSupported | g.FesEmbeddedSdDataHeld | g.FesEmbeddedSdClosedOnly
	if (flags != base|g.FesEmbeddedSdContained && flags != base|g.FesEmbeddedSdActive &&
		flags != base|g.FesEmbeddedSdActive|g.FesEmbeddedSdPrepared) || r[2] == 0 || r[3] == 0 {
		return Receipt{}, errors.New("sdcontrol: invalid firmware SD state")
	}
	return Receipt{Owner: r[2], Generation: r[3], Active: flags&g.FesEmbeddedSdActive != 0,
		Prepared: flags&g.FesEmbeddedSdPrepared != 0}, nil
}

// NewClient requires a fresh contained observation. An already active lease
// cannot be adopted by reconstructing a client after a failure.
func NewClient(cpu Caller) (*Client, error) {
	if cpu == nil {
		return nil, errors.New("sdcontrol: missing firmware transport")
	}
	r, err := cpu.Invoke([7]uint64{uint64(g.FesEmbeddedSdCall), uint64(g.FesEmbeddedSdQuery), 0, uint64(g.FesEmbeddedSdCookie)})
	if err != nil {
		return nil, err
	}
	receipt, err := decode(r)
	if err != nil {
		return nil, err
	}
	if receipt.Active {
		return nil, errors.New("sdcontrol: firmware SD lease already active")
	}
	return &Client{cpu: cpu, last: receipt}, nil
}

func (c *Client) LastReceipt() (Receipt, bool) {
	if c == nil {
		return Receipt{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.last, !c.retired && c.cpu != nil && c.last.Owner != 0
}
func (c *Client) retire() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.retired = true
	c.mu.Unlock()
}
func (c *Client) transition(op uint32) error {
	if c == nil {
		return ErrRetired
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.retired || c.cpu == nil || c.last.Owner == 0 || c.last.Generation == 0 {
		return ErrRetired
	}
	before := c.last
	switch op {
	case g.FesEmbeddedSdResume:
		if before.Active {
			return ErrTransition
		}
	case g.FesEmbeddedSdPrepare:
		if !before.Active || before.Prepared {
			return ErrTransition
		}
	case g.FesEmbeddedSdStop:
		if !before.Active {
			return ErrTransition
		}
	default:
		return ErrTransition
	}
	fail := func(err error) error { c.retired = true; return errors.Join(ErrRetired, err) }
	if before.Generation == ^uint64(0) {
		return fail(errors.New("sdcontrol: generation exhausted"))
	}
	raw, err := c.cpu.Invoke([7]uint64{uint64(g.FesEmbeddedSdCall), uint64(op), before.Owner,
		uint64(g.FesEmbeddedSdCookie), before.Generation})
	if err != nil {
		return fail(err)
	}
	after, err := decode(raw)
	if err != nil {
		return fail(err)
	}
	active := op != g.FesEmbeddedSdStop
	prepared := op == g.FesEmbeddedSdPrepare
	if after.Owner != before.Owner || after.Generation != before.Generation+1 ||
		after.Active != active || after.Prepared != prepared {
		return fail(errors.New("sdcontrol: firmware transition receipt mismatch"))
	}
	c.last = after
	return nil
}
func (c *Client) Resume() error  { return c.transition(g.FesEmbeddedSdResume) }
func (c *Client) Prepare() error { return c.transition(g.FesEmbeddedSdPrepare) }
func (c *Client) Stop() error    { return c.transition(g.FesEmbeddedSdStop) }

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
// Closed-bridge receipts share the bridge channel's namespace; Shared receipts
// use a private SD generation starting at zero.
type Receipt struct {
	Owner, Generation uint64
	Active, Prepared  bool
	Shared            bool
}

type Client struct {
	mu       sync.Mutex
	cpu      Caller
	last     Receipt
	retired  bool
	shared   bool
	dataHeld bool
}

func decode(r [4]uint64, shared bool) (Receipt, bool, error) {
	tag := uint64(g.FesEmbeddedSdTag) << 32
	if r[1]>>32 != uint64(g.FesEmbeddedSdTag) {
		return Receipt{}, false, errors.New("sdcontrol: unknown firmware tag")
	}
	if r[0] != 0 {
		if r[1] != tag || r[2] != 0 || r[3] != 0 {
			return Receipt{}, false, errors.New("sdcontrol: unsuccessful firmware reply contains state")
		}
		switch r[0] {
		case uint64(g.FesEmbeddedSdNotReady):
			return Receipt{}, false, ErrNotReady
		case uint64(g.FesEmbeddedSdBusy), uint64(g.FesEmbeddedSdRejected), uint64(g.FesEmbeddedSdError):
			return Receipt{}, false, fmt.Errorf("sdcontrol: firmware status %d", r[0])
		default:
			return Receipt{}, false, errors.New("sdcontrol: unknown firmware status")
		}
	}
	flags := uint32(r[1])
	base := g.FesEmbeddedSdSupported | g.FesEmbeddedSdDataHeld | g.FesEmbeddedSdClosedOnly
	normalized := flags
	if shared {
		base = g.FesEmbeddedSdSupported | g.FesEmbeddedSdShared | g.FesEmbeddedSdPrivateGeneration
		normalized &^= g.FesEmbeddedSdDataHeld
	}
	if normalized != base|g.FesEmbeddedSdContained && normalized != base|g.FesEmbeddedSdActive &&
		normalized != base|g.FesEmbeddedSdActive|g.FesEmbeddedSdPrepared {
		return Receipt{}, false, errors.New("sdcontrol: invalid firmware SD state")
	}
	v := Receipt{
		Owner:      r[2],
		Generation: r[3],
		Active:     flags&g.FesEmbeddedSdActive != 0,
		Prepared:   flags&g.FesEmbeddedSdPrepared != 0,
		Shared:     shared,
	}
	if v.Owner == 0 || (!shared && v.Generation == 0) || (v.Active && v.Generation == 0) || (v.Prepared && v.Generation < 2) {
		return Receipt{}, false, errors.New("sdcontrol: invalid firmware SD state")
	}
	return v, flags&g.FesEmbeddedSdDataHeld != 0, nil
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
	receipt, held, err := decode(r, false)
	if err != nil {
		return nil, err
	}
	if receipt.Active {
		return nil, errors.New("sdcontrol: firmware SD lease already active")
	}
	return &Client{cpu: cpu, last: receipt, dataHeld: held}, nil
}

// NewSharedClient opens an opt-in concurrent SD lease client using private SD
// generations starting at zero for the given trusted bootOwner.
func NewSharedClient(cpu Caller, bootOwner uint64) (*Client, error) {
	if cpu == nil || bootOwner == 0 {
		return nil, ErrNotReady
	}
	r, err := cpu.Invoke([7]uint64{uint64(g.FesEmbeddedSdCall), uint64(g.FesEmbeddedSdQuery), 0, uint64(g.FesEmbeddedSdCookie)})
	if err != nil {
		return nil, err
	}
	receipt, held, err := decode(r, true)
	if err != nil {
		return nil, err
	}
	if receipt != (Receipt{Owner: bootOwner, Shared: true}) {
		return nil, ErrTransition
	}
	return &Client{cpu: cpu, last: receipt, shared: true, dataHeld: held}, nil
}

func (c *Client) Shared() bool {
	if c == nil {
		return false
	}
	return c.shared
}

func (c *Client) DataHeld() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.retired && c.dataHeld
}

// Observe queries live firmware SD state, refreshing DataHeld while verifying
// that owner, generation, and active/prepared flags remain unchanged.
func (c *Client) Observe() (Receipt, error) {
	if c == nil {
		return Receipt{}, ErrRetired
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.retired || c.cpu == nil || c.last.Owner == 0 {
		return Receipt{}, ErrRetired
	}
	r, err := c.cpu.Invoke([7]uint64{uint64(g.FesEmbeddedSdCall), uint64(g.FesEmbeddedSdQuery), 0, uint64(g.FesEmbeddedSdCookie)})
	if err != nil {
		c.retired = true
		return Receipt{}, errors.Join(ErrRetired, err)
	}
	observed, held, err := decode(r, c.shared)
	if err != nil {
		c.retired = true
		return Receipt{}, errors.Join(ErrRetired, err)
	}
	if observed != c.last {
		c.retired = true
		return Receipt{}, errors.Join(ErrRetired, errors.New("sdcontrol: observed SD lease state differs"))
	}
	c.dataHeld = held
	return observed, nil
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
	return c.transitionLocked(op)
}

func (c *Client) transitionLocked(op uint32) error {
	if c.retired || c.cpu == nil || c.last.Owner == 0 || (!c.shared && c.last.Generation == 0) {
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
	if before.Generation == ^uint64(0) || (op == g.FesEmbeddedSdResume && before.Generation > ^uint64(0)-3) {
		return fail(errors.New("sdcontrol: generation exhausted"))
	}
	raw, err := c.cpu.Invoke([7]uint64{uint64(g.FesEmbeddedSdCall), uint64(op), before.Owner,
		uint64(g.FesEmbeddedSdCookie), before.Generation})
	if err != nil {
		return fail(err)
	}
	after, held, err := decode(raw, c.shared)
	if err != nil {
		return fail(err)
	}
	active := op != g.FesEmbeddedSdStop
	prepared := op == g.FesEmbeddedSdPrepare
	if after.Owner != before.Owner || after.Generation != before.Generation+1 ||
		after.Active != active || after.Prepared != prepared || after.Shared != c.shared {
		return fail(errors.New("sdcontrol: firmware transition receipt mismatch"))
	}
	c.last = after
	c.dataHeld = held
	return nil
}

// ResumeAfterReturn explicitly crosses a verified fabric return in the shared
// generation namespace. The caller supplies the expected closed return; the
// secure provider must independently confirm its retained producer-stop and
// bridge-closure proof before this query or any controller release succeeds.
// A value supplied by the caller alone grants no hardware access.
func (c *Client) ResumeAfterReturn(owner, generation uint64) error {
	if c == nil {
		return ErrRetired
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.retired || c.cpu == nil || c.last.Owner == 0 {
		return ErrRetired
	}
	if c.shared || c.last.Active || c.last.Prepared || owner != c.last.Owner ||
		generation <= c.last.Generation || generation > ^uint64(0)-3 {
		return ErrTransition
	}
	fail := func(err error) error { c.retired = true; return errors.Join(ErrRetired, err) }
	raw, err := c.cpu.Invoke([7]uint64{uint64(g.FesEmbeddedSdCall), uint64(g.FesEmbeddedSdQuery), 0, uint64(g.FesEmbeddedSdCookie)})
	if err != nil {
		return fail(err)
	}
	observed, held, err := decode(raw, false)
	if err != nil {
		return fail(err)
	}
	if observed != (Receipt{Owner: owner, Generation: generation}) {
		return fail(errors.New("sdcontrol: live SD containment differs from the expected fabric return"))
	}
	c.last = observed
	c.dataHeld = held
	return c.transitionLocked(g.FesEmbeddedSdResume)
}
func (c *Client) Resume() error  { return c.transition(g.FesEmbeddedSdResume) }
func (c *Client) Prepare() error { return c.transition(g.FesEmbeddedSdPrepare) }
func (c *Client) Stop() error    { return c.transition(g.FesEmbeddedSdStop) }

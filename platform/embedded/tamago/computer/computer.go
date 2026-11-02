// SPDX-License-Identifier: GPL-3.0-or-later
// Package computer controls the DE25 Avalon fes.computer mailbox. The platform
// owns mapping, ordering, bridge lifetime and containment; this package supplies
// no physical address, boot proof or runtime admission.
package computer

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	g "github.com/DeanoC/fes/platform/embedded/tamago/computer/generated"
)

var (
	ErrIdentity = errors.New("computer identity differs from the selected package")
	ErrProtocol = errors.New("computer transport fault or inconsistent acknowledgement")
	ErrRetired  = errors.New("computer transport retired; platform containment required")
	ErrBusy     = errors.New("computer operation already in progress")
	ErrLimit    = errors.New("computer operation exceeded its time or poll limit")
	ErrArgument = errors.New("invalid computer operation argument")
	errChanging = errors.New("computer snapshot changed while being read")
)

// IO uses peripheral-relative byte offsets and exactly one full-width write for
// GPO submission. Calls must finish synchronously within a platform bound,
// preserve order and report bus errors. Neither context nor a poll limit can
// interrupt a hook that does not return. Only one Computer may own an IO epoch.
type IO interface {
	Read32(uint32) (uint32, error)
	Write32(uint32, uint32) error
}

// Limits apply to an entire public operation; MaxPolls bounds each command's
// ACK observations independently. Now must be monotonic and Yield must return
// within a platform bound. Yield allows cooperative native scheduling.
type Limits struct {
	Timeout  time.Duration
	MaxPolls uint32
	Now      func() time.Time
	Yield    func()
}

// Identity is supplied from the already selected, verified package and platform
// contract. ABI minor 0 and 1 have explicit identity lengths; no probe fallback
// or inferred capability is permitted. Build contains bytes in manifest order.
type Identity struct {
	Minor        uint16
	Capabilities uint32
	Build        [16]byte
}

type Computer struct {
	io       IO
	limits   Limits
	identity Identity
	active   atomic.Bool
	retired  bool
	gpo      uint32
}

type operation struct {
	c           *Computer
	ctx         context.Context
	start, last time.Time
}

func (o *operation) check() error {
	if err := o.ctx.Err(); err != nil {
		return err
	}
	now := o.c.limits.Now()
	if now.Before(o.last) || now.Sub(o.start) >= o.c.limits.Timeout {
		return ErrLimit
	}
	o.last = now
	return nil
}

func (c *Computer) begin(ctx context.Context) (*operation, error) {
	if ctx == nil {
		return nil, ErrArgument
	}
	if !c.active.CompareAndSwap(false, true) {
		return nil, ErrBusy
	}
	if c.retired {
		c.active.Store(false)
		return nil, ErrRetired
	}
	if err := ctx.Err(); err != nil {
		c.active.Store(false)
		return nil, err
	}
	now := c.limits.Now()
	return &operation{c: c, ctx: ctx, start: now, last: now}, nil
}

func (o *operation) finish(err error) {
	// Once I/O begins any failure retires this epoch. An uncertain write is
	// never replayed and a partial input/media update is not called atomic.
	if err != nil {
		o.c.retired = true
	}
	o.c.active.Store(false)
}

func (o *operation) read(offset uint32) (uint32, error) {
	if err := o.check(); err != nil {
		return 0, err
	}
	v, err := o.c.io.Read32(offset)
	if err != nil {
		return 0, err
	}
	return v, o.check()
}

type snapshot struct{ status, gpo, gpi, fault uint32 }

func (o *operation) snapshot() (s snapshot, err error) {
	// Bracket a complete snapshot. State may complete while observed, so a
	// changing pair is retried by the bounded caller, never accepted as ACK.
	for _, f := range []struct {
		offset uint32
		dst    *uint32
	}{
		{g.FesDe25ComputerStatusOffset, &s.status},
		{g.FesDe25ComputerGpoOffset, &s.gpo},
		{g.FesDe25ComputerGpiOffset, &s.gpi},
		{g.FesDe25ComputerFaultOffset, &s.fault},
	} {
		*f.dst, err = o.read(f.offset)
		if err != nil {
			return s, err
		}
	}
	if s.fault != 0 || s.status & ^(g.FesDe25ComputerStatusReady|g.FesDe25ComputerStatusBusy) != 0 ||
		s.status&g.FesDe25ComputerStatusReady == 0 ||
		s.gpi & ^(g.FesComputerAckMask|g.FesComputerErrorMask|g.FesComputerResponseMask) != g.FesComputerSignature {
		return s, ErrProtocol
	}
	return s, nil
}

func acknowledged(s snapshot) bool {
	return (s.gpo&g.FesComputerRequestMask != 0) == (s.gpi&g.FesComputerAckMask != 0)
}

func (o *operation) stable() (snapshot, error) {
	a, err := o.snapshot()
	if err != nil {
		return a, err
	}
	b, err := o.snapshot()
	if err != nil {
		return b, err
	}
	if a != b {
		return b, errChanging
	}
	if (b.status&g.FesDe25ComputerStatusBusy == 0) != acknowledged(b) {
		return b, ErrProtocol
	}
	return b, nil
}

func (o *operation) exchange(opcode, index uint32, argument uint16) (uint16, error) {
	c := o.c
	s, err := o.stable()
	if err != nil {
		return 0, err
	}
	if s.gpo != c.gpo || !acknowledged(s) {
		return 0, ErrProtocol
	}
	word := ((c.gpo ^ g.FesComputerRequestMask) & g.FesComputerRequestMask) |
		opcode<<24 | index<<16 | uint32(argument)
	if err := o.check(); err != nil {
		return 0, err
	}
	// Unlike a direct GP port, the Avalon adapter rejects writes that do not
	// toggle. There is exactly one write, including for an eventual timeout.
	if err := c.io.Write32(g.FesDe25ComputerGpoOffset, word); err != nil {
		return 0, err
	}
	if err := o.check(); err != nil {
		return 0, err
	}
	for poll := uint32(0); poll < c.limits.MaxPolls; poll++ {
		s, err = o.stable()
		if err != nil && err != errChanging {
			return 0, err
		}
		if err == nil {
			if s.gpo != word {
				return 0, ErrProtocol
			}
			if acknowledged(s) {
				if s.gpi&g.FesComputerErrorMask != 0 {
					return 0, fmt.Errorf("%w: opcode %d rejected with code %d", ErrProtocol, opcode, s.gpi&g.FesComputerResponseMask)
				}
				c.gpo = word
				return uint16(s.gpi & g.FesComputerResponseMask), nil
			}
		}
		c.limits.Yield()
		if err := o.check(); err != nil {
			return 0, err
		}
	}
	return 0, ErrLimit
}

func (o *operation) mutate(opcode, index uint32, argument uint16) error {
	response, err := o.exchange(opcode, index, argument)
	if err == nil && response != 0 {
		return ErrProtocol
	}
	return err
}

// Open verifies the adapter and the entire live GP identity before returning a
// command-capable handle. It does not reset or release execution. The caller
// must contain/reconfigure the platform on failure; Open must not be retried on
// the same uncertain epoch.
func Open(ctx context.Context, io IO, limits Limits, want Identity) (_ *Computer, err error) {
	if io == nil || limits.Now == nil || limits.Yield == nil || limits.Timeout <= 0 || limits.MaxPolls == 0 ||
		want.Minor > 1 || (want.Minor == 0 && want.Capabilities>>16 != 0) || want.Build == [16]byte{} ||
		want.Capabilities&g.FesComputerCapabilityVideoFixed720p60 == 0 {
		return nil, ErrArgument
	}
	c := &Computer{io: io, limits: limits, identity: want}
	o, err := c.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { o.finish(err) }()
	id, err := o.read(g.FesDe25ComputerIdOffset)
	if err != nil {
		return nil, err
	}
	version, err := o.read(g.FesDe25ComputerVersionOffset)
	if err != nil {
		return nil, err
	}
	if id != g.FesDe25ComputerIdentity || version != g.FesDe25ComputerVersion {
		return nil, ErrIdentity
	}
	s, err := o.stable()
	if err != nil {
		return nil, err
	}
	if !acknowledged(s) {
		return nil, ErrProtocol
	}
	c.gpo = s.gpo
	var words [17]uint16
	count := int(g.FesComputerIdentityWordCount)
	if want.Minor == 1 {
		count = int(g.FesComputerIdentityExtendedWordCount)
	}
	for i := 0; i < count; i++ {
		words[i], err = o.exchange(g.FesComputerOpcodeIdentity, uint32(i), 0)
		if err != nil {
			return nil, err
		}
	}
	if words[0] != uint16(g.FesComputerIdentityMagic0) || words[1] != uint16(g.FesComputerIdentityMagic1) ||
		words[2] != uint16(g.FesComputerTransportMajor) || words[3] != uint16(g.FesComputerTransportMinor) ||
		words[4] != uint16(g.FesComputerAbiTag) || words[5] != uint16(g.FesComputerAbiMajor) || words[6] != want.Minor ||
		uint32(words[7])|uint32(words[16])<<16 != want.Capabilities {
		return nil, ErrIdentity
	}
	var build [16]byte
	for i := range 8 {
		word := words[int(g.FesComputerIdentityBuildIDStartIndex)+i]
		build[2*i], build[2*i+1] = byte(word), byte(word>>8)
	}
	if build != want.Build {
		return nil, ErrIdentity
	}
	return c, nil
}

func (c *Computer) execution(ctx context.Context, value uint16) (err error) {
	o, err := c.begin(ctx)
	if err != nil {
		return err
	}
	defer func() { o.finish(err) }()
	return o.mutate(g.FesComputerOpcodeExecution, g.FesComputerControlIndex, value)
}

// Hold holds execution reset and clears input without resetting the GP epoch.
func (c *Computer) Hold(ctx context.Context) error {
	return c.execution(ctx, uint16(g.FesComputerExecutionHoldReset))
}

// Run releases execution reset. An ACK proves command acceptance, not CPU work
// or visible video; that requires independent execution/physical observation.
func (c *Computer) Run(ctx context.Context) error {
	return c.execution(ctx, uint16(g.FesComputerExecutionRelease))
}

// Keyboard submits nine HID rows in order. Validation precedes all I/O. A
// partial failure retires the transport and requires platform containment.
func (c *Computer) Keyboard(ctx context.Context, rows [9]uint16) (err error) {
	if c.identity.Capabilities&g.FesComputerCapabilityKeyboardHid == 0 ||
		uint32(rows[0])&g.FesComputerKeyboardReservedRow0Mask != 0 ||
		uint32(rows[g.FesComputerKeyboardModifierRow]) & ^g.FesComputerKeyboardModifierMask != 0 {
		return ErrArgument
	}
	o, err := c.begin(ctx)
	if err != nil {
		return err
	}
	defer func() { o.finish(err) }()
	for i, row := range rows {
		if err = o.mutate(g.FesComputerOpcodeKeyboardHid, uint32(i), row); err != nil {
			return err
		}
	}
	return nil
}

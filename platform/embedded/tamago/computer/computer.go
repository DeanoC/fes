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

// AccessTiming is optional diagnostic evidence about the last synchronous IO
// call. It supplies no permission, timeout allowance or retry decision.
type AccessTiming struct {
	Offset                  uint32
	Write                   bool
	Before, Register, After time.Duration
	Completed               bool
}

// AccessTimer must return saved counters without performing IO. Computer asks
// for them only when reporting a limit failure; they never affect acceptance.
type AccessTimer interface{ LastAccessTiming() AccessTiming }

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

// FetchActivity observes instruction-fetch bus cycles since FPGA configuration,
// modulo 2^32, and the address sampled by that count read. It is independent of
// GP command acknowledgements. It does not count retired instructions or prove
// correct emulation, video output, memory containment or runtime admission.
type FetchActivity struct {
	Cycles  uint32
	Address uint16
}

type Computer struct {
	io          IO
	limits      Limits
	identity    Identity
	active      atomic.Bool
	retired     bool
	gpo         uint32
	initialTape InitialTapeLoader
}

type operation struct {
	c                            *Computer
	ctx                          context.Context
	start, last                  time.Time
	name, phase                  string
	timeout                      time.Duration
	exchangeStart                time.Time
	inExchange                   bool
	offset, opcode, index, polls uint32
	reads, writes                uint32
}

func (o *operation) limit(reason string, now time.Time) error {
	err := fmt.Errorf("%w: reason=%s operation=%s phase=%s elapsed=%s step=%s limit=%s offset=%#x opcode=%d index=%d polls=%d/%d reads=%d writes=%d",
		ErrLimit, reason, o.name, o.phase, now.Sub(o.start), now.Sub(o.last), o.timeout,
		o.offset, o.opcode, o.index, o.polls, o.c.limits.MaxPolls, o.reads, o.writes)
	if timer, ok := o.c.io.(AccessTimer); ok {
		t := timer.LastAccessTiming()
		return fmt.Errorf("%w access_offset=%#x access_write=%t ownership_before=%s register_io=%s ownership_after=%s access_complete=%t",
			err, t.Offset, t.Write, t.Before, t.Register, t.After, t.Completed)
	}
	return err
}

func (o *operation) check() error {
	if err := o.ctx.Err(); err != nil {
		return err
	}
	now := o.c.limits.Now()
	if now.Before(o.last) {
		return o.limit("backwards-clock", now)
	}
	if now.Sub(o.start) >= o.timeout {
		return o.limit("deadline", now)
	}
	if o.inExchange && now.Sub(o.exchangeStart) >= o.c.limits.Timeout {
		return o.limit("ack-deadline", now)
	}
	o.last = now
	return nil
}

func (c *Computer) begin(ctx context.Context, name string) (*operation, error) {
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
	return &operation{c: c, ctx: ctx, start: now, last: now, name: name, phase: "begin", timeout: c.limits.Timeout}, nil
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
	o.phase, o.offset = "before-read", offset
	if err := o.check(); err != nil {
		return 0, err
	}
	o.reads++
	v, err := o.c.io.Read32(offset)
	if err != nil {
		return 0, err
	}
	o.phase = "after-read"
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
	if err := o.check(); err != nil {
		return 0, err
	}
	o.exchangeStart, o.inExchange = o.last, true
	defer func() { o.inExchange = false }()
	o.opcode, o.index, o.polls = opcode, index, 0
	s, err := o.stable()
	if err != nil {
		return 0, err
	}
	if s.gpo != c.gpo || !acknowledged(s) {
		return 0, ErrProtocol
	}
	word := ((c.gpo ^ g.FesComputerRequestMask) & g.FesComputerRequestMask) |
		opcode<<24 | index<<16 | uint32(argument)
	o.phase, o.offset = "before-write", g.FesDe25ComputerGpoOffset
	if err := o.check(); err != nil {
		return 0, err
	}
	// Unlike a direct GP port, the Avalon adapter rejects writes that do not
	// toggle. There is exactly one write, including for an eventual timeout.
	o.writes++
	if err := c.io.Write32(g.FesDe25ComputerGpoOffset, word); err != nil {
		return 0, err
	}
	o.phase = "after-write"
	if err := o.check(); err != nil {
		return 0, err
	}
	for poll := uint32(0); poll < c.limits.MaxPolls; poll++ {
		o.polls = poll + 1
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
		o.phase = "ack-yield"
		c.limits.Yield()
		if err := o.check(); err != nil {
			return 0, err
		}
	}
	o.phase = "ack-polls"
	return 0, o.limit("ack-polls", o.last)
}

func (o *operation) mutate(opcode, index uint32, argument uint16) error {
	response, err := o.exchange(opcode, index, argument)
	if err == nil && response != 0 {
		return ErrProtocol
	}
	return err
}

// Activity reads one count/address observation without sending a GP command.
// The adapter latches the corresponding last-fetch address on the count read;
// ongoing CPU activity cannot tear that pair while the address is read later.
func (c *Computer) Activity(ctx context.Context) (activity FetchActivity, err error) {
	o, err := c.begin(ctx, "activity")
	if err != nil {
		return activity, err
	}
	defer func() { o.finish(err) }()
	check := func() error {
		s, err := o.stable()
		if err == nil && (s.gpo != c.gpo || !acknowledged(s)) {
			err = ErrProtocol
		}
		return err
	}
	if err = check(); err != nil {
		return activity, err
	}
	cycles, err := o.read(g.FesDe25ComputerFetchCountOffset)
	if err != nil {
		return activity, err
	}
	address, err := o.read(g.FesDe25ComputerFetchAddressOffset)
	if err != nil {
		return activity, err
	}
	if address > 0xffff {
		return activity, ErrProtocol
	}
	if err = check(); err != nil {
		return activity, err
	}
	return FetchActivity{Cycles: cycles, Address: uint16(address)}, nil
}

// Open verifies the adapter and the entire live GP identity before returning a
// command-capable handle. It does not reset or release execution. The caller
// must contain/reconfigure the platform on failure; Open must not be retried on
// the same uncertain epoch.
func Open(ctx context.Context, io IO, limits Limits, want Identity) (_ *Computer, err error) {
	return open(ctx, io, limits, want, nil)
}

// OpenWithInitialTape explicitly selects a trusted bounded platform transfer
// for initial media while held. Failure never falls back to per-CSR loading.
// Ordinary LoadTape retains its existing execution-preserving protocol.
func OpenWithInitialTape(ctx context.Context, io IO, limits Limits, want Identity, load InitialTapeLoader) (*Computer, error) {
	if load == nil || want.Minor != 0 {
		return nil, ErrArgument
	}
	return open(ctx, io, limits, want, load)
}

func open(ctx context.Context, io IO, limits Limits, want Identity, load InitialTapeLoader) (_ *Computer, err error) {
	if io == nil || limits.Now == nil || limits.Yield == nil || limits.Timeout <= 0 || limits.MaxPolls == 0 ||
		want.Minor > 1 || (want.Minor == 0 && want.Capabilities>>16 != 0) || want.Build == [16]byte{} ||
		want.Capabilities&g.FesComputerCapabilityVideoFixed720p60 == 0 {
		return nil, ErrArgument
	}
	c := &Computer{io: io, limits: limits, identity: want, initialTape: load}
	o, err := c.begin(ctx, "open")
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
	o, err := c.begin(ctx, "execution")
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
	o, err := c.begin(ctx, "keyboard")
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

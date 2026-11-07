// SPDX-License-Identifier: GPL-3.0-or-later
package scanout

import (
	"context"
	"errors"
	"fmt"
	"image"
	"math"
	"sync/atomic"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol"
	g "github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol/generated"
)

const (
	MenuControlABI  uint32 = 0x10203043
	MenuArenaBytes  uint64 = 8 << 20
	menuSlotBytes   uint64 = 4 << 20
	menuCommand     uint32 = 0x80
	menuExpectLo    uint32 = 0x84
	menuSequence    uint32 = 0x8c
	menuSlot        uint32 = 0x90
	menuStatus      uint32 = 0x94
	menuResultLo    uint32 = 0x98
	menuBase        uint32 = 0xb0
	menuReady       uint32 = 1
	menuRunning     uint32 = 2
	menuSubmitReady uint32 = 4
	menuStopped     uint32 = 8
	menuFault       uint32 = 16
	menuDisabled    uint32 = 32
)

// MenuHooks are trusted platform services for one already enabled read scope.
// Memory is an exclusive boot-pinned, uncached 8 MiB reservation. Owns checks
// the secure platform owner and mappings before/after every I/O and CPU copy;
// it must still work after this endpoint disables. No hook may release Memory.
// This driver supplies no boot, bridge, physical mapping or HDMI admission.
type MenuHooks struct {
	IO     embeddedcontrol.IO
	Memory Memory
	Owns   func() error
}

// MenuFrame observes a complete scanned frame within the still-owned scope.
// The old slot may then be updated by this presenter. It is no allocation
// release, all-master drain or physical HDMI/link receipt.
type MenuFrame struct {
	Generation                            uint64
	Sequence, Submitted, Slot, Underflows uint32
}

// MenuLimits bounds CPU copy/publication separately from endpoint I/O. The
// complete spare-slot copy and platform integrity check share one publication
// budget; neither that work nor the subsequent wait may reset its own deadline.
type MenuLimits struct {
	ReaderLimits
	PublicationTimeout time.Duration
}

func (l MenuLimits) Validate() error {
	if l.Now == nil || l.Yield == nil || l.Timeout <= 0 || l.Timeout > time.Second || l.MaxPolls == 0 || l.MaxPolls > 64 ||
		l.PublicationTimeout <= 0 || l.PublicationTimeout > 4*time.Second {
		return ErrReaderArgument
	}
	return nil
}

type MenuFailure struct {
	Cause            error
	Operation, Stage string
	CommandAttempted bool
	CopiedBytes      uint64
	Polls, Flags     uint32
	Last             MenuFrame
}

func (f *MenuFailure) Error() string {
	return fmt.Sprintf("%s at %s (flags=%x, last=%+v): %v", f.Operation, f.Stage, f.Flags, f.Last, f.Cause)
}
func (f *MenuFailure) Unwrap() error { return f.Cause }

// MenuPresenter copies only the spare slot and submits each sequence once.
// It never exposes the mapped arena or replays an uncertain operation. Keep
// the presenter and its platform allocation rooted until reboot on all paths.
type MenuPresenter struct {
	self                       *MenuPresenter
	hooks                      MenuHooks
	want                       embeddedcontrol.Identity
	generation                 uint64
	limits                     MenuLimits
	context                    context.Context
	surfaces                   [2]*Surface
	operation                  *menuOperation
	active                     atomic.Bool
	retired, started           bool
	sequence, slot, underflows uint32
}

type menuOperation struct {
	m           *MenuPresenter
	ctx         context.Context
	start, last time.Time
	timeout     time.Duration
	failure     MenuFailure
}
type menuMemory struct {
	m    *MenuPresenter
	slot uint32
}

func (v menuMemory) Size() uint64 { return menuSlotBytes }
func (v menuMemory) Write(offset uint64, data []byte) error {
	o := v.m.operation
	if o == nil || offset > menuSlotBytes || uint64(len(data)) > menuSlotBytes-offset {
		return ErrReaderArgument
	}
	if err := o.check(); err != nil {
		return err
	}
	if err := v.m.hooks.Memory.Write(uint64(v.slot)*menuSlotBytes+offset, data); err != nil {
		return err
	}
	o.failure.CopiedBytes += uint64(len(data))
	return o.check()
}
func (v menuMemory) Publish() error {
	o := v.m.operation
	if o == nil {
		return ErrReaderArgument
	}
	if err := o.check(); err != nil {
		return err
	}
	o.failure.Stage = "publish"
	if err := v.m.hooks.Memory.Publish(); err != nil {
		return err
	}
	return o.check()
}

// NewMenuPresenter validates the compiled bus base, geometry and live reader.
// The platform has already enabled this generation and pinned both slots.
// Its reservation remains owned even when construction fails.
func NewMenuPresenter(ctx context.Context, hooks MenuHooks, want embeddedcontrol.Identity, generation uint64, base uint32, limits MenuLimits) (*MenuPresenter, error) {
	if ctx == nil || hooks.IO == nil || hooks.Memory == nil || hooks.Owns == nil ||
		hooks.Memory.Size() != MenuArenaBytes || want.ABI != MenuControlABI || want.Build == ([4]uint32{}) || generation == 0 ||
		base == 0 || base&((2<<20)-1) != 0 || uint64(base)+MenuArenaBytes > 1<<32 ||
		limits.Validate() != nil {
		return nil, ErrReaderArgument
	}
	m := &MenuPresenter{hooks: hooks, want: want, generation: generation, limits: limits, context: ctx}
	m.self = m
	for slot := range m.surfaces {
		surface, err := New(1280, 720, 5120, menuMemory{m, uint32(slot)})
		if err != nil {
			return nil, err
		}
		m.surfaces[slot] = surface
	}
	o, err := m.begin(ctx, "menu-open")
	if err != nil {
		return nil, err
	}
	defer func() { m.operation = nil; m.active.Store(false) }()
	if err = o.owner(); err != nil {
		return nil, o.failed(err)
	}
	o.failure.Stage = "geometry"
	for i, w := range []uint32{base, uint32(MenuArenaBytes), 5120, 1280, 720, 0x31534d46, 1} {
		v, e := o.Read32(menuBase + uint32(i)*4)
		if e != nil {
			return nil, o.failed(e)
		}
		if v != w {
			return nil, o.failed(ErrReaderIdentity)
		}
	}
	s, flags, err := o.snapshot()
	if err != nil {
		return nil, o.failed(err)
	}
	if flags&menuReady == 0 || flags&(menuRunning|menuFault|menuDisabled) != 0 || s.Generation >= generation || s.Submitted == math.MaxUint32 {
		return nil, o.failed(ErrReaderProtocol)
	}
	if err = o.owner(); err != nil {
		return nil, o.failed(err)
	}
	m.sequence = s.Submitted
	m.underflows = s.Underflows
	return m, nil
}
func (m *MenuPresenter) begin(ctx context.Context, name string) (*menuOperation, error) {
	if m == nil || m.self != m || ctx == nil {
		return nil, ErrReaderArgument
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !m.active.CompareAndSwap(false, true) {
		return nil, ErrReaderBusy
	}
	if m.retired {
		m.active.Store(false)
		return nil, ErrReaderRetired
	}
	now := m.limits.Now()
	o := &menuOperation{m: m, ctx: ctx, start: now, last: now, timeout: m.limits.Timeout, failure: MenuFailure{Operation: name}}
	m.operation = o
	return o, nil
}
func (o *menuOperation) failed(err error) error {
	o.m.retired = true
	o.failure.Cause = err
	f := o.failure
	return &f
}
func (o *menuOperation) check() error {
	if err := o.ctx.Err(); err != nil {
		return err
	}
	if err := o.m.context.Err(); err != nil {
		return err
	}
	now := o.m.limits.Now()
	if now.Before(o.last) || now.Sub(o.start) >= o.timeout {
		return ErrReaderLimit
	}
	o.last = now
	if o.m.hooks.Memory.Size() != MenuArenaBytes {
		return ErrReaderIdentity
	}
	if err := o.m.hooks.Owns(); err != nil {
		return err
	}
	// A platform callback can cancel the operation or take time itself.
	if err := o.ctx.Err(); err != nil {
		return err
	}
	if err := o.m.context.Err(); err != nil {
		return err
	}
	now = o.m.limits.Now()
	if now.Before(o.last) || now.Sub(o.start) >= o.timeout {
		return ErrReaderLimit
	}
	o.last = now
	return nil
}

// Change phase only after the old deadline, ownership and cancellation checks
// pass. Keep the last clock sample so a phase boundary cannot hide regression.
func (o *menuOperation) phase(timeout time.Duration) error {
	if err := o.check(); err != nil {
		return err
	}
	o.start, o.timeout = o.last, timeout
	return nil
}
func (o *menuOperation) Read32(offset uint32) (uint32, error) {
	if err := o.check(); err != nil {
		return 0, err
	}
	v, err := o.m.hooks.IO.Read32(offset)
	if err != nil {
		return 0, err
	}
	return v, o.check()
}
func (o *menuOperation) Write32(offset, value uint32) error {
	if err := o.check(); err != nil {
		return err
	}
	if err := o.m.hooks.IO.Write32(offset, value); err != nil {
		return err
	}
	return o.check()
}
func (o *menuOperation) owner() error {
	o.failure.Stage = "owner"
	values := []uint32{g.FesEmbeddedMagic, g.FesEmbeddedVersion, o.m.want.ABI}
	values = append(values, o.m.want.Build[:]...)
	for i, w := range values {
		v, err := o.Read32(uint32(i) * 4)
		if err != nil {
			return err
		}
		if v != w {
			return ErrReaderIdentity
		}
	}
	// Pending traffic normally changes while scanning. Verify stable ownership,
	// not a whole-endpoint double snapshot which would reject normal progress.
	for i := 0; i < 2; i++ {
		lo, err := o.Read32(g.FesEmbeddedRegGenerationLo)
		if err != nil {
			return err
		}
		hi, err := o.Read32(g.FesEmbeddedRegGenerationHi)
		if err != nil {
			return err
		}
		flags, err := o.Read32(g.FesEmbeddedRegStatus)
		if err != nil {
			return err
		}
		if uint64(hi)<<32|uint64(lo) != o.m.generation {
			return ErrReaderIdentity
		}
		if flags != g.FesEmbeddedStatusEnabled {
			return ErrReaderProtocol
		}
	}
	return nil
}
func (o *menuOperation) snapshot() (s MenuFrame, flags uint32, err error) {
	o.failure.Stage = "snapshot"
	flags, err = o.Read32(menuStatus)
	o.failure.Flags = flags
	if err != nil {
		return
	}
	var values [6]uint32
	for i := range values {
		values[i], err = o.Read32(menuResultLo + uint32(i)*4)
		if err != nil {
			return
		}
	}
	s = MenuFrame{Generation: uint64(values[1])<<32 | uint64(values[0]), Sequence: values[3], Submitted: values[2], Slot: values[5], Underflows: values[4]}
	o.failure.Last = s
	if flags&^uint32(63) != 0 || flags&(menuFault|menuDisabled) != 0 || values[5] > 1 || values[3] > values[2] ||
		flags&menuReady != 0 && flags&menuRunning != 0 || flags&menuSubmitReady != 0 && flags&menuRunning == 0 ||
		flags&menuStopped != 0 && flags&menuRunning != 0 {
		err = ErrReaderProtocol
	}
	return
}
func (o *menuOperation) command(command, sequence, slot uint32) error {
	o.failure.Stage = "prepare"
	for _, w := range [][2]uint32{{menuExpectLo, uint32(o.m.generation)}, {menuExpectLo + 4, uint32(o.m.generation >> 32)},
		{menuSequence, sequence}, {menuSlot, slot}, {menuCommand, command}} {
		if w[0] == menuCommand {
			o.failure.CommandAttempted = true
			o.failure.Stage = "command"
		}
		if err := o.Write32(w[0], w[1]); err != nil {
			return err
		}
	}
	return nil
}
func (o *menuOperation) poll() error {
	if o.failure.Polls >= o.m.limits.MaxPolls {
		return ErrReaderLimit
	}
	o.failure.Polls++
	return o.check()
}
func (o *menuOperation) submitReady() error {
	for {
		if err := o.poll(); err != nil {
			return err
		}
		if err := o.owner(); err != nil {
			return err
		}
		s, flags, err := o.snapshot()
		if err != nil {
			return err
		}
		m := o.m
		if s.Generation != m.generation || s.Submitted != m.sequence || s.Sequence != m.sequence ||
			s.Slot != m.slot^1 || flags&menuRunning == 0 || flags&menuStopped != 0 {
			return ErrReaderProtocol
		}
		if flags&menuSubmitReady != 0 {
			return nil
		}
		m.limits.Yield()
	}
}
func (m *MenuPresenter) Present(frame *image.RGBA) error {
	if m == nil {
		return ErrReaderArgument
	}
	_, err := m.PresentFrame(m.context, frame)
	return err
}
func (m *MenuPresenter) PresentFrame(ctx context.Context, frame *image.RGBA) (result MenuFrame, err error) {
	o, err := m.begin(ctx, "menu-frame")
	if err != nil {
		return result, err
	}
	defer func() { m.operation = nil; m.active.Store(false) }()
	// Invalid application frames reject without consuming an otherwise usable owner.
	if err = m.surfaces[m.slot].CheckFrame(frame); err != nil {
		return result, err
	}
	defer func() {
		if err != nil {
			result = MenuFrame{}
			err = o.failed(err)
		}
	}()
	if m.sequence == math.MaxUint32 {
		return result, ErrReaderLimit
	}
	if err = o.owner(); err != nil {
		return result, err
	}
	if err = o.phase(m.limits.PublicationTimeout); err != nil {
		return result, err
	}
	o.failure.Stage = "copy"
	if err = m.surfaces[m.slot].Present(frame); err != nil {
		return result, err
	}
	if err = o.phase(m.limits.Timeout); err != nil {
		return result, err
	}
	if err = o.owner(); err != nil {
		return result, err
	}
	sequence := m.sequence + 1
	command := uint32(2)
	if !m.started {
		command = 1
	}
	if m.started {
		if err = o.submitReady(); err != nil {
			return result, err
		}
	}
	if err = o.command(command, sequence, m.slot); err != nil {
		return result, err
	}
	for poll := uint32(0); poll < m.limits.MaxPolls; poll++ {
		if err = o.poll(); err != nil {
			return result, err
		}
		if err = o.owner(); err != nil {
			return result, err
		}
		var flags uint32
		if result, flags, err = o.snapshot(); err != nil {
			return MenuFrame{}, err
		}
		if result.Generation != m.generation || result.Underflows < m.underflows || flags&menuRunning == 0 || flags&menuStopped != 0 ||
			result.Submitted != sequence || result.Sequence > sequence || result.Slot != m.slot {
			return MenuFrame{}, ErrReaderProtocol
		}
		if result.Sequence == sequence {

			if err = o.owner(); err != nil {
				return MenuFrame{}, err
			}
			m.started = true
			m.sequence = sequence
			m.slot ^= 1
			m.underflows = result.Underflows
			return result, nil
		}
		m.limits.Yield()
	}
	return MenuFrame{}, ErrReaderLimit
}

// Stop quiesces this menu once, then disables/drains its exact read generation.
// The caller must still invoke the platform's all-master stop and bridge closure.
// Both slots stay pinned. No further Present or Stop is allowed after an attempt.
func (m *MenuPresenter) Stop(ctx context.Context, cookie uint64) (result ReaderDrain, err error) {
	if cookie == 0 {
		return result, ErrReaderArgument
	}
	o, err := m.begin(ctx, "menu-stop")
	if err != nil {
		return result, err
	}
	defer func() {
		m.retired = true
		m.operation = nil
		m.active.Store(false)
		if err != nil {
			result = ReaderDrain{}
			err = o.failed(err)
		}
	}()
	if err = o.owner(); err != nil {
		return result, err
	}
	if m.started {
		if err = o.command(3, m.sequence, m.slot); err != nil {
			return result, err
		}
		stopped := false
		for poll := uint32(0); poll < m.limits.MaxPolls; poll++ {
			if err = o.poll(); err != nil {
				return result, err
			}
			if err = o.owner(); err != nil {
				return result, err
			}
			var s MenuFrame
			var flags uint32
			if s, flags, err = o.snapshot(); err != nil {
				return result, err
			}
			if s.Generation != m.generation || s.Sequence != m.sequence || s.Submitted != m.sequence {
				return result, ErrReaderProtocol
			}
			if flags&menuStopped != 0 && flags&menuRunning == 0 {
				stopped = true
				break
			}
			m.limits.Yield()
		}
		if !stopped {
			return result, ErrReaderLimit
		}
	} else {
		s, flags, e := o.snapshot()
		if e != nil {
			return result, e
		}
		if flags&menuReady == 0 || flags&menuRunning != 0 || s.Generation >= m.generation || s.Submitted != m.sequence {
			return result, ErrReaderProtocol
		}
	}
	control := embeddedcontrol.Control{IO: o}
	o.failure.Stage = "disable"
	if err = control.Disable(m.want, m.generation, cookie); err != nil {
		return result, err
	}
	o.failure.Stage = "drain"
	for poll := uint32(0); poll < m.limits.MaxPolls; poll++ {
		if err = o.poll(); err != nil {
			return result, err
		}
		err = control.Drained(m.want, m.generation, cookie)
		if err == nil {
			return ReaderDrain{m.generation, cookie}, nil
		}
		if !errors.Is(err, embeddedcontrol.ErrBusy) {
			return result, err
		}
		m.limits.Yield()
	}
	return result, ErrReaderLimit
}

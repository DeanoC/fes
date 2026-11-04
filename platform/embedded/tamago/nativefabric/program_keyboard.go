// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"context"
	"errors"

	"github.com/DeanoC/fes/platform/embedded/tamago/input"
	"github.com/DeanoC/fes/platform/embedded/tamago/usb"
)

// ProgramKeyboard is an opaque local input binding. Copies share its private
// decoder and closed state. Capturing a public service generation cannot create
// this permission. Keep the backend/service and failed platform resources rooted.
// This adapter opens no USB controller and advertises no physical input device.
type ProgramKeyboard struct{ state *programKeyboard }
type programKeyboard struct {
	backend       *ProgramBackend
	session       ProgramSession
	decoder       usb.BootKeyboard
	rows          [9]uint16
	armed, closed bool
}

// BindKeyboard transfers exclusive local keyboard ownership for the current
// play. Initial neutralization must complete before the binding is returned.
// The caller supplies the generation of its displayed play, not a filesystem
// path or a fabricated ProgramSession. A replacement play needs a new binding.
func (b *ProgramBackend) BindKeyboard(ctx context.Context, generation uint64) (ProgramKeyboard, error) {
	if b == nil || ctx == nil {
		return ProgramKeyboard{}, ErrProgramSession
	}
	if err := ctx.Err(); err != nil {
		return ProgramKeyboard{}, err
	}
	if !b.operation.TryLock() {
		return ProgramKeyboard{}, ErrProgramBusy
	}
	defer b.operation.Unlock()
	return b.bindKeyboard(ctx, generation)
}

// Caller holds backend.operation across binding and input routing.
func (b *ProgramBackend) bindKeyboard(ctx context.Context, generation uint64) (ProgramKeyboard, error) {
	v := b.service.Status()
	if v.State != ProgramRunning || v.Generation != generation || b.session.Generation() != generation || b.play.Generation != generation {
		return ProgramKeyboard{}, ErrProgramSession
	}
	if b.keyboard != nil && !b.keyboard.closed && b.keyboard.session == b.session {
		return ProgramKeyboard{}, ErrProgramBusy
	}
	if err := b.service.Keyboard(ctx, b.session, [9]uint16{}); err != nil {
		b.publish()
		return ProgramKeyboard{}, err
	}
	k := &programKeyboard{backend: b, session: b.session}
	b.keyboard = k
	return ProgramKeyboard{k}, nil
}

func (k ProgramKeyboard) begin(ctx context.Context) (*programKeyboard, error) {
	if k.state == nil || ctx == nil {
		return nil, ErrProgramSession
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p := k.state
	b := p.backend
	if !b.operation.TryLock() {
		return nil, ErrProgramBusy
	}
	v := b.service.Status()
	if p.closed || b.keyboard != p || v.State != ProgramRunning || v.Generation != p.session.Generation() || b.session != p.session {
		b.operation.Unlock()
		return nil, ErrProgramSession
	}
	return p, nil
}

// Report consumes one real boot-protocol snapshot. Malformed/rollover reports
// preserve the previous valid state. Menu keys held across a launch cannot leak
// into the core: each fresh binding waits for a complete all-up report first.
// The ABI represents usages04..7f and modifiersE0..E7; other valid USB usages
// have no representation in this interface and do not invent a key mapping.
func (k ProgramKeyboard) Report(ctx context.Context, report []byte) error {
	p, err := k.begin(ctx)
	if err != nil {
		return err
	}
	defer p.backend.operation.Unlock()
	return p.report(ctx, report)
}

func (p *programKeyboard) report(ctx context.Context, report []byte) error {
	next := p.decoder
	events, err := next.Feed(report)
	if err != nil {
		return err
	}
	if !p.armed {
		up := report[0] == 0
		for _, usage := range report[2:] {
			up = up && usage == 0
		}
		p.decoder = next
		p.armed = up
		return nil
	}
	rows := p.rows
	for _, event := range events {
		usage := event.Usage
		row, bit := int(usage)/16, uint(usage%16)
		if usage >= 0xe0 && usage <= 0xe7 {
			row, bit = 8, uint(usage-0xe0)
		} else if usage < 4 || usage > 0x7f {
			continue
		}
		if event.Pressed {
			rows[row] |= 1 << bit
		} else {
			rows[row] &^= 1 << bit
		}
	}
	if rows != p.rows {
		if err := p.backend.service.Keyboard(ctx, p.session, rows); err != nil {
			if p.backend.service.Status().State == ProgramRebootRequired || (!errors.Is(err, ErrProgramBusy) && !errors.Is(err, context.Canceled)) {
				p.closed = true
			}
			p.backend.publish()
			return err
		}
	}
	p.decoder, p.rows = next, rows
	return nil
}

// Poll consumes at most eight snapshots from the board driver's report
// interface. Nil means no report, not unplug or all keys released. Source
// failures preserve the last key state; the platform owns recovery policy.
func (k ProgramKeyboard) Poll(ctx context.Context, source input.USBReports) error {
	if source == nil {
		return ErrProgramSession
	}
	p, err := k.begin(ctx)
	if err != nil {
		return err
	}
	defer p.backend.operation.Unlock()
	for n := 0; n < 8; n++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		report, err := source.PollKeyboard()
		if err != nil || report == nil {
			return err
		}
		if err := p.report(ctx, report); err != nil && !errors.Is(err, usb.ErrKeyboardRollover) {
			return err
		}
	}
	return nil
}

// Disconnect is for a confirmed unplug, not a timeout or missing report. It
// neutralizes this play once. A stale binding cannot neutralize a later core.
// On an uncertain write the service is retired; this handle never retries it.
func (k ProgramKeyboard) Disconnect(ctx context.Context) error {
	p, err := k.begin(ctx)
	if err != nil {
		return err
	}
	defer p.backend.operation.Unlock()
	if err := p.backend.service.Keyboard(ctx, p.session, [9]uint16{}); err != nil {
		if p.backend.service.Status().State == ProgramRebootRequired || (!errors.Is(err, ErrProgramBusy) && !errors.Is(err, context.Canceled)) {
			p.closed = true
		}
		p.backend.publish()
		return err
	}
	p.closed = true
	return nil
}

// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"context"

	"github.com/DeanoC/fes/platform/embedded/tamago/input"
	"github.com/DeanoC/fes/platform/embedded/tamago/usb"
)

// ProgramGamepadKeys explicitly maps directions/buttons to the sealed
// computer's HID key matrix. Zero means unmapped. This value is copied at bind.
// The report descriptor determines raw controls; this profile assigns keys.
type ProgramGamepadKeys struct {
	Up, Down, Left, Right uint8
	Buttons               [32]uint8
}

func (m ProgramGamepadKeys) valid() bool {
	any := false
	for _, usage := range append([]uint8{m.Up, m.Down, m.Left, m.Right}, m.Buttons[:]...) {
		if usage == 0 {
			continue
		}
		if usage < 4 || usage > 0x7f && (usage < 0xe0 || usage > 0xe7) {
			return false
		}
		any = true
	}
	return any
}
func (m ProgramGamepadKeys) rows(state usb.GamepadState) [9]uint16 {
	var rows [9]uint16
	put := func(usage uint8) {
		if usage == 0 {
			return
		}
		if usage >= 0xe0 {
			rows[8] |= 1 << uint(usage-0xe0)
		} else {
			rows[usage/16] |= 1 << uint(usage%16)
		}
	}
	directions := input.GamepadDirections(state)
	for n, usage := range [4]uint8{m.Up, m.Down, m.Left, m.Right} {
		if directions[n] {
			put(usage)
		}
	}
	for n, usage := range m.Buttons {
		if state.Buttons&(uint32(1)<<n) != 0 {
			put(usage)
		}
	}
	return rows
}

// ProgramGamepad is an opaque binding to one exact play. Copies share its
// decoder, release fence and permission. It accepts HID-described pad reports;
// mapped rows are delivered directly through the owned computer service.
type ProgramGamepad struct{ state *programGamepad }
type programGamepad struct {
	keys    ProgramKeyboard
	decoder *usb.Gamepad
	mapping ProgramGamepadKeys
	armed   bool
}

func (b *ProgramBackend) BindGamepad(ctx context.Context, generation uint64, descriptor []byte, mapping ProgramGamepadKeys) (ProgramGamepad, error) {
	if b == nil || ctx == nil {
		return ProgramGamepad{}, ErrProgramSession
	}
	if err := ctx.Err(); err != nil {
		return ProgramGamepad{}, err
	}
	if !b.operation.TryLock() {
		return ProgramGamepad{}, ErrProgramBusy
	}
	defer b.operation.Unlock()
	return b.bindGamepad(ctx, generation, descriptor, mapping)
}
func (b *ProgramBackend) bindGamepad(ctx context.Context, generation uint64, descriptor []byte, mapping ProgramGamepadKeys) (ProgramGamepad, error) {
	if !mapping.valid() {
		return ProgramGamepad{}, ErrProgramSession
	}
	decoder, err := usb.ParseGamepad(descriptor)
	if err != nil {
		return ProgramGamepad{}, err
	}
	keys, err := b.bindKeyboard(ctx, generation)
	if err != nil {
		return ProgramGamepad{}, err
	}
	return ProgramGamepad{&programGamepad{keys: keys, decoder: decoder, mapping: mapping}}, nil
}
func (g ProgramGamepad) Report(ctx context.Context, report []byte) error {
	if g.state == nil {
		return ErrProgramSession
	}
	k, err := g.state.keys.begin(ctx)
	if err != nil {
		return err
	}
	defer k.backend.operation.Unlock()
	return g.state.report(ctx, report)
}
func (p *programGamepad) report(ctx context.Context, report []byte) error {
	// The layout is immutable; Feed changes only the value state. Copying the
	// decoder lets busy/canceled writes preserve the previous complete state.
	next := *p.decoder
	state, _, err := next.Feed(report)
	if err != nil {
		return err
	}
	if !p.armed {
		p.armed = input.GamepadNeutral(state)
		*p.decoder = next
		return nil
	}
	if err := p.keys.state.writeRows(ctx, p.mapping.rows(state)); err != nil {
		return err
	}
	*p.decoder = next
	return nil
}
func (g ProgramGamepad) Disconnect(ctx context.Context) error {
	if g.state == nil {
		return ErrProgramSession
	}
	return g.state.keys.Disconnect(ctx)
}

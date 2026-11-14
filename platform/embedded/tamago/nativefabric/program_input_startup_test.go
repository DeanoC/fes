// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"context"
	"testing"

	"github.com/DeanoC/FogCast/ui/tenfoot"
	"github.com/DeanoC/fes/platform/embedded/tamago/input"
)

func TestProgramInputStartupRequiresNeutralThenFreshPress(t *testing.T) {
	for _, gamepad := range []bool{false, true} {
		t.Run(map[bool]string{false: "keyboard", true: "gamepad"}[gamepad], func(t *testing.T) {
			f, b, _ := backendFixture(t)
			var p *ProgramInputPlatform
			var err error
			var set func([][]byte)
			var held, neutral []byte
			if gamepad {
				source := &programPadReports{}
				p, err = NewProgramGamepadPlatform(&programInputBase{}, b, source, gamepadDescriptor(t), input.GamepadButtons{Confirm: 1, Back: 2}, gamepadKeys())
				set = func(reports [][]byte) { source.reports = reports }
				held, neutral = padReport(128, 128, 1), padReport(128, 128, 0)
			} else {
				source := &programKeyboardReports{}
				p, err = NewProgramInputPlatform(&programInputBase{}, b, source)
				set = func(reports [][]byte) { source.reports = reports }
				held, neutral = []byte{0, 0, 40, 0, 0, 0, 0, 0}, make([]byte, 8)
			}
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			// A held first snapshot, repeated held snapshots and NAKs cannot create
			// a command or buffered action during slow menu presentation.
			for _, reports := range [][][]byte{{held}, nil, {held}, {held}} {
				set(reports)
				if err := p.serviceMenuInput(ctx); err != nil {
					t.Fatal(err)
				}
				frame, err := p.Next(ctx)
				if err != nil || len(frame.Commands) != 0 || frame.Text != "" || f.cpu.calls != 0 {
					t.Fatal("startup held control or missing report became input", frame, err, f.cpu.calls)
				}
			}
			if !gamepad {
				// Rollover and held modifiers are not a valid all-up observation.
				for _, report := range [][]byte{{0, 0, 1, 1, 1, 1, 1, 1}, {2, 0, 0, 0, 0, 0, 0, 0}, held} {
					set([][]byte{report})
					frame, err := p.Next(ctx)
					if err != nil || len(frame.Commands) != 0 || frame.Text != "" {
						t.Fatal(frame, err)
					}
				}
			}
			set([][]byte{neutral, held})
			frame, err := p.Next(ctx)
			if err != nil || len(frame.Commands) != 1 || frame.Commands[0] != tenfoot.CmdSelect || f.cpu.calls != 0 {
				t.Fatal("first fresh press after neutral lost", frame, err, f.cpu.calls)
			}
			set([][]byte{held, held})
			if frame, err = p.Next(ctx); err != nil || len(frame.Commands) != 0 {
				t.Fatal("held control repeated", frame, err)
			}
		})
	}
}

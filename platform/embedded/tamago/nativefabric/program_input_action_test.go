// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/ui/tenfoot"
	"github.com/DeanoC/fes/platform/embedded/tamago/input"
)

func TestProgramInputActionDistinguishesStartupSources(t *testing.T) {
	for _, tc := range []struct {
		name, uart, source string
		report             []byte
		buffer             bool
	}{
		{name: "idle", report: make([]byte, 8)},
		{name: "serial-return", uart: "\r", report: make([]byte, 8), source: "uart"},
		{name: "first-usb-return", report: []byte{0, 0, 40, 0, 0, 0, 0, 0}, source: "usb-boot-keyboard"},
		{name: "buffered-usb-return", report: []byte{0, 0, 40, 0, 0, 0, 0, 0}, source: "usb-boot-keyboard", buffer: true},
		{name: "text", uart: "abxyz", report: []byte{0, 0, 4, 0, 0, 0, 0, 0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, b, _ := backendFixture(t)
			queue := []byte(tc.uart)
			var output strings.Builder
			base, err := NewProgramSerialPlatform(f.s, func() (byte, bool) {
				if len(queue) == 0 {
					return 0, false
				}
				v := queue[0]
				queue = queue[1:]
				return v, true
			}, func(_ context.Context, raw []byte) error { _, err := output.Write(raw); return err })
			if err != nil {
				t.Fatal(err)
			}
			source := &programKeyboardReports{reports: [][]byte{tc.report}}
			p, err := NewProgramInputPlatform(base, b, source)
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			if tc.buffer {
				if err := p.serviceMenuInput(ctx); err != nil || output.Len() != 0 {
					t.Fatal("buffered action reported before delivery", err, output.String())
				}
			}
			frame, err := p.Next(ctx)
			if err != nil {
				t.Fatal(err)
			}
			expected := ""
			if tc.source != "" {
				expected = fmt.Sprintf("FES_NATIVE_INPUT_ACTION source=%s action=select state=ready owner=%x native_generation=0\n", tc.source, f.s.Status().Owner)
				if len(frame.Commands) != 1 || frame.Commands[0] != tenfoot.CmdSelect {
					t.Fatal("observation changed decoded command", frame)
				}
			}
			if output.String() != expected || f.cpu.calls != 0 {
				t.Fatal("wrong source, leaked text or diagnostic touched firmware", output.String(), f.cpu.calls)
			}
			if _, err = p.Next(ctx); err != nil || output.String() != expected {
				t.Fatal("action was replayed without a fresh press", err, output.String())
			}
		})
	}
}

func TestProgramInputActionGamepadAndEmissionFailure(t *testing.T) {
	f, b, _ := backendFixture(t)
	var output strings.Builder
	emitErr := errors.New("diagnostic output lost")
	var fail bool
	base, _ := NewProgramSerialPlatform(f.s, func() (byte, bool) { return 0, false }, func(_ context.Context, raw []byte) error {
		if fail {
			return emitErr
		}
		_, err := output.Write(raw)
		return err
	})
	source := &programPadReports{reports: [][]byte{padReport(128, 128, 1)}}
	p, err := NewProgramGamepadPlatform(base, b, source, gamepadDescriptor(t), input.GamepadButtons{Confirm: 1, Back: 2}, gamepadKeys())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	frame, err := p.Next(ctx)
	if err != nil || len(frame.Commands) != 1 || frame.Commands[0] != tenfoot.CmdSelect || !strings.HasPrefix(output.String(), "FES_NATIVE_INPUT_ACTION source=usb-gamepad action=select ") {
		t.Fatal(frame, err, output.String())
	}
	fail = true
	source.reports = [][]byte{padReport(128, 128, 0), padReport(128, 128, 1)}
	frame, err = p.Next(ctx)
	if !errors.Is(err, emitErr) || len(frame.Commands) != 0 {
		t.Fatal("failed output delivered an action", frame, err)
	}
	polls := source.polls
	if _, err = p.Next(ctx); !errors.Is(err, emitErr) || source.polls != polls {
		t.Fatal("failed action observation retried input", err)
	}
}

func TestProgramSerialActionQuitWaitsAndNoOutputAfterCancellation(t *testing.T) {
	f, _, c := backendFixture(t)
	ctx := context.Background()
	if _, err := c.Launch(ctx, "native-0"); err != nil {
		t.Fatal(err)
	}
	queue := []byte("q")
	var output strings.Builder
	base, _ := NewProgramSerialPlatform(f.s, func() (byte, bool) {
		if len(queue) == 0 {
			return 0, false
		}
		v := queue[0]
		queue = queue[1:]
		return v, true
	}, func(_ context.Context, raw []byte) error { _, err := output.Write(raw); return err })
	frame, err := base.Next(ctx)
	if err != nil || len(frame.Commands) != 1 || frame.Commands[0] != tenfoot.CmdStop || !strings.Contains(output.String(), "source=uart action=stop state=running ") {
		t.Fatal("quit observation bypassed owned Stop", frame, err, output.String())
	}
	before := output.String()
	if _, err = base.Next(ctx); err != nil || output.String() != before {
		t.Fatal("queued Stop observation was repeated", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = base.Next(cancelled); !errors.Is(err, context.Canceled) || output.String() != before {
		t.Fatal("cancelled input emitted a command", err)
	}
}

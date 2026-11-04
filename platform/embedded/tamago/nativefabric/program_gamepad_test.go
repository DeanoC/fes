// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/DeanoC/FogCast/hostclient"
)

func gamepadDescriptor(t *testing.T) []byte {
	t.Helper()
	d, err := os.ReadFile("../../../../tests/fixtures/nano-usb-gamepad/joystick-report.bin")
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func gamepadKeys() ProgramGamepadKeys {
	m := ProgramGamepadKeys{Up: 0x52, Down: 0x51, Left: 0x50, Right: 0x4f}
	m.Buttons[0], m.Buttons[1], m.Buttons[2] = 0x16, 0xe1, 0x16
	return m
}

// Constructed inputs use the captured schema. They are not physical presses.
func padReport(x, y byte, buttons uint16) []byte {
	return []byte{x, y, 0, 128, 128, 15 | byte(buttons&15)<<4, byte(buttons>>4) & 63, 0}
}
func TestProgramGamepadMappedRowsAreAtomicAndCopiesShareReleaseFence(t *testing.T) {
	f, b, c := backendFixture(t)
	ctx := context.Background()
	play, err := c.Launch(ctx, "native-0")
	if err != nil {
		t.Fatal(err)
	}
	mapping := gamepadKeys()
	g, err := b.BindGamepad(ctx, play.CorePackage.Generation, gamepadDescriptor(t), mapping)
	if err != nil {
		t.Fatal(err)
	}
	mapping.Buttons[0] = 4 // The caller cannot change an already bound profile.
	copy := g
	for _, r := range [][]byte{padReport(128, 128, 1), padReport(128, 128, 0)} {
		if err := copy.Report(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	if f.cpu.keyboardWrites != 9 {
		t.Fatal("held launch button leaked")
	}
	if err := g.Report(ctx, padReport(255, 0, 7)); err != nil {
		t.Fatal(err)
	}
	want := [9]uint16{1: 1 << 6, 4: 1 << 15, 5: 1 << 2, 8: 2}
	if f.cpu.keyboardRows != want || f.cpu.keyboardWrites != 18 {
		t.Fatal(f.cpu.keyboardRows, f.cpu.keyboardWrites)
	}
	if err := g.Report(ctx, []byte{0}); err == nil || f.cpu.keyboardRows != want || f.cpu.keyboardWrites != 18 {
		t.Fatal("bad report changed rows", err)
	}
	if err := copy.Report(ctx, padReport(128, 128, 4)); err != nil {
		t.Fatal(err)
	}
	writes := f.cpu.keyboardWrites
	if err := g.Report(ctx, padReport(128, 128, 1)); err != nil || f.cpu.keyboardWrites != writes || f.cpu.keyboardRows[1] != 1<<6 {
		t.Fatal("duplicate key mapping lost held key", err)
	}
	if _, err := b.BindKeyboard(ctx, play.CorePackage.Generation); !errors.Is(err, ErrProgramBusy) {
		t.Fatal("second input owner accepted", err)
	}
	if err := copy.Disconnect(ctx); err != nil || f.cpu.keyboardRows != ([9]uint16{}) {
		t.Fatal(err)
	}
	calls := f.cpu.calls
	if err := g.Report(ctx, padReport(128, 128, 1)); !errors.Is(err, ErrProgramSession) || f.cpu.calls != calls {
		t.Fatal("copied binding not revoked", err)
	}
}
func TestProgramGamepadStaleGenerationCannotWriteOrDisconnectReplacement(t *testing.T) {
	f, b, c := backendFixture(t)
	ctx := context.Background()
	d := gamepadDescriptor(t)
	play, err := c.Launch(ctx, "native-0")
	if err != nil {
		t.Fatal(err)
	}
	old, err := b.BindGamepad(ctx, play.CorePackage.Generation, d, gamepadKeys())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.StopExpectedStamped(ctx, play, hostclient.ClientStamp{}); err != nil {
		t.Fatal(err)
	}
	play, err = c.Launch(ctx, "native-1")
	if err != nil {
		t.Fatal(err)
	}
	current, err := b.BindGamepad(ctx, play.CorePackage.Generation, d, gamepadKeys())
	if err != nil {
		t.Fatal(err)
	}
	calls := f.cpu.calls
	for _, op := range []func() error{func() error { return old.Report(ctx, padReport(128, 128, 1)) }, func() error { return old.Disconnect(ctx) }} {
		if err := op(); !errors.Is(err, ErrProgramSession) || f.cpu.calls != calls {
			t.Fatal("stale binding changed core", err)
		}
	}
	if err := current.Report(ctx, padReport(128, 128, 1)); err != nil || f.cpu.keyboardRows != ([9]uint16{}) {
		t.Fatal("new play missed release fence", err)
	}
	if err := current.Report(ctx, padReport(128, 128, 0)); err != nil {
		t.Fatal(err)
	}
	if err := current.Report(ctx, padReport(128, 128, 1)); err != nil || f.cpu.keyboardRows[1] != 1<<6 {
		t.Fatal(err)
	}
}
func TestProgramGamepadValidationBusyCancellationAndPartialWriteFailure(t *testing.T) {
	f, b, c := backendFixture(t)
	ctx := context.Background()
	d := gamepadDescriptor(t)
	play, err := c.Launch(ctx, "native-0")
	if err != nil {
		t.Fatal(err)
	}
	calls := f.cpu.calls
	for _, m := range []ProgramGamepadKeys{{}, {Up: 1}, {Up: 0x80}, {Up: 0xe8}} {
		if _, err := b.BindGamepad(ctx, play.CorePackage.Generation, d, m); err == nil || f.cpu.calls != calls {
			t.Fatal("bad profile performed I/O", err)
		}
	}
	if _, err := b.BindGamepad(ctx, play.CorePackage.Generation, []byte{0}, gamepadKeys()); err == nil || f.cpu.calls != calls {
		t.Fatal("bad descriptor performed I/O", err)
	}
	if _, err := b.BindGamepad(ctx, play.CorePackage.Generation+1, d, gamepadKeys()); err == nil || f.cpu.calls != calls {
		t.Fatal("wrong play performed I/O", err)
	}
	g, err := b.BindGamepad(ctx, play.CorePackage.Generation, d, gamepadKeys())
	if err != nil {
		t.Fatal(err)
	}
	if err := g.Report(ctx, padReport(128, 128, 0)); err != nil {
		t.Fatal(err)
	}
	calls = f.cpu.calls
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := g.Report(canceled, padReport(128, 128, 1)); !errors.Is(err, context.Canceled) || f.cpu.calls != calls {
		t.Fatal(err)
	}
	b.operation.Lock()
	err = g.Report(ctx, padReport(128, 128, 1))
	b.operation.Unlock()
	if !errors.Is(err, ErrProgramBusy) || f.cpu.calls != calls {
		t.Fatal(err)
	}
	f.cpu.failKeyboardAt = f.cpu.keyboardWrites + 2
	if err := g.Report(ctx, padReport(128, 128, 1)); err == nil || f.s.Status().State != ProgramRebootRequired || f.lease.r.Active || !g.state.keys.state.closed {
		t.Fatal("uncertain rows not retired", err)
	}
	calls = f.cpu.calls
	if err := g.Report(ctx, padReport(128, 128, 1)); err == nil || f.cpu.calls != calls {
		t.Fatal("failed write retried", err)
	}
	if err := g.Disconnect(ctx); err == nil || f.cpu.calls != calls {
		t.Fatal("failed owner released replacement", err)
	}
}

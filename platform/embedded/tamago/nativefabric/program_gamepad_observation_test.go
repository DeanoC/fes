// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/fes/platform/embedded/tamago/input"
	"github.com/DeanoC/fes/platform/embedded/tamago/usb"
)

type observingPadPlatform struct {
	programInputBase
	records []ProgramGamepadObservation
	err     error
}

func (p *observingPadPlatform) ObserveProgramGamepad(_ context.Context, o ProgramGamepadObservation) error {
	p.records = append(p.records, o)
	return p.err
}

func TestProgramGamepadReceiptsFollowOwnedWritesAndRelease(t *testing.T) {
	f, b, c := backendFixture(t)
	ctx := context.Background()
	source := &programPadReports{}
	base := &observingPadPlatform{}
	descriptor := gamepadDescriptor(t)
	p, err := NewProgramGamepadPlatform(base, b, source, descriptor, input.GamepadButtons{Confirm: 1, Back: 2}, gamepadKeys())
	if err != nil {
		t.Fatal(err)
	}
	play, err := c.Launch(ctx, "native-0")
	if err != nil {
		t.Fatal(err)
	}
	// Held launch buttons, the arming report and idle duplicates are not writes.
	source.reports = [][]byte{padReport(128, 128, 1), padReport(128, 128, 0), padReport(128, 128, 0)}
	if _, err = p.Next(ctx); err != nil || len(base.records) != 0 {
		t.Fatal(err, base.records)
	}
	source.reports = [][]byte{padReport(128, 128, 1), padReport(128, 128, 1), padReport(128, 128, 0)}
	if _, err = p.Next(ctx); err != nil || len(base.records) != 2 || f.cpu.keyboardWrites != 27 {
		t.Fatal(err, base.records, f.cpu.keyboardWrites)
	}
	digest := sha256.Sum256(descriptor)
	decoder, err := usb.ParseGamepad(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	var previous [9]uint16
	for n, o := range base.records {
		raw, err := hex.DecodeString(o.Report)
		if err != nil {
			t.Fatal(err)
		}
		state, _, err := decoder.Feed(raw)
		v := b.service.Status()
		if err != nil || o.Scope != "nano-computer-gamepad-v1" || o.Owner != v.Owner || o.Generation != play.CorePackage.Generation || o.PackageID != v.Identity.PackageID || o.BuildID != v.Identity.BuildID || o.Sequence != uint64(n+1) || o.DescriptorSHA256 != hex.EncodeToString(digest[:]) || o.State != state || o.Before != previous || o.Rows != o.Mapping.rows(state) || o.Rows == o.Before {
			t.Fatal("invalid completed write receipt", o, err)
		}
		previous = o.Rows
	}
	if previous != ([9]uint16{}) {
		t.Fatal("release not observed")
	}
	saved := base.records[1]
	base.records[1].Rows[0] = 65535
	if p.gamepad.state.observation != saved {
		t.Fatal("observer mutated binding")
	}
	old := p.gamepad
	if _, err = c.StopExpectedStamped(ctx, play, hostclient.ClientStamp{}); err != nil {
		t.Fatal(err)
	}
	if err = old.Report(ctx, padReport(128, 128, 1)); !errors.Is(err, ErrProgramSession) || len(base.records) != 2 {
		t.Fatal("stale input observed", err)
	}
}

func TestProgramGamepadFailedWriteDoesNotPublishReceipt(t *testing.T) {
	f, b, c := backendFixture(t)
	ctx := context.Background()
	source := &programPadReports{}
	base := &observingPadPlatform{}
	p, err := NewProgramGamepadPlatform(base, b, source, gamepadDescriptor(t), input.GamepadButtons{Confirm: 1, Back: 2}, gamepadKeys())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.Launch(ctx, "native-0"); err != nil {
		t.Fatal(err)
	}
	source.reports = [][]byte{padReport(128, 128, 0)}
	if _, err = p.Next(ctx); err != nil {
		t.Fatal(err)
	}
	f.cpu.failKeyboardAt = f.cpu.keyboardWrites + 2
	source.reports = [][]byte{padReport(128, 128, 1)}
	if _, err = p.Next(ctx); err == nil || len(base.records) != 0 || p.gamepad.state.observation.Sequence != 0 || b.service.Status().State != ProgramRebootRequired {
		t.Fatal("partial write published", err, base.records)
	}
	calls := f.cpu.calls
	if _, err = p.Next(ctx); err == nil || f.cpu.calls != calls {
		t.Fatal("uncertain write replayed", err)
	}
}

func TestProgramGamepadObserverFailureParksWithoutReplayingReport(t *testing.T) {
	_, b, c := backendFixture(t)
	ctx := context.Background()
	source := &programPadReports{}
	base := &observingPadPlatform{err: errors.New("serial output failed")}
	p, err := NewProgramGamepadPlatform(base, b, source, gamepadDescriptor(t), input.GamepadButtons{Confirm: 1, Back: 2}, gamepadKeys())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.Launch(ctx, "native-0"); err != nil {
		t.Fatal(err)
	}
	source.reports = [][]byte{padReport(128, 128, 0), padReport(128, 128, 1), padReport(128, 128, 0)}
	if _, err = p.Next(ctx); !errors.Is(err, base.err) || len(base.records) != 1 || b.service.Status().State != ProgramParked {
		t.Fatal(err, base.records, b.service.Status())
	}
	polls := source.polls
	if _, err = p.Next(ctx); !errors.Is(err, base.err) || source.polls != polls {
		t.Fatal("failed observer consumed another report", err)
	}
}

func TestProgramGamepadSerialReceiptAndEmitFailure(t *testing.T) {
	f, _, _ := backendFixture(t)
	ctx := context.Background()
	var output []byte
	var emitErr error
	p, err := NewProgramSerialPlatform(f.s, func() (byte, bool) { return 0, false }, func(_ context.Context, raw []byte) error { output = append(output, raw...); return emitErr })
	if err != nil {
		t.Fatal(err)
	}
	o := ProgramGamepadObservation{Scope: "nano-computer-gamepad-v1", Sequence: 1, Rows: [9]uint16{1: 64}}
	if err = p.ObserveProgramGamepad(ctx, o); err != nil {
		t.Fatal(err)
	}
	const prefix = "FES_NATIVE_CORE_GAMEPAD "
	var got ProgramGamepadObservation
	if len(output) <= len(prefix) || string(output[:len(prefix)]) != prefix || json.Unmarshal(output[len(prefix):], &got) != nil || got != o {
		t.Fatal(string(output))
	}
	emitErr = errors.New("serial disconnected")
	if err = p.ObserveProgramGamepad(ctx, o); !errors.Is(err, emitErr) {
		t.Fatal(err)
	}
	before := len(output)
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err = p.ObserveProgramGamepad(cancelled, o); !errors.Is(err, context.Canceled) || len(output) != before {
		t.Fatal(err)
	}
}

// SPDX-License-Identifier: GPL-3.0-or-later
package computer

import (
	"context"
	"errors"
	"testing"

	g "github.com/DeanoC/fes/platform/embedded/tamago/computer/generated"
)

type batchedInputFake struct {
	*faultIO
	batches int
}

func (b *batchedInputFake) WithInputBatch(send func(IO) error) error {
	b.batches++
	return send(b.faultIO)
}

func TestKeyboardDirtyRowsAndInputBatch(t *testing.T) {
	id := wanted()
	id.Capabilities |= g.FesComputerCapabilityGamepadPorts | g.FesComputerCapabilityMouseRelative
	f := &batchedInputFake{faultIO: fixture(id)}
	c, err := Open(context.Background(), f, limits(), id)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	ctx := context.Background()

	// First call syncs all 9 rows inside a single batch.
	beforeWrites := len(f.writes)
	if err := c.Keyboard(ctx, [9]uint16{}); err != nil {
		t.Fatalf("initial Keyboard: %v", err)
	}
	if got := len(f.writes) - beforeWrites; got != 9 {
		t.Fatalf("initial Keyboard writes=%d, want 9", got)
	}
	if f.batches != 1 {
		t.Fatalf("initial batches=%d, want 1", f.batches)
	}

	// Unchanged state sends 0 writes and 0 batches.
	beforeWrites = len(f.writes)
	if err := c.Keyboard(ctx, [9]uint16{}); err != nil {
		t.Fatalf("unchanged Keyboard: %v", err)
	}
	if got := len(f.writes) - beforeWrites; got != 0 || f.batches != 1 {
		t.Fatalf("unchanged Keyboard writes=%d batches=%d, want 0/1", got, f.batches)
	}

	// Pressing one key sends ONLY the single dirty row inside 1 batch.
	beforeWrites = len(f.writes)
	if err := c.Keyboard(ctx, [9]uint16{0: 1 << 4}); err != nil {
		t.Fatalf("single-key Keyboard: %v", err)
	}
	if got := len(f.writes) - beforeWrites; got != 1 {
		t.Fatalf("single-key Keyboard writes=%d, want 1", got)
	}
	if f.batches != 2 {
		t.Fatalf("single-key batches=%d, want 2", f.batches)
	}

	// Hold clears keyboard shadow state so the next Keyboard call resyncs all 9 rows.
	if err := c.Hold(ctx); err != nil {
		t.Fatalf("Hold: %v", err)
	}
	beforeWrites = len(f.writes)
	if err := c.Keyboard(ctx, [9]uint16{0: 1 << 4}); err != nil {
		t.Fatalf("post-Hold Keyboard: %v", err)
	}
	if got := len(f.writes) - beforeWrites; got != 9 {
		t.Fatalf("post-Hold Keyboard writes=%d, want 9", got)
	}
}

func TestControllerButtonsAndMouseRelative(t *testing.T) {
	id := wanted()
	id.Capabilities |= g.FesComputerCapabilityGamepadPorts | g.FesComputerCapabilityMouseRelative
	f := &batchedInputFake{faultIO: fixture(id)}
	c, err := Open(context.Background(), f, limits(), id)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	ctx := context.Background()

	// Invalid controller port/mask rejected without touching hardware.
	before := len(f.writes)
	if err := c.ControllerButtons(ctx, 2, 0x01); !errors.Is(err, ErrArgument) {
		t.Fatalf("out-of-range port err=%v, want ErrArgument", err)
	}
	if err := c.ControllerButtons(ctx, 0, 0x100); !errors.Is(err, ErrArgument) {
		t.Fatalf("out-of-range mask err=%v, want ErrArgument", err)
	}
	if err := c.MouseRelative(ctx, 1, -1, 4); !errors.Is(err, ErrArgument) {
		t.Fatalf("out-of-range mouse buttons err=%v, want ErrArgument", err)
	}
	if len(f.writes) != before || f.batches != 0 {
		t.Fatalf("validation failures touched hardware: writes=%d batches=%d", len(f.writes)-before, f.batches)
	}

	// Valid ControllerButtons writes exactly 1 word inside 1 batch.
	if err := c.ControllerButtons(ctx, 1, 0x35); err != nil {
		t.Fatalf("ControllerButtons: %v", err)
	}
	if len(f.writes)-before != 1 || f.batches != 1 {
		t.Fatalf("ControllerButtons writes=%d batches=%d, want 1/1", len(f.writes)-before, f.batches)
	}
	last := f.writes[len(f.writes)-1]
	if (last>>24)&0x7f != g.FesComputerOpcodeControllerButtons || (last>>16)&0xff != 1 || last&0xffff != 0x35 {
		t.Fatalf("unexpected controller word: %#x", last)
	}

	// Valid MouseRelative packs signed int8 dx/dy into low/high bytes of argument.
	before = len(f.writes)
	if err := c.MouseRelative(ctx, -5, 12, 3); err != nil {
		t.Fatalf("MouseRelative: %v", err)
	}
	if len(f.writes)-before != 1 || f.batches != 2 {
		t.Fatalf("MouseRelative writes=%d batches=%d, want 1/2", len(f.writes)-before, f.batches)
	}
	last = f.writes[len(f.writes)-1]
	dx, dy := int8(-5), int8(12)
	wantArg := uint32(uint16(uint8(dx)) | uint16(uint8(dy))<<8)
	if (last>>24)&0x7f != g.FesComputerOpcodeMouseRelative || (last>>16)&0xff != 3 || last&0xffff != wantArg {
		t.Fatalf("unexpected mouse word: %#x want arg=%#x", last, wantArg)
	}
}

// SPDX-License-Identifier: GPL-3.0-or-later
package computer

import (
	"context"
	"errors"
	"fmt"
	"testing"

	g "github.com/DeanoC/fes/platform/embedded/tamago/computer/generated"
)

func rtlInitialTape(r *rtlIO, mode int, calls *int) InitialTapeLoader {
	return func(_ context.Context, request InitialTapeRequest) (InitialTapeReceipt, error) {
		*calls++
		v, err := r.call("secure %x %x %x %x %x %x %x 11 9", len(request.Image), request.GPO, request.Identity.Capabilities, request.Identity.Build, request.CRC32, mode, request.Image)
		if err != nil {
			return InitialTapeReceipt{}, err
		}
		if len(v) != 12 || v[0] != 0 || v[1] != 1 || v[10] != 17 || v[11] != 9 {
			return InitialTapeReceipt{}, ErrProtocol
		}
		return InitialTapeReceipt{GPO: uint32(v[2]), Bytes: uint32(v[3]), CRC32: uint32(v[4])}, nil
	}
}

func TestSecureTapeEngineUsesActualComputerAndSynchronizesReturn(t *testing.T) {
	for _, size := range []int{1, 917, 1025, 16375} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			r, want := startRTL(t)
			calls := 0
			c, err := OpenWithInitialTape(context.Background(), r, limits(), want, rtlInitialTape(r, 0, &calls))
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			if err := c.Hold(ctx); err != nil {
				t.Fatal(err)
			}
			before, err := c.Activity(ctx)
			if err != nil {
				t.Fatal(err)
			}
			image := make([]byte, size)
			for i := range image {
				image[i] = byte(i*37 + 11)
			}
			if err := c.LoadInitialTape(ctx, image); err != nil || calls != 1 {
				t.Fatal("secure hook", err, calls)
			}
			state := inspect(t, r)
			if state[0] != 1 || state[4] != uint64(g.FesComputerMediaStateReady) {
				t.Fatal("tape did not stay held and ready", state)
			}
			for i, expected := range image {
				got, err := r.call("byte %x", i)
				if err != nil || len(got) != 1 || got[0] != uint64(expected) {
					t.Fatalf("tape byte %d: %v %v", i, got, err)
				}
			}
			after, err := c.Activity(ctx)
			if err != nil || before != after {
				t.Fatal("secure transfer advanced held execution", before, after, err)
			}
			if err := c.Run(ctx); err != nil {
				t.Fatal("returned GPO did not continue protocol", err)
			}
			advance(t, r, 10000)
			active, err := c.Activity(ctx)
			if err != nil || active.Cycles == after.Cycles {
				t.Fatal("CPU did not fetch after caller Run", active, err)
			}
			if err := c.LoadTape(ctx, []byte{1, 2, 3}); err != nil || calls != 1 || inspect(t, r)[0] != 0 {
				t.Fatal("ordinary media change used initial hook or reset execution", err, calls)
			}
		})
	}
}

func TestSecureTapeEngineFailuresLeaveActualCPUHeldAndNeverReplay(t *testing.T) {
	for mode := 1; mode <= 5; mode++ {
		t.Run(fmt.Sprint(mode), func(t *testing.T) {
			r, want := startRTL(t)
			calls := 0
			c, err := OpenWithInitialTape(context.Background(), r, limits(), want, rtlInitialTape(r, mode, &calls))
			if err != nil {
				t.Fatal(err)
			}
			if err := c.Hold(context.Background()); err != nil {
				t.Fatal(err)
			}
			image := make([]byte, 917)
			for i := range image {
				image[i] = byte(i*37 + 11)
			}
			if err := c.LoadInitialTape(context.Background(), image); err == nil || calls != 1 || inspect(t, r)[0] != 1 {
				t.Fatal("failed secure hook", err, calls)
			}
			writes := r.writes
			if !errors.Is(c.Run(context.Background()), ErrRetired) || !errors.Is(c.LoadInitialTape(context.Background(), image), ErrRetired) || r.writes != writes || calls != 1 {
				t.Fatal("failed hook replayed or issued Run", calls, r.writes)
			}
		})
	}
}

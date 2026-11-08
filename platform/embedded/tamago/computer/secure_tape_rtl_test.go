// SPDX-License-Identifier: GPL-3.0-or-later
package computer

import (
	"context"
	"fmt"
	"hash/crc32"
	"testing"

	g "github.com/DeanoC/fes/platform/embedded/tamago/computer/generated"
)

func TestSecureTapeEngineUsesActualComputerAndSynchronizesReturn(t *testing.T) {
	for _, size := range []int{1, 917, 1025, 16375} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			r, want := startRTL(t)
			c, err := Open(context.Background(), r, limits(), want)
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
			checksum := crc32.ChecksumIEEE(image)
			v, err := r.call("secure %x %x %x %x %x 0", size, c.gpo, want.Capabilities, want.Build, checksum)
			if err != nil || len(v) != 10 || v[0] != 0 || v[1] != 1 || v[3] != uint64(size) ||
				v[4] != uint64(checksum) || v[8] != uint64(before.Cycles) || v[9] != 0 {
				t.Fatalf("secure receipt: %v %v", v, err)
			}
			// Prototype adapter only: the production Go backend does not yet
			// expose a secure tape hook. Copy only a verified successful GPO.
			c.gpo = uint32(v[2])
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
		})
	}
}

func TestSecureTapeEngineFailuresLeaveActualCPUHeldAndNeverReplay(t *testing.T) {
	for mode := 1; mode <= 5; mode++ {
		t.Run(fmt.Sprint(mode), func(t *testing.T) {
			r, want := startRTL(t)
			c, err := Open(context.Background(), r, limits(), want)
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
			v, err := r.call("secure %x %x %x %x %x %x", len(image), c.gpo, want.Capabilities, want.Build, crc32.ChecksumIEEE(image), mode)
			if err != nil || len(v) != 10 || v[0] == 0 || v[1] != 0 || v[2] != 0 || v[3] != 0 || inspect(t, r)[0] != 1 {
				t.Fatalf("failed secure transfer: %v %v", v, err)
			}
			again, err := r.call("secure %x %x %x %x %x %x", len(image), c.gpo, want.Capabilities, want.Build, crc32.ChecksumIEEE(image), mode)
			if err != nil || len(again) != 10 || again[0] == 0 || again[5] != v[5] || again[6] != v[6] || again[7] != v[7] {
				t.Fatal("failed secure transfer replayed I/O", again, v, err)
			}
		})
	}
}

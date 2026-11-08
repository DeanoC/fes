// SPDX-License-Identifier: GPL-3.0-or-later
package computer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"hash/crc32"
	"testing"
	"time"

	g "github.com/DeanoC/fes/platform/embedded/tamago/computer/generated"
)

func initialReceipt(request InitialTapeRequest) InitialTapeReceipt {
	words := (len(request.Image)+1)/2 + 3*((len(request.Image)+511)/512) + 32 + 12 + 4 + 1
	bit := request.GPO & g.FesComputerRequestMask
	if words%2 != 0 {
		bit ^= g.FesComputerRequestMask
	}
	return InitialTapeReceipt{GPO: bit | g.FesComputerOpcodeIdentity<<24 | 15<<16, Bytes: uint32(len(request.Image)), CRC32: crc32.ChecksumIEEE(request.Image)}
}

func TestInitialTapeHookPrivateSourceAndProtocolContinuation(t *testing.T) {
	for _, size := range []int{1, 2, 511, 512, 513, 917, 1025, 16375} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			want := wanted()
			f := fixture(want)
			data := bytes.Repeat([]byte{3}, size)
			calls := 0
			load := func(_ context.Context, request InitialTapeRequest) (InitialTapeReceipt, error) {
				calls++
				if request.Identity != want || request.CRC32 != crc32.ChecksumIEEE(data) || !bytes.Equal(request.Image, data) {
					t.Fatal("selected source lost")
				}
				data[0] = 9
				r := initialReceipt(request)
				f.gpo = r.GPO
				f.gpi = g.FesComputerSignature
				if r.GPO&g.FesComputerRequestMask != 0 {
					f.gpi |= g.FesComputerAckMask
				}
				return r, nil
			}
			c, err := OpenWithInitialTape(context.Background(), f, limits(), want, load)
			if err != nil {
				t.Fatal(err)
			}
			if err = c.Hold(context.Background()); err != nil {
				t.Fatal(err)
			}
			writes := len(f.writes)
			if err = c.LoadInitialTape(context.Background(), data); err != nil || calls != 1 || len(f.writes) != writes {
				t.Fatal(err, calls, len(f.writes))
			}
			if err = c.Run(context.Background()); err != nil {
				t.Fatal("returned protocol state rejected", err)
			}
		})
	}
}

func TestInitialTapeHookFailuresRetireWithoutRunOrFallback(t *testing.T) {
	for _, fault := range []string{"error", "bytes", "crc", "gpo", "parity", "mutation", "deadline", "backwards", "cancel"} {
		t.Run(fault, func(t *testing.T) {
			f := fixture(wanted())
			now := time.Unix(100, 0)
			limit := limits()
			limit.Now = func() time.Time { return now }
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			load := func(_ context.Context, request InitialTapeRequest) (InitialTapeReceipt, error) {
				calls++
				r := initialReceipt(request)
				switch fault {
				case "error":
					return r, errors.New("uncertain secure transfer")
				case "bytes":
					r.Bytes++
				case "crc":
					r.CRC32++
				case "gpo":
					r.GPO ^= 1
				case "parity":
					r.GPO ^= g.FesComputerRequestMask
				case "mutation":
					request.Image[0] ^= 1
				case "deadline":
					now = now.Add(time.Second)
				case "backwards":
					now = now.Add(-time.Second)
				case "cancel":
					cancel()
				}
				return r, nil
			}
			c, err := OpenWithInitialTape(ctx, f, limit, wanted(), load)
			if err != nil {
				t.Fatal(err)
			}
			if err = c.Hold(ctx); err != nil {
				t.Fatal(err)
			}
			writes := len(f.writes)
			if err = c.LoadInitialTape(ctx, []byte{1, 2, 3}); err == nil {
				t.Fatal("failed receipt accepted")
			}
			if !errors.Is(c.Run(context.Background()), ErrRetired) || !errors.Is(c.LoadInitialTape(context.Background(), []byte{1}), ErrRetired) || calls != 1 || len(f.writes) != writes {
				t.Fatal("failed hook replayed or fell back", calls, len(f.writes))
			}
		})
	}
}

func TestInitialTapeHookSelectionAndSerialization(t *testing.T) {
	f := fixture(wanted())
	if _, err := OpenWithInitialTape(context.Background(), f, limits(), wanted(), nil); err == nil || f.reads != 0 {
		t.Fatal("missing selection touched IO")
	}
	var c *Computer
	calls := 0
	load := func(_ context.Context, r InitialTapeRequest) (InitialTapeReceipt, error) {
		calls++
		if !errors.Is(c.Run(context.Background()), ErrBusy) {
			t.Fatal("hook permitted concurrent command")
		}
		return initialReceipt(r), nil
	}
	var err error
	c, err = OpenWithInitialTape(context.Background(), f, limits(), wanted(), load)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Hold(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err = c.LoadInitialTape(context.Background(), []byte{1}); err != nil || calls != 1 {
		t.Fatal(err, calls)
	}
}

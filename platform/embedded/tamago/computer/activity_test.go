// SPDX-License-Identifier: GPL-3.0-or-later
package computer

import (
	"context"
	"errors"
	"testing"

	g "github.com/DeanoC/fes/platform/embedded/tamago/computer/generated"
)

func TestActivityReadsAStoredPairWithoutManufacturingProgress(t *testing.T) {
	c, f := opened(t)
	f.readHook = func(offset uint32) (uint32, error, bool) {
		switch offset {
		case g.FesDe25ComputerFetchCountOffset:
			return 0xffffffff, nil, true
		case g.FesDe25ComputerFetchAddressOffset:
			return 0x1234, nil, true
		}
		return 0, nil, false
	}
	ctx := context.Background()
	writes := len(f.writes)
	for i := 0; i < 2; i++ {
		v, err := c.Activity(ctx)
		if err != nil || v != (FetchActivity{Cycles: 0xffffffff, Address: 0x1234}) {
			t.Fatal(v, err)
		}
	}
	if len(f.writes) != writes {
		t.Fatal("observing activity sent a command")
	}
	if err := c.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.Hold(ctx); err != nil {
		t.Fatal(err)
	}
	v, err := c.Activity(ctx)
	if err != nil || v.Cycles != 0xffffffff {
		t.Fatal("GP acknowledgements manufactured CPU activity", v, err)
	}
}

func TestActivityReadFailuresRetireWithoutReplay(t *testing.T) {
	busError := errors.New("injected activity bus error")
	for _, kind := range []string{"count", "address", "address-width", "control-drift"} {
		t.Run(kind, func(t *testing.T) {
			c, f := opened(t)
			f.readHook = func(offset uint32) (uint32, error, bool) {
				switch offset {
				case g.FesDe25ComputerFetchCountOffset:
					if kind == "count" {
						return 0, busError, true
					}
					return 17, nil, true
				case g.FesDe25ComputerFetchAddressOffset:
					if kind == "address" {
						return 0, busError, true
					}
					if kind == "address-width" {
						return 0x10000, nil, true
					}
					if kind == "control-drift" {
						f.gpo ^= 1
					}
					return 0x1234, nil, true
				}
				return 0, nil, false
			}
			_, err := c.Activity(context.Background())
			want := ErrProtocol
			if kind == "count" || kind == "address" {
				want = busError
			}
			if !errors.Is(err, want) {
				t.Fatal(err)
			}
			reads, writes := f.reads, len(f.writes)
			if _, err := c.Activity(context.Background()); !errors.Is(err, ErrRetired) {
				t.Fatal(err)
			}
			if f.reads != reads || len(f.writes) != writes {
				t.Fatal("retired activity observation accessed the bus")
			}
		})
	}
}

func TestActivityCannotProbeAnOlderTransportOrUseACancelledContext(t *testing.T) {
	f := fixture(wanted())
	f.readHook = func(offset uint32) (uint32, error, bool) {
		return 0x10000, nil, offset == g.FesDe25ComputerVersionOffset
	}
	if _, err := Open(context.Background(), f, limits(), wanted()); !errors.Is(err, ErrIdentity) || len(f.writes) != 0 {
		t.Fatal("old transport was probed or given a command", err, f.writes)
	}
	c, f := opened(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reads := f.reads
	if _, err := c.Activity(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := c.Activity(nil); !errors.Is(err, ErrArgument) {
		t.Fatal(err)
	}
	if f.reads != reads {
		t.Fatal("invalid activity request accessed the bus")
	}
}

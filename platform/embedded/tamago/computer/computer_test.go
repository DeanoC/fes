// SPDX-License-Identifier: GPL-3.0-or-later
package computer

import (
	"context"
	"errors"
	"testing"
	"time"

	g "github.com/DeanoC/fes/platform/embedded/tamago/computer/generated"
)

// This fixture supplies injected transport failures. Successful media and CPU
// control are also checked against the selected, unmodified Avalon/ZX81 RTL by
// verify.py; this fixture is not an alternate core model.
type faultIO struct {
	gpo, gpi, status, fault uint32
	words                   [17]uint16
	writes                  []uint32
	reads                   int
	readHook                func(uint32) (uint32, error, bool)
	writeHook               func(uint32) error
}

func wanted() Identity {
	return Identity{Capabilities: g.FesComputerCapabilityVideoFixed720p60 | g.FesComputerCapabilityKeyboardHid | g.FesComputerCapabilityMediaZx81Tape,
		Build: [16]byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}}
}
func fixture(want Identity) *faultIO {
	f := &faultIO{gpi: g.FesComputerSignature, status: g.FesDe25ComputerStatusReady}
	f.words = [17]uint16{0x4546, 0x3153, 1, 0, 4, 1, want.Minor, uint16(want.Capabilities)}
	for i := range 8 {
		f.words[8+i] = uint16(want.Build[2*i]) | uint16(want.Build[2*i+1])<<8
	}
	f.words[16] = uint16(want.Capabilities >> 16)
	return f
}
func (f *faultIO) Read32(offset uint32) (uint32, error) {
	f.reads++
	if f.readHook != nil {
		if v, err, handled := f.readHook(offset); handled {
			return v, err
		}
	}
	switch offset {
	case g.FesDe25ComputerIdOffset:
		return g.FesDe25ComputerIdentity, nil
	case g.FesDe25ComputerVersionOffset:
		return g.FesDe25ComputerVersion, nil
	case g.FesDe25ComputerStatusOffset:
		return f.status, nil
	case g.FesDe25ComputerGpoOffset:
		return f.gpo, nil
	case g.FesDe25ComputerGpiOffset:
		return f.gpi, nil
	case g.FesDe25ComputerFaultOffset:
		return f.fault, nil
	default:
		panic("driver used an undefined CSR")
	}
}
func (f *faultIO) Write32(offset, word uint32) error {
	if offset != g.FesDe25ComputerGpoOffset {
		panic("driver wrote a non-GPO CSR")
	}
	f.writes = append(f.writes, word)
	if f.writeHook != nil {
		return f.writeHook(word)
	}
	if (f.gpo^word)&g.FesComputerRequestMask == 0 {
		panic("GPO write failed to toggle")
	}
	f.gpo = word
	f.gpi = g.FesComputerSignature
	if word&g.FesComputerRequestMask != 0 {
		f.gpi |= g.FesComputerAckMask
	}
	if word>>24&0x7f == g.FesComputerOpcodeIdentity {
		f.gpi |= uint32(f.words[(word>>16)&0xff])
	}
	return nil
}
func limits() Limits {
	return Limits{Timeout: time.Second, MaxPolls: 4, Now: func() time.Time { return time.Unix(100, 0) }, Yield: func() {}}
}
func opened(t *testing.T) (*Computer, *faultIO) {
	t.Helper()
	f := fixture(wanted())
	c, err := Open(context.Background(), f, limits(), wanted())
	if err != nil {
		t.Fatal(err)
	}
	if len(f.writes) != 16 {
		t.Fatalf("identity writes: %d", len(f.writes))
	}
	return c, f
}

func TestExplicitIdentityVersionsAndBuildByteOrder(t *testing.T) {
	for _, minor := range []uint16{0, 1} {
		want := wanted()
		want.Minor = minor
		if minor == 1 {
			want.Capabilities |= 1 << 18
		}
		f := fixture(want)
		if _, err := Open(context.Background(), f, limits(), want); err != nil {
			t.Fatal(err)
		}
		if len(f.writes) != 16+int(minor) {
			t.Fatal("identity length changed")
		}
	}
	// Every identity field, including all sixteen build bytes, is compared.
	for index := 0; index < 17; index++ {
		want := wanted()
		want.Minor = 1
		f := fixture(want)
		f.words[index] ^= 1
		if _, err := Open(context.Background(), f, limits(), want); !errors.Is(err, ErrIdentity) {
			t.Fatalf("word %d: %v", index, err)
		}
		for _, word := range f.writes {
			if word>>24&0x7f != g.FesComputerOpcodeIdentity {
				t.Fatal("mismatch permitted mutation")
			}
		}
	}
}

func TestOpenRejectsAdapterAndUnknownState(t *testing.T) {
	for _, offset := range []uint32{g.FesDe25ComputerIdOffset, g.FesDe25ComputerVersionOffset,
		g.FesDe25ComputerStatusOffset, g.FesDe25ComputerFaultOffset, g.FesDe25ComputerGpiOffset} {
		f := fixture(wanted())
		f.readHook = func(o uint32) (uint32, error, bool) { return 0xffffffff, nil, o == offset }
		if _, err := Open(context.Background(), f, limits(), wanted()); err == nil {
			t.Fatalf("offset %d accepted", offset)
		}
		if len(f.writes) != 0 {
			t.Fatal("invalid adapter got a command")
		}
	}
	f := fixture(wanted())
	f.status |= g.FesDe25ComputerStatusBusy
	if _, err := Open(context.Background(), f, limits(), wanted()); err == nil {
		t.Fatal("incoherent busy state accepted")
	}
	f = fixture(wanted())
	f.gpo = g.FesComputerRequestMask
	f.status |= g.FesDe25ComputerStatusBusy
	if _, err := Open(context.Background(), f, limits(), wanted()); err == nil {
		t.Fatal("inherited command accepted")
	}
}

func TestUncertainCommandsAreNeverReplayed(t *testing.T) {
	for _, mode := range []string{"timeout", "write-error", "cancel", "fault", "wrong-gpo", "bad-signature", "error-ack", "reserved-gpi", "nonzero-success", "read-error", "read-busy-error"} {
		t.Run(mode, func(t *testing.T) {
			c, f := opened(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			f.writeHook = func(word uint32) error {
				f.gpo = word
				f.status |= g.FesDe25ComputerStatusBusy
				switch mode {
				case "write-error":
					return errors.New("lost Avalon completion")
				case "cancel":
					cancel()
				case "fault":
					f.fault = g.FesDe25ComputerFaultReset
				case "wrong-gpo":
					f.gpo ^= 1
				case "bad-signature":
					f.gpi ^= 1 << 24
				case "reserved-gpi":
					f.gpi |= 1 << 16
				case "read-error":
					f.readHook = func(uint32) (uint32, error, bool) { return 0, errors.New("bus error"), true }
				case "read-busy-error":
					f.readHook = func(uint32) (uint32, error, bool) { return 0, ErrBusy, true }
				case "error-ack", "nonzero-success":
					f.status = g.FesDe25ComputerStatusReady
					f.gpi = g.FesComputerSignature | 3
					if word&g.FesComputerRequestMask != 0 {
						f.gpi |= g.FesComputerAckMask
					}
					if mode == "error-ack" {
						f.gpi |= g.FesComputerErrorMask
					}
				}
				return nil
			}
			before := len(f.writes)
			if err := c.Run(ctx); err == nil {
				t.Fatal("uncertain command succeeded")
			}
			if len(f.writes) != before+1 {
				t.Fatal("command replayed")
			}
			reads := f.reads
			if err := c.Hold(context.Background()); !errors.Is(err, ErrRetired) {
				t.Fatalf("retirement: %v", err)
			}
			if len(f.writes) != before+1 || f.reads != reads {
				t.Fatal("retired transport touched hardware")
			}
		})
	}
}

func TestPartialKeyboardFailureRetiresWithoutNeutralizationRetry(t *testing.T) {
	c, f := opened(t)
	before := len(f.writes)
	f.writeHook = func(word uint32) error {
		if len(f.writes) == before+5 {
			return errors.New("lost keyboard write")
		}
		f.gpo = word
		f.gpi = g.FesComputerSignature
		if word&g.FesComputerRequestMask != 0 {
			f.gpi |= g.FesComputerAckMask
		}
		return nil
	}
	if err := c.Keyboard(context.Background(), [9]uint16{1: 2}); err == nil {
		t.Fatal("partial input succeeded")
	}
	if len(f.writes) != before+5 {
		t.Fatal("partial operation retried or continued")
	}
	if err := c.Hold(context.Background()); !errors.Is(err, ErrRetired) {
		t.Fatal(err)
	}
	if len(f.writes) != before+5 {
		t.Fatal("retired handle tried to neutralize through uncertain GP epoch")
	}
}

func TestClockDeadlineAndCancellation(t *testing.T) {
	for _, delta := range []time.Duration{-time.Nanosecond, time.Second} {
		c, f := opened(t)
		now := time.Unix(100, 0)
		c.limits.Now = func() time.Time { return now }
		f.writeHook = func(uint32) error { now = now.Add(delta); return nil }
		if err := c.Run(context.Background()); !errors.Is(err, ErrLimit) {
			t.Fatalf("clock %v: %v", delta, err)
		}
		if err := c.Run(context.Background()); !errors.Is(err, ErrRetired) {
			t.Fatal(err)
		}
	}
	c, f := opened(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	before := len(f.writes)
	if err := c.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if len(f.writes) != before {
		t.Fatal("canceled command issued")
	}
	if err := c.Hold(context.Background()); err != nil {
		t.Fatal("preexisting cancellation retired untouched epoch", err)
	}
}

func TestValidationAndSerialization(t *testing.T) {
	c, f := opened(t)
	before := len(f.writes)
	for _, rows := range [][9]uint16{{1}, {8: 256}} {
		if err := c.Keyboard(context.Background(), rows); !errors.Is(err, ErrArgument) {
			t.Fatal(err)
		}
	}
	for _, data := range [][]byte{nil, make([]byte, 16376)} {
		if err := c.LoadTape(context.Background(), data); !errors.Is(err, ErrArgument) {
			t.Fatal(err)
		}
	}
	if len(f.writes) != before {
		t.Fatal("invalid input touched hardware")
	}
	if err := c.Keyboard(context.Background(), [9]uint16{1: 16, 8: 1}); err != nil {
		t.Fatal(err)
	}
	c.active.Store(true)
	if err := c.Run(context.Background()); !errors.Is(err, ErrBusy) {
		t.Fatal(err)
	}
	c.active.Store(false)
	if err := c.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := c.Hold(context.Background()); err != nil {
		t.Fatal(err)
	}
}

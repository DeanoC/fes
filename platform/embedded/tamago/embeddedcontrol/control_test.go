// SPDX-License-Identifier: GPL-3.0-or-later

package embeddedcontrol

import (
	"errors"
	"testing"

	g "github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol/generated"
)

type fakeIO struct {
	regs      map[uint32]uint32
	writes    [][2]uint32
	onRead    func(offset uint32, f *fakeIO) (uint32, bool, error)
	failRead  map[uint32]error
	failWrite map[uint32]error
}

func newFakeIO(id Identity) *fakeIO {
	return &fakeIO{
		regs: map[uint32]uint32{
			g.FesEmbeddedRegMagic:   g.FesEmbeddedMagic,
			g.FesEmbeddedRegVersion: g.FesEmbeddedVersion,
			g.FesEmbeddedRegAbi:     id.ABI,
			g.FesEmbeddedRegBuild0:  id.Build[0],
			g.FesEmbeddedRegBuild1:  id.Build[1],
			g.FesEmbeddedRegBuild2:  id.Build[2],
			g.FesEmbeddedRegBuild3:  id.Build[3],
		},
		failRead:  make(map[uint32]error),
		failWrite: make(map[uint32]error),
	}
}

func (f *fakeIO) set64(offset uint32, v uint64) {
	f.regs[offset] = uint32(v)
	f.regs[offset+4] = uint32(v >> 32)
}

func (f *fakeIO) Read32(offset uint32) (uint32, error) {
	if err := f.failRead[offset]; err != nil {
		return 0, err
	}
	if f.onRead != nil {
		if v, ok, err := f.onRead(offset, f); ok || err != nil {
			return v, err
		}
	}
	return f.regs[offset], nil
}

func (f *fakeIO) Write32(offset, val uint32) error {
	if err := f.failWrite[offset]; err != nil {
		return err
	}
	f.writes = append(f.writes, [2]uint32{offset, val})
	f.regs[offset] = val
	return nil
}

func TestControlSnapshotAndVerify(t *testing.T) {
	id := Identity{ABI: 0x00010002, Build: [4]uint32{0x11, 0x22, 0x33, 0x44}}

	// Nil IO fails explicitly.
	if _, err := (Control{}).Snapshot(); err == nil {
		t.Fatal("nil IO Snapshot succeeded")
	}

	// Clean initial Verify passes.
	io := newFakeIO(id)
	c := Control{IO: io}
	if err := c.Verify(id); err != nil {
		t.Fatalf("Verify clean: %v", err)
	}

	// Magic and version mismatches return ErrIdentity.
	for _, tc := range []struct {
		name string
		reg  uint32
		val  uint32
	}{
		{name: "bad-magic", reg: g.FesEmbeddedRegMagic, val: g.FesEmbeddedMagic ^ 1},
		{name: "bad-version", reg: g.FesEmbeddedRegVersion, val: g.FesEmbeddedVersion + 1},
	} {
		f := newFakeIO(id)
		f.regs[tc.reg] = tc.val
		if _, err := (Control{IO: f}).Snapshot(); !errors.Is(err, ErrIdentity) {
			t.Fatalf("%s err=%v, want ErrIdentity", tc.name, err)
		}
	}

	// Torn snapshot between first and second read returns ErrBusy.
	tornIO := newFakeIO(id)
	var reads int
	tornIO.onRead = func(offset uint32, f *fakeIO) (uint32, bool, error) {
		if offset == g.FesEmbeddedRegPending {
			reads++
			return uint32(reads), true, nil
		}
		return 0, false, nil
	}
	if _, err := (Control{IO: tornIO}).Snapshot(); !errors.Is(err, ErrBusy) {
		t.Fatalf("torn Snapshot err=%v, want ErrBusy", err)
	}

	// Verify rejects wrong identity and non-zero state fields.
	wrongID := Identity{ABI: id.ABI + 1, Build: id.Build}
	if err := c.Verify(wrongID); !errors.Is(err, ErrIdentity) {
		t.Fatalf("Verify wrong identity err=%v, want ErrIdentity", err)
	}
	for _, tc := range []struct {
		name string
		prep func(*fakeIO)
	}{
		{name: "status", prep: func(f *fakeIO) { f.regs[g.FesEmbeddedRegStatus] = g.FesEmbeddedStatusEnabled }},
		{name: "pending", prep: func(f *fakeIO) { f.regs[g.FesEmbeddedRegPending] = 1 }},
		{name: "generation", prep: func(f *fakeIO) { f.set64(g.FesEmbeddedRegGenerationLo, 1) }},
		{name: "ack-generation", prep: func(f *fakeIO) { f.set64(g.FesEmbeddedRegAckGenerationLo, 1) }},
		{name: "cookie", prep: func(f *fakeIO) { f.set64(g.FesEmbeddedRegAckCookieLo, 1) }},
	} {
		f := newFakeIO(id)
		tc.prep(f)
		if err := (Control{IO: f}).Verify(id); !errors.Is(err, ErrFault) {
			t.Fatalf("Verify dirty %s err=%v, want ErrFault", tc.name, err)
		}
	}

	// Read error propagation across Snapshot fields.
	readBoom := errors.New("csr read fault")
	for _, reg := range []uint32{
		g.FesEmbeddedRegMagic, g.FesEmbeddedRegVersion, g.FesEmbeddedRegAbi,
		g.FesEmbeddedRegBuild0, g.FesEmbeddedRegBuild3,
		g.FesEmbeddedRegGenerationLo, g.FesEmbeddedRegGenerationHi,
		g.FesEmbeddedRegAckGenerationLo, g.FesEmbeddedRegAckCookieLo,
		g.FesEmbeddedRegStatus, g.FesEmbeddedRegPending,
	} {
		f := newFakeIO(id)
		f.failRead[reg] = readBoom
		if _, err := (Control{IO: f}).Snapshot(); !errors.Is(err, readBoom) {
			t.Fatalf("read fault at %#x err=%v, want %v", reg, err, readBoom)
		}
	}
}

func TestControlEnableDisableDrainedTransitions(t *testing.T) {
	id := Identity{ABI: 0x00010000, Build: [4]uint32{1, 2, 3, 4}}
	const gen = uint64(0x0102030405060708)
	const cookie = uint64(0x1112131415161718)

	io := newFakeIO(id)
	c := Control{IO: io}
	io.onRead = func(offset uint32, f *fakeIO) (uint32, bool, error) {
		return 0, false, nil
	}
	// Simulate hardware latching enabled state upon Command=Enable.
	// Intercept Write32 by wrapping or pre-populating via custom IO wrapper below.
	hw := &transitionIO{fakeIO: newFakeIO(id)}
	ctrl := Control{IO: hw}

	// 1. Enable validation failures and success.
	if err := ctrl.Enable(id, 0); !errors.Is(err, ErrFault) {
		t.Fatalf("Enable(gen=0) err=%v, want ErrFault", err)
	}
	if err := ctrl.Enable(id, gen); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if hw.regs[g.FesEmbeddedRegRequestGenerationLo] != uint32(gen&0xffffffff) ||
		hw.regs[g.FesEmbeddedRegRequestGenerationHi] != uint32(gen>>32) {
		t.Fatal("Enable did not write 64-bit request generation")
	}

	// Post-enable status/identity errors.
	hwBad := &transitionIO{fakeIO: newFakeIO(id), skipEnableStatus: true}
	if err := (Control{IO: hwBad}).Enable(id, gen); !errors.Is(err, ErrFault) {
		t.Fatalf("Enable missing StatusEnabled err=%v, want ErrFault", err)
	}
	if err := ctrl.Enable(Identity{ABI: 999}, gen); !errors.Is(err, ErrIdentity) {
		t.Fatalf("Enable wrong identity err=%v, want ErrIdentity", err)
	}

	// 2. Disable validation and success.
	if err := ctrl.Disable(id, 0, cookie); !errors.Is(err, ErrFault) {
		t.Fatalf("Disable(gen=0) err=%v, want ErrFault", err)
	}
	if err := ctrl.Disable(id, gen, 0); !errors.Is(err, ErrFault) {
		t.Fatalf("Disable(cookie=0) err=%v, want ErrFault", err)
	}
	if err := ctrl.Disable(id, gen+1, cookie); !errors.Is(err, ErrIdentity) {
		t.Fatalf("Disable wrong gen err=%v, want ErrIdentity", err)
	}
	if err := ctrl.Disable(id, gen, cookie); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	// Calling Disable while draining must fail with ErrFault.
	if err := ctrl.Disable(id, gen, cookie); !errors.Is(err, ErrFault) {
		t.Fatalf("Disable while draining err=%v, want ErrFault", err)
	}

	// 3. Drained checks: ErrBusy while draining -> nil once drained -> ErrFault on mismatches/fault bit.
	if err := ctrl.Drained(id, gen, cookie); !errors.Is(err, ErrBusy) {
		t.Fatalf("Drained while draining err=%v, want ErrBusy", err)
	}
	hw.regs[g.FesEmbeddedRegStatus] = g.FesEmbeddedStatusDrained
	hw.regs[g.FesEmbeddedRegPending] = 0
	hw.set64(g.FesEmbeddedRegAckGenerationLo, gen)
	hw.set64(g.FesEmbeddedRegAckCookieLo, cookie)
	if err := ctrl.Drained(id, gen, cookie); err != nil {
		t.Fatalf("Drained complete: %v", err)
	}
	if err := ctrl.Drained(Identity{ABI: 999}, gen, cookie); !errors.Is(err, ErrIdentity) {
		t.Fatalf("Drained wrong identity err=%v, want ErrIdentity", err)
	}

	for _, tc := range []struct {
		name string
		prep func(*fakeIO)
	}{
		{name: "fault-bit", prep: func(f *fakeIO) {
			f.regs[g.FesEmbeddedRegStatus] = g.FesEmbeddedStatusDrained | g.FesEmbeddedStatusFault
		}},
		{name: "pending-nonzero", prep: func(f *fakeIO) { f.regs[g.FesEmbeddedRegPending] = 2 }},
		{name: "wrong-ack-gen", prep: func(f *fakeIO) { f.set64(g.FesEmbeddedRegAckGenerationLo, gen^1) }},
		{name: "wrong-cookie", prep: func(f *fakeIO) { f.set64(g.FesEmbeddedRegAckCookieLo, cookie^1) }},
		{name: "wrong-status", prep: func(f *fakeIO) { f.regs[g.FesEmbeddedRegStatus] = 0 }},
	} {
		f := newFakeIO(id)
		f.set64(g.FesEmbeddedRegGenerationLo, gen)
		f.regs[g.FesEmbeddedRegStatus] = g.FesEmbeddedStatusDrained
		f.set64(g.FesEmbeddedRegAckGenerationLo, gen)
		f.set64(g.FesEmbeddedRegAckCookieLo, cookie)
		tc.prep(f)
		if err := (Control{IO: f}).Drained(id, gen, cookie); !errors.Is(err, ErrFault) {
			t.Fatalf("Drained %s err=%v, want ErrFault", tc.name, err)
		}
	}

	// Write error propagation in Enable and Disable.
	writeBoom := errors.New("csr write fault")
	c.IO.Write32(g.FesEmbeddedRegStatus, g.FesEmbeddedStatusEnabled)
	ioErr := newFakeIO(id)
	ioErr.failWrite[g.FesEmbeddedRegCommand] = writeBoom
	if err := (Control{IO: ioErr}).Enable(id, gen); !errors.Is(err, writeBoom) {
		t.Fatalf("Enable write fault err=%v, want %v", err, writeBoom)
	}
	ioErr.regs[g.FesEmbeddedRegStatus] = g.FesEmbeddedStatusEnabled
	ioErr.set64(g.FesEmbeddedRegGenerationLo, gen)
	if err := (Control{IO: ioErr}).Disable(id, gen, cookie); !errors.Is(err, writeBoom) {
		t.Fatalf("Disable write fault err=%v, want %v", err, writeBoom)
	}
}

type transitionIO struct {
	*fakeIO
	skipEnableStatus bool
}

func (t *transitionIO) Write32(offset, val uint32) error {
	if err := t.fakeIO.Write32(offset, val); err != nil {
		return err
	}
	if offset == g.FesEmbeddedRegCommand {
		reqGen := uint64(t.regs[g.FesEmbeddedRegRequestGenerationLo]) | uint64(t.regs[g.FesEmbeddedRegRequestGenerationHi])<<32
		switch val {
		case g.FesEmbeddedCommandEnable:
			t.set64(g.FesEmbeddedRegGenerationLo, reqGen)
			if !t.skipEnableStatus {
				t.regs[g.FesEmbeddedRegStatus] = g.FesEmbeddedStatusEnabled
			}
		case g.FesEmbeddedCommandDisable:
			t.regs[g.FesEmbeddedRegStatus] = g.FesEmbeddedStatusDraining
		}
	}
	return nil
}

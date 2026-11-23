// SPDX-License-Identifier: GPL-3.0-or-later

package sdcontrol

import (
	"bytes"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/gic"
	"github.com/DeanoC/fes/platform/embedded/tamago/sdhci"
)

type mockGICAccess struct {
	mu      sync.Mutex
	regs32  map[uint64]uint32
	pending []uint32
}

func newMockGIC(t *testing.T) (*gic.Controller, *mockGICAccess) {
	t.Helper()
	const gicd, gicr = uint64(0x1d000000), uint64(0x1d060000)
	hw := &mockGICAccess{
		regs32: map[uint64]uint32{
			gicd + gic.GICD_TYPER: 9, // 320 INTIDs (covers Agilex 5 IRQSDMMC = 128)
		},
	}
	ctrl := gic.NewWithAccess(gicd, gicr, hw)
	if err := ctrl.Init(); err != nil {
		t.Fatalf("gic.Init: %v", err)
	}
	return ctrl, hw
}

func (m *mockGICAccess) HasGICv3() bool { return true }
func (m *mockGICAccess) MPIDR() uint64  { return 0 }
func (m *mockGICAccess) Read32(addr uint64) uint32 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.regs32[addr]
}
func (m *mockGICAccess) Write32(addr uint64, val uint32) {
	m.mu.Lock()
	defer m.mu.Unlock()
	const gicd = uint64(0x1d000000)
	if addr >= gicd+gic.GICD_ISENABLER && addr < gicd+gic.GICD_ICENABLER {
		m.regs32[addr] |= val
		return
	}
	if addr >= gicd+gic.GICD_ICENABLER && addr < gicd+gic.GICD_ISPENDR {
		isenabler := addr - gic.GICD_ICENABLER + gic.GICD_ISENABLER
		m.regs32[isenabler] &^= val
		return
	}
	m.regs32[addr] = val
}
func (m *mockGICAccess) Read64(addr uint64) uint64 {
	if addr == 0x1d060000+gic.GICR_TYPER {
		return 1 << 4
	}
	return 0
}
func (m *mockGICAccess) Write64(uint64, uint64) {}
func (m *mockGICAccess) InitCPU(uint8)          {}
func (m *mockGICAccess) SendSGI(uint64)         {}
func (m *mockGICAccess) Ack() uint32 {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.pending) == 0 {
		return gic.SpuriousID
	}
	id := m.pending[0]
	m.pending = m.pending[1:]
	return id
}
func (m *mockGICAccess) End(uint32) {}

func (m *mockGICAccess) isEnabled(id uint32) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	word := uint64(0x1d000000) + gic.GICD_ISENABLER + uint64(id/32)*4
	return m.regs32[word]&(1<<(id%32)) != 0
}

func (m *mockGICAccess) fire(ctrl *gic.Controller, id uint32) {
	m.mu.Lock()
	m.pending = append(m.pending, id)
	m.mu.Unlock()
	ctrl.Dispatch()
}

type syncCard struct {
	mu   sync.Mutex
	base *card
}

func (s *syncCard) Read32(reg uint32) uint32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.base.Read32(reg)
}

func (s *syncCard) Write32(reg, value uint32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.base.Write32(reg, value)
}

func (s *syncCard) Now() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.base.Now()
}

func (s *syncCard) accesses() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.base.accesses
}

const testSDIRQ = uint32(128) // Agilex 5 IRQSDMMC

func TestSessionIRQQuiesceResumeAndRetirement(t *testing.T) {
	// Nil receiver guards.
	var nilSess *Session
	if err := nilSess.EnableIRQ(nil, testSDIRQ); !errors.Is(err, ErrRetired) {
		t.Fatalf("nil EnableIRQ=%v", err)
	}
	nilSess.DisableIRQ()
	if nilSess.IRQCount() != 0 {
		t.Fatal("nil IRQCount non-zero")
	}

	ctrl, ghw := newMockGIC(t)
	firmware, raw := newRPC(t, "")
	sc := &syncCard{base: raw}
	raw.image = make([]byte, 8*512)
	want := bytes.Repeat([]byte{0x5c}, 512)
	copy(raw.image[3*512:4*512], want)

	s, err := Open(firmware, sc, Config{InputClockHz: 200000000})
	if err != nil {
		t.Fatal(err)
	}
	// DisableIRQ before EnableIRQ must touch zero MMIO.
	before := sc.accesses()
	s.DisableIRQ()
	if sc.accesses() != before {
		t.Fatalf("DisableIRQ before EnableIRQ touched MMIO: %d->%d", before, sc.accesses())
	}

	if err := s.EnableIRQ(ctrl, testSDIRQ); err != nil {
		t.Fatal(err)
	}
	if !ghw.isEnabled(testSDIRQ) {
		t.Fatal("EnableIRQ did not enable GIC SPI")
	}

	// Read with IRQ enabled works cleanly and leaves signalEnable==0.
	buf := make([]byte, 512)
	if err := s.ReadSectors(3, buf); err != nil || !bytes.Equal(buf, want) {
		t.Fatalf("IRQ-enabled ReadSectors err=%v", err)
	}
	// Firing the level-triggered handler while active increments IRQCount.
	ghw.fire(ctrl, testSDIRQ)
	if s.IRQCount() != 1 {
		t.Fatalf("IRQCount=%d, want 1", s.IRQCount())
	}

	// Quiesce suspends the SPI and blocks EnableIRQ; a spurious dispatch while
	// quiesced must perform zero MMIO.
	if err := s.Quiesce(); err != nil {
		t.Fatal(err)
	}
	if ghw.isEnabled(testSDIRQ) {
		t.Fatal("Quiesce left GIC SPI enabled")
	}
	if err := s.EnableIRQ(ctrl, testSDIRQ); !errors.Is(err, sdhci.ErrQuiesced) {
		t.Fatalf("quiesced EnableIRQ=%v, want ErrQuiesced", err)
	}
	quiescedAccesses := sc.accesses()
	ghw.fire(ctrl, testSDIRQ)
	if sc.accesses() != quiescedAccesses || s.IRQCount() != 1 {
		t.Fatalf("quiesced IRQ touched MMIO: accesses %d->%d count=%d", quiescedAccesses, sc.accesses(), s.IRQCount())
	}

	// Resume re-enables the GIC SPI automatically after card rediscovery.
	if err := s.Resume(); err != nil {
		t.Fatal(err)
	}
	if !ghw.isEnabled(testSDIRQ) {
		t.Fatal("Resume did not re-enable GIC SPI")
	}
	if err := s.ReadSectors(3, buf); err != nil || !bytes.Equal(buf, want) {
		t.Fatalf("post-resume ReadSectors err=%v", err)
	}
	ghw.fire(ctrl, testSDIRQ)
	if s.IRQCount() != 2 {
		t.Fatalf("post-resume IRQCount=%d, want 2", s.IRQCount())
	}

	// Quiesce and change CID -> Resume must fail and permanently retire/unregister IRQ.
	if err := s.Quiesce(); err != nil {
		t.Fatal(err)
	}
	sc.mu.Lock()
	raw.cid++
	sc.mu.Unlock()
	if err := s.Resume(); !errors.Is(err, ErrRetired) {
		t.Fatalf("changed card Resume=%v, want ErrRetired", err)
	}
	if ghw.isEnabled(testSDIRQ) {
		t.Fatal("failed Resume left GIC SPI enabled")
	}
	retiredAccesses := sc.accesses()
	ghw.fire(ctrl, testSDIRQ)
	s.DisableIRQ()
	if sc.accesses() != retiredAccesses {
		t.Fatalf("retired IRQ/DisableIRQ touched MMIO: %d->%d", retiredAccesses, sc.accesses())
	}
	if err := s.EnableIRQ(ctrl, testSDIRQ); !errors.Is(err, ErrRetired) {
		t.Fatalf("retired EnableIRQ=%v, want ErrRetired", err)
	}
}

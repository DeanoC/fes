// SPDX-License-Identifier: GPL-3.0-or-later
package arm64map

import (
	"errors"
	"testing"
)

type tableModel struct {
	cpu                                 CPUState
	words                               map[uint64]uint64
	reads, probes, states, sets, writes int
	readError, probeError, writeError   int
	setError, stale, afterWriteError    bool
	afterRead                           func(*tableModel, uint64)
}

func tables() *tableModel {
	const ram = uint64(0x84000000)
	m := &tableModel{cpu: CPUState{EL: 4, SCTLR: 0x30d01805, TCR: pinnedTCR, TTBR0: ram + 0x4000, MAIR: 0xff00, MMFR0: 2, MPIDR: 0x81000000}, words: map[uint64]uint64{}}
	dev, _, _, _ := kindAttributes(Device)
	wb, _, _, _ := kindAttributes(WriteBack)
	for i := uint64(0); i < 512; i++ {
		m.words[ram+0x4000+i*8] = i<<30 | dev | 1
		m.words[ram+0x5000+i*8] = i<<21 | dev | 1
		m.words[ram+0x7000+i*8] = i<<12 | dev | 3
		addr := uint64(0x80000000) + i<<21
		attr := dev
		if addr >= ram && addr < ram+0x10000000 {
			attr = wb | xn
		}
		if addr == ram {
			attr = wb
		}
		m.words[ram+0x6000+i*8] = addr | attr | 1
		attr = wb
		if i >= 256 {
			attr |= xn
		}
		m.words[ram+0x8000+i*8] = (ram + 2<<20 + i<<12) | attr | 3
	}
	m.words[ram+0x4000] = ram + 0x5000 | 3
	m.words[ram+0x4010] = ram + 0x6000 | 3
	m.words[ram+0x5000] = ram + 0x7000 | 3
	m.words[ram+0x7000] = 0
	m.words[ram+0x6000+33*8] = ram + 0x8000 | 3
	return m
}
func (m *tableModel) State() (CPUState, error) { m.states++; return m.cpu, nil }
func (m *tableModel) TableWord(address uint64) (uint64, error) {
	m.reads++
	if m.reads == m.readError {
		return 0, errors.New("read failed")
	}
	v, ok := m.words[address]
	if !ok {
		panic("read outside fixture table pages")
	}
	if m.afterRead != nil {
		m.afterRead(m, address)
	}
	return v, nil
}
func (m *tableModel) Translate(address uint64, write bool) (uint64, error) {
	m.probes++
	if m.probes == m.probeError {
		return 0, errors.New("AT failed")
	}
	d := m.words[m.cpu.TTBR0+((address>>30)&511)*8]
	level := 1
	if d&3 == 3 {
		level = 2
		d = m.words[(d&pageMask)+((address>>21)&511)*8]
	}
	if d&3 == 3 && level == 2 {
		level = 3
		d = m.words[(d&pageMask)+((address>>12)&511)*8]
	}
	if d&1 == 0 || d&(1<<10) == 0 || write && d&(2<<6) != 0 {
		return 1, nil
	}
	shift := uint(30)
	if level == 2 {
		shift = 21
	}
	if level == 3 {
		shift = 12
	}
	pa := (d&pageMask)&^((1<<shift)-1) | address&((1<<shift)-1)
	attr := (m.cpu.MAIR >> (8 * ((d >> 2) & 7))) & 255
	share := (d >> 8) & 3
	if attr == 0 || attr == 0x44 {
		share = 2
	}
	if m.stale && m.writes > 0 && address >= 0x94800000 && address < 0x95000000 {
		attr = 0
		share = 2
	}
	return (pa & pageMask) | attr<<56 | share<<7, nil
}
func (m *tableModel) SetDMAAttribute() error {
	m.sets++
	if m.setError {
		return errors.New("MAIR failed")
	}
	m.cpu.MAIR = 0x44ff00
	return nil
}
func (m *tableModel) Publish(address, value uint64) error {
	m.writes++
	if m.writes == m.writeError {
		if m.afterWriteError {
			m.words[address] = value
		}
		return errors.New("publication failed")
	}
	m.words[address] = value
	return nil
}
func dmaRange() Range { return Range{0x94800000, 8 << 20, Uncached} }
func inspector(t *testing.T, m *tableModel, ranges []Range) Observation {
	t.Helper()
	v, e := Inspect(m, 0x84000000, 256<<20, ranges)
	if e != nil {
		t.Fatal(e)
	}
	return v
}

func TestLiveHeapDeviceAndDMAUseActualReadWriteTranslations(t *testing.T) {
	m := tables()
	before := inspector(t, m, []Range{{0x84000000, 256 << 20, WriteBack}, {0x20010400, 256, Device}})
	if before.Bytes != (256<<20)+256 || before.TableReads == 0 || before.Translations == 0 || before.DescriptorSHA256 == [32]byte{} {
		t.Fatal(before)
	}
	mapper, e := NewMapper(m, 0x84000000, 256<<20)
	if e != nil {
		t.Fatal(e)
	}
	dma, e := mapper.Map(dmaRange().Base, dmaRange().Bytes)
	if e != nil {
		t.Fatal(e)
	}
	if dma.CPU.MAIR != 0x44ff00 || m.sets != 1 || m.writes != 8 {
		t.Fatal(dma, m.sets, m.writes)
	}
	if _, e = mapper.Inspect([]Range{{0x84000000, 256 << 20, WriteBack}, {0x20010400, 256, Device}, dmaRange()}); e != nil {
		t.Fatal(e)
	}
	if _, e = mapper.Map(0x95000000, 2<<20); e != nil {
		t.Fatal(e)
	}
	if m.sets != 1 || m.writes != 10 {
		t.Fatal("global attribute was reinitialized", m.sets, m.writes)
	}
}

func TestUnsupportedCPUStateRejectsBeforeAnyTableOrTargetAccess(t *testing.T) {
	for _, change := range []func(*CPUState){
		func(s *CPUState) { s.EL = 8 }, func(s *CPUState) { s.SCTLR &^= 1 }, func(s *CPUState) { s.SCTLR &^= 4 }, func(s *CPUState) { s.SCTLR |= 2 },
		func(s *CPUState) { s.SCTLR |= 1 << 24 }, func(s *CPUState) { s.SCTLR |= 1 << 25 }, func(s *CPUState) { s.SCTLR |= 1 << 19 },
		func(s *CPUState) { s.TCR ^= 1 << 14 }, func(s *CPUState) { s.TCR ^= 1 << 32 }, func(s *CPUState) { s.TTBR0 += 4096 },
		func(s *CPUState) { s.TTBR0 |= 1 << 48 }, func(s *CPUState) { s.MAIR = 0x44ffff }, func(s *CPUState) { s.MMFR0 = 1 }, func(s *CPUState) { s.MMFR0 |= 15 << 28 },
	} {
		m := tables()
		change(&m.cpu)
		if v, e := Inspect(m, 0x84000000, 256<<20, []Range{{0x20010400, 256, Device}}); e == nil || v != (Observation{}) || m.reads != 0 || m.probes != 0 {
			t.Fatal(v, e, m.reads, m.probes)
		}
	}
}

func TestCompleteWindowUsesBothUncachedBlocksAndRejectsTheSecondBlockChanging(t *testing.T) {
	for fault := 0; fault < 3; fault++ {
		m := tables()
		mapper, err := NewMapper(m, 0x84000000, 256<<20)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = mapper.Map(0x95000000, 4<<20); err != nil {
			t.Fatal(err)
		}
		if m.sets != 1 || m.writes != 4 {
			t.Fatal("the complete arena did not publish both blocks", m.sets, m.writes)
		}
		second := uint64(0x84006000) + ((uint64(0x95200000)>>21)&511)*8
		if fault == 1 {
			m.words[second] &^= 7 << 2 // The later half is Device, not Normal uncached.
		} else if fault == 2 {
			m.words[second] ^= 1 << 21 // The later half aliases the first block.
		}
		observation, err := mapper.Inspect([]Range{{0x95000000, 4 << 20, Uncached}})
		if fault != 0 {
			if err == nil || observation != (Observation{}) {
				t.Fatal(fault, observation, err)
			}
			continue
		}
		if err != nil || observation.Bytes != 4<<20 || observation.Translations == 0 {
			t.Fatal(observation, err)
		}
		for _, write := range []bool{false, true} {
			physical, err := m.Translate(0x953fffff, write)
			if err != nil || (physical&pageMask) != 0x953ff000 || physical>>56 != 0x44 {
				t.Fatal("last page lacks the requested mapping", physical, err)
			}
		}
	}
}

func TestUnmappedOrAliasedTablePageIsNeverDereferenced(t *testing.T) {
	for _, bad := range []uint64{0, 0x86000000 | 0x705, 0x84000000 | 0x701, 0x84000000 | 0x785} {
		m := tables()
		m.words[0x84006000+32*8] = bad
		if v, e := Inspect(m, 0x84000000, 256<<20, []Range{{0x20010400, 256, Device}}); e == nil || v != (Observation{}) || m.reads != 0 {
			t.Fatal(bad, v, e, m.reads)
		}
	}
}

func TestDescriptorPermissionAliasUnknownPointersAndAttributesReject(t *testing.T) {
	for _, fault := range []struct{ address, value uint64 }{
		{0x84004000, 0x84009003}, {0x84005000 + 256*8, 0x30000000 | 0x60000000000601},
		{0x84005000 + 256*8, 0x20000000 | 0x60000000000681}, {0x84005000 + 256*8, 0x20000000 | 0x60000000000201},
		{0x84005000 + 256*8, 0x20000000 | 0x601}, {0x84005000 + 256*8, 0x20000000 | 0x60000000000605},
		{0x84005000 + 256*8, 0x84008003}, {0x84005000 + 256*8, 0x20001000 | 0x60000000000601},
	} {
		m := tables()
		m.words[fault.address] = fault.value
		if v, e := Inspect(m, 0x84000000, 256<<20, []Range{{0x20010400, 256, Device}}); e == nil || v != (Observation{}) {
			t.Fatal(fault, v, e)
		}
	}
}

func TestChangedValidSamplesAndCPUOwnershipReject(t *testing.T) {
	for _, state := range []bool{false, true} {
		m := tables()
		m.afterRead = func(m *tableModel, address uint64) {
			if m.reads == 2 {
				if state {
					m.cpu.MPIDR ^= 0x100
				} else {
					m.words[address] ^= xn
				}
			}
		}
		if v, e := Inspect(m, 0x84000000, 256<<20, []Range{{0x84800000, 4096, WriteBack}}); e == nil || v != (Observation{}) {
			t.Fatal(v, e)
		}
	}
}

func TestNoGlobalAttributeChangeWhileSlotIsAlreadyUsedOrUnknownTreeExists(t *testing.T) {
	for _, fault := range []func(*tableModel){
		func(m *tableModel) { m.words[0x84004000+4*8] |= 2 << 2 },
		func(m *tableModel) { m.words[0x84005000+200*8] |= 2 << 2 },
		func(m *tableModel) { m.words[0x84008000+200*8] |= 2 << 2 },
		func(m *tableModel) { m.words[0x84004000+4*8] = 0x84009003 },
		func(m *tableModel) { m.cpu.MAIR = 0x44ff00 },
	} {
		m := tables()
		fault(m)
		mapper, e := NewMapper(m, 0x84000000, 256<<20)
		if e != nil {
			t.Fatal(e)
		}
		if v, e := mapper.Map(dmaRange().Base, dmaRange().Bytes); e == nil || v != (Observation{}) || m.sets != 0 || m.writes != 0 {
			t.Fatal(v, e, m.sets, m.writes)
		}
		reads, probes := m.reads, m.probes
		if _, e := mapper.Map(dmaRange().Base, dmaRange().Bytes); e == nil || m.reads != reads || m.probes != probes {
			t.Fatal("retired mapper retried")
		}
	}
}

func TestReadTranslationAndPublicationErrorsRetireWithoutReplay(t *testing.T) {
	baseline := tables()
	ok, _ := NewMapper(baseline, 0x84000000, 256<<20)
	if _, e := ok.Map(dmaRange().Base, dmaRange().Bytes); e != nil {
		t.Fatal(e)
	}
	for _, read := range []int{1, 2, 16, 128, 512, 513, 1024, 1536, 2048, 4096, baseline.reads} {
		m := tables()
		m.readError = read
		mapper, _ := NewMapper(m, 0x84000000, 256<<20)
		if v, e := mapper.Map(dmaRange().Base, dmaRange().Bytes); e == nil || v != (Observation{}) {
			t.Fatal("read", read, v, e)
		}
		reads, probes, writes := m.reads, m.probes, m.writes
		if _, e := mapper.Inspect([]Range{dmaRange()}); e == nil || m.reads != reads || m.probes != probes || m.writes != writes {
			t.Fatal("read failure replayed")
		}
	}
	for _, probe := range []int{1, 2, 3, 4, 5, 6, 16, baseline.probes} {
		m := tables()
		m.probeError = probe
		mapper, _ := NewMapper(m, 0x84000000, 256<<20)
		if v, e := mapper.Map(dmaRange().Base, dmaRange().Bytes); e == nil || v != (Observation{}) {
			t.Fatal("probe", probe, v, e)
		}
	}
	for write := 1; write <= 8; write++ {
		for _, uncertain := range []bool{false, true} {
			m := tables()
			m.writeError = write
			m.afterWriteError = uncertain
			mapper, _ := NewMapper(m, 0x84000000, 256<<20)
			if v, e := mapper.Map(dmaRange().Base, dmaRange().Bytes); e == nil || v != (Observation{}) || m.writes != write {
				t.Fatal(write, v, e, m.writes)
			}
			if _, e := mapper.Map(dmaRange().Base, dmaRange().Bytes); e == nil || m.writes != write {
				t.Fatal("uncertain publication replayed")
			}
		}
	}
	for _, fault := range []string{"attribute", "stale-tlb"} {
		m := tables()
		m.setError = fault == "attribute"
		m.stale = fault == "stale-tlb"
		mapper, _ := NewMapper(m, 0x84000000, 256<<20)
		if v, e := mapper.Map(dmaRange().Base, dmaRange().Bytes); e == nil || v != (Observation{}) {
			t.Fatal(fault, v, e)
		}
	}
}

func TestRangeBoundsAreValidatedBeforeWalking(t *testing.T) {
	for _, ranges := range [][]Range{nil, {{0, 4, Device}}, {{0x20010400, 0, Device}}, {{0x20010400, 4, 0}},
		{{0x20010400, 4, WriteBack}}, {{0x84000000, 4, Uncached}}, {{1 << 39, 4, Device}}, {{1<<39 - 4, 8, Device}},
		{{0x20010400, 8, Device}, {0x20010404, 8, Device}}, {{0x20000000, 1 << 30, Device}, {0x90000000, 1 << 30, Device}},
	} {
		m := tables()
		if v, e := Inspect(m, 0x84000000, 256<<20, ranges); e == nil || v != (Observation{}) || m.reads != 0 || m.probes != 0 {
			t.Fatal(ranges, v, e, m.reads, m.probes)
		}
	}
}

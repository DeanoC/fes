// SPDX-License-Identifier: GPL-3.0-or-later
package arm64map

import "testing"

func excludedTables() *tableModel {
	m := tables()
	for address := FirmwareRAMBase; address < FirmwareRAMBase+FirmwareRAMBytes; address += 4096 {
		m.words[0x84007000+(address>>12)*8] = 0
	}
	return m
}

func protectedMapper(t *testing.T, m Mutation) *Mapper {
	t.Helper()
	v, err := NewMapper(m, 0x84000000, 256<<20)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = v.RequireFirmwareExclusion(); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestFirmwareExclusionRejectsUnremovedPagesAndCannotBeReplayed(t *testing.T) {
	for _, removed := range []bool{false, true} {
		m := tables()
		if removed {
			m = excludedTables()
		}
		mapper, _ := NewMapper(m, 0x84000000, 256<<20)
		v, err := mapper.RequireFirmwareExclusion()
		if (err == nil) != removed || m.writes != 0 || m.sets != 0 {
			t.Fatal(removed, v, err, m.writes, m.sets)
		}
		if removed && (v.Bytes != FirmwareRAMBytes || v.Translations == 0 || v.DescriptorSHA256 == [32]byte{}) {
			t.Fatal(v)
		}
		reads, probes, states := m.reads, m.probes, m.states
		if _, err = mapper.RequireFirmwareExclusion(); err == nil || !mapper.failed {
			t.Fatal("exclusion replay accepted", err)
		}
		if _, err = mapper.Map(dmaRange().Base, dmaRange().Bytes); err == nil || m.reads != reads || m.probes != probes || m.states != states || m.writes != 0 {
			t.Fatal("retired mapper observed or wrote", err)
		}
	}
}

func TestFirmwareExclusionIsRequiredBeforeAttributeChangeAndPreservedAroundDMA(t *testing.T) {
	m := excludedTables()
	legacy, _ := NewMapper(m, 0x84000000, 256<<20)
	if _, err := legacy.Map(dmaRange().Base, dmaRange().Bytes); err == nil || m.sets != 0 || m.writes != 0 {
		t.Fatal("unselected low holes accepted", err)
	}
	m = tables()
	late, _ := NewMapper(m, 0x84000000, 256<<20)
	if _, err := late.Map(dmaRange().Base, dmaRange().Bytes); err != nil {
		t.Fatal(err)
	}
	if _, err := late.RequireFirmwareExclusion(); err == nil {
		t.Fatal("late exclusion accepted")
	}
	m = excludedTables()
	mapper := protectedMapper(t, m)
	v, err := mapper.Map(dmaRange().Base, dmaRange().Bytes)
	if err != nil || v.Bytes != dmaRange().Bytes+FirmwareRAMBytes || m.writes != 8 || m.sets != 1 {
		t.Fatal(v, err, m.writes, m.sets)
	}
	ranges := make([]Range, 1, 2)
	ranges[0] = dmaRange()
	ranges[:2][1] = Range{123, 456, Device}
	if _, err = mapper.Inspect(ranges); err != nil || ranges[:2][1].Base != 123 {
		t.Fatal("caller slice changed", err, ranges[:2])
	}
	m.words[0x84007000+(FirmwareRAMBase>>12)*8] = FirmwareRAMBase | 0x60000000000603
	if _, err = mapper.Inspect([]Range{dmaRange()}); err == nil || !mapper.failed {
		t.Fatal("restored firmware access was accepted", err)
	}
}

func TestEveryFirmwarePageAndReadWriteBoundaryMustRemainUnmapped(t *testing.T) {
	for page := uint64(0); page < FirmwareRAMBytes/4096; page++ {
		address := FirmwareRAMBase + page*4096
		m := excludedTables()
		m.words[0x84007000+(address>>12)*8] = address | 0x60000000000603
		mapper, _ := NewMapper(m, 0x84000000, 256<<20)
		if _, err := mapper.RequireFirmwareExclusion(); err == nil || m.writes != 0 || m.sets != 0 {
			t.Fatal("mapped firmware page accepted", page, err)
		}
		for _, edge := range []uint64{0, 4095} {
			for _, write := range []bool{false, true} {
				for _, stale := range []uint64{0, 1, 1 | 6<<1, 1 | 0x10<<1, 15 | 1<<8, 15 | 1<<9} {
					m = excludedTables()
					m.probeOverride = func(a uint64, w bool) (uint64, bool) {
						return stale, a == address+edge && w == write
					}
					mapper, _ = NewMapper(m, 0x84000000, 256<<20)
					if _, err := mapper.RequireFirmwareExclusion(); err == nil || m.writes != 0 || m.sets != 0 {
						t.Fatal("stale translation or unknown fault accepted", page, edge, write, stale)
					}
				}
			}
		}
	}
}

func TestFirmwareExclusionReadProbeAndStateFailuresRetireWithoutMutation(t *testing.T) {
	control := excludedTables()
	if _, err := Inspect(control, 0x84000000, 256<<20, []Range{firmwareRAMRange()}); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"read", "probe", "state"} {
		limit := control.reads
		if kind == "probe" {
			limit = control.probes
		}
		if kind == "state" {
			limit = 1
		}
		for at := 1; at <= limit; at++ {
			m := excludedTables()
			switch kind {
			case "read":
				m.readError = at
			case "probe":
				m.probeError = at
			case "state":
				m.afterRead = func(m *tableModel, _ uint64) { m.cpu.TCR ^= 1 }
			}
			mapper, _ := NewMapper(m, 0x84000000, 256<<20)
			if _, err := mapper.RequireFirmwareExclusion(); err == nil || !mapper.failed || m.writes != 0 || m.sets != 0 {
				t.Fatal(kind, at, err)
			}
		}
	}
}

func TestFirmwareExclusionRejectsAliasesAtEveryTreeLevel(t *testing.T) {
	for _, address := range []uint64{0x84004000 + 3*8, 0x84005000 + 100*8, 0x84006000 + 400*8, 0x84007000 + 0x61*8, 0x84008000 + 17*8} {
		m := excludedTables()
		original := m.words[address]
		// Preserve the leaf's original permissions/type; alias low physical
		// memory through an otherwise permitted virtual address.
		m.words[address] = original &^ pageMask
		if address >= 0x84007000 {
			m.words[address] |= FirmwareRAMBase
		}
		mapper, _ := NewMapper(m, 0x84000000, 256<<20)
		if _, err := mapper.RequireFirmwareExclusion(); err == nil || !mapper.failed || m.writes != 0 || m.sets != 0 {
			t.Fatal("aliased firmware tree accepted", address, err)
		}
	}
}

type restoreDuringPublish struct{ *tableModel }

func (m restoreDuringPublish) Publish(address, value uint64) error {
	err := m.tableModel.Publish(address, value)
	m.words[0x84007000+(FirmwareRAMBase>>12)*8] = FirmwareRAMBase | 0x60000000000603
	return err
}

func TestLostExclusionAfterDMAPublicationCannotReportSuccess(t *testing.T) {
	m := restoreDuringPublish{excludedTables()}
	mapper := protectedMapper(t, m)
	if _, err := mapper.Map(dmaRange().Base, dmaRange().Bytes); err == nil || !mapper.failed || m.writes != 8 {
		t.Fatal("lost exclusion published success", err, m.writes)
	}
	writes, reads, probes := m.writes, m.reads, m.probes
	if _, err := mapper.Map(0x95000000, 2<<20); err == nil || m.writes != writes || m.reads != reads || m.probes != probes {
		t.Fatal("uncertain mapper retried", err)
	}
}

func TestFirmwareInspectionCannotGrantArbitraryMissingMappings(t *testing.T) {
	m := excludedTables()
	for _, r := range []Range{{FirmwareRAMBase, 4096, Inaccessible}, {0x4f000, FirmwareRAMBytes, Inaccessible}, {0x60000, FirmwareRAMBytes, Inaccessible}} {
		if _, err := Inspect(m, 0x84000000, 256<<20, []Range{r}); err == nil {
			t.Fatal("arbitrary exclusion accepted", r)
		}
	}
	if _, err := Inspect(m, 0x84000000, 256<<20, []Range{{FirmwareRAMBase, FirmwareRAMBytes, Device}}); err == nil {
		t.Fatal("unmapped firmware pages accepted as device memory")
	}
	if _, err := protectedMapper(t, excludedTables()).Inspect(nil); err == nil {
		t.Fatal("empty requested observation replaced by exclusion")
	}
	m = excludedTables()
	mapper := protectedMapper(t, m)
	m.words[0x84007000+0x61*8] = 0
	if _, err := mapper.Map(dmaRange().Base, dmaRange().Bytes); err == nil || m.sets != 0 || m.writes != 0 {
		t.Fatal("unrelated missing page accepted", err)
	}
}

// SPDX-License-Identifier: GPL-3.0-or-later
package arm64map

import (
	"crypto/sha256"
	"errors"
	"sync"
)

// Mutation belongs to the board's one mapper. Publish must store the supplied
// descriptor, clean it to the required coherence point, invalidate EL1 TLBs
// and finish the required barriers before returning. Unknown outcomes reject.
type Mutation interface {
	Access
	SetDMAAttribute() error
	Publish(address, descriptor uint64) error
}

// Mapper serializes the pinned page tables, owns MAIR slot 2 and retires after
// a failed observation or mutation. It supplies no physical memory reservation.
type Mapper struct {
	mu                                  sync.Mutex
	io                                  Mutation
	ramBase, ramSize                    uint64
	attribute, failed                   bool
	firmwareAttempted, firmwareExcluded bool
}

func NewMapper(io Mutation, ramBase, ramSize uint64) (*Mapper, error) {
	if io == nil || !validRAM(ramBase, ramSize) {
		return nil, errors.New("explicit mapper and pinned RAM required")
	}
	return &Mapper{io: io, ramBase: ramBase, ramSize: ramSize}, nil
}

func (m *Mapper) fail(err error) error { m.failed = true; return err }

// RequireFirmwareExclusion checks the early startup removal before the mapper
// may change attributes or publish DMA mappings. It never removes a page or
// grants firmware ownership. Any uncertainty or replay retires this mapper.
func (m *Mapper) RequireFirmwareExclusion() (Observation, error) {
	if m == nil {
		return Observation{}, errors.New("mapper unavailable")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failed || m.firmwareAttempted || m.attribute {
		return Observation{}, m.fail(errors.New("firmware exclusion must precede device mapping exactly once"))
	}
	m.firmwareAttempted = true
	v, err := Inspect(m.io, m.ramBase, m.ramSize, []Range{firmwareRAMRange()})
	if err != nil {
		return Observation{}, m.fail(err)
	}
	if v.CPU.MAIR != 0xff00 {
		return Observation{}, m.fail(errors.New("firmware exclusion requires the initial attribute owner"))
	}
	m.firmwareExcluded = true
	// The canonical hole is insufficient if another VA aliases it. Audit
	// every leaf of the complete pinned tree before accepting this binding.
	if err = m.unusedAttribute(v.CPU); err != nil {
		return Observation{}, m.fail(err)
	}
	return v, nil
}

// Called only under the mapper lock. Every later observation includes the
// exclusion, without changing the caller-owned range slice.
func (m *Mapper) inspect(ranges []Range) (Observation, error) {
	if len(ranges) == 0 {
		return Observation{}, errors.New("explicit mapping ranges required")
	}
	if m.firmwareExcluded {
		found := false
		for _, r := range ranges {
			found = found || r == firmwareRAMRange()
		}
		if !found {
			ranges = append(append([]Range(nil), ranges...), firmwareRAMRange())
		}
	}
	return Inspect(m.io, m.ramBase, m.ramSize, ranges)
}

// Before the first global MAIR change, traverse the entire bounded pinned
// tree twice and prove that no active descriptor uses the previously unused
// slot. Unexpected table pointers are never followed.
func (m *Mapper) unusedAttribute(state CPUState) error {
	var previous [32]byte
	for pass := 0; pass < 2; pass++ {
		s := inspection{io: m.io, ramBase: m.ramBase, state: state, digest: sha256.New()}
		if err := s.stateOK(); err != nil {
			return err
		}
		for index := uint64(0); index < 512; index++ {
			v, err := s.read(state.TTBR0 + index*8)
			if err != nil {
				return err
			}
			if index == 0 || index == m.ramBase>>30 {
				table := m.ramBase + 0x5000
				if index != 0 {
					table += 0x1000
				}
				if v != table|3 {
					return errors.New("unexpected table before global attribute initialization")
				}
			} else if v&3 != 1 || (v>>2)&7 > 1 {
				return errors.New("unknown or active DMA attribute in L1")
			} else if m.firmwareExcluded && v&pageMask&^((1<<30)-1) != index<<30 {
				return errors.New("nonidentity L1 alias in firmware-excluded tree")
			}
		}
		for _, offset := range []uint64{0x5000, 0x6000} {
			seenText := false
			for index := uint64(0); index < 512; index++ {
				v, err := s.read(m.ramBase + offset + index*8)
				if err != nil {
					return err
				}
				if v&3 == 1 {
					if offset == 0x5000 && index == 0 || (v>>2)&7 > 1 {
						return errors.New("unknown or active DMA attribute in L2")
					}
					physical := index << 21
					if offset == 0x6000 {
						physical += m.ramBase &^ ((1 << 30) - 1)
					}
					if m.firmwareExcluded && v&pageMask&^((2<<20)-1) != physical {
						return errors.New("nonidentity L2 alias in firmware-excluded tree")
					}
					continue
				}
				table := m.ramBase + 0x7000
				if offset == 0x5000 {
					if index != 0 {
						return errors.New("unexpected low L3 table")
					}
				} else {
					if seenText {
						return errors.New("duplicate text L3 table")
					}
					seenText = true
					table += 0x1000
				}
				if v != table|3 {
					return errors.New("unexpected L3 pointer before global attribute initialization")
				}
				for page := uint64(0); page < 512; page++ {
					v, err := s.read(table + page*8)
					if err != nil {
						return err
					}
					if offset == 0x5000 && (page == 0 || m.firmwareExcluded && page >= FirmwareRAMBase>>12 && page < (FirmwareRAMBase+FirmwareRAMBytes)>>12) {
						if v != 0 {
							return errors.New("protected low page differs from required invalid mapping")
						}
					} else if v&3 != 3 || (v>>2)&7 > 1 {
						return errors.New("unknown or active DMA attribute in L3")
					} else if m.firmwareExcluded {
						physical := page << 12
						if offset == 0x6000 {
							physical += (m.ramBase &^ ((1 << 30) - 1)) + (index << 21)
						}
						if v&pageMask != physical {
							return errors.New("nonidentity L3 alias in firmware-excluded tree")
						}
					}
				}
			}
		}
		if err := s.stateOK(); err != nil {
			return err
		}
		var digest [32]byte
		copy(digest[:], s.digest.Sum(nil))
		if pass != 0 && digest != previous {
			return errors.New("table tree changed before global attribute initialization")
		}
		previous = digest
	}
	return nil
}

func (m *Mapper) Map(base, size uint64) (Observation, error) {
	if m == nil {
		return Observation{}, errors.New("mapper unavailable")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failed {
		return Observation{}, errors.New("mapper retired until reboot")
	}
	if err := Validate(base, size, m.ramBase, m.ramSize); err != nil {
		return Observation{}, m.fail(err)
	}
	before, err := m.inspect([]Range{{base, size, Device}})
	if err != nil {
		return Observation{}, m.fail(err)
	}
	if !m.attribute {
		if before.CPU.MAIR != 0xff00 {
			return Observation{}, m.fail(errors.New("DMA attribute was initialized by another owner"))
		}
		if err = m.unusedAttribute(before.CPU); err != nil {
			return Observation{}, m.fail(err)
		}
		if err = m.io.SetDMAAttribute(); err != nil {
			return Observation{}, m.fail(err)
		}
		expected := before.CPU
		expected.MAIR = 0x44ff00
		actual, e := m.io.State()
		if e != nil {
			return Observation{}, m.fail(e)
		}
		if actual != expected {
			return Observation{}, m.fail(errors.New("DMA attribute update/readback changed CPU state"))
		}
		m.attribute = true
	} else if before.CPU.MAIR != 0x44ff00 {
		return Observation{}, m.fail(errors.New("owned DMA attribute changed"))
	}
	// Validate every target again before the first invalidation. No unsupported
	// descriptor late in the range can cause a partial successful conversion.
	type change struct{ address, old, value uint64 }
	changes := make([]change, 0, size/(2<<20))
	for address := base; address < base+size; address += 2 << 20 {
		entry := m.ramBase + 0x6000 + ((address>>21)&511)*8
		old, e := m.io.TableWord(entry)
		if e != nil {
			return Observation{}, m.fail(e)
		}
		if old != address|1<<10|2<<8|xn|1 {
			return Observation{}, m.fail(errors.New("DMA source mapping changed before conversion"))
		}
		changes = append(changes, change{entry, old, address | 1<<10 | 2<<8 | 2<<2 | xn | 1})
	}
	state, e := m.io.State()
	expected := before.CPU
	expected.MAIR = 0x44ff00
	if e != nil {
		return Observation{}, m.fail(e)
	}
	if state != expected {
		return Observation{}, m.fail(errors.New("CPU state changed before mapping publication"))
	}
	for _, c := range changes {
		v, e := m.io.TableWord(c.address)
		if e != nil {
			return Observation{}, m.fail(e)
		}
		if v != c.old {
			return Observation{}, m.fail(errors.New("descriptor changed before break-before-make"))
		}
		if e = m.io.Publish(c.address, 0); e != nil {
			return Observation{}, m.fail(e)
		}
		if e = m.io.Publish(c.address, c.value); e != nil {
			return Observation{}, m.fail(e)
		}
	}
	observed, err := m.inspect([]Range{{base, size, Uncached}})
	if err != nil {
		return Observation{}, m.fail(err)
	}
	return observed, nil
}

func (m *Mapper) Inspect(ranges []Range) (Observation, error) {
	if m == nil {
		return Observation{}, errors.New("mapper unavailable")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failed {
		return Observation{}, errors.New("mapper retired until reboot")
	}
	v, err := m.inspect(ranges)
	if err != nil {
		return Observation{}, m.fail(err)
	}
	return v, nil
}

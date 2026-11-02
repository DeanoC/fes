// SPDX-License-Identifier: GPL-3.0-or-later
package arm64map

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
)

// This is the pinned TamaGo ARM64 layout: 39-bit VA, 4 KiB granules,
// 40-bit output addresses, and five reserved tables before the linked image.
const (
	pinnedTCR = uint64(2<<32 | 0x3519)
	pageMask  = uint64(0x000000fffffff000)
	xn        = uint64(3 << 53)
	maxReads  = 8192
)

type Kind uint8

const (
	Device Kind = iota + 1
	WriteBack
	Uncached
)

// CPUState is read synchronously on the calling CPU. EL contains raw CurrentEL.
// This contract covers EL1 translation. The matched boot must separately prove
// stage 2 disabled, physical RAM ownership and exclusive CPU/device ownership.
type CPUState struct {
	EL, SCTLR, TCR, TTBR0, MAIR, MMFR0, MPIDR uint64
}

// Access queries translations without reading or writing the target range.
// TableWord is called only for pinned reserved table pages after the CPU has
// confirmed their identity mapping and ordinary cached read/write attributes.
// Calls must be serialized with every managed page-table modification.
type Access interface {
	State() (CPUState, error)
	TableWord(address uint64) (uint64, error)
	Translate(address uint64, write bool) (uint64, error)
}

type Range struct {
	Base, Bytes uint64
	Kind        Kind
}

// Observation is a read-only mapping result, not a boot or programming grant.
type Observation struct {
	CPU                      CPUState
	DescriptorSHA256         [32]byte
	Bytes                    uint64
	TableReads, Translations uint32
}

func validRAM(base, size uint64) bool {
	return base >= 1<<30 && base < 1<<39 && size >= 128<<10 && size <= 1<<30 &&
		base%(2<<20) == 0 && size%(2<<20) == 0 && size <= 1<<39-base &&
		base>>30 == (base+size-1)>>30
}

func checkCPU(v CPUState, ramBase uint64) error {
	// MAIR indices 0/1 are the pinned Device/WB attributes. Index 2 is owned
	// by this mapper; all other slots remain unused. Cacheable and uncached
	// mappings both require the actual MMU and data/instruction caches enabled.
	if v.EL != 4 || v.SCTLR&0x1005 != 0x1005 || v.SCTLR&(3<<24|1<<19|1<<1) != 0 ||
		v.TCR != pinnedTCR || v.TTBR0 != ramBase+0x4000 ||
		(v.MAIR != 0xff00 && v.MAIR != 0x44ff00) ||
		v.MMFR0&15 < 2 || v.MMFR0&15 > 6 || (v.MMFR0>>28)&15 > 1 {
		return errors.New("unsupported live EL1 translation state")
	}
	return nil
}

func kindAttributes(k Kind) (descriptor, attribute, share uint64, err error) {
	switch k {
	case Device:
		return 1<<10 | 2<<8 | xn, 0, 2, nil
	case WriteBack:
		return 1<<10 | 3<<8 | 1<<2, 0xff, 3, nil
	case Uncached:
		return 1<<10 | 2<<8 | 2<<2 | xn, 0x44, 2, nil
	default:
		return 0, 0, 0, errors.New("unspecified mapping attributes")
	}
}

type inspection struct {
	io            Access
	ramBase       uint64
	state         CPUState
	pages         map[uint64]bool
	digest        hash.Hash
	reads, probes uint32
}

func (s *inspection) stateOK() error {
	v, err := s.io.State()
	if err != nil {
		return err
	}
	if v != s.state {
		return errors.New("CPU translation state changed during observation")
	}
	return nil
}

func (s *inspection) translation(address uint64, k Kind) error {
	_, attribute, share, err := kindAttributes(k)
	if err != nil {
		return err
	}
	for _, write := range []bool{false, true} {
		if s.probes >= maxReads {
			return errors.New("translation probe bound exceeded")
		}
		par, err := s.io.Translate(address, write)
		s.probes++
		if err != nil {
			return err
		}
		// PAR.NS and implementation-defined/reserved fields are not authority.
		// Compare the output page, resulting memory type, shareability and fault.
		if par&1 != 0 || par&pageMask != address&pageMask || par>>56 != attribute || (par>>7)&3 != share {
			return fmt.Errorf("CPU translation differs at %#x (write=%t PAR=%#x)", address, write, par)
		}
	}
	return nil
}

func (s *inspection) read(address uint64) (uint64, error) {
	page := address &^ uint64(4095)
	if address&7 != 0 || page < s.ramBase+0x4000 || page > s.ramBase+0x8000 || s.reads >= maxReads {
		return 0, errors.New("page-table read outside pinned reserved pages or bound")
	}
	if !s.pages[page] {
		if err := s.translation(page, WriteBack); err != nil {
			return 0, err
		}
		s.pages[page] = true
	}
	v, err := s.io.TableWord(address)
	s.reads++
	if err != nil {
		return 0, err
	}
	var raw [16]byte
	binary.LittleEndian.PutUint64(raw[:8], address)
	binary.LittleEndian.PutUint64(raw[8:], v)
	_, _ = s.digest.Write(raw[:])
	return v, nil
}

func (s *inspection) leaf(address uint64, k Kind) (uint64, error) {
	index := address >> 30
	var table uint64
	switch index {
	case 0:
		table = s.ramBase + 0x5000
	case s.ramBase >> 30:
		table = s.ramBase + 0x6000
	default:
		return 0, errors.New("range outside the pinned L1 table sections")
	}
	l1, err := s.read(s.state.TTBR0 + index*8)
	if err != nil {
		return 0, err
	}
	if l1 != table|3 {
		return 0, errors.New("L1 table descriptor differs from pinned ownership")
	}
	index = (address >> 21) & 511
	descriptor, err := s.read(table + index*8)
	if err != nil {
		return 0, err
	}
	span, kind := uint64(2<<20), uint64(1)
	if descriptor&3 == 3 {
		var expected uint64
		if table == s.ramBase+0x5000 && index == 0 && k == Device {
			expected = s.ramBase + 0x7000
		} else if table == s.ramBase+0x6000 && k == WriteBack {
			expected = s.ramBase + 0x8000
		} else {
			return 0, errors.New("unexpected L3 table in reserved mapping")
		}
		if descriptor != expected|3 {
			return 0, errors.New("L3 table descriptor differs from pinned ownership")
		}
		descriptor, err = s.read(expected + ((address>>12)&511)*8)
		if err != nil {
			return 0, err
		}
		span, kind = 4096, 3
	}
	attributes, _, _, err := kindAttributes(k)
	if err != nil {
		return 0, err
	}
	if k == Uncached && (s.state.MAIR>>16)&255 != 0x44 {
		return 0, errors.New("uncached attribute slot is absent")
	}
	addressBits := pageMask &^ (span - 1)
	withoutXN := descriptor
	if k == WriteBack {
		if descriptor&xn != 0 && descriptor&xn != xn {
			return 0, errors.New("inconsistent heap execute permissions")
		}
		withoutXN &^= xn
	}
	if withoutXN != (address&addressBits)|attributes|kind {
		return 0, fmt.Errorf("identity/attribute/permission descriptor differs at %#x (%#x)", address, descriptor)
	}
	return span, nil
}

// Inspect checks two complete descriptor samples and hardware read/write
// translations at both boundaries of every leaf touched by each range.
// It neither changes a mapping nor reads/writes the requested target memory.
func Inspect(io Access, ramBase, ramSize uint64, ranges []Range) (Observation, error) {
	if io == nil || !validRAM(ramBase, ramSize) || len(ranges) == 0 || len(ranges) > 16 {
		return Observation{}, errors.New("explicit pinned RAM and bounded mapping ranges required")
	}
	state, err := io.State()
	if err != nil {
		return Observation{}, err
	}
	if err = checkCPU(state, ramBase); err != nil {
		return Observation{}, err
	}
	var total uint64
	for index, r := range ranges {
		if r.Bytes == 0 || r.Base == 0 || r.Base >= 1<<39 || r.Bytes > 1<<39-r.Base || r.Bytes > 1<<30 {
			return Observation{}, errors.New("mapping range exceeds address or byte bound")
		}
		if _, _, _, err = kindAttributes(r.Kind); err != nil {
			return Observation{}, err
		}
		if r.Kind == WriteBack && (r.Base < ramBase || r.Base+r.Bytes > ramBase+ramSize) {
			return Observation{}, errors.New("cached mapping lies outside owned Go RAM")
		}
		if r.Kind != WriteBack && r.Base < ramBase+ramSize && ramBase < r.Base+r.Bytes {
			return Observation{}, errors.New("device or uncached mapping overlaps Go RAM")
		}
		if total > 1<<30-r.Bytes {
			return Observation{}, errors.New("aggregate mapping byte bound exceeded")
		}
		for _, earlier := range ranges[:index] {
			if r.Base < earlier.Base+earlier.Bytes && earlier.Base < r.Base+r.Bytes {
				return Observation{}, errors.New("mapping ranges overlap")
			}
		}
		total += r.Bytes
	}
	result := Observation{CPU: state, Bytes: total}
	for pass := 0; pass < 2; pass++ {
		s := inspection{io: io, ramBase: ramBase, state: state, pages: make(map[uint64]bool), digest: sha256.New()}
		for _, r := range ranges {
			for address := r.Base; address < r.Base+r.Bytes; {
				if err = s.stateOK(); err != nil {
					return Observation{}, err
				}
				span, err := s.leaf(address, r.Kind)
				if err != nil {
					return Observation{}, err
				}
				next := (address &^ (span - 1)) + span
				if next > r.Base+r.Bytes {
					next = r.Base + r.Bytes
				}
				if err = s.translation(address, r.Kind); err != nil {
					return Observation{}, err
				}
				if err = s.translation(next-1, r.Kind); err != nil {
					return Observation{}, err
				}
				if err = s.stateOK(); err != nil {
					return Observation{}, err
				}
				address = next
			}
		}
		var digest [32]byte
		copy(digest[:], s.digest.Sum(nil))
		if pass == 0 {
			result.DescriptorSHA256 = digest
		} else if digest != result.DescriptorSHA256 {
			return Observation{}, errors.New("page-table samples differ")
		}
		result.TableReads += s.reads
		result.Translations += s.probes
	}
	return result, nil
}

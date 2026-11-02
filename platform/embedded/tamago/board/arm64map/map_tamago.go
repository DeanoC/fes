//go:build tamago && arm64

// SPDX-License-Identifier: GPL-3.0-or-later
package arm64map

import (
	"errors"
	"sync"
	"sync/atomic"
	"unsafe"
)

type nativeAccess struct{}

func (nativeAccess) State() (CPUState, error) {
	v := cpuState()
	return CPUState{v[0], v[1], v[2], v[3], v[4], v[5], v[6]}, nil
}
func (nativeAccess) TableWord(address uint64) (uint64, error) {
	return atomic.LoadUint64((*uint64)(unsafe.Pointer(uintptr(address)))), nil
}
func (nativeAccess) Translate(address uint64, write bool) (uint64, error) {
	if write {
		return translateWrite(address), nil
	}
	return translateRead(address), nil
}
func (nativeAccess) SetDMAAttribute() error { setUncachedAttribute(); return nil }
func (nativeAccess) Publish(address, descriptor uint64) error {
	atomic.StoreUint64((*uint64)(unsafe.Pointer(uintptr(address))), descriptor)
	publishTableEntry(uintptr(address))
	return nil
}

var nativeMu sync.Mutex
var nativeMapper *Mapper

func mapper(ramBase, ramSize uint64) (*Mapper, error) {
	nativeMu.Lock()
	defer nativeMu.Unlock()
	if nativeMapper == nil {
		var err error
		nativeMapper, err = NewMapper(nativeAccess{}, ramBase, ramSize)
		if err != nil {
			return nil, err
		}
	}
	if nativeMapper.ramBase != ramBase || nativeMapper.ramSize != ramSize {
		return nil, errors.New("live mapper RAM ownership changed")
	}
	return nativeMapper, nil
}

// NormalUncached converts only checked, unused identity-mapped Device blocks
// outside Go RAM. It proves MAIR slot ownership before changing that slot,
// follows break-before-make and verifies actual read/write translations after
// publication. Failure retires this boot's mapper; there is no retry or remap.
func NormalUncached(base, size, ramBase, ramSize uint64) error {
	m, err := mapper(ramBase, ramSize)
	if err != nil {
		return err
	}
	_, err = m.Map(base, size)
	return err
}

// Verify checks live CPU state, two page-table samples and actual read/write
// translations under the same lock used for all managed mapping changes.
func Verify(ramBase, ramSize uint64, ranges []Range) (Observation, error) {
	m, err := mapper(ramBase, ramSize)
	if err != nil {
		return Observation{}, err
	}
	return m.Inspect(ranges)
}

func cpuState() (values [7]uint64)
func translateRead(address uint64) (par uint64)
func translateWrite(address uint64) (par uint64)
func setUncachedAttribute()
func publishTableEntry(uintptr)

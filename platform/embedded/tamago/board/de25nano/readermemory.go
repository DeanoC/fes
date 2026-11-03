//go:build tamago && arm64 && de25nano

// SPDX-License-Identifier: GPL-3.0-or-later
package de25nano

import (
	"errors"
	"sync"
	"unsafe"

	"github.com/DeanoC/fes/platform/embedded/tamago/board/arm64map"
	"github.com/DeanoC/fes/platform/embedded/tamago/nativefabric"
)

var readerMemoryMu sync.Mutex
var readerMemoryAttempted bool
var readerMemoryRegion *nativefabric.Region

// ReserveReaderMemory uses only the source-derived arena excluded from the
// heap, FIT, configuration DMA and entry staging. It stays owned until reboot,
// including after a failed mapping or caller check. It supplies no Release.
func ReserveReaderMemory(source []byte) (*nativefabric.Region, error) {
	return reserveReaderMemory(source, 2<<20, 8192)
}

// ReserveWindowMemory retains the complete compiled 4 MiB arena through reboot.
// The same single-attempt owner prevents overlap with the fixed-reader API.
func ReserveWindowMemory(source []byte) (*nativefabric.Region, error) {
	return reserveReaderMemory(source, 4<<20, 4<<20)
}

func reserveReaderMemory(source []byte, arenaBytes uint64, sourceBytes int) (*nativefabric.Region, error) {
	readerMemoryMu.Lock()
	defer readerMemoryMu.Unlock()
	if readerMemoryAttempted || profileReaderMemoryBase != 0x95000000 || profileReaderMemoryArenaBytes != arenaBytes || len(source) != sourceBytes {
		return nil, errors.New("DE25: cold reader reservation unavailable")
	}
	readerMemoryAttempted = true
	if err := InitDevices(); err != nil {
		return nil, err
	}
	ranges := []arm64map.Range{{Base: profileReaderMemoryBase, Bytes: profileReaderMemoryArenaBytes, Kind: arm64map.Uncached}}
	before, err := arm64map.Verify(profileRAMStart, profileRAMSize, ranges)
	if err != nil {
		return nil, err
	}
	data := unsafe.Slice((*byte)(unsafe.Pointer(uintptr(profileReaderMemoryBase))), len(source))
	copy(data, source)
	memoryBarrier()
	readerMemoryRegion = &nativefabric.Region{Address: profileReaderMemoryBase, Bytes: data}
	after, err := arm64map.Verify(profileRAMStart, profileRAMSize, ranges)
	if err != nil {
		return nil, err
	}
	if before != after {
		return nil, errors.New("DE25: reader mapping changed while publishing memory")
	}
	return readerMemoryRegion, nil
}

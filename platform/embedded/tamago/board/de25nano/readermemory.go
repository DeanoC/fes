//go:build tamago && arm64 && de25nano

// SPDX-License-Identifier: GPL-3.0-or-later
package de25nano

import (
	"errors"
	"sync"
	"unsafe"

	"github.com/DeanoC/fes/platform/embedded/tamago/nativefabric"
)

var readerMemoryMu sync.Mutex
var readerMemoryAttempted bool
var readerMemoryRegion *nativefabric.Region

// ReserveReaderMemory uses only the source-derived arena excluded from the
// heap, FIT, configuration DMA and entry staging. It stays owned until reboot,
// including after a failed mapping or caller check. It supplies no Release.
func ReserveReaderMemory(source []byte) (*nativefabric.Region, error) {
	readerMemoryMu.Lock()
	defer readerMemoryMu.Unlock()
	if readerMemoryAttempted || profileReaderMemoryBase != 0x95000000 || profileReaderMemoryArenaBytes != 2<<20 || len(source) != 8192 {
		return nil, errors.New("DE25: cold reader reservation unavailable")
	}
	readerMemoryAttempted = true
	if err := InitDevices(); err != nil {
		return nil, err
	}
	data := unsafe.Slice((*byte)(unsafe.Pointer(uintptr(profileReaderMemoryBase))), len(source))
	copy(data, source)
	memoryBarrier()
	readerMemoryRegion = &nativefabric.Region{Address: profileReaderMemoryBase, Bytes: data}
	return readerMemoryRegion, nil
}

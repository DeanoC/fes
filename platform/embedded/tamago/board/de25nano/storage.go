//go:build tamago && arm64 && de25nano

// SPDX-License-Identifier: GPL-3.0-or-later
package de25nano

import (
	"fmt"
	"github.com/DeanoC/fes/platform/embedded/tamago/block"
	"github.com/DeanoC/fes/platform/embedded/tamago/sdcontrol"
	"github.com/DeanoC/fes/platform/embedded/tamago/sdhci"
	"sync"
	"time"
)

type registers struct{}

func load32(addr uintptr) uint32
func store32(addr uintptr, value uint32)
func (registers) Read32(offset uint32) uint32  { return load32(0x10808000 + uintptr(offset)) }
func (registers) Write32(offset, value uint32) { store32(0x10808000+uintptr(offset), value) }
func (registers) Now() time.Time               { return time.Now() }

var storageLock sync.Mutex
var attempted bool

// Retain an unresolved lease even when initialization returns an error.
var storageSession *sdcontrol.Session

// StorageOptions configures opt-in SDHCI throughput modes forwarded through
// sdcontrol. Zero values preserve the conservative 1-bit, single-sector,
// identification-clock PIO transport. ADMA2 stays disabled at this board
// boundary until EL3 BL31 SMMU stream authorization lands.
type StorageOptions struct {
	TransferClockHz uint32
	BusWidth4Bit    bool
	MultiBlock      bool
}

func OpenStorage() (block.Device, error) {
	return OpenStorageWithOptions(StorageOptions{})
}

// OpenStorageWithOptions opens read-only Nano storage with explicit opt-in
// SDHCI clock, 4-bit bus width, and multi-block modes.
func OpenStorageWithOptions(opts StorageOptions) (block.Device, error) {
	return openStorage(sdhci.Identity{}, opts)
}

// OpenWritableStorage is used only by separately bound SD write diagnostics.
// It shares the one-attempt ownership boundary with ordinary read-only access.
func OpenWritableStorage(expected sdhci.Identity) (*sdcontrol.Session, error) {
	return OpenWritableStorageWithOptions(expected, StorageOptions{})
}

// OpenWritableStorageAtClock selects the trusted default-speed experiment.
// Card discovery/PHY preparation and the firmware stop boundary stay slow.
func OpenWritableStorageAtClock(expected sdhci.Identity, transferClockHz uint32) (*sdcontrol.Session, error) {
	return OpenWritableStorageWithOptions(expected, StorageOptions{TransferClockHz: transferClockHz})
}

// OpenWritableStorageWithOptions opens bounded writable Nano storage with
// explicit opt-in SDHCI clock, 4-bit bus width, and multi-block modes.
func OpenWritableStorageWithOptions(expected sdhci.Identity, opts StorageOptions) (*sdcontrol.Session, error) {
	if expected == (sdhci.Identity{}) {
		return nil, fmt.Errorf("de25-nano: expected writable card required")
	}
	return openStorage(expected, opts)
}

func openStorage(writeCard sdhci.Identity, opts StorageOptions) (*sdcontrol.Session, error) {
	if opts.TransferClockHz != 0 && opts.TransferClockHz != 25000000 {
		return nil, fmt.Errorf("de25-nano: unsupported SD transfer clock")
	}
	if !profileSDControlEnabled || profileSDInputClockHz == 0 {
		return nil, fmt.Errorf("de25-nano: SD is disabled by the firmware handoff")
	}
	storageLock.Lock()
	defer storageLock.Unlock()
	if attempted {
		return nil, fmt.Errorf("de25-nano: storage initialization already attempted")
	}
	attempted = true
	var err error
	// DE25-Nano manual revB, Figure 3-28/Table 3-23: microSD CLK/CMD/DAT[3:0]
	// pass through the level translator; no socket write-protect input exists.
	// This board property does not enable writes without the expected identity.
	storageSession, err = sdcontrol.Open(FabricCPUTransport{}, registers{}, sdcontrol.Config{
		InputClockHz:         profileSDInputClockHz,
		TransferClockHz:      opts.TransferClockHz,
		BusWidth4Bit:         opts.BusWidth4Bit,
		MultiBlock:           opts.MultiBlock,
		WriteCard:            writeCard,
		NoSocketWriteProtect: true,
	})
	return storageSession, err
}

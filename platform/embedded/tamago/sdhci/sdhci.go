// SPDX-License-Identifier: GPL-3.0-or-later

// Package sdhci provides read-only, single-block PIO for a firmware-initialized
// SDHC card on the Agilex 5 Cadence controller. It does not initialize its
// clock, ComboPHY, pinmux or card. The firmware must relinquish exclusive use.
package sdhci

import (
	"encoding/binary"
	"fmt"
	"runtime"
	"sync"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/block"
)

// Register offsets follow Altera's MIT-0 socfpga_sdmmc_reg.h, commit
// 66bdcc347564c5698a8997169bce83a31647d618 (Copyright 2026 Altera).
const (
	blockSize    = 0x204
	argument     = 0x208
	command      = 0x20c
	response     = 0x210
	dataPort     = 0x220
	present      = 0x224
	host         = 0x228
	clock        = 0x22c
	status       = 0x230
	statusEnable = 0x234
	signalEnable = 0x238
	statusErrors = uint32(0xffff8000)
)

// Registers supplies ordered 32-bit device accesses relative to controller base.
// Now must be monotonic. Every wait also has a poll budget for a stalled clock.
type Registers interface {
	Read32(uint32) uint32
	Write32(uint32, uint32)
	Now() time.Time
}

type Device struct {
	mu      sync.Mutex
	io      Registers
	sectors uint64
	rca     uint16
	failed  bool
}

// Open adopts a selected, block-addressed SDHC/SDXC card.
// sectors and RCA must come from this boot's firmware card discovery, never a
// guessed example profile. SDSC and eMMC are deliberately unsupported.
func Open(io Registers, sectors uint64, rca uint16) (*Device, error) {
	if io == nil || sectors == 0 || sectors > 1<<32 || rca == 0 {
		return nil, fmt.Errorf("sdhci: invalid SDHC handoff")
	}
	d := &Device{io: io, sectors: sectors, rca: rca}
	// Card-inserted is unreliable on this SoC. Check bus power and live clock,
	// then query the actual selected card instead of treating that bit as proof.
	if io.Read32(host)&(1<<8) == 0 || io.Read32(clock)&7 != 7 {
		return nil, fmt.Errorf("sdhci: firmware clock/power handoff missing")
	}
	io.Write32(signalEnable, 0) // polling only, firmware IRQ ownership released
	io.Write32(statusEnable, 0xffff8033)
	if err := d.ready(); err != nil {
		return nil, err
	}
	return d, nil
}
func (d *Device) SectorCount() uint64 { return d.sectors }
func (d *Device) wait(offset, mask, value uint32, checkErrors bool) error {
	deadline := d.io.Now().Add(500 * time.Millisecond)
	for n := 0; n < 1000000; n++ {
		if checkErrors {
			if s := d.io.Read32(status); s&statusErrors != 0 {
				return fmt.Errorf("sdhci: status error %#x", s)
			}
		}
		if d.io.Read32(offset)&mask == value {
			return nil
		}
		if n&255 == 255 {
			runtime.Gosched()
		}
		if !d.io.Now().Before(deadline) {
			break
		}
	}
	return fmt.Errorf("sdhci: timeout at register %#x", offset)
}
func (d *Device) issue(index uint32, arg uint32, read bool) error {
	if err := d.wait(present, 3, 0, false); err != nil {
		return err
	}
	d.io.Write32(status, 0xffffffff)
	d.io.Write32(argument, arg)
	flags := uint32(0x1a0000) // 48-bit response, CRC and command-index checks
	transfer := uint32(0)
	if read {
		flags |= 1 << 21
		transfer = 1 << 4
		d.io.Write32(blockSize, 1<<16|512)
	}
	d.io.Write32(command, index<<24|flags|transfer)
	if err := d.wait(status, 1, 1, true); err != nil {
		return err
	}
	d.io.Write32(status, 1)
	if r := d.io.Read32(response); r&0xfdffe008 != 0 {
		return fmt.Errorf("sdhci: card rejected CMD%d: %#x", index, r)
	}
	return nil
}
func (d *Device) ready() error {
	if err := d.issue(13, uint32(d.rca)<<16, false); err != nil {
		return err
	}
	r := d.io.Read32(response)
	if r&(1<<8) == 0 || (r>>9)&15 != 4 {
		return fmt.Errorf("sdhci: card not ready in transfer state: %#x", r)
	}
	return nil
}
func (d *Device) ReadSectors(lba uint64, dst []byte) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := block.CheckRange(d.sectors, lba, len(dst)); err != nil {
		return err
	}
	if d.failed {
		return fmt.Errorf("sdhci: prior failure requires firmware reinitialization")
	}
	for n := 0; n < len(dst); n += 512 {
		// Stage each sector until transfer completion; uncertain data never appears
		// as a successful sector. Earlier sectors may have completed on batch error.
		var sector [512]byte
		err := d.issue(17, uint32(lba), true)
		if err == nil {
			err = d.wait(status, 1<<5, 1<<5, true)
		}
		if err == nil {
			for i := 0; i < 512; i += 4 {
				binary.LittleEndian.PutUint32(sector[i:], d.io.Read32(dataPort))
			}
			d.io.Write32(status, 1<<5)
			err = d.wait(status, 2, 2, true)
		}
		if err != nil {
			d.failed = true
			return err
		}
		d.io.Write32(status, 2)
		copy(dst[n:n+512], sector[:])
		lba++
	}
	return nil
}
func (d *Device) WriteSectors(uint64, []byte) error { return block.ErrReadOnly }

// Flush queries readiness; it is not a durability promise for writes (refused).
func (d *Device) Flush() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.failed {
		return fmt.Errorf("sdhci: prior failure requires firmware reinitialization")
	}
	if err := d.ready(); err != nil {
		d.failed = true
		return err
	}
	return nil
}

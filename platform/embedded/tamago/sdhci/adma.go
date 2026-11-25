// SPDX-License-Identifier: GPL-3.0-or-later

package sdhci

import (
	"errors"
	"fmt"

	"github.com/DeanoC/fes/platform/embedded/tamago/block"
	"github.com/DeanoC/fes/platform/embedded/tamago/dmaops"
)

// SDHCI 3.0 ADMA2 register offsets (Cadence SRS block at +0x200) and flags.
const (
	capabilities   = 0x240
	admaError      = 0x254
	admaAddrLow    = 0x258
	admaAddrHigh   = 0x25c
	cap33V         = uint32(1 << 24)
	capHighSpeed   = uint32(1 << 21)
	capADMA2       = uint32(1 << 19)
	cap64BitBus    = uint32(1 << 28)
	host4BitWidth  = uint32(1 << 1)
	hostHighSpeed  = uint32(1 << 2)
	hostADMA2_32   = uint32(2 << 3)
	hostADMA2_64   = uint32(3 << 3)
	admaAttrValid  = uint32(1 << 0)
	admaAttrEnd    = uint32(1 << 1)
	admaAttrTran   = uint32(2 << 4)
	admaChunkBytes = 32768 // 32 KiB per descriptor avoids 64 KiB length==0 quirks
	maxMultiBlocks = 128   // 64 KiB max transfer per CMD18/CMD25 or ADMA2 chain
	admaDescBytes  = 256
)

// admaEngine owns the pinned ADMA2 descriptor table and 64 KiB data staging
// buffer allocated from a caller-supplied dmaops.Pool. On any transfer error or
// timeout both buffers are poisoned and stay permanently pinned until reboot.
type admaEngine struct {
	pool     *dmaops.Pool
	descBuf  *dmaops.Buffer
	dataBuf  *dmaops.Buffer
	use64Bit bool
	hostBits uint32
}

func newADMAEngine(pool *dmaops.Pool, caps uint32) (*admaEngine, error) {
	if pool == nil {
		return nil, fmt.Errorf("sdhci: ADMA2 requires a dmaops.Pool")
	}
	if caps&capADMA2 == 0 {
		return nil, fmt.Errorf("sdhci: controller capabilities (%#x) do not advertise ADMA2", caps)
	}
	descBuf, err := pool.Alloc(admaDescBytes, 64)
	if err != nil {
		return nil, fmt.Errorf("sdhci: allocate ADMA2 descriptor table: %w", err)
	}
	dataBuf, err := pool.Alloc(maxMultiBlocks*block.SectorSize, 64)
	if err != nil {
		_ = descBuf.Free()
		return nil, fmt.Errorf("sdhci: allocate ADMA2 data buffer: %w", err)
	}
	need64 := descBuf.PhysAddr() >= 1<<32 || dataBuf.PhysAddr()+dataBuf.Len() > 1<<32
	if need64 && caps&cap64BitBus == 0 {
		_ = dataBuf.Free()
		_ = descBuf.Free()
		return nil, fmt.Errorf("sdhci: ADMA2 buffers above 4 GiB require 64-bit bus support")
	}
	hostBits := hostADMA2_32
	if need64 {
		hostBits = hostADMA2_64
	}
	return &admaEngine{
		pool:     pool,
		descBuf:  descBuf,
		dataBuf:  dataBuf,
		use64Bit: need64,
		hostBits: hostBits,
	}, nil
}

// buildDescriptors encodes a contiguous SDHCI v3 ADMA2 scatter-gather
// descriptor chain pointing at dataBuf[0:length] in <=32 KiB chunks and syncs
// the table for DMA. 32-bit mode uses 64-bit (8-byte) descriptors; v3 64-bit
// mode uses 96-bit (12-byte) descriptors (attr/len at +0, addr lo at +4, hi at +8).
func (a *admaEngine) buildDescriptors(length uint64) error {
	if length == 0 || length > a.dataBuf.Len() || length%block.SectorSize != 0 {
		return fmt.Errorf("sdhci: invalid ADMA2 transfer length %d", length)
	}
	descStride := uint64(8)
	if a.use64Bit {
		descStride = 12
	}
	var offset uint64
	var descOffset uint64
	for offset < length {
		chunk := length - offset
		if chunk > admaChunkBytes {
			chunk = admaChunkBytes
		}
		attr := admaAttrValid | admaAttrTran
		if offset+chunk == length {
			attr |= admaAttrEnd
		}
		header := attr | (uint32(chunk) << 16)
		addr := a.dataBuf.PhysAddr() + offset
		if err := a.descBuf.Write32(descOffset, header); err != nil {
			return err
		}
		if err := a.descBuf.Write32(descOffset+4, uint32(addr)); err != nil {
			return err
		}
		if a.use64Bit {
			if err := a.descBuf.Write32(descOffset+8, uint32(addr>>32)); err != nil {
				return err
			}
		}
		offset += chunk
		descOffset += descStride
	}
	return a.descBuf.SyncRangeForDevice(0, descOffset, dmaops.ToDevice)
}

func (a *admaEngine) poison(err error) {
	if a == nil {
		return
	}
	a.descBuf.Poison(err)
	a.dataBuf.Poison(err)
}

func (a *admaEngine) close() error {
	if a == nil {
		return nil
	}
	if err := a.descBuf.Err(); err != nil {
		return fmt.Errorf("%w: %v", dmaops.ErrPoisoned, err)
	}
	if err := a.dataBuf.Err(); err != nil {
		return fmt.Errorf("%w: %v", dmaops.ErrPoisoned, err)
	}
	return errors.Join(a.dataBuf.Free(), a.descBuf.Free())
}

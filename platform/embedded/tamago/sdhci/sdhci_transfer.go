// SPDX-License-Identifier: GPL-3.0-or-later

package sdhci

import (
	"encoding/binary"
	"errors"
	"fmt"
	"runtime"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/block"
	"github.com/DeanoC/fes/platform/embedded/tamago/dmaops"
)

func (d *Device) wait(offset, mask, value uint32, checkErrors bool) error {
	if offset == status && value == mask && d.irqActive.Load() {
		return d.waitStatusIRQ(mask, checkErrors)
	}
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

func (d *Device) waitStatusIRQ(mask uint32, checkErrors bool) error {
	check := func() (done bool, err error) {
		s := d.io.Read32(status)
		if checkErrors && s&statusErrors != 0 {
			return true, fmt.Errorf("sdhci: status error %#x", s)
		}
		if s&mask == mask {
			return true, nil
		}
		return false, nil
	}
	if done, err := check(); done {
		return err
	}
	// Fast path: commands and ready FIFO blocks often complete within ~1-2 us,
	// avoiding GIC masking/unmasking and timer setup overhead.
	for range 32 {
		if done, err := check(); done {
			return err
		}
	}
	defer d.io.Write32(signalEnable, 0)
	deadline := d.io.Now().Add(500 * time.Millisecond)
	timer := d.irqTimer
	if timer == nil {
		timer = time.NewTimer(500 * time.Millisecond)
		d.irqTimer = timer
	} else {
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(500 * time.Millisecond)
	}
	defer func() {
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
	}()
	for n := 0; n < 1000000; n++ {
		select {
		case <-d.irqWake:
		default:
		}
		sigMask := mask
		if checkErrors {
			sigMask |= statusErrors
		}
		d.io.Write32(signalEnable, sigMask)
		if done, err := check(); done {
			return err
		}
		if !d.io.Now().Before(deadline) {
			break
		}
		select {
		case <-d.irqWake:
		case <-timer.C:
			if done, err := check(); done {
				return err
			}
			return fmt.Errorf("sdhci: timeout at register %#x", status)
		}
		if done, err := check(); done {
			return err
		}
	}
	return fmt.Errorf("sdhci: timeout at register %#x", status)
}

type responseType uint8

const (
	responseNone responseType = iota
	responseR1
	responseR1Busy
	responseR2
	responseR3
	responseR6
	responseR7
)

type dataDirection uint8

const (
	noData dataDirection = iota
	readData
	writeData
)

func (d *Device) issue(index uint32, arg uint32, kind responseType, data dataDirection) error {
	return d.issueTransfer(index, arg, kind, data, 1, block.SectorSize, false)
}

func (d *Device) issueTransfer(index uint32, arg uint32, kind responseType, data dataDirection, blocks uint32, blockBytes uint32, useDMA bool) error {
	if err := d.wait(present, 3, 0, false); err != nil {
		return err
	}
	d.io.Write32(status, 0xffffffff)
	d.io.Write32(argument, arg)
	flags := uint32(0)
	switch kind {
	case responseR1, responseR6, responseR7:
		flags = 0x1a0000
	case responseR1Busy:
		flags = 0x1b0000
	case responseR2:
		flags = 0x090000 // 136-bit, CRC, no index check
	case responseR3:
		flags = 0x020000 // OCR has no CRC or command index
	}
	transfer := uint32(0)
	if data != noData {
		flags |= 1 << 21
		if data == readData {
			transfer |= 1 << 4
		}
		if blocks > 1 {
			transfer |= (1 << 5) | (1 << 1) // Multi-block + Block Count Enable
			if !d.cmd23 {
				transfer |= 1 << 2 // Auto-CMD12 when CMD23 pre-defined count is not used
			}
		}
		if useDMA {
			transfer |= 1 << 0 // DMA Enable
		}
		d.io.Write32(blockSize, blocks<<16|blockBytes)
	}
	d.io.Write32(command, index<<24|flags|transfer)
	if err := d.wait(status, 1, 1, true); err != nil {
		return err
	}
	d.io.Write32(status, 1)
	if r := d.io.Read32(response); (kind == responseR1 || kind == responseR1Busy) && r&0xfdffe008 != 0 {
		return fmt.Errorf("sdhci: card rejected CMD%d: %#x", index, r)
	}
	if kind == responseR1Busy {
		if err := d.wait(status, 2, 2, true); err != nil {
			return err
		}
		d.io.Write32(status, 2)
	}
	return nil
}

func (d *Device) ready() error {
	if err := d.issue(13, uint32(d.rca)<<16, responseR1, noData); err != nil {
		return err
	}
	r := d.io.Read32(response)
	if r&(1<<8) == 0 || (r>>9)&15 != 4 {
		return fmt.Errorf("sdhci: card not ready in transfer state: %#x", r)
	}
	return nil
}

func (d *Device) modeUnchanged(checkErrors bool) bool {
	if d.io.Read32(host) != d.expectedHost || d.io.Read32(signalEnable) != 0 {
		return false
	}
	if !d.config.UseADMA2 && d.io.Read32(command)&1 != 0 {
		return false
	}
	if d.config.TransferClockHz != 0 && d.io.Read32(clock)&clockModeMask != d.dataClock|7 {
		return false
	}
	if checkErrors && d.io.Read32(status)&statusErrors != 0 {
		return false
	}
	return true
}

func (d *Device) containTransferError(err error, poisonDMA bool) error {
	d.retireLocked()
	if poisonDMA && d.adma != nil {
		d.adma.poison(err)
	}
	// Contain command/data state, but never replay a transfer after a failure.
	d.io.Write32(clock, d.io.Read32(clock)|6<<24)
	resetErr := d.wait(clock, 6<<24, 0, false)
	return errors.Join(err, resetErr)
}

func (d *Device) ReadSectors(lba uint64, dst []byte) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := block.CheckRange(d.sectors, lba, len(dst)); err != nil {
		return err
	}
	if d.failed {
		return ErrRetired
	}
	if d.quiesced || d.stopRequested.Load() {
		return ErrQuiesced
	}
	if (d.config.TransferClockHz != 0 || d.config.BusWidth4Bit || d.config.UseADMA2) && !d.modeUnchanged(false) {
		d.retireLocked()
		return errors.New("sdhci: transfer mode changed before read")
	}
	if d.adma != nil {
		return d.readADMA2Locked(lba, dst)
	}
	if d.config.MultiBlock && len(dst) > block.SectorSize {
		return d.readMultiPIOLocked(lba, dst)
	}
	for n := 0; n < len(dst); n += 512 {
		if d.stopRequested.Load() {
			return ErrQuiesced
		}
		// Stage each sector until transfer completion; uncertain data never appears
		// as a successful sector. Earlier sectors may have completed on batch error.
		var sector [512]byte
		err := d.issue(17, uint32(lba), responseR1, readData)
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
			return d.containTransferError(err, false)
		}
		d.io.Write32(status, 2)
		copy(dst[n:n+512], sector[:])
		d.recordChunkLocked(false, false, false, 1)
		lba++
	}
	if len(dst) > 0 {
		d.stats.readOps++
	}
	return nil
}

func (d *Device) readMultiPIOLocked(lba uint64, dst []byte) error {
	for offset := 0; offset < len(dst); {
		if d.stopRequested.Load() {
			return ErrQuiesced
		}
		blocks := (len(dst) - offset) / block.SectorSize
		if blocks > maxMultiBlocks {
			blocks = maxMultiBlocks
		}
		cmd := uint32(17)
		if blocks > 1 {
			cmd = 18
		}
		usedCMD23 := blocks > 1 && d.cmd23
		chunkBytes := blocks * block.SectorSize
		if len(d.pioScratch) < chunkBytes {
			d.pioScratch = make([]byte, maxMultiBlocks*block.SectorSize)
		}
		staged := d.pioScratch[:chunkBytes]
		var err error
		if usedCMD23 {
			err = d.issue(23, uint32(blocks), responseR1, noData)
		}
		if err == nil {
			err = d.issueTransfer(cmd, uint32(lba), responseR1, readData, uint32(blocks), block.SectorSize, false)
		}
		for b := 0; err == nil && b < blocks; b++ {
			err = d.wait(status, 1<<5, 1<<5, true)
			if err != nil {
				break
			}
			// Clear Buffer Read Ready before draining the block's FIFO words so
			// hardware can assert BRR again as soon as the next block arrives.
			d.io.Write32(status, 1<<5)
			base := b * block.SectorSize
			for i := 0; i < block.SectorSize; i += 4 {
				binary.LittleEndian.PutUint32(staged[base+i:], d.io.Read32(dataPort))
			}
		}
		if err == nil {
			err = d.wait(status, 2, 2, true)
		}
		if err != nil {
			return d.containTransferError(err, false)
		}
		d.io.Write32(status, 2)
		copy(dst[offset:offset+chunkBytes], staged)
		d.recordChunkLocked(false, false, usedCMD23, blocks)
		lba += uint64(blocks)
		offset += chunkBytes
	}
	if len(dst) > 0 {
		d.stats.readOps++
	}
	return nil
}

func (d *Device) readADMA2Locked(lba uint64, dst []byte) error {
	for offset := 0; offset < len(dst); {
		if d.stopRequested.Load() {
			return ErrQuiesced
		}
		blocks := (len(dst) - offset) / block.SectorSize
		maxBlocks := min(maxMultiBlocks, int(d.adma.dataBuf.Len()/block.SectorSize))
		if blocks > maxBlocks {
			blocks = maxBlocks
		}
		if !d.config.MultiBlock && blocks > 1 {
			blocks = 1
		}
		usedCMD23 := blocks > 1 && d.cmd23
		chunkBytes := uint64(blocks * block.SectorSize)
		cmd := uint32(17)
		if blocks > 1 {
			cmd = 18
		}
		if err := d.adma.dataBuf.SyncRangeForDevice(0, chunkBytes, dmaops.FromDevice); err != nil {
			return d.containTransferError(err, true)
		}
		if err := d.adma.buildDescriptors(chunkBytes); err != nil {
			return d.containTransferError(err, true)
		}
		if usedCMD23 {
			if err := d.issue(23, uint32(blocks), responseR1, noData); err != nil {
				return d.containTransferError(err, true)
			}
		}
		descPhys := d.adma.descBuf.PhysAddr()
		d.io.Write32(admaAddrLow, uint32(descPhys))
		d.io.Write32(admaAddrHigh, uint32(descPhys>>32))
		err := d.issueTransfer(cmd, uint32(lba), responseR1, readData, uint32(blocks), block.SectorSize, true)
		if err == nil {
			err = d.wait(status, 2, 2, true)
		}
		if err != nil {
			admaSt := d.io.Read32(admaError)
			return d.containTransferError(fmt.Errorf("%w (adma_err=%#x)", err, admaSt), true)
		}
		d.io.Write32(status, 2)
		if err := d.adma.dataBuf.SyncRangeForCPU(0, chunkBytes, dmaops.FromDevice); err != nil {
			return d.containTransferError(err, true)
		}
		copy(dst[offset:offset+int(chunkBytes)], d.adma.dataBuf.Bytes()[:chunkBytes])
		d.recordChunkLocked(false, true, usedCMD23, blocks)
		lba += uint64(blocks)
		offset += int(chunkBytes)
	}
	if len(dst) > 0 {
		d.stats.readOps++
	}
	return nil
}

// WriteSectors serializes CMD24/CMD25 transfers. A batch succeeds only after
// data transfer completion and the card's ready/TRAN response. A batch error
// may follow committed sectors; an uncertain write is never replayed. The caller
// owns src until return. This is not an atomic multi-sector transaction.
func (d *Device) WriteSectors(lba uint64, src []byte) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.config.AllowWrites {
		return block.ErrReadOnly
	}
	if err := block.CheckRange(d.sectors, lba, len(src)); err != nil {
		return err
	}
	if d.failed {
		return ErrRetired
	}
	if d.quiesced || d.stopRequested.Load() {
		return ErrQuiesced
	}
	if d.cardWriteProtected {
		return fmt.Errorf("sdhci: card CSD write protection: %w", block.ErrReadOnly)
	}
	if d.adma != nil {
		return d.writeADMA2Locked(lba, src)
	}
	if d.config.MultiBlock && len(src) > block.SectorSize {
		return d.writeMultiPIOLocked(lba, src)
	}
	for n := 0; n < len(src); n += 512 {
		if d.stopRequested.Load() {
			return ErrQuiesced
		}
		// Check both the transport selection and live write-protect signal before
		// each sector. No unexpected DMA, bus-width or clock-mode change is permitted.
		if d.io.Read32(clock)&clockModeMask != d.dataClock|7 || !d.modeUnchanged(true) {
			d.retireLocked()
			return errors.New("sdhci: transfer mode changed before write")
		}
		// SDHCI WPSL is high when a wired socket switch is off. A board
		// without that input must explicitly select its actual wiring.
		if !d.config.NoSocketWriteProtect {
			state := d.io.Read32(present)
			if state&(1<<19) == 0 {
				return fmt.Errorf("sdhci: socket write protection (present=%#x): %w", state, block.ErrReadOnly)
			}
		}
		var sector [512]byte
		copy(sector[:], src[n:n+512])
		err := d.issue(24, uint32(lba), responseR1, writeData)
		if err == nil {
			err = d.wait(status, 1<<4, 1<<4, true)
		}
		if err == nil {
			err = d.wait(present, 1<<10, 1<<10, true)
		}
		if err == nil && d.io.Read32(status)&2 != 0 {
			err = errors.New("sdhci: write completed before its data was supplied")
		}
		if err == nil {
			for i := 0; i < 512; i += 4 {
				d.io.Write32(dataPort, binary.LittleEndian.Uint32(sector[i:]))
			}
			d.io.Write32(status, 1<<4)
			err = d.wait(status, 2, 2, true)
		}
		if err == nil {
			d.io.Write32(status, 2)
			err = d.writeReady()
		}
		if err != nil {
			return d.containTransferError(err, false)
		}
		d.recordChunkLocked(true, false, false, 1)
		lba++
	}
	if len(src) > 0 {
		d.stats.writeOps++
	}
	return nil
}

func (d *Device) checkWriteGateLocked() error {
	if d.io.Read32(clock)&clockModeMask != d.dataClock|7 || !d.modeUnchanged(true) {
		d.retireLocked()
		return errors.New("sdhci: transfer mode changed before write")
	}
	if !d.config.NoSocketWriteProtect {
		state := d.io.Read32(present)
		if state&(1<<19) == 0 {
			return fmt.Errorf("sdhci: socket write protection (present=%#x): %w", state, block.ErrReadOnly)
		}
	}
	return nil
}

func (d *Device) writeMultiPIOLocked(lba uint64, src []byte) error {
	for offset := 0; offset < len(src); {
		if d.stopRequested.Load() {
			return ErrQuiesced
		}
		if err := d.checkWriteGateLocked(); err != nil {
			return err
		}
		blocks := (len(src) - offset) / block.SectorSize
		if blocks > maxMultiBlocks {
			blocks = maxMultiBlocks
		}
		cmd := uint32(24)
		if blocks > 1 {
			cmd = 25
		}
		usedCMD23 := blocks > 1 && d.cmd23
		var err error
		if usedCMD23 {
			err = d.issue(23, uint32(blocks), responseR1, noData)
		}
		if err == nil {
			err = d.issueTransfer(cmd, uint32(lba), responseR1, writeData, uint32(blocks), block.SectorSize, false)
		}
		for b := 0; err == nil && b < blocks; b++ {
			err = d.wait(status, 1<<4, 1<<4, true)
			if err == nil {
				err = d.wait(present, 1<<10, 1<<10, true)
			}
			if err == nil && d.io.Read32(status)&2 != 0 {
				err = errors.New("sdhci: write completed before its data was supplied")
			}
			if err != nil {
				break
			}
			// Clear Buffer Write Ready before filling the block's FIFO words so
			// hardware can assert BWR again as soon as space opens for the next block.
			d.io.Write32(status, 1<<4)
			base := offset + b*block.SectorSize
			for i := 0; i < block.SectorSize; i += 4 {
				d.io.Write32(dataPort, binary.LittleEndian.Uint32(src[base+i:]))
			}
		}
		if err == nil {
			err = d.wait(status, 2, 2, true)
		}
		if err == nil {
			d.io.Write32(status, 2)
			err = d.writeReady()
		}
		if err != nil {
			return d.containTransferError(err, false)
		}
		d.recordChunkLocked(true, false, usedCMD23, blocks)
		lba += uint64(blocks)
		offset += blocks * block.SectorSize
	}
	if len(src) > 0 {
		d.stats.writeOps++
	}
	return nil
}

func (d *Device) writeADMA2Locked(lba uint64, src []byte) error {
	for offset := 0; offset < len(src); {
		if d.stopRequested.Load() {
			return ErrQuiesced
		}
		if err := d.checkWriteGateLocked(); err != nil {
			return err
		}
		blocks := (len(src) - offset) / block.SectorSize
		maxBlocks := min(maxMultiBlocks, int(d.adma.dataBuf.Len()/block.SectorSize))
		if blocks > maxBlocks {
			blocks = maxBlocks
		}
		if !d.config.MultiBlock && blocks > 1 {
			blocks = 1
		}
		usedCMD23 := blocks > 1 && d.cmd23
		chunkBytes := uint64(blocks * block.SectorSize)
		cmd := uint32(24)
		if blocks > 1 {
			cmd = 25
		}
		copy(d.adma.dataBuf.Bytes()[:chunkBytes], src[offset:offset+int(chunkBytes)])
		if err := d.adma.dataBuf.SyncRangeForDevice(0, chunkBytes, dmaops.ToDevice); err != nil {
			return d.containTransferError(err, true)
		}
		if err := d.adma.buildDescriptors(chunkBytes); err != nil {
			return d.containTransferError(err, true)
		}
		if usedCMD23 {
			if err := d.issue(23, uint32(blocks), responseR1, noData); err != nil {
				return d.containTransferError(err, true)
			}
		}
		descPhys := d.adma.descBuf.PhysAddr()
		d.io.Write32(admaAddrLow, uint32(descPhys))
		d.io.Write32(admaAddrHigh, uint32(descPhys>>32))
		err := d.issueTransfer(cmd, uint32(lba), responseR1, writeData, uint32(blocks), block.SectorSize, true)
		if err == nil {
			err = d.wait(status, 2, 2, true)
		}
		if err == nil {
			d.io.Write32(status, 2)
			err = d.writeReady()
		}
		if err != nil {
			admaSt := d.io.Read32(admaError)
			return d.containTransferError(fmt.Errorf("%w (adma_err=%#x)", err, admaSt), true)
		}
		d.recordChunkLocked(true, true, usedCMD23, blocks)
		lba += uint64(blocks)
		offset += int(chunkBytes)
	}
	if len(src) > 0 {
		d.stats.writeOps++
	}
	return nil
}

// Match the vendor MMC driver's bounded CMD13 programming-state wait. Neither
// a successful FIFO write nor a transfer-complete bit substitutes for card-ready.
func (d *Device) writeReady() error {
	start := d.io.Now()
	last, deadline := start, start.Add(time.Second)
	for n := 0; n < 1000; n++ {
		if err := d.issue(13, uint32(d.rca)<<16, responseR1, noData); err != nil {
			return err
		}
		now := d.io.Now()
		if now.Before(last) || !now.Before(deadline) {
			return errors.New("sdhci: card programming deadline or clock regression")
		}
		last = now
		r := d.io.Read32(response)
		state := (r >> 9) & 15
		if state == 4 && r&(1<<8) != 0 {
			return nil
		}
		if state != 7 {
			return fmt.Errorf("sdhci: unexpected card state after write: %#x", r)
		}
		if err := d.pause(time.Millisecond); err != nil {
			return err
		}
	}
	return errors.New("sdhci: card programming poll limit")
}

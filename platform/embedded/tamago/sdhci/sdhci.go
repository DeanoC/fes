// SPDX-License-Identifier: GPL-3.0-or-later

// Package sdhci initializes SDHC/SDXC cards and provides read-only, single-block
// PIO on the Agilex 5 Cadence controller. Firmware must establish the input
// clock, ComboPHY and pinmux, then relinquish exclusive controller ownership.
package sdhci

import (
	"encoding/binary"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
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
	busy         = uint32(0xb07)
)

var (
	ErrQuiesced    = errors.New("sdhci: device quiesced")
	ErrRetired     = errors.New("sdhci: prior failure requires fresh controller ownership and a new filesystem mount")
	ErrCardChanged = errors.New("sdhci: card identity changed across resume")
)

// Registers supplies ordered 32-bit device accesses relative to controller base.
// Now must be monotonic. Every wait also has a poll budget for a stalled clock.
type Registers interface {
	Read32(uint32) uint32
	Write32(uint32, uint32)
	Now() time.Time
}

type Device struct {
	lifecycle     sync.Mutex
	mu            sync.Mutex
	io            Registers
	sectors       uint64
	rca           uint16
	failed        bool
	quiesced      bool
	config        Config
	cid           [15]byte
	stopRequested atomic.Bool
}

// Identity contains the CID bits supplied by SDHCI's R2 registers (CID[127:8],
// excluding the CRC/end byte) and discovered geometry. Both remain immutable
// for this Device, including while quiesced or retired.
type Identity struct {
	CID     [15]byte
	Sectors uint64
}

func (d *Device) Identity() Identity { return Identity{CID: d.cid, Sectors: d.sectors} }

// Config describes the actual input clock established by firmware, not the
// requested SD bus rate. The initial transport stays at <=400 kHz, one bit.
type Config struct {
	InputClockHz uint32
	// PrepareClock, when supplied, runs after host software reset and stable
	// internal clock, before enabling card clocks or issuing any command.
	// A firmware-controlled platform uses it for its qualified PHY preparation.
	PrepareClock func() error
}

// Open resets the SDHCI host, power-cycles and discovers a block-addressed SD
// card. SDSC, SDUC and eMMC reject. Platform clocks/reset/pinmux must permit
// register access. PrepareClock, or prior firmware setup, must establish PHY
// readiness before the driver enables card clocks.
func Open(io Registers, config Config) (*Device, error) {
	if io == nil || config.InputClockHz < 400000 || config.InputClockHz > 200000000 {
		return nil, fmt.Errorf("sdhci: invalid firmware input clock")
	}
	d := &Device{io: io, config: config}
	if err := d.initialize(config); err != nil {
		return nil, err
	}
	return d, nil
}

func (d *Device) pause(duration time.Duration) error {
	end := d.io.Now().Add(duration)
	for n := 0; n < 1000000; n++ {
		if !d.io.Now().Before(end) {
			return nil
		}
		if n&255 == 255 {
			runtime.Gosched()
		}
	}
	return fmt.Errorf("sdhci: clock stalled during required delay")
}

func (d *Device) initialize(config Config) error {
	d.io.Write32(signalEnable, 0)
	d.io.Write32(clock, 1<<24) // SDHCI software reset all, including DMA state
	if err := d.wait(clock, 1<<24, 0, false); err != nil {
		return err
	}
	if d.io.Read32(0x240)&(1<<24) == 0 {
		return fmt.Errorf("sdhci: 3.3V bus power unsupported")
	}
	d.io.Write32(signalEnable, 0)
	d.io.Write32(statusEnable, 0xffff8033)
	d.io.Write32(host, 0) // 1-bit, default speed, bus power off
	if err := d.pause(10 * time.Millisecond); err != nil {
		return err
	}
	d.io.Write32(host, 7<<9) // select 3.3V before enabling bus power
	d.io.Write32(host, 7<<9|1<<8)
	if err := d.pause(time.Millisecond); err != nil {
		return err
	}
	// SDHCI divided-clock mode: divisor encoding 0 means /1, otherwise /2N.
	divisor := (uint64(config.InputClockHz) + 799999) / 800000
	if divisor == 0 {
		divisor = 1
	}
	d.io.Write32(clock, uint32(divisor)<<8|14<<16|1)
	if err := d.wait(clock, 2, 2, false); err != nil {
		return err
	}
	if config.PrepareClock != nil {
		if err := config.PrepareClock(); err != nil {
			return fmt.Errorf("sdhci: prepare controller clock: %w", err)
		}
		if d.io.Read32(clock)&(7|7<<24) != 3 || d.io.Read32(signalEnable) != 0 {
			return fmt.Errorf("sdhci: clock or interrupt state changed during platform preparation")
		}
	}
	d.io.Write32(clock, d.io.Read32(clock)|4)
	if err := d.pause(time.Millisecond); err != nil {
		return err
	} // >=74 startup clocks
	if err := d.issue(0, 0, responseNone, false); err != nil {
		return err
	}
	if err := d.issue(8, 0x1aa, responseR7, false); err != nil {
		return err
	}
	if d.io.Read32(response)&0xfff != 0x1aa {
		return fmt.Errorf("sdhci: unsupported CMD8 voltage/echo")
	}
	deadline := d.io.Now().Add(time.Second)
	ready := false
	for n := 0; n < 100; n++ {
		if err := d.issue(55, 0, responseR1, false); err != nil {
			return err
		}
		if d.io.Read32(response)&(1<<5) == 0 {
			return fmt.Errorf("sdhci: card refused APP_CMD")
		}
		// HCS, 3.2-3.4V; no 1.8V switch or extended-power request.
		if err := d.issue(41, 1<<30|0x00300000, responseR3, false); err != nil {
			return err
		}
		ocr := d.io.Read32(response)
		if ocr&(1<<31) != 0 {
			if ocr&(1<<30) == 0 || ocr&0x00300000 == 0 {
				return fmt.Errorf("sdhci: non-SDHC or incompatible OCR")
			}
			ready = true
			break
		}
		if !d.io.Now().Before(deadline) {
			break
		}
		if err := d.pause(10 * time.Millisecond); err != nil {
			return err
		}
	}
	if !ready {
		return fmt.Errorf("sdhci: card power-up timeout")
	}
	if err := d.issue(2, 0, responseR2, false); err != nil {
		return err
	}
	// R2 strips the final CRC/end byte and shifts the remaining bits down.
	top := d.io.Read32(response + 12)
	d.cid[0], d.cid[1], d.cid[2] = byte(top>>16), byte(top>>8), byte(top)
	binary.BigEndian.PutUint32(d.cid[3:7], d.io.Read32(response+8))
	binary.BigEndian.PutUint32(d.cid[7:11], d.io.Read32(response+4))
	binary.BigEndian.PutUint32(d.cid[11:15], d.io.Read32(response))
	var nonzero, notFF byte
	for _, value := range d.cid {
		nonzero |= value
		notFF |= ^value
	}
	if nonzero == 0 || notFF == 0 {
		return fmt.Errorf("sdhci: invalid card CID")
	}
	if err := d.issue(3, 0, responseR6, false); err != nil {
		return err
	}
	r6 := d.io.Read32(response)
	d.rca = uint16(r6 >> 16)
	if d.rca == 0 || r6&0xe000 != 0 {
		return fmt.Errorf("sdhci: invalid card RCA/R6 %#x", r6)
	}
	if err := d.issue(9, uint32(d.rca)<<16, responseR2, false); err != nil {
		return err
	}
	// SDHCI R2 registers hold CSD[127:8] shifted down by 8 bits.
	if (d.io.Read32(response+12)>>22)&3 != 1 {
		return fmt.Errorf("sdhci: unsupported CSD structure")
	}
	csize := (d.io.Read32(response+4) >> 8) & 0x3fffff
	d.sectors = (uint64(csize) + 1) * 1024
	if err := d.issue(7, uint32(d.rca)<<16, responseR1Busy, false); err != nil {
		return err
	}
	return d.ready()
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

func (d *Device) issue(index uint32, arg uint32, kind responseType, read bool) error {
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
	if err := d.issue(13, uint32(d.rca)<<16, responseR1, false); err != nil {
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
		return ErrRetired
	}
	if d.quiesced || d.stopRequested.Load() {
		return ErrQuiesced
	}
	for n := 0; n < len(dst); n += 512 {
		if d.stopRequested.Load() {
			return ErrQuiesced
		}
		// Stage each sector until transfer completion; uncertain data never appears
		// as a successful sector. Earlier sectors may have completed on batch error.
		var sector [512]byte
		err := d.issue(17, uint32(lba), responseR1, true)
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
			// Contain command/data state, but never replay a read into an existing
			// filesystem after a possible removal. A fresh Open and FAT mount is needed.
			d.io.Write32(clock, d.io.Read32(clock)|6<<24)
			resetErr := d.wait(clock, 6<<24, 0, false)
			return errors.Join(err, resetErr)
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
		return ErrRetired
	}
	if d.quiesced || d.stopRequested.Load() {
		return ErrQuiesced
	}
	if err := d.ready(); err != nil {
		d.failed = true
		return err
	}
	return nil
}

// Quiesce serializes with reads, stops further access and independently observes
// controller idle before and after stopping the card clock. Firmware can then
// assert its SoC resets under its own ownership contract. Success covers this
// PIO controller only; the platform must separately contain other masters and
// verify reset/bridge state. A repeated call supplies no fresh stop receipt.
func (d *Device) Quiesce() error {
	d.lifecycle.Lock()
	defer d.lifecycle.Unlock()
	d.stopRequested.Store(true)
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.failed {
		return ErrRetired
	}
	if d.quiesced {
		return ErrQuiesced
	}
	fail := func(err error) error { d.failed = true; return err }
	if d.io.Read32(command)&1 != 0 || d.io.Read32(clock)&(7|7<<24) != 7 || d.io.Read32(status)&statusErrors != 0 {
		return fail(fmt.Errorf("sdhci: unexpected DMA, clock or pending error before quiesce"))
	}
	d.io.Write32(signalEnable, 0)
	if d.io.Read32(signalEnable) != 0 {
		return fail(fmt.Errorf("sdhci: interrupt signals did not stop"))
	}
	if err := d.wait(present, busy, 0, true); err != nil {
		return fail(err)
	}
	// Query the selected card while command-complete status is still enabled.
	if err := d.ready(); err != nil {
		return fail(err)
	}
	d.io.Write32(statusEnable, 0)
	if err := d.wait(present, busy, 0, true); err != nil {
		return fail(err)
	}
	value := d.io.Read32(clock)
	if value&(7|7<<24) != 7 || d.io.Read32(command)&1 != 0 || d.io.Read32(signalEnable) != 0 ||
		d.io.Read32(statusEnable) != 0 || d.io.Read32(status)&statusErrors != 0 {
		return fail(fmt.Errorf("sdhci: controller changed during quiesce"))
	}
	d.io.Write32(clock, value&^4)
	if err := d.wait(present, busy, 0, true); err != nil {
		return fail(err)
	}
	if d.io.Read32(clock)&(7|7<<24) != 3 || d.io.Read32(signalEnable) != 0 ||
		d.io.Read32(statusEnable) != 0 || d.io.Read32(command)&1 != 0 || d.io.Read32(status)&statusErrors != 0 {
		return fail(fmt.Errorf("sdhci: quiesce readback changed"))
	}
	d.quiesced = true
	return nil
}

// Resume requires the caller to reestablish exclusive firmware ownership and
// SoC clock/ComboPHY/reset readiness first. Reinitialization can change RCA,
// but CID and geometry must match before the existing filesystem can read.
// Failure retires this Device; the caller retains its unresolved hardware lease.
func (d *Device) Resume() error {
	d.lifecycle.Lock()
	defer d.lifecycle.Unlock()
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.failed {
		return ErrRetired
	}
	if !d.quiesced {
		return errors.New("sdhci: resume requires quiescence")
	}
	next := &Device{io: d.io, config: d.config}
	if err := next.initialize(d.config); err != nil {
		d.failed = true
		return err
	}
	if next.Identity() != d.Identity() {
		d.failed = true
		return ErrCardChanged
	}
	d.rca = next.rca
	d.quiesced = false
	d.stopRequested.Store(false)
	return nil
}

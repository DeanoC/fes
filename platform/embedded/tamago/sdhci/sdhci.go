// SPDX-License-Identifier: GPL-3.0-or-later

// Package sdhci initializes SDHC/SDXC cards and provides single-block PIO,
// optional 4-bit bus width, multi-block CMD18/CMD25, GICv3 IRQ completion, and
// opt-in ADMA2 DMA on the Agilex 5 Cadence controller. Firmware must establish
// the input clock, ComboPHY, pinmux (and SMMU stream policy before ADMA2), then
// relinquish exclusive controller ownership.
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
	"github.com/DeanoC/fes/platform/embedded/tamago/dmaops"
	"github.com/DeanoC/fes/platform/embedded/tamago/gic"
)

// Register offsets follow Altera's MIT-0 socfpga_sdmmc_reg.h, commit
// 66bdcc347564c5698a8997169bce83a31647d618 (Copyright 2026 Altera).
const (
	blockSize     = 0x204
	argument      = 0x208
	command       = 0x20c
	response      = 0x210
	dataPort      = 0x220
	present       = 0x224
	host          = 0x228
	clock         = 0x22c
	status        = 0x230
	statusEnable  = 0x234
	signalEnable  = 0x238
	statusErrors  = uint32(0xffff8000)
	busy          = uint32(0xb07)
	clockModeMask = uint32(0x070fffff)
	baseHost33V   = uint32(7<<9 | 1<<8)
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
	lifecycle          sync.Mutex
	mu                 sync.Mutex
	io                 Registers
	sectors            uint64
	rca                uint16
	failed             bool
	quiesced           bool
	config             Config
	cid                [15]byte
	stopRequested      atomic.Bool
	identClock         uint32
	dataClock          uint32
	dataClockHz        uint32
	defaultSpeed       bool
	cardWriteProtected bool
	expectedHost       uint32
	busWidth4          bool
	adma               *admaEngine
	irqGIC             *gic.Controller
	irqID              uint32
	irqWake            chan struct{}
	irqActive          atomic.Bool
	irqCount           atomic.Uint64
}

// Identity contains the CID bits supplied by SDHCI's R2 registers (CID[127:8],
// excluding the CRC/end byte) and discovered geometry. Both remain immutable
// for this Device, including while quiesced or retired.
type Identity struct {
	CID     [15]byte
	Sectors uint64
}

func (d *Device) Identity() Identity { return Identity{CID: d.cid, Sectors: d.sectors} }

// BusWidth returns the active SD data bus width (1 or 4).
func (d *Device) BusWidth() int {
	if d.busWidth4 {
		return 4
	}
	return 1
}

// ADMA2Active reports whether ADMA2 DMA transfers are enabled on this device.
func (d *Device) ADMA2Active() bool { return d.adma != nil }

// Config describes the actual input clock established by firmware, not the
// requested SD bus rate. The initial transport stays at <=400 kHz, one bit, PIO.
type Config struct {
	InputClockHz uint32
	// AllowWrites explicitly selects sector writes. The platform must own
	// writable media and a qualified board write-protect policy. Default is read-only.
	AllowWrites bool
	// NoSocketWriteProtect describes board wiring, not write authorization.
	// Select it only when the socket has no mechanical write-protect input.
	// The default samples SDHCI WPSL. Card CSD protection and R1 errors remain
	// enforced with either wiring; AllowWrites is still required independently.
	NoSocketWriteProtect bool
	// TransferClockHz explicitly selects 25 MHz maximum after identification.
	// Zero retains the existing <=400 kHz transport; the identification divider
	// is restored at quiescence.
	TransferClockHz uint32
	// BusWidth4Bit opts into 4-bit SD bus mode after card selection (verified
	// via ACMD51 SCR and switched via ACMD6). Default false retains 1-bit mode.
	BusWidth4Bit bool
	// MultiBlock opts into CMD18/CMD25 multi-sector transfers (with Block Count
	// and Auto-CMD12, up to 128 sectors per command). Default false retains
	// single-sector CMD17/CMD24.
	MultiBlock bool
	// UseADMA2 opts into SDHCI ADMA2 DMA transfers backed by DMAPool (requiring
	// controller ADMA2 capability and platform SMMU stream authorization).
	// Default false retains PIO transfers.
	UseADMA2 bool
	// DMAPool supplies the pinned ADMA2 descriptor table and 64 KiB data buffer
	// when UseADMA2 is true. Ignored when UseADMA2 is false.
	DMAPool *dmaops.Pool
	// PrepareClock, when supplied, runs after host software reset and stable
	// internal clock, before enabling card clocks or issuing any command.
	// A firmware-controlled platform uses it for its qualified PHY preparation.
	PrepareClock func() error
}

// Open resets the SDHCI host, power-cycles and discovers a block-addressed SD
// card. SDSC, SDUC and eMMC reject. Platform clocks/reset/pinmux must permit
// register access. PrepareClock, or prior firmware setup, must establish PHY
// readiness before the driver enables card clocks.
func validateConfig(config Config) error {
	if config.InputClockHz < 400000 || config.InputClockHz > 200000000 ||
		(config.TransferClockHz != 0 && config.TransferClockHz != 25000000) ||
		(config.TransferClockHz != 0 && config.InputClockHz < config.TransferClockHz) {
		return fmt.Errorf("sdhci: invalid firmware input or selected transfer clock")
	}
	if config.UseADMA2 && config.DMAPool == nil {
		return fmt.Errorf("sdhci: UseADMA2 requires DMAPool")
	}
	return nil
}

func Open(io Registers, config Config) (*Device, error) {
	if io == nil {
		return nil, fmt.Errorf("sdhci: controller required")
	}
	if err := validateConfig(config); err != nil {
		return nil, err
	}
	d := &Device{io: io, config: config, expectedHost: baseHost33V}
	if err := d.initialize(config); err != nil {
		return nil, err
	}
	if err := d.configureADMA(); err != nil {
		return nil, err
	}
	if err := d.selectBusWidth(); err != nil {
		_ = d.adma.close()
		d.adma = nil
		return nil, err
	}
	if err := d.selectTransferClock(); err != nil {
		_ = d.adma.close()
		d.adma = nil
		return nil, err
	}
	return d, nil
}

// Close releases optional ADMA2 buffers and disables any bound IRQ when the
// device is healthy. Retired or poisoned devices keep DMA spans pinned.
func (d *Device) Close() error {
	d.lifecycle.Lock()
	defer d.lifecycle.Unlock()
	d.mu.Lock()
	defer d.mu.Unlock()
	d.disableIRQLocked()
	if d.failed {
		return ErrRetired
	}
	if d.adma != nil {
		if err := d.adma.close(); err != nil {
			d.failed = true
			return err
		}
		d.adma = nil
	}
	d.quiesced = true
	return nil
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
	d.expectedHost = baseHost33V
	d.busWidth4 = false
	d.io.Write32(signalEnable, 0)
	d.io.Write32(clock, 1<<24) // SDHCI software reset all, including DMA state
	if err := d.wait(clock, 1<<24, 0, false); err != nil {
		return err
	}
	if d.io.Read32(capabilities)&cap33V == 0 {
		return fmt.Errorf("sdhci: 3.3V bus power unsupported")
	}
	d.io.Write32(signalEnable, 0)
	d.io.Write32(statusEnable, 0xffff8033)
	d.io.Write32(host, 0) // 1-bit, default speed, bus power off
	if err := d.pause(10 * time.Millisecond); err != nil {
		return err
	}
	d.io.Write32(host, 7<<9) // select 3.3V before enabling bus power
	d.io.Write32(host, baseHost33V)
	if err := d.pause(time.Millisecond); err != nil {
		return err
	}
	// SDHCI divided-clock mode: divisor encoding 0 means /1, otherwise /2N.
	divisor := (uint64(config.InputClockHz) + 799999) / 800000
	if divisor == 0 {
		divisor = 1
	}
	d.identClock = uint32(divisor)<<8 | 14<<16
	d.dataClock = d.identClock
	d.dataClockHz = uint32(uint64(config.InputClockHz) / (2 * divisor))
	d.io.Write32(clock, d.identClock|1)
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
	if err := d.issue(0, 0, responseNone, noData); err != nil {
		return err
	}
	if err := d.issue(8, 0x1aa, responseR7, noData); err != nil {
		return err
	}
	if d.io.Read32(response)&0xfff != 0x1aa {
		return fmt.Errorf("sdhci: unsupported CMD8 voltage/echo")
	}
	deadline := d.io.Now().Add(time.Second)
	ready := false
	for n := 0; n < 100; n++ {
		if err := d.issue(55, 0, responseR1, noData); err != nil {
			return err
		}
		if d.io.Read32(response)&(1<<5) == 0 {
			return fmt.Errorf("sdhci: card refused APP_CMD")
		}
		// HCS, 3.2-3.4V; no 1.8V switch or extended-power request.
		if err := d.issue(41, 1<<30|0x00300000, responseR3, noData); err != nil {
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
	if err := d.issue(2, 0, responseR2, noData); err != nil {
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
	if err := d.issue(3, 0, responseR6, noData); err != nil {
		return err
	}
	r6 := d.io.Read32(response)
	d.rca = uint16(r6 >> 16)
	if d.rca == 0 || r6&0xe000 != 0 {
		return fmt.Errorf("sdhci: invalid card RCA/R6 %#x", r6)
	}
	if err := d.issue(9, uint32(d.rca)<<16, responseR2, noData); err != nil {
		return err
	}
	// SDHCI R2 registers hold CSD[127:8] shifted down by 8 bits.
	if (d.io.Read32(response+12)>>22)&3 != 1 {
		return fmt.Errorf("sdhci: unsupported CSD structure")
	}
	csize := (d.io.Read32(response+4) >> 8) & 0x3fffff
	// CSD[13] PERM_WRITE_PROTECT and [12] TMP_WRITE_PROTECT, shifted down
	// by eight bits in SDHCI's R2 response registers like the other CSD fields.
	d.cardWriteProtected = d.io.Read32(response)&(1<<5|1<<4) != 0
	// CSD[103:96] is R2 register 2's high byte after SDHCI's eight-bit
	// shift. SDHC/SDXC Default Speed advertises TRAN_SPEED 0x32 (25 MHz).
	d.defaultSpeed = byte(d.io.Read32(response+8)>>24) == 0x32
	d.sectors = (uint64(csize) + 1) * 1024
	if err := d.issue(7, uint32(d.rca)<<16, responseR1Busy, noData); err != nil {
		return err
	}
	return d.ready()
}

func (d *Device) configureADMA() error {
	if !d.config.UseADMA2 {
		return nil
	}
	caps := d.io.Read32(capabilities)
	eng := d.adma
	allocated := false
	if eng == nil {
		var err error
		eng, err = newADMAEngine(d.config.DMAPool, caps)
		if err != nil {
			return err
		}
		allocated = true
	}
	desired := d.expectedHost | eng.hostBits
	d.io.Write32(host, desired)
	if d.io.Read32(host) != desired {
		if allocated {
			_ = eng.close()
		}
		return errors.New("sdhci: ADMA2 host control readback differs")
	}
	d.adma = eng
	d.expectedHost = desired
	return nil
}

func (d *Device) selectBusWidth() error {
	if !d.config.BusWidth4Bit {
		return nil
	}
	// Query 64-bit SCR via CMD55 + ACMD51 (8-byte PIO read) to confirm card 4-bit support.
	if err := d.issue(55, uint32(d.rca)<<16, responseR1, noData); err != nil {
		return err
	}
	if d.io.Read32(response)&(1<<5) == 0 {
		return errors.New("sdhci: card refused APP_CMD for ACMD51")
	}
	if err := d.issueTransfer(51, 0, responseR1, readData, 1, 8, false); err != nil {
		return err
	}
	if err := d.wait(status, 1<<5, 1<<5, true); err != nil {
		return err
	}
	var scr [8]byte
	binary.LittleEndian.PutUint32(scr[0:4], d.io.Read32(dataPort))
	binary.LittleEndian.PutUint32(scr[4:8], d.io.Read32(dataPort))
	d.io.Write32(status, 1<<5)
	if err := d.wait(status, 2, 2, true); err != nil {
		return err
	}
	d.io.Write32(status, 2)
	// SCR[51:48] is SD_BUS_WIDTHS (wire byte 1 low nibble); bit 2 (0x04) = 4-bit.
	if scr[1]&0x04 == 0 {
		return fmt.Errorf("sdhci: card SCR (%x) does not advertise 4-bit bus width", scr)
	}
	if err := d.issue(55, uint32(d.rca)<<16, responseR1, noData); err != nil {
		return err
	}
	if d.io.Read32(response)&(1<<5) == 0 {
		return errors.New("sdhci: card refused APP_CMD for ACMD6")
	}
	if err := d.issue(6, 2, responseR1, noData); err != nil {
		return err
	}
	desired := d.expectedHost | host4BitWidth
	d.io.Write32(host, desired)
	if d.io.Read32(host) != desired {
		return errors.New("sdhci: 4-bit host control readback differs")
	}
	d.expectedHost = desired
	d.busWidth4 = true
	return d.ready()
}

func (d *Device) selectTransferClock() error {
	if d.config.TransferClockHz == 0 {
		return nil
	}
	if !d.defaultSpeed {
		return errors.New("sdhci: card does not declare default-speed 25 MHz")
	}
	divisor := uint64(0)
	if d.config.InputClockHz > d.config.TransferClockHz {
		denominator := 2 * uint64(d.config.TransferClockHz)
		divisor = (uint64(d.config.InputClockHz) + denominator - 1) / denominator
	}
	selected := uint32(divisor)<<8 | 14<<16
	if err := d.programClock(d.identClock|7, selected, true); err != nil {
		return err
	}
	if err := d.ready(); err != nil {
		return err
	}
	d.dataClock = selected
	d.dataClockHz = d.config.InputClockHz
	if divisor != 0 {
		d.dataClockHz = uint32(uint64(d.config.InputClockHz) / (2 * divisor))
	}
	return nil
}

// Change only the SDHCI divider while the command/data engines are idle.
// Keep the firmware-owned input/PHY, 3.3V, configured bus/DMA mode and IRQ policy.
func (d *Device) programClock(expected, selected uint32, enable bool) error {
	value := d.io.Read32(clock)
	if value&clockModeMask != expected || d.io.Read32(host) != d.expectedHost ||
		d.io.Read32(signalEnable) != 0 || d.io.Read32(command)&1 != 0 || d.io.Read32(status)&statusErrors != 0 {
		return errors.New("sdhci: unexpected mode before clock transition")
	}
	if err := d.wait(present, busy, 0, true); err != nil {
		return err
	}
	d.io.Write32(clock, value&^4)
	if d.io.Read32(clock)&4 != 0 {
		return errors.New("sdhci: card clock did not stop")
	}
	if err := d.wait(present, busy, 0, true); err != nil {
		return err
	}
	d.io.Write32(clock, value&^7)
	if d.io.Read32(clock)&1 != 0 {
		return errors.New("sdhci: internal clock did not stop")
	}
	d.io.Write32(clock, selected|1)
	if err := d.wait(clock, 2, 2, true); err != nil {
		return err
	}
	if d.io.Read32(clock)&clockModeMask != selected|3 {
		return errors.New("sdhci: divider/stable readback differs")
	}
	if enable {
		d.io.Write32(clock, selected|5)
		if d.io.Read32(clock)&clockModeMask != selected|7 {
			return errors.New("sdhci: transfer clock readback differs")
		}
		return d.pause(time.Millisecond)
	}
	return nil
}

func (d *Device) SectorCount() uint64 { return d.sectors }

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
	defer d.io.Write32(signalEnable, 0)
	deadline := d.io.Now().Add(500 * time.Millisecond)
	timer := time.NewTimer(500 * time.Millisecond)
	defer timer.Stop()
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
			transfer |= (1 << 5) | (1 << 2) | (1 << 1) // Multi-block + Auto-CMD12 + Block Count Enable
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
	d.failed = true
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
		d.failed = true
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
		lba++
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
		chunkBytes := blocks * block.SectorSize
		staged := make([]byte, chunkBytes)
		err := d.issueTransfer(cmd, uint32(lba), responseR1, readData, uint32(blocks), block.SectorSize, false)
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
		lba += uint64(blocks)
		offset += chunkBytes
	}
	return nil
}

func (d *Device) readADMA2Locked(lba uint64, dst []byte) error {
	for offset := 0; offset < len(dst); {
		if d.stopRequested.Load() {
			return ErrQuiesced
		}
		blocks := (len(dst) - offset) / block.SectorSize
		if blocks > maxMultiBlocks {
			blocks = maxMultiBlocks
		}
		if !d.config.MultiBlock && blocks > 1 {
			blocks = 1
		}
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
		lba += uint64(blocks)
		offset += int(chunkBytes)
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
			d.failed = true
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
		lba++
	}
	return nil
}

func (d *Device) checkWriteGateLocked() error {
	if d.io.Read32(clock)&clockModeMask != d.dataClock|7 || !d.modeUnchanged(true) {
		d.failed = true
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
		err := d.issueTransfer(cmd, uint32(lba), responseR1, writeData, uint32(blocks), block.SectorSize, false)
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
		lba += uint64(blocks)
		offset += blocks * block.SectorSize
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
		if blocks > maxMultiBlocks {
			blocks = maxMultiBlocks
		}
		if !d.config.MultiBlock && blocks > 1 {
			blocks = 1
		}
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
		lba += uint64(blocks)
		offset += int(chunkBytes)
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

// Flush requires card-ready. Write completion is card-reported programming
// completion, not a power-loss atomicity guarantee for multi-sector updates.
func (d *Device) Flush() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.failed {
		return ErrRetired
	}
	if d.quiesced || d.stopRequested.Load() {
		return ErrQuiesced
	}
	ready := d.ready
	if d.config.AllowWrites {
		ready = d.writeReady
	}
	if err := ready(); err != nil {
		d.failed = true
		return err
	}
	return nil
}

// Quiesce serializes with reads, stops further access and independently observes
// controller idle before and after stopping the card clock. Firmware can then
// assert its SoC resets under its own ownership contract. Success covers this
// controller only; the platform must separately contain other masters and
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
	// Stop IRQ-backed waits before masking the GIC line so the final CMD13
	// inside Quiesce polls synchronously with signalEnable==0 instead of
	// waiting on a masked SPI.
	d.irqActive.Store(false)
	if d.irqGIC != nil {
		d.irqGIC.Disable(d.irqID)
	}
	fail := func(err error) error {
		d.failed = true
		if d.adma != nil {
			d.adma.poison(err)
		}
		return err
	}
	if (!d.config.UseADMA2 && d.io.Read32(command)&1 != 0) || d.io.Read32(clock)&(7|7<<24) != 7 || d.io.Read32(status)&statusErrors != 0 {
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
	// This also clears Transfer Mode DMA Enable (SRS03[0]) when ADMA2 was used.
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
	if d.config.TransferClockHz != 0 {
		if err := d.programClock(d.dataClock|3, d.identClock, false); err != nil {
			return fail(err)
		}
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
	next := &Device{io: d.io, config: d.config, expectedHost: baseHost33V, adma: d.adma}
	// Close releases the ADMA2 engine, so configureADMA may allocate a fresh
	// one on next. Poison both on failure and adopt next's engine on success,
	// otherwise later transfers would silently fall back to PIO.
	fail := func(err error) error {
		d.failed = true
		d.adma.poison(err)
		if next.adma != d.adma {
			next.adma.poison(err)
		}
		return err
	}
	if err := next.initialize(d.config); err != nil {
		return fail(err)
	}
	if next.Identity() != d.Identity() {
		return fail(ErrCardChanged)
	}
	if err := next.configureADMA(); err != nil {
		return fail(err)
	}
	if err := next.selectBusWidth(); err != nil {
		return fail(err)
	}
	if err := next.selectTransferClock(); err != nil {
		return fail(err)
	}
	if next.dataClockHz != d.dataClockHz || next.dataClock != d.dataClock ||
		next.expectedHost != d.expectedHost || next.busWidth4 != d.busWidth4 {
		return fail(errors.New("sdhci: transfer mode changed across resume"))
	}
	d.adma = next.adma
	d.rca = next.rca
	d.cardWriteProtected = next.cardWriteProtected
	d.quiesced = false
	d.stopRequested.Store(false)
	if d.irqGIC != nil {
		d.irqActive.Store(true)
		d.irqGIC.Enable(d.irqID)
	}
	return nil
}

// SPDX-License-Identifier: GPL-3.0-or-later

// Package sdhci initializes SDHC/SDXC cards and provides single-block PIO,
// optional 4-bit bus width, multi-block CMD18/CMD25, GICv3 IRQ completion, and
// opt-in ADMA2 DMA on the Agilex 5 Cadence controller. Firmware must establish
// the input clock, ComboPHY, pinmux (and SMMU stream policy before ADMA2), then
// relinquish exclusive controller ownership.
package sdhci

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/dmaops"
	"github.com/DeanoC/fes/platform/embedded/tamago/gic"
)

// Register offsets follow Altera's MIT-0 socfpga_sdmmc_reg.h, commit
// 66bdcc347564c5698a8997169bce83a31647d618 (Copyright 2026 Altera).
const (
	blockSize                 = 0x204
	argument                  = 0x208
	command                   = 0x20c
	response                  = 0x210
	dataPort                  = 0x220
	present                   = 0x224
	host                      = 0x228
	clock                     = 0x22c
	status                    = 0x230
	statusEnable              = 0x234
	signalEnable              = 0x238
	statusErrors              = uint32(0xffff8000)
	busy                      = uint32(0xb07)
	clockModeMask             = uint32(0x070fffff)
	baseHost33V               = uint32(7<<9 | 1<<8)
	defaultHighSpeedMaxCurrMA = uint16(200)
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
	csd                [15]byte
	stopRequested      atomic.Bool
	identClock         uint32
	dataClock          uint32
	dataClockHz        uint32
	defaultSpeed       bool
	highSpeed          bool
	hasSCR             bool
	scr                [8]byte
	cmd23              bool
	cardWriteProtected bool
	expectedHost       uint32
	busWidth4          bool
	adma               *admaEngine
	irqMu              sync.Mutex
	irqGIC             *gic.Controller
	irqID              uint32
	irqWake            chan struct{}
	irqActive          atomic.Bool
	irqCount           atomic.Uint64
	totalIRQs          atomic.Uint64
	irqTimer           *time.Timer
	pioScratch         []byte
	stats              sdhciStats
}

type sdhciStats struct {
	readOps        uint64
	writeOps       uint64
	sectorsRead    uint64
	sectorsWritten uint64
	pioOps         uint64
	adma2Ops       uint64
	cmd23Ops       uint64
	errors         uint64
	quiesceCount   uint64
	resumeCount    uint64
}

// Stats reports cumulative transfer telemetry and active controller state.
type Stats struct {
	ReadOps         uint64
	WriteOps        uint64
	SectorsRead     uint64
	SectorsWritten  uint64
	PIOOps          uint64
	ADMA2Ops        uint64
	CMD23Ops        uint64
	Errors          uint64
	QuiesceCount    uint64
	ResumeCount     uint64
	IRQCount        uint64
	BusWidth        int
	TransferClockHz uint32
	HighSpeed       bool
	ADMA2Active     bool
	CMD23Active     bool
	Quiesced        bool
	Retired         bool
}

// Stats returns a point-in-time snapshot of transfer counters and active state.
func (d *Device) Stats() Stats {
	if d == nil {
		return Stats{}
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	busWidth := 1
	if d.busWidth4 {
		busWidth = 4
	}
	return Stats{
		ReadOps:         d.stats.readOps,
		WriteOps:        d.stats.writeOps,
		SectorsRead:     d.stats.sectorsRead,
		SectorsWritten:  d.stats.sectorsWritten,
		PIOOps:          d.stats.pioOps,
		ADMA2Ops:        d.stats.adma2Ops,
		CMD23Ops:        d.stats.cmd23Ops,
		Errors:          d.stats.errors,
		QuiesceCount:    d.stats.quiesceCount,
		ResumeCount:     d.stats.resumeCount,
		IRQCount:        d.totalIRQs.Load(),
		BusWidth:        busWidth,
		TransferClockHz: d.dataClockHz,
		HighSpeed:       d.highSpeed,
		ADMA2Active:     d.adma != nil,
		CMD23Active:     d.cmd23,
		Quiesced:        d.quiesced,
		Retired:         d.failed,
	}
}

func (d *Device) recordChunkLocked(isWrite, isADMA, usedCMD23 bool, blocks int) {
	if isADMA {
		d.stats.adma2Ops++
	} else {
		d.stats.pioOps++
	}
	if usedCMD23 {
		d.stats.cmd23Ops++
	}
	if isWrite {
		d.stats.sectorsWritten += uint64(blocks)
	} else {
		d.stats.sectorsRead += uint64(blocks)
	}
}

func (d *Device) retireLocked() {
	d.failed = true
	d.stats.errors++
	d.disableIRQLocked()
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

// HighSpeed reports whether SD High-Speed (50 MHz via CMD6) is active.
func (d *Device) HighSpeed() bool { return d.highSpeed }

// TransferClockHz returns the active SD clock rate in Hz.
func (d *Device) TransferClockHz() uint32 { return d.dataClockHz }

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
	// TransferClockHz explicitly selects 25 MHz Default Speed or 50 MHz
	// High-Speed (via CMD6 Switch Function) after identification.
	// Zero retains the existing <=400 kHz transport; the identification divider
	// is restored at quiescence.
	TransferClockHz uint32
	// HighSpeedMaxCurrentMA sets the board's maximum 3.3V SD supply current
	// budget in mA for CMD6 High-Speed admission (default 0 -> 200 mA, the
	// baseline SD v1.10+ 3.3V limit per Physical Layer Spec §4.3.10.4). Cards
	// reporting 0 or more than this budget are rejected before switching.
	HighSpeedMaxCurrentMA uint16
	// BusWidth4Bit opts into 4-bit SD bus mode after card selection (verified
	// via ACMD51 SCR and switched via ACMD6). Default false retains 1-bit mode.
	BusWidth4Bit bool
	// MultiBlock opts into CMD18/CMD25 multi-sector transfers (with Block Count
	// and Auto-CMD12, up to 256 sectors per command). Default false retains
	// single-sector CMD17/CMD24.
	MultiBlock bool
	// UseCMD23 opts into issuing CMD23 (SET_BLOCK_COUNT) before multi-sector
	// CMD18/CMD25 transfers instead of relying on Auto-CMD12. Requires
	// MultiBlock and card SCR[33] support.
	UseCMD23 bool
	// UseADMA2 opts into SDHCI ADMA2 DMA transfers backed by DMAPool (requiring
	// controller ADMA2 capability and platform SMMU stream authorization).
	// Default false retains PIO transfers.
	UseADMA2 bool
	// DMAPool supplies the pinned ADMA2 descriptor table and 128 KiB data buffer
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
		(config.TransferClockHz != 0 && config.TransferClockHz != 25000000 && config.TransferClockHz != 50000000) ||
		(config.TransferClockHz != 0 && config.InputClockHz < config.TransferClockHz) {
		return fmt.Errorf("sdhci: invalid firmware input or selected transfer clock")
	}
	if config.UseADMA2 && config.DMAPool == nil {
		return fmt.Errorf("sdhci: UseADMA2 requires DMAPool")
	}
	if config.UseCMD23 && !config.MultiBlock {
		return fmt.Errorf("sdhci: UseCMD23 requires MultiBlock")
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
	if err := d.selectCMD23(); err != nil {
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
			d.retireLocked()
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

func (d *Device) SectorCount() uint64 { return d.sectors }

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
		d.retireLocked()
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
	d.suspendIRQLocked()
	fail := func(err error) error {
		d.retireLocked()
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
	d.stats.quiesceCount++
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
		d.retireLocked()
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
	if err := next.selectCMD23(); err != nil {
		return fail(err)
	}
	if err := next.selectTransferClock(); err != nil {
		return fail(err)
	}
	if next.dataClockHz != d.dataClockHz || next.dataClock != d.dataClock ||
		next.expectedHost != d.expectedHost || next.busWidth4 != d.busWidth4 ||
		next.highSpeed != d.highSpeed || next.cmd23 != d.cmd23 {
		return fail(errors.New("sdhci: transfer mode changed across resume"))
	}
	d.adma = next.adma
	d.rca = next.rca
	d.csd = next.csd
	d.hasSCR = next.hasSCR
	d.scr = next.scr
	d.cardWriteProtected = next.cardWriteProtected
	d.quiesced = false
	d.stopRequested.Store(false)
	d.stats.resumeCount++
	d.resumeIRQLocked()
	return nil
}

// SPDX-License-Identifier: GPL-3.0-or-later

// Package i2c provides a bare-metal Synopsys DesignWare APB I2C master driver
// compatible with Intel/Altera Agilex 5 SoC HPS I2C controllers (0x10c02800..0x10c02c00).
package i2c

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
)

// Agilex 5 HPS DesignWare APB I2C physical controller base addresses and SPIs.
const (
	Agilex5I2C0Base uint64 = 0x10c02800
	Agilex5I2C1Base uint64 = 0x10c02900
	Agilex5I2C2Base uint64 = 0x10c02a00
	Agilex5I2C3Base uint64 = 0x10c02b00
	Agilex5I2C4Base uint64 = 0x10c02c00

	Agilex5I2C0SPI uint32 = 103
	Agilex5I2C1SPI uint32 = 104
)

// Synopsys DesignWare APB I2C register offsets.
const (
	RegCon          uint32 = 0x00
	RegTar          uint32 = 0x04
	RegDataCmd      uint32 = 0x10
	RegSSHCNT       uint32 = 0x14
	RegSSLCNT       uint32 = 0x18
	RegFSHCNT       uint32 = 0x1c
	RegFSLCNT       uint32 = 0x20
	RegIntrStat     uint32 = 0x2c
	RegIntrMask     uint32 = 0x30
	RegRawIntrStat  uint32 = 0x34
	RegRxTL         uint32 = 0x38
	RegTxTL         uint32 = 0x3c
	RegClrIntr      uint32 = 0x40
	RegClrTxAbrt    uint32 = 0x54
	RegClrStopDet   uint32 = 0x60
	RegEnable       uint32 = 0x6c
	RegStatus       uint32 = 0x70
	RegTxFLR        uint32 = 0x74
	RegRxFLR        uint32 = 0x78
	RegSDAHold      uint32 = 0x7c
	RegTxAbrtSource uint32 = 0x80
	RegEnableStatus uint32 = 0x9c
	RegCompParam1   uint32 = 0xf4
	RegCompVersion  uint32 = 0xf8
	RegCompType     uint32 = 0xfc

	// DWCompType is the Synopsys component type tag for DW_apb_i2c.
	DWCompType uint32 = 0x44570140
)

const (
	conMasterMode   uint32 = 1 << 0
	conSpeedStd     uint32 = 1 << 1
	conSpeedFast    uint32 = 2 << 1
	conRestartEn    uint32 = 1 << 5
	conSlaveDisable uint32 = 1 << 6

	cmdRead    uint32 = 1 << 8
	cmdStop    uint32 = 1 << 9
	cmdRestart uint32 = 1 << 10

	intrTxAbrt  uint32 = 1 << 6
	intrStopDet uint32 = 1 << 9

	statusTFNF        uint32 = 1 << 1
	statusTFE         uint32 = 1 << 2
	statusRFNE        uint32 = 1 << 3
	statusMstActivity uint32 = 1 << 5

	abrt7bAddrNoAck uint32 = 1 << 0
	abrtTxDataNoAck uint32 = 1 << 3
	abrtArbLost     uint32 = 1 << 12

	defaultInputClockHz   uint64 = 100_000_000
	minInputClockHz       uint64 = 2_000_000
	defaultMaxPollIters   int    = 50_000
	defaultFIFODepth      int    = 16
	minSCLHighCount       uint32 = 6
	minSCLLowCount        uint32 = 8
	minValid7BitSlaveAddr uint8  = 0x08
	maxValid7BitSlaveAddr uint8  = 0x77
)

var (
	// ErrNilAccess is returned when Open receives a nil Registers implementation.
	ErrNilAccess = errors.New("i2c: nil register access")
	// ErrNotFound is returned when the DesignWare component type ID does not match.
	ErrNotFound = errors.New("i2c: DesignWare I2C controller not found")
	// ErrInvalidAddr is returned for reserved or out-of-range 7-bit I2C addresses.
	ErrInvalidAddr = errors.New("i2c: invalid 7-bit slave address")
	// ErrInvalidConfig is returned for invalid bus speed, clock, or buffer arguments.
	ErrInvalidConfig = errors.New("i2c: invalid controller or transfer configuration")
	// ErrNACK is returned when a slave fails to ACK its address or transmitted byte.
	ErrNACK = errors.New("i2c: NACK from target")
	// ErrArbitrationLost is returned when master arbitration is lost on the bus.
	ErrArbitrationLost = errors.New("i2c: arbitration lost")
	// ErrAbort is returned when hardware raises TX_ABRT without NACK or arbitration loss.
	ErrAbort = errors.New("i2c: transfer aborted")
	// ErrTimeout is returned when hardware polling exceeds MaxPollIterations.
	ErrTimeout = errors.New("i2c: controller poll timeout")
)

// Registers abstracts 32-bit little-endian MMIO access to a DesignWare APB I2C controller.
type Registers interface {
	Read32(offset uint32) uint32
	Write32(offset uint32, val uint32)
}

// Bus is the minimal SMBus/I2C register transfer interface consumed by peripheral drivers.
type Bus interface {
	WriteReg(ctx context.Context, addr uint8, reg uint8, data []byte) error
	ReadReg(ctx context.Context, addr uint8, reg uint8, dst []byte) error
	WriteRegByte(ctx context.Context, addr uint8, reg uint8, val uint8) error
	ReadRegByte(ctx context.Context, addr uint8, reg uint8) (uint8, error)
	Probe(ctx context.Context, addr uint8) error
}

// Speed selects I2C bus bit rate.
type Speed uint32

const (
	SpeedStandard Speed = 100_000
	SpeedFast     Speed = 400_000
)

// Config configures Open for a DesignWare APB I2C instance.
type Config struct {
	InputClockHz      uint64
	Speed             Speed
	MaxPollIterations int
	SDAHoldCycles     uint32
}

// Stats tracks lifetime I2C transactions, payload counts, and bus errors.
type Stats struct {
	Writes       uint64
	Reads        uint64
	BytesWritten uint64
	BytesRead    uint64
	NACKs        uint64
	ArbLost      uint64
	Timeouts     uint64
	Aborts       uint64
}

// Controller manages one Synopsys DesignWare APB I2C master.
type Controller struct {
	mu       sync.Mutex
	regs     Registers
	cfg      Config
	rxDepth  int
	txDepth  int
	stats    Stats
	lastAddr uint8
	hasAddr  bool
}

var _ Bus = (*Controller)(nil)

func normalizeConfig(cfg Config) (Config, error) {
	if cfg.InputClockHz == 0 {
		cfg.InputClockHz = defaultInputClockHz
	}
	if cfg.InputClockHz < minInputClockHz {
		return cfg, fmt.Errorf("%w: InputClockHz %d below %d", ErrInvalidConfig, cfg.InputClockHz, minInputClockHz)
	}
	if cfg.Speed == 0 {
		cfg.Speed = SpeedStandard
	}
	if cfg.Speed != SpeedStandard && cfg.Speed != SpeedFast {
		return cfg, fmt.Errorf("%w: unsupported speed %d", ErrInvalidConfig, cfg.Speed)
	}
	if cfg.MaxPollIterations == 0 {
		cfg.MaxPollIterations = defaultMaxPollIters
	}
	if cfg.MaxPollIterations < 1 {
		return cfg, fmt.Errorf("%w: MaxPollIterations %d", ErrInvalidConfig, cfg.MaxPollIterations)
	}
	return cfg, nil
}

func computeSCLCounts(clkHz uint64, speed Speed) (hcnt, lcnt uint32) {
	periodCycles := clkHz / uint64(speed)
	var hiPct uint64 = 40
	if speed == SpeedFast {
		hiPct = 36
	}
	hi := (periodCycles * hiPct) / 100
	lo := periodCycles - hi
	if hi < uint64(minSCLHighCount) {
		hi = uint64(minSCLHighCount)
	}
	if lo < uint64(minSCLLowCount) {
		lo = uint64(minSCLLowCount)
	}
	if hi > 0xffff {
		hi = 0xffff
	}
	if lo > 0xffff {
		lo = 0xffff
	}
	return uint32(hi), uint32(lo)
}

func decodeFIFODepths(compParam1 uint32) (txDepth, rxDepth int) {
	rxDepth = int((compParam1>>8)&0xff) + 1
	txDepth = int((compParam1>>16)&0xff) + 1
	if compParam1 == 0 {
		return defaultFIFODepth, defaultFIFODepth
	}
	if txDepth < 2 {
		txDepth = defaultFIFODepth
	}
	if rxDepth < 2 {
		rxDepth = defaultFIFODepth
	}
	return txDepth, rxDepth
}

func validateAddr(addr uint8) error {
	if addr < minValid7BitSlaveAddr || addr > maxValid7BitSlaveAddr {
		return fmt.Errorf("%w: 0x%02x", ErrInvalidAddr, addr)
	}
	return nil
}

// Open initializes a DesignWare APB I2C master and programs SCL timing registers.
func Open(regs Registers, cfg Config) (*Controller, error) {
	if regs == nil {
		return nil, ErrNilAccess
	}
	if comp := regs.Read32(RegCompType); comp != DWCompType {
		return nil, fmt.Errorf("%w: got %#08x", ErrNotFound, comp)
	}
	norm, err := normalizeConfig(cfg)
	if err != nil {
		return nil, err
	}
	txDepth, rxDepth := decodeFIFODepths(regs.Read32(RegCompParam1))

	c := &Controller{
		regs:    regs,
		cfg:     norm,
		txDepth: txDepth,
		rxDepth: rxDepth,
	}
	if err := c.disableLocked(context.Background()); err != nil {
		return nil, err
	}

	speedBits := conSpeedStd
	if norm.Speed == SpeedFast {
		speedBits = conSpeedFast
	}
	con := conMasterMode | conRestartEn | conSlaveDisable | speedBits
	c.regs.Write32(RegCon, con)

	hcnt, lcnt := computeSCLCounts(norm.InputClockHz, norm.Speed)
	if norm.Speed == SpeedFast {
		c.regs.Write32(RegFSHCNT, hcnt)
		c.regs.Write32(RegFSLCNT, lcnt)
	} else {
		c.regs.Write32(RegSSHCNT, hcnt)
		c.regs.Write32(RegSSLCNT, lcnt)
	}
	if norm.SDAHoldCycles != 0 {
		c.regs.Write32(RegSDAHold, norm.SDAHoldCycles)
	}
	c.regs.Write32(RegRxTL, 0)
	c.regs.Write32(RegTxTL, 0)
	c.regs.Write32(RegIntrMask, 0)
	_ = c.regs.Read32(RegClrIntr)
	return c, nil
}

// Stats returns a snapshot of controller counters.
func (c *Controller) Stats() Stats {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stats
}

// Probe checks whether a 7-bit I2C slave ACKs its address on the bus.
func (c *Controller) Probe(ctx context.Context, addr uint8) (err error) {
	if ctx == nil {
		return fmt.Errorf("%w: context required", ErrInvalidConfig)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateAddr(addr); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	defer func() {
		if err != nil {
			c.resetOnErrorLocked()
		}
	}()

	if err := c.setTargetLocked(ctx, addr); err != nil {
		return err
	}
	_ = c.regs.Read32(RegClrIntr)
	if err := c.waitTXSlotLocked(ctx); err != nil {
		return err
	}
	c.regs.Write32(RegDataCmd, cmdRead|cmdStop)
	if err := c.waitRXByteLocked(ctx); err != nil {
		return err
	}
	_ = c.regs.Read32(RegDataCmd)
	return c.waitIdleLocked(ctx)
}

// WriteRegByte writes one byte val to register reg at 7-bit address addr.
func (c *Controller) WriteRegByte(ctx context.Context, addr uint8, reg uint8, val uint8) error {
	var buf [1]byte
	buf[0] = val
	return c.WriteReg(ctx, addr, reg, buf[:])
}

// ReadRegByte reads one byte from register reg at 7-bit address addr.
func (c *Controller) ReadRegByte(ctx context.Context, addr uint8, reg uint8) (uint8, error) {
	var buf [1]byte
	if err := c.ReadReg(ctx, addr, reg, buf[:]); err != nil {
		return 0, err
	}
	return buf[0], nil
}

// WriteReg writes reg followed by data bytes with a trailing STOP condition.
func (c *Controller) WriteReg(ctx context.Context, addr uint8, reg uint8, data []byte) (err error) {
	if ctx == nil {
		return fmt.Errorf("%w: context required", ErrInvalidConfig)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateAddr(addr); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	defer func() {
		if err != nil {
			c.resetOnErrorLocked()
		}
	}()

	if err := c.setTargetLocked(ctx, addr); err != nil {
		return err
	}
	_ = c.regs.Read32(RegClrIntr)

	regCmd := uint32(reg)
	if len(data) == 0 {
		regCmd |= cmdStop
	}
	if err := c.waitTXSlotLocked(ctx); err != nil {
		return err
	}
	c.regs.Write32(RegDataCmd, regCmd)

	for i, b := range data {
		if err := c.waitTXSlotLocked(ctx); err != nil {
			return err
		}
		cmd := uint32(b)
		if i == len(data)-1 {
			cmd |= cmdStop
		}
		c.regs.Write32(RegDataCmd, cmd)
	}

	if err := c.waitIdleLocked(ctx); err != nil {
		return err
	}
	c.stats.Writes++
	c.stats.BytesWritten += uint64(len(data))
	return nil
}

// ReadReg writes reg, issues a repeated START, and fills dst using FIFO-bounded reads.
func (c *Controller) ReadReg(ctx context.Context, addr uint8, reg uint8, dst []byte) (err error) {
	if ctx == nil {
		return fmt.Errorf("%w: context required", ErrInvalidConfig)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateAddr(addr); err != nil {
		return err
	}
	if len(dst) == 0 {
		return fmt.Errorf("%w: empty read destination", ErrInvalidConfig)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	defer func() {
		if err != nil {
			c.resetOnErrorLocked()
		}
	}()

	if err := c.setTargetLocked(ctx, addr); err != nil {
		return err
	}
	_ = c.regs.Read32(RegClrIntr)

	if err := c.waitTXSlotLocked(ctx); err != nil {
		return err
	}
	c.regs.Write32(RegDataCmd, uint32(reg))

	readIssued := 0
	rxDone := 0
	stalls := 0
	for rxDone < len(dst) {
		if err := c.checkAbortLocked(); err != nil {
			return err
		}
		progressed := false
		for rxDone < len(dst) && c.regs.Read32(RegStatus)&statusRFNE != 0 {
			dst[rxDone] = byte(c.regs.Read32(RegDataCmd) & 0xff)
			rxDone++
			progressed = true
		}
		for readIssued < len(dst) && (readIssued-rxDone) < c.rxDepth && c.regs.Read32(RegStatus)&statusTFNF != 0 {
			cmd := cmdRead
			if readIssued == 0 {
				cmd |= cmdRestart
			}
			if readIssued == len(dst)-1 {
				cmd |= cmdStop
			}
			c.regs.Write32(RegDataCmd, cmd)
			readIssued++
			progressed = true
		}
		if progressed {
			stalls = 0
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		stalls++
		if stalls > c.cfg.MaxPollIterations {
			c.stats.Timeouts++
			return ErrTimeout
		}
		runtime.Gosched()
	}

	if err := c.waitIdleLocked(ctx); err != nil {
		return err
	}
	c.stats.Reads++
	c.stats.BytesRead += uint64(len(dst))
	return nil
}

func (c *Controller) disableLocked(ctx context.Context) error {
	c.regs.Write32(RegEnable, 0)
	for i := 0; i < c.cfg.MaxPollIterations; i++ {
		if c.regs.Read32(RegEnableStatus)&1 == 0 {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		runtime.Gosched()
	}
	c.stats.Timeouts++
	return ErrTimeout
}

func (c *Controller) enableLocked(ctx context.Context) error {
	c.regs.Write32(RegEnable, 1)
	for i := 0; i < c.cfg.MaxPollIterations; i++ {
		if c.regs.Read32(RegEnableStatus)&1 == 1 {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		runtime.Gosched()
	}
	c.stats.Timeouts++
	return ErrTimeout
}

func (c *Controller) drainRXFIFOLocked() {
	for i := 0; i < c.rxDepth && c.regs.Read32(RegStatus)&statusRFNE != 0; i++ {
		_ = c.regs.Read32(RegDataCmd)
	}
}

func (c *Controller) resetOnErrorLocked() {
	c.hasAddr = false
	_ = c.disableLocked(context.Background())
	_ = c.regs.Read32(RegClrIntr)
	_ = c.regs.Read32(RegClrTxAbrt)
	c.drainRXFIFOLocked()
}

func (c *Controller) setTargetLocked(ctx context.Context, addr uint8) error {
	if c.hasAddr && c.lastAddr == addr && c.regs.Read32(RegEnableStatus)&1 == 1 {
		st := c.regs.Read32(RegStatus)
		if st&statusMstActivity == 0 && st&statusTFE != 0 && st&statusRFNE == 0 {
			return nil
		}
	}
	c.hasAddr = false
	if err := c.disableLocked(ctx); err != nil {
		return err
	}
	c.drainRXFIFOLocked()
	c.regs.Write32(RegTar, uint32(addr))
	if err := c.enableLocked(ctx); err != nil {
		return err
	}
	c.lastAddr = addr
	c.hasAddr = true
	return nil
}

func (c *Controller) checkAbortLocked() error {
	if c.regs.Read32(RegRawIntrStat)&intrTxAbrt == 0 {
		return nil
	}
	src := c.regs.Read32(RegTxAbrtSource)
	_ = c.regs.Read32(RegClrTxAbrt)
	_ = c.regs.Read32(RegClrIntr)
	if src&(abrt7bAddrNoAck|abrtTxDataNoAck) != 0 {
		c.stats.NACKs++
		return fmt.Errorf("%w: abrt_source=%#x", ErrNACK, src)
	}
	if src&abrtArbLost != 0 {
		c.stats.ArbLost++
		return fmt.Errorf("%w: abrt_source=%#x", ErrArbitrationLost, src)
	}
	c.stats.Aborts++
	return fmt.Errorf("%w: abrt_source=%#x", ErrAbort, src)
}

func (c *Controller) waitTXSlotLocked(ctx context.Context) error {
	for i := 0; i < c.cfg.MaxPollIterations; i++ {
		if err := c.checkAbortLocked(); err != nil {
			return err
		}
		if c.regs.Read32(RegStatus)&statusTFNF != 0 {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		runtime.Gosched()
	}
	c.stats.Timeouts++
	return ErrTimeout
}

func (c *Controller) waitRXByteLocked(ctx context.Context) error {
	for i := 0; i < c.cfg.MaxPollIterations; i++ {
		if err := c.checkAbortLocked(); err != nil {
			return err
		}
		if c.regs.Read32(RegStatus)&statusRFNE != 0 {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		runtime.Gosched()
	}
	c.stats.Timeouts++
	return ErrTimeout
}

func (c *Controller) waitIdleLocked(ctx context.Context) error {
	for i := 0; i < c.cfg.MaxPollIterations; i++ {
		if err := c.checkAbortLocked(); err != nil {
			return err
		}
		st := c.regs.Read32(RegStatus)
		if st&statusTFE != 0 && st&statusMstActivity == 0 {
			if c.regs.Read32(RegRawIntrStat)&intrStopDet != 0 {
				_ = c.regs.Read32(RegClrStopDet)
			}
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		runtime.Gosched()
	}
	c.stats.Timeouts++
	return ErrTimeout
}

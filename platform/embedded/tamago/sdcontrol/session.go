// SPDX-License-Identifier: GPL-3.0-or-later
package sdcontrol

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/DeanoC/fes/platform/embedded/tamago/block"
	"github.com/DeanoC/fes/platform/embedded/tamago/dmaops"
	"github.com/DeanoC/fes/platform/embedded/tamago/sdhci"
)

// Session binds the actual SDHCI driver to one private firmware client.
// The caller must exclusively own both controller and secure-call transport.
// Concurrent emulation-core/SD access is outside this closed-bridge contract.
type Session struct {
	control         sync.Mutex
	ioMu            sync.RWMutex
	firmware        *Client
	device          *sdhci.Device
	cache           *block.Cache
	active          atomic.Bool
	retired         atomic.Bool
	busWidth        atomic.Int32
	admaActive      atomic.Bool
	highSpeed       atomic.Bool
	cmd23Active     atomic.Bool
	transferClockHz atomic.Uint32
}

var _ block.Device = (*Session)(nil)

// Config is supplied by the trusted platform, never by package metadata.
type Config struct {
	InputClockHz uint32
	// TransferClockHz is the trusted platform's opt-in post-discovery rate.
	// Zero preserves identification speed; 25 MHz or 50 MHz (CMD6 High-Speed)
	// require card/board support.
	TransferClockHz uint32
	// HighSpeedMaxCurrentMA caps accepted CMD6 High-Speed card current (mA);
	// zero enforces the 200 mA SD v1.10+ 3.3V baseline.
	HighSpeedMaxCurrentMA uint16
	// BusWidth4Bit opts into 4-bit SD bus mode (ACMD51 SCR + ACMD6) after card
	// selection. Default false preserves conservative 1-bit mode.
	BusWidth4Bit bool
	// MultiBlock opts into CMD18/CMD25 multi-sector transfers (with Auto-CMD12
	// up to 128 sectors / 64 KiB). Default false preserves single-sector I/O.
	MultiBlock bool
	// UseCMD23 opts into issuing CMD23 (SET_BLOCK_COUNT) before multi-sector
	// CMD18/CMD25 transfers instead of Auto-CMD12. Requires MultiBlock and card
	// SCR[33] support.
	UseCMD23 bool
	// UseADMA2 opts into SDHCI ADMA2 DMA transfers backed by DMAPool (requiring
	// controller ADMA2 capability and platform SMMU stream authorization).
	// Default false preserves PIO transfers.
	UseADMA2 bool
	// DMAPool supplies the pinned ADMA2 descriptor table and 64 KiB staging
	// buffer when UseADMA2 is true. Ignored when UseADMA2 is false.
	DMAPool *dmaops.Pool
	// WriteCard authorizes writes only to this independently expected CID and
	// capacity. Zero selects read-only access. Physical write-protect and
	// exclusive writable-media ownership still need platform qualification.
	WriteCard sdhci.Identity
	// NoSocketWriteProtect is a trusted board-wiring property, not package
	// policy or write permission. An expected WriteCard is still mandatory.
	NoSocketWriteProtect bool
}

// Open grants the lease before the first register access. It returns a retired
// non-nil Session on failures after the grant attempt so the caller can retain
// the last receipt. It never attempts an automatic firmware stop after failure.
// InputClockHz must match the qualified platform's actual controller input.
func Open(cpu Caller, io sdhci.Registers, config Config) (*Session, error) {
	if io == nil || config.InputClockHz < 400000 || config.InputClockHz > 200000000 ||
		(config.TransferClockHz != 0 && config.TransferClockHz != 25000000 && config.TransferClockHz != 50000000) ||
		(config.TransferClockHz != 0 && config.InputClockHz < config.TransferClockHz) {
		return nil, fmt.Errorf("sdcontrol: invalid controller or input clock")
	}
	if config.UseADMA2 && config.DMAPool == nil {
		return nil, fmt.Errorf("sdcontrol: UseADMA2 requires DMAPool")
	}
	if config.UseCMD23 && !config.MultiBlock {
		return nil, fmt.Errorf("sdcontrol: UseCMD23 requires MultiBlock")
	}
	writable := config.WriteCard != (sdhci.Identity{})
	if writable {
		var nonzero, notFF byte
		for _, b := range config.WriteCard.CID {
			nonzero |= b
			notFF |= ^b
		}
		// This driver supports CSD v2 SDHC/SDXC geometry only.
		if nonzero == 0 || notFF == 0 || config.WriteCard.Sectors < 1024 ||
			config.WriteCard.Sectors > 1<<32 || config.WriteCard.Sectors%1024 != 0 {
			return nil, fmt.Errorf("sdcontrol: invalid expected writable card")
		}
	}
	firmware, err := NewClient(cpu)
	if err != nil {
		return nil, err
	}
	s := &Session{firmware: firmware}
	if err = firmware.Resume(); err != nil {
		return s, s.fail(err)
	}
	s.active.Store(true)
	s.device, err = sdhci.Open(io, sdhci.Config{
		InputClockHz:          config.InputClockHz,
		TransferClockHz:       config.TransferClockHz,
		HighSpeedMaxCurrentMA: config.HighSpeedMaxCurrentMA,
		BusWidth4Bit:          config.BusWidth4Bit,
		MultiBlock:            config.MultiBlock,
		UseCMD23:              config.UseCMD23,
		UseADMA2:              config.UseADMA2,
		DMAPool:               config.DMAPool,
		PrepareClock:          firmware.Prepare,
		AllowWrites:           writable,
		NoSocketWriteProtect:  config.NoSocketWriteProtect,
	})
	if err != nil {
		return s, s.fail(err)
	}
	if writable && s.device.Identity() != config.WriteCard {
		return s, s.fail(fmt.Errorf("sdcontrol: writable card identity mismatch"))
	}
	s.busWidth.Store(int32(s.device.BusWidth()))
	s.admaActive.Store(s.device.ADMA2Active())
	s.highSpeed.Store(s.device.HighSpeed())
	s.cmd23Active.Store(s.device.CMD23Active())
	s.transferClockHz.Store(s.device.TransferClockHz())
	return s, nil
}
func (s *Session) fail(err error) error {
	s.retired.Store(true)
	s.ioMu.Lock()
	s.active.Store(false)
	if s.cache != nil {
		s.cache.Invalidate()
	}
	s.ioMu.Unlock()
	if s.device != nil {
		s.device.DisableIRQ()
	}
	s.firmware.retire()
	return errors.Join(ErrRetired, err)
}

// EnableCache installs a bounded write-through sector cache on this session.
// The cache is invalidated automatically on every Quiesce, Resume, and fatal
// session retirement, and cached reads are refused whenever the lease is
// quiesced or retired.
func (s *Session) EnableCache(cfg block.CacheConfig) error {
	if s == nil {
		return ErrRetired
	}
	s.control.Lock()
	defer s.control.Unlock()
	if s.firmware == nil || s.device == nil || s.retired.Load() {
		return ErrRetired
	}
	c, err := block.NewCache(s.device, cfg)
	if err != nil {
		return err
	}
	s.ioMu.Lock()
	if s.retired.Load() {
		s.ioMu.Unlock()
		return ErrRetired
	}
	if s.cache != nil {
		s.cache.Invalidate()
	}
	s.cache = c
	s.ioMu.Unlock()
	return nil
}

// DisableCache invalidates and detaches any active sector cache on this session.
func (s *Session) DisableCache() {
	if s == nil {
		return
	}
	s.control.Lock()
	defer s.control.Unlock()
	s.ioMu.Lock()
	if s.cache != nil {
		s.cache.Invalidate()
		s.cache = nil
	}
	s.ioMu.Unlock()
}

// CacheStats returns the active sector cache statistics and true when caching
// is enabled, or zero statistics and false otherwise.
func (s *Session) CacheStats() (block.CacheStats, bool) {
	if s == nil {
		return block.CacheStats{}, false
	}
	s.ioMu.RLock()
	defer s.ioMu.RUnlock()
	if s.cache == nil {
		return block.CacheStats{}, false
	}
	return s.cache.Stats(), true
}
func (s *Session) LastReceipt() (Receipt, bool) {
	if s == nil {
		return Receipt{}, false
	}
	receipt, valid := s.firmware.LastReceipt()
	return receipt, valid && !s.retired.Load()
}
func (s *Session) Identity() sdhci.Identity {
	if s == nil || s.device == nil {
		return sdhci.Identity{}
	}
	return s.device.Identity()
}

// CardInfo returns decoded CID, CSD, and (when queried) SCR metadata for the
// bound SD card, or zero if the session is nil or uninitialized.
func (s *Session) CardInfo() sdhci.CardInfo {
	if s == nil || s.device == nil {
		return sdhci.CardInfo{}
	}
	return s.device.CardInfo()
}

// BusWidth returns the active SD data bus width (1 or 4), or 0 if uninitialized.
func (s *Session) BusWidth() int {
	if s == nil {
		return 0
	}
	return int(s.busWidth.Load())
}

// ADMA2Active reports whether ADMA2 DMA transfers are enabled on this session.
func (s *Session) ADMA2Active() bool {
	return s != nil && s.admaActive.Load()
}

// HighSpeed reports whether SD High-Speed (50 MHz via CMD6) is active.
func (s *Session) HighSpeed() bool {
	return s != nil && s.highSpeed.Load()
}

// CMD23Active reports whether pre-defined CMD23 (SET_BLOCK_COUNT) multi-block
// transfers are active on this session.
func (s *Session) CMD23Active() bool {
	return s != nil && s.cmd23Active.Load()
}

// TransferClockHz returns the active SD clock rate in Hz, or 0 if uninitialized.
func (s *Session) TransferClockHz() uint32 {
	if s == nil {
		return 0
	}
	return s.transferClockHz.Load()
}

// Retired reports whether s is nil or has entered terminal ErrRetired containment.
// Lock-free and safe for watchdog/liveness probes.
func (s *Session) Retired() bool       { return s == nil || s.retired.Load() }
func (s *Session) SectorCount() uint64 { return s.Identity().Sectors }
func (s *Session) ReadSectors(lba uint64, dst []byte) error {
	if s == nil || s.firmware == nil || s.device == nil || s.retired.Load() {
		return ErrRetired
	}
	s.ioMu.RLock()
	if s.retired.Load() {
		s.ioMu.RUnlock()
		return ErrRetired
	}
	if !s.active.Load() {
		s.ioMu.RUnlock()
		return sdhci.ErrQuiesced
	}
	var err error
	if s.cache != nil {
		err = s.cache.ReadSectors(lba, dst)
	} else {
		err = s.device.ReadSectors(lba, dst)
	}
	fatal := err != nil && !errors.Is(err, sdhci.ErrQuiesced) && !errors.Is(err, block.ErrBounds) && !errors.Is(err, block.ErrAlignment)
	if fatal {
		s.retired.Store(true)
	}
	s.ioMu.RUnlock()
	if fatal {
		return s.fail(err)
	}
	return err
}
func (s *Session) WriteSectors(lba uint64, src []byte) error {
	if s == nil || s.firmware == nil || s.device == nil || s.retired.Load() {
		return ErrRetired
	}
	s.ioMu.RLock()
	if s.retired.Load() {
		s.ioMu.RUnlock()
		return ErrRetired
	}
	if !s.active.Load() {
		s.ioMu.RUnlock()
		return sdhci.ErrQuiesced
	}
	var err error
	if s.cache != nil {
		err = s.cache.WriteSectors(lba, src)
	} else {
		err = s.device.WriteSectors(lba, src)
	}
	fatal := err != nil && !errors.Is(err, sdhci.ErrQuiesced) && !errors.Is(err, block.ErrReadOnly) &&
		!errors.Is(err, block.ErrBounds) && !errors.Is(err, block.ErrAlignment)
	if fatal {
		s.retired.Store(true)
	}
	s.ioMu.RUnlock()
	if fatal {
		return s.fail(err)
	}
	return err
}
func (s *Session) Flush() error {
	if s == nil || s.firmware == nil || s.device == nil || s.retired.Load() {
		return ErrRetired
	}
	s.ioMu.RLock()
	if s.retired.Load() {
		s.ioMu.RUnlock()
		return ErrRetired
	}
	if !s.active.Load() {
		s.ioMu.RUnlock()
		return sdhci.ErrQuiesced
	}
	var err error
	if s.cache != nil {
		err = s.cache.Flush()
	} else {
		err = s.device.Flush()
	}
	fatal := err != nil && !errors.Is(err, sdhci.ErrQuiesced)
	if fatal {
		s.retired.Store(true)
	}
	s.ioMu.RUnlock()
	if fatal {
		return s.fail(err)
	}
	return err
}

// Quiesce fences the PIO driver before asking firmware to independently drain
// and contain SD. A success receipt supplies the new shared bridge generation.
func (s *Session) Quiesce() error {
	if s == nil {
		return ErrRetired
	}
	s.control.Lock()
	defer s.control.Unlock()
	// Drain in-flight cached/uncached I/O under ioMu before checking retired/active
	// so a concurrent fatal I/O cannot slip past the check into hardware teardown.
	s.ioMu.Lock()
	if s.firmware == nil || s.device == nil || s.retired.Load() {
		s.ioMu.Unlock()
		return ErrRetired
	}
	if !s.active.Load() {
		s.ioMu.Unlock()
		return sdhci.ErrQuiesced
	}
	s.active.Store(false)
	if s.cache != nil {
		s.cache.Invalidate()
	}
	s.ioMu.Unlock()
	if err := s.device.Quiesce(); err != nil {
		return s.fail(err)
	}
	if err := s.firmware.Stop(); err != nil {
		return s.fail(err)
	}
	return nil
}

// Resume obtains a new lease, prepares the clock at the host-reset boundary and
// rediscovers the same card before allowing access to the retained filesystem.
func (s *Session) Resume() error { return s.resume(0, 0, false) }

// ResumeAfterReturn accepts an explicit expected closed fabric return. Secure
// firmware independently checks that provenance before granting SD access;
// controller/card reinitialization still requires the original CID/capacity.
func (s *Session) ResumeAfterReturn(owner, generation uint64) error {
	return s.resume(owner, generation, true)
}

func (s *Session) resume(owner, generation uint64, returned bool) error {
	if s == nil {
		return ErrRetired
	}
	s.control.Lock()
	defer s.control.Unlock()
	if s.firmware == nil || s.device == nil || s.retired.Load() {
		return ErrRetired
	}
	if s.active.Load() {
		return ErrTransition
	}
	var err error
	if returned {
		last, valid := s.firmware.LastReceipt()
		if !valid {
			return s.fail(ErrRetired)
		}
		if owner != last.Owner || generation <= last.Generation || generation > ^uint64(0)-3 {
			return ErrTransition
		}
		err = s.firmware.ResumeAfterReturn(owner, generation)
	} else {
		err = s.firmware.Resume()
	}
	if err != nil {
		return s.fail(err)
	}
	if err := s.device.Resume(); err != nil {
		return s.fail(err)
	}
	s.busWidth.Store(int32(s.device.BusWidth()))
	s.admaActive.Store(s.device.ADMA2Active())
	s.highSpeed.Store(s.device.HighSpeed())
	s.cmd23Active.Store(s.device.CMD23Active())
	s.transferClockHz.Store(s.device.TransferClockHz())
	s.ioMu.Lock()
	if s.retired.Load() {
		s.ioMu.Unlock()
		return ErrRetired
	}
	if s.cache != nil {
		s.cache.Invalidate()
	}
	s.active.Store(true)
	s.ioMu.Unlock()
	return nil
}

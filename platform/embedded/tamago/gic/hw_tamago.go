//go:build tamago && arm64

// SPDX-License-Identifier: GPL-3.0-or-later

package gic

import (
	"os"
	"os/signal"
	"runtime"
	"runtime/goos"

	"github.com/usbarmory/tamago/arm64"
)

type nativeAccess struct{}

func (nativeAccess) HasGICv3() bool                  { return hasGICv3() }
func (nativeAccess) MPIDR() uint64                   { return readMPIDR() }
func (nativeAccess) Read32(addr uint64) uint32       { return read32(addr) }
func (nativeAccess) Write32(addr uint64, val uint32) { write32(addr, val) }
func (nativeAccess) Read64(addr uint64) uint64       { return read64(addr) }
func (nativeAccess) Write64(addr uint64, val uint64) { write64(addr, val) }
func (nativeAccess) InitCPU(pmr uint8)               { initCPUInterface(pmr) }
func (nativeAccess) Ack() uint32                     { return ackIRQ() }
func (nativeAccess) End(id uint32)                   { endIRQ(id) }
func (nativeAccess) SendSGI(val uint64)              { sendSGI(val) }

// New creates a GICv3 Controller wired directly to ARM64 MMIO and EL1 system registers.
func New(gicd, gicr uint64) *Controller {
	return NewWithAccess(gicd, gicr, nativeAccess{})
}

// Serve dispatches Group 1 IRQs relayed by TamaGo's VBAR_EL1 vector until stop
// is closed, then masks CPU IRQs and unregisters the signal handler.
func (g *Controller) Serve(stop <-chan struct{}) {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, arm64.IRQ_SIGNAL)
	defer func() {
		DisableCPUIRQ()
		signal.Stop(sig)
	}()

	for {
		// Wait until os/signal's watch goroutine is parked in time.Sleep so
		// arm64.handleInterrupt -> os/signal.Relay can wake it without losing
		// an immediately pending IRQ.
		for !signal.Waiting() {
			select {
			case <-stop:
				return
			default:
				runtime.Gosched()
			}
		}
		EnableCPUIRQ()
		select {
		case <-stop:
			return
		case <-sig:
			g.Dispatch()
		}
	}
}

// EnableCPUIRQ unmasks IRQ exceptions in DAIF.
func EnableCPUIRQ()

// DisableCPUIRQ masks IRQ exceptions in DAIF.
func DisableCPUIRQ()

// ArmPhysicalTimer arms the EL1 non-secure physical timer (PPI 30) to fire
// after ticks counts of CNTPCT_EL0.
func ArmPhysicalTimer(ticks uint32)

// DisarmPhysicalTimer disables and masks the EL1 physical timer (PPI 30).
func DisarmPhysicalTimer()

// WaitForInterrupt suspends the core until an enabled interrupt becomes pending.
func WaitForInterrupt()

type nativeIdleAccess struct{}

func (nativeIdleAccess) CounterFreq() uint64   { return counterFreq() }
func (nativeIdleAccess) NowNanos() int64       { return goos.Nanotime() }
func (nativeIdleAccess) IRQEnabled() bool      { return irqEnabled() }
func (nativeIdleAccess) MaskIRQ() bool         { return maskIRQ() }
func (nativeIdleAccess) UnmaskIRQ()            { EnableCPUIRQ() }
func (nativeIdleAccess) ArmTimer(ticks uint32) { ArmPhysicalTimer(ticks) }
func (nativeIdleAccess) DisarmTimer()          { DisarmPhysicalTimer() }
func (nativeIdleAccess) WFI()                  { WaitForInterrupt() }

// EnableIdle installs a WFI-backed runtime/goos.Idle governor driven by the
// EL1 physical timer (PPI 30). Call after Init().
func (g *Controller) EnableIdle() error {
	if g.gicr == 0 {
		return ErrInvalidConfig
	}
	g.Register(IRQPhysTimer, DisarmPhysicalTimer)
	g.Enable(IRQPhysTimer)
	g.SetIdleAccess(nativeIdleAccess{})
	goos.Idle = g.IdleStep
	return nil
}

// DisableIdle restores the polling no-op runtime/goos.Idle stub and disables PPI 30.
func (g *Controller) DisableIdle() {
	goos.Idle = func(int64) {}
	DisarmPhysicalTimer()
	g.Disable(IRQPhysTimer)
	g.Register(IRQPhysTimer, nil)
	g.SetIdleAccess(nil)
}

func hasGICv3() bool
func readMPIDR() uint64
func read32(addr uint64) uint32
func write32(addr uint64, val uint32)
func read64(addr uint64) uint64
func write64(addr uint64, val uint64)
func initCPUInterface(pmr uint8)
func ackIRQ() uint32
func endIRQ(id uint32)
func sendSGI(val uint64)
func counterFreq() uint64
func irqEnabled() bool
func maskIRQ() bool

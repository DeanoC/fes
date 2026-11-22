// SPDX-License-Identifier: GPL-3.0-or-later
package dwc2

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/gic"
	"github.com/DeanoC/fes/platform/embedded/tamago/input"
	"github.com/DeanoC/fes/platform/embedded/tamago/usbcontrol"
)

type mockGICAccess struct {
	mu      sync.Mutex
	regs32  map[uint64]uint32
	pending []uint32
}

func newMockGIC(t *testing.T) (*gic.Controller, *mockGICAccess) {
	t.Helper()
	const gicd, gicr = uint64(0x1d000000), uint64(0x1d060000)
	hw := &mockGICAccess{
		regs32: map[uint64]uint32{
			gicd + gic.GICD_TYPER: 9, // 320 INTIDs (covers Agilex 5 IRQUSB0 = 125)
		},
	}
	ctrl := gic.NewWithAccess(gicd, gicr, hw)
	if err := ctrl.Init(); err != nil {
		t.Fatalf("gic.Init: %v", err)
	}
	return ctrl, hw
}

func (m *mockGICAccess) HasGICv3() bool { return true }
func (m *mockGICAccess) MPIDR() uint64  { return 0 }
func (m *mockGICAccess) Read32(addr uint64) uint32 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.regs32[addr]
}
func (m *mockGICAccess) Write32(addr uint64, val uint32) {
	m.mu.Lock()
	defer m.mu.Unlock()
	const gicd = uint64(0x1d000000)
	if addr >= gicd+gic.GICD_ISENABLER && addr < gicd+gic.GICD_ICENABLER {
		m.regs32[addr] |= val
		return
	}
	if addr >= gicd+gic.GICD_ICENABLER && addr < gicd+gic.GICD_ISPENDR {
		isenabler := addr - gic.GICD_ICENABLER + gic.GICD_ISENABLER
		m.regs32[isenabler] &^= val
		return
	}
	m.regs32[addr] = val
}
func (m *mockGICAccess) Read64(addr uint64) uint64 {
	if addr == 0x1d060000+gic.GICR_TYPER {
		return 1 << 4
	}
	return 0
}
func (m *mockGICAccess) Write64(uint64, uint64) {}
func (m *mockGICAccess) InitCPU(uint8)          {}
func (m *mockGICAccess) SendSGI(uint64)         {}
func (m *mockGICAccess) Ack() uint32 {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.pending) == 0 {
		return gic.SpuriousID
	}
	id := m.pending[0]
	m.pending = m.pending[1:]
	return id
}
func (m *mockGICAccess) End(uint32) {}

func (m *mockGICAccess) isEnabled(id uint32) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	word := uint64(0x1d000000) + gic.GICD_ISENABLER + uint64(id/32)*4
	return m.regs32[word]&(1<<(id%32)) != 0
}

func (m *mockGICAccess) fire(ctrl *gic.Controller, id uint32) {
	m.mu.Lock()
	m.pending = append(m.pending, id)
	m.mu.Unlock()
	ctrl.Dispatch()
}

// syncController wraps controller with a mutex for concurrent -race IRQ tests.
type syncController struct {
	mu   sync.Mutex
	base *controller
}

func newSyncController() *syncController {
	return &syncController{base: newController()}
}

func (s *syncController) Read32(reg uint32) uint32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.base.Read32(reg)
}

func (s *syncController) Write32(reg, value uint32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.base.Write32(reg, value)
}

func (s *syncController) Now() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.base.Now()
}

func (s *syncController) reg(r uint32) uint32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.base.regs[r]
}

func (s *syncController) setReg(r, v uint32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.base.regs[r] = v
}

func (s *syncController) accesses() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.base.accesses
}

const testUSBIRQ = uint32(125) // Agilex 5 IRQUSB0

func TestDWC2IRQValidationAndNilReceivers(t *testing.T) {
	var nilHost *Host
	if err := nilHost.EnableIRQ(nil, testUSBIRQ); err == nil {
		t.Fatal("nil host accepted EnableIRQ")
	}
	nilHost.DisableIRQ()
	if nilHost.Ready() != nil || nilHost.IRQCount() != 0 {
		t.Fatal("nil host returned non-zero IRQ state")
	}

	var nilGamepad *GamepadHost
	if err := nilGamepad.EnableIRQ(nil, testUSBIRQ); err == nil {
		t.Fatal("nil gamepad accepted EnableIRQ")
	}
	nilGamepad.DisableIRQ()
	if nilGamepad.Ready() != nil || nilGamepad.IRQCount() != 0 {
		t.Fatal("nil gamepad returned non-zero IRQ state")
	}

	ctrl, ghw := newMockGIC(t)
	hw := newSyncController()
	host, err := Open(hw, Config{PHYClockHz: 60000000})
	if err != nil {
		t.Fatal(err)
	}
	if host.Ready() != nil {
		t.Fatal("polling host exposed Ready channel before EnableIRQ")
	}
	if err := host.EnableIRQ(nil, testUSBIRQ); err == nil {
		t.Fatal("nil GIC accepted")
	}
	if err := host.EnableIRQ(ctrl, ctrl.Lines()); err != gic.ErrInvalidConfig {
		t.Fatalf("out-of-range INTID got %v, want ErrInvalidConfig", err)
	}
	// PPI 30 is the physical timer used by the runtime idle governor. DWC2
	// must reject it without replacing the timer's registered handler or
	// touching controller state.
	const physicalTimerPPI = uint32(30)
	timerFired := false
	ctrl.Register(physicalTimerPPI, func() { timerFired = true })
	beforeMMIO := hw.accesses()
	if err := host.EnableIRQ(ctrl, physicalTimerPPI); err != gic.ErrInvalidConfig {
		t.Fatalf("physical-timer PPI got %v, want ErrInvalidConfig", err)
	}
	if got := hw.accesses(); got != beforeMMIO {
		t.Fatalf("invalid PPI EnableIRQ touched DWC2 registers: accesses %d->%d", beforeMMIO, got)
	}
	ghw.fire(ctrl, physicalTimerPPI)
	if !timerFired {
		t.Fatal("invalid PPI EnableIRQ replaced the physical-timer handler")
	}

	// PausePolling blocks EnableIRQ.
	if err := host.PausePolling(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := host.EnableIRQ(ctrl, testUSBIRQ); err == nil {
		t.Fatal("paused host accepted EnableIRQ")
	}
	if err := host.ResumePolling(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Foreign DMA ownership blocks EnableIRQ without modifying registers.
	hw.setReg(gahb, 1<<5)
	if err := host.EnableIRQ(ctrl, testUSBIRQ); err == nil {
		t.Fatal("DMA-owned host accepted EnableIRQ")
	}
	hw.setReg(gahb, 0)

	if err := host.EnableIRQ(ctrl, testUSBIRQ); err != nil {
		t.Fatal(err)
	}
	if err := host.EnableIRQ(ctrl, testUSBIRQ); err == nil {
		t.Fatal("duplicate EnableIRQ accepted")
	}
	host.DisableIRQ()
	if host.Ready() != nil {
		t.Fatal("Ready remained non-nil after DisableIRQ")
	}
	// Faulted host rejects EnableIRQ.
	hw.setReg(hprt, 0)
	if _, err := host.PollKeyboard(); err == nil {
		t.Fatal("disconnected host succeeded")
	}
	if err := host.EnableIRQ(ctrl, testUSBIRQ); err == nil {
		t.Fatal("faulted host accepted EnableIRQ")
	}
}

func TestDWC2IRQKeyboardReportAndStormGuard(t *testing.T) {
	ctrl, ghw := newMockGIC(t)
	hw := newSyncController()
	want := []byte{0, 0, 4, 0, 0, 0, 0, 0}
	hw.base.reports = [][]byte{want}

	host, err := Open(hw, Config{PHYClockHz: 60000000})
	if err != nil {
		t.Fatal(err)
	}
	if err := host.EnableIRQ(ctrl, testUSBIRQ); err != nil {
		t.Fatal(err)
	}
	defer host.DisableIRQ()

	if !ghw.isEnabled(testUSBIRQ) {
		t.Fatal("GIC SPI not enabled")
	}
	if hw.reg(gahb) != gahbGlobalIRQ || hw.reg(haintmsk) != haintCh0 || hw.reg(gintmask) != irqMask {
		t.Fatalf("unexpected IRQ regs gahb=%#x haintmsk=%#x gintmask=%#x", hw.reg(gahb), hw.reg(haintmsk), hw.reg(gintmask))
	}
	ready := host.Ready()
	if ready == nil {
		t.Fatal("Ready channel nil after EnableIRQ")
	}

	// First poll arms channel 0 (and queues RX/HCINT in fake controller).
	r, err := host.PollKeyboard()
	if err != nil || r != nil {
		t.Fatalf("arm poll: report=%v err=%v", r, err)
	}
	// Drain any immediate readiness notice from armIRQ's GINTSTS re-check.
	select {
	case <-ready:
	default:
	}

	// Simulate level-triggered GIC SPI firing: handler must immediately clear
	// GINTMSK=0 (preventing storms), increment IRQCount, and signal Ready().
	ghw.fire(ctrl, testUSBIRQ)
	if hw.reg(gintmask) != 0 {
		t.Fatalf("handler left GINTMSK=%#x non-zero", hw.reg(gintmask))
	}
	if host.IRQCount() != 1 {
		t.Fatalf("IRQCount=%d, want 1", host.IRQCount())
	}
	select {
	case <-ready:
	default:
		t.Fatal("Ready not signalled by IRQ handler")
	}

	// Poll until the report completes; armIRQ re-enables GINTMSK and narrows
	// HCINTMSK to CHHLTD while retiring the channel.
	var got []byte
	for range 16 {
		got, err = host.PollKeyboard()
		if err != nil {
			t.Fatal(err)
		}
		if got != nil {
			break
		}
		if host.pending.active && host.pending.halting {
			if hw.reg(hcmask) != hcintChHltd {
				t.Fatalf("halting channel left hcmask=%#x, want %#x", hw.reg(hcmask), hcintChHltd)
			}
		}
		if hw.reg(gintmask) != irqMask {
			t.Fatalf("pollInput did not re-arm GINTMSK: got %#x", hw.reg(gintmask))
		}
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("report=%x, want %x", got, want)
	}
	if hw.reg(gintmask) != irqMask {
		t.Fatalf("completed poll left GINTMSK=%#x", hw.reg(gintmask))
	}

	host.DisableIRQ()
	if ghw.isEnabled(testUSBIRQ) {
		t.Fatal("DisableIRQ left GIC SPI enabled")
	}
	if hw.reg(gahb) != 0 || hw.reg(gintmask) != 0 || hw.reg(haintmsk) != 0 {
		t.Fatalf("DisableIRQ left IRQ regs dirty: gahb=%#x gintmask=%#x haintmsk=%#x",
			hw.reg(gahb), hw.reg(gintmask), hw.reg(haintmsk))
	}
}

func TestDWC2IRQGamepadAndPauseResumeZeroMMIO(t *testing.T) {
	ctrl, ghw := newMockGIC(t)
	hw := &syncController{base: gamepadController(t)}
	want1 := []byte{0x00, 0x80, 0x01, 0x00}
	want2 := []byte{0x80, 0x80, 0x02, 0x00}
	hw.base.reports = [][]byte{want1, want2}

	gp, err := OpenGamepad(hw, Config{PHYClockHz: 60000000})
	if err != nil {
		t.Fatal(err)
	}
	if err := gp.EnableIRQ(ctrl, testUSBIRQ); err != nil {
		t.Fatal(err)
	}
	if gp.Ready() == nil {
		t.Fatal("GamepadHost Ready nil")
	}

	// Arm a packet, then PausePolling must drain it, disable GIC SPI, clear
	// IRQ registers, and allow zero MMIO while paused.
	if _, err := gp.PollGamepad(); err != nil {
		t.Fatal(err)
	}
	if err := gp.PausePolling(context.Background()); err != nil {
		t.Fatal(err)
	}
	if ghw.isEnabled(testUSBIRQ) {
		t.Fatal("PausePolling left GIC SPI enabled")
	}
	if hw.reg(gahb) != 0 || hw.reg(gintmask) != 0 || hw.reg(haintmsk) != 0 {
		t.Fatalf("PausePolling left IRQ regs set: gahb=%#x gintmask=%#x haintmsk=%#x",
			hw.reg(gahb), hw.reg(gintmask), hw.reg(haintmsk))
	}
	// Ready channel stays non-nil across pause/resume so Tenfoot doesn't need
	// to re-register it.
	if gp.Ready() == nil {
		t.Fatal("PausePolling cleared Ready channel")
	}

	beforePauseMMIO := hw.accesses()
	// Spurious GIC dispatch while paused must touch zero MMIO.
	ghw.fire(ctrl, testUSBIRQ)
	if hw.accesses() != beforePauseMMIO || gp.IRQCount() != 0 {
		t.Fatalf("paused IRQ touched MMIO: accesses %d->%d count=%d", beforePauseMMIO, hw.accesses(), gp.IRQCount())
	}
	// ResumePolling must also touch zero MMIO.
	if err := gp.ResumePolling(context.Background()); err != nil {
		t.Fatal(err)
	}
	if hw.accesses() != beforePauseMMIO {
		t.Fatalf("ResumePolling touched MMIO: %d->%d", beforePauseMMIO, hw.accesses())
	}

	// Next PollGamepad verifies ownership and restores GIC + DWC2 IRQ state.
	var got []byte
	for range 20 {
		hw.mu.Lock()
		hw.base.tick = hw.base.tick.Add(15 * time.Millisecond)
		hw.mu.Unlock()
		got, err = gp.PollGamepad()
		if err != nil {
			t.Fatal(err)
		}
		if got != nil {
			break
		}
	}
	if !bytes.Equal(got, want2) {
		t.Fatalf("post-resume gamepad report=%x, want %x", got, want2)
	}
	if !ghw.isEnabled(testUSBIRQ) || hw.reg(gahb) != gahbGlobalIRQ || hw.reg(haintmsk) != haintCh0 {
		t.Fatalf("post-resume IRQ state not restored: enabled=%t gahb=%#x haintmsk=%#x",
			ghw.isEnabled(testUSBIRQ), hw.reg(gahb), hw.reg(haintmsk))
	}
	ghw.fire(ctrl, testUSBIRQ)
	if gp.IRQCount() != 1 {
		t.Fatalf("post-resume IRQCount=%d, want 1", gp.IRQCount())
	}

	// Pause again and DisableIRQ while paused: must unregister GIC with ZERO MMIO.
	if err := gp.PausePolling(context.Background()); err != nil {
		t.Fatal(err)
	}
	pausedAccesses := hw.accesses()
	gp.DisableIRQ()
	if hw.accesses() != pausedAccesses {
		t.Fatalf("DisableIRQ while paused touched MMIO: %d->%d", pausedAccesses, hw.accesses())
	}
	if ghw.isEnabled(testUSBIRQ) || gp.Ready() != nil {
		t.Fatal("DisableIRQ while paused did not unregister GIC")
	}
}

func TestDWC2IRQPIOOwnershipAndFailContainment(t *testing.T) {
	cases := []struct {
		name      string
		mutate    func(*syncController)
		allowMMIO bool
	}{
		{"dma", func(hw *syncController) { hw.setReg(gahb, gahbGlobalIRQ|1<<5) }, false},
		{"desc-dma", func(hw *syncController) { hw.setReg(hcfg, 1<<23) }, true},
		{"cleared-global-irq", func(hw *syncController) { hw.setReg(gahb, 0) }, true},
		{"foreign-gintmask", func(hw *syncController) { hw.setReg(gintmask, 1) }, true},
		{"foreign-haintmsk", func(hw *syncController) { hw.setReg(haintmsk, 3) }, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctrl, ghw := newMockGIC(t)
			hw := newSyncController()
			host, err := Open(hw, Config{PHYClockHz: 60000000})
			if err != nil {
				t.Fatal(err)
			}
			if err := host.EnableIRQ(ctrl, testUSBIRQ); err != nil {
				t.Fatal(err)
			}
			ready := host.Ready()
			select {
			case <-ready:
			default:
			}
			tc.mutate(hw)
			if _, err := host.PollKeyboard(); err == nil {
				t.Fatal("corrupted PIO/IRQ state accepted")
			}
			if ghw.isEnabled(testUSBIRQ) {
				t.Fatal("fail() left GIC SPI enabled")
			}
			if host.Ready() != nil {
				t.Fatal("fail() left Ready channel non-nil")
			}
			select {
			case <-ready:
			default:
				t.Fatal("fail() did not wake Ready waiter")
			}
			if !tc.allowMMIO {
				if hw.reg(gahb)&(1<<5) == 0 {
					t.Fatal("fail() modified GAHBCFG after external DMA takeover")
				}
			} else if hw.reg(gahb) != 0 || hw.reg(gintmask) != 0 || hw.reg(haintmsk) != 0 {
				t.Fatalf("fail() left IRQ regs set: gahb=%#x gintmask=%#x haintmsk=%#x",
					hw.reg(gahb), hw.reg(gintmask), hw.reg(haintmsk))
			}
		})
	}
}

func TestDWC2IRQResumeRejectsPauseTampering(t *testing.T) {
	ctrl, ghw := newMockGIC(t)
	hw := newSyncController()
	host, err := Open(hw, Config{PHYClockHz: 60000000})
	if err != nil {
		t.Fatal(err)
	}
	if err := host.EnableIRQ(ctrl, testUSBIRQ); err != nil {
		t.Fatal(err)
	}
	if err := host.PausePolling(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := host.ResumePolling(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Tamper with gintmask while suspended before first poll.
	hw.setReg(gintmask, irqMask)
	if _, err := host.PollKeyboard(); err == nil {
		t.Fatal("resume accepted tampered gintmask")
	}
	if ghw.isEnabled(testUSBIRQ) {
		t.Fatal("failed resume enabled GIC SPI")
	}
}

func TestDWC2IRQConcurrentDispatchRace(t *testing.T) {
	ctrl, ghw := newMockGIC(t)
	hw := newSyncController()
	for i := range 8 {
		hw.base.reports = append(hw.base.reports, []byte{0, 0, byte(4 + i), 0, 0, 0, 0, 0})
	}
	host, err := Open(hw, Config{PHYClockHz: 60000000})
	if err != nil {
		t.Fatal(err)
	}
	if err := host.EnableIRQ(ctrl, testUSBIRQ); err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
				ghw.fire(ctrl, testUSBIRQ)
				time.Sleep(100 * time.Microsecond)
			}
		}
	}()

	got := 0
	for range 200 {
		hw.mu.Lock()
		hw.base.tick = hw.base.tick.Add(2 * time.Millisecond)
		hw.mu.Unlock()
		r, err := host.PollKeyboard()
		if err != nil {
			close(stop)
			<-done
			t.Fatal(err)
		}
		if r != nil {
			got++
			if got == 8 {
				break
			}
		}
	}
	host.DisableIRQ()
	close(stop)
	<-done
	if got != 8 {
		t.Fatalf("got %d reports, want 8", got)
	}
	if host.IRQCount() == 0 {
		t.Fatal("expected at least one handled IRQ")
	}
}

func TestDWC2IRQWithOwnedUSBControlClose(t *testing.T) {
	ctrl, ghw := newMockGIC(t)
	hw := newSyncController()
	fw := &ownedFirmware{t: t, controller: hw.base}
	ctx := context.Background()
	client, err := usbcontrol.New(ctx, fw, 17)
	if err != nil {
		t.Fatal(err)
	}
	source, err := usbcontrol.Open(ctx, client, func() (input.USBReports, error) {
		return Open(hw, Config{PHYClockHz: 60000000})
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := source.EnableIRQ(ctrl, testUSBIRQ); err != nil {
		t.Fatal(err)
	}
	if source.Ready() == nil || !ghw.isEnabled(testUSBIRQ) {
		t.Fatal("owned source did not enable DWC2 IRQ")
	}
	ghw.fire(ctrl, testUSBIRQ)
	if source.IRQCount() != 1 {
		t.Fatalf("owned IRQCount=%d, want 1", source.IRQCount())
	}
	if err := source.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if ghw.isEnabled(testUSBIRQ) || source.Ready() != nil {
		t.Fatal("owned Close did not tear down GIC SPI")
	}
}

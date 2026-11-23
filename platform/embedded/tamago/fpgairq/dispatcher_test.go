// SPDX-License-Identifier: GPL-3.0-or-later

package fpgairq

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/gic"
)

type fakeGICAccess struct {
	mu      sync.Mutex
	regs32  map[uint64]uint32
	regs64  map[uint64]uint64
	pending []uint32
}

func newFakeGIC(t *testing.T, itLinesField uint32) (*gic.Controller, *fakeGICAccess) {
	t.Helper()
	acc := &fakeGICAccess{
		regs32: map[uint64]uint32{
			0x1d000004: itLinesField, // GICD_TYPER: (N+1)*32 lines
			0x1d060008: 0x00000010,
		},
		regs64: map[uint64]uint64{
			0x1d060008: 0x00000010, // GICR_TYPER Last=1
		},
	}
	ctrl := gic.NewWithAccess(0x1d000000, 0x1d060000, acc)
	if err := ctrl.Init(); err != nil {
		t.Fatalf("gic.Init: %v", err)
	}
	return ctrl, acc
}

func (a *fakeGICAccess) HasGICv3() bool { return true }
func (a *fakeGICAccess) MPIDR() uint64  { return 0 }
func (a *fakeGICAccess) Read32(addr uint64) uint32 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.regs32[addr]
}
func (a *fakeGICAccess) Write32(addr uint64, val uint32) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.regs32[addr] = val
	// Track GICD_ISPENDR writes so TriggerPending can be dispatched in host tests.
	const ispendrBase = 0x1d000200
	if addr >= ispendrBase && addr < ispendrBase+0x80 && val != 0 {
		word := uint32((addr - ispendrBase) / 4)
		for bit := uint32(0); bit < 32; bit++ {
			if val&(1<<bit) != 0 {
				a.pending = append(a.pending, word*32+bit)
			}
		}
	}
}
func (a *fakeGICAccess) Read64(addr uint64) uint64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.regs64[addr]
}
func (a *fakeGICAccess) Write64(addr uint64, val uint64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.regs64[addr] = val
}
func (a *fakeGICAccess) InitCPU(uint8) {}
func (a *fakeGICAccess) Ack() uint32 {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.pending) == 0 {
		return 1023
	}
	id := a.pending[0]
	a.pending = a.pending[1:]
	return id
}
func (a *fakeGICAccess) End(uint32)     {}
func (a *fakeGICAccess) SendSGI(uint64) {}

func (a *fakeGICAccess) raise(id uint32) {
	a.mu.Lock()
	a.pending = append(a.pending, id)
	a.mu.Unlock()
}

func TestDispatcherValidationAndExclusivity(t *testing.T) {
	if _, err := New(nil); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("nil GIC err=%v", err)
	}
	smallCtrl, _ := newFakeGIC(t, 0) // 32 lines -> <= BaseSPI (48)
	if _, err := New(smallCtrl); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("small GIC err=%v", err)
	}

	// 64 lines -> 16 FPGA lines (48..63).
	partialCtrl, _ := newFakeGIC(t, 1)
	partialDisp, err := New(partialCtrl)
	if err != nil {
		t.Fatalf("partial New: %v", err)
	}
	if partialDisp.LineCount() != 16 {
		t.Fatalf("partial LineCount=%d, want 16", partialDisp.LineCount())
	}

	// 256 lines -> full 64 FPGA lines (48..111).
	ctrl, _ := newFakeGIC(t, 7)
	disp, err := New(ctrl)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if disp.LineCount() != LineCount {
		t.Fatalf("LineCount=%d, want %d", disp.LineCount(), LineCount)
	}

	if _, err := disp.Open(Config{Line: LineCount, Trigger: Edge}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("out-of-range line err=%v", err)
	}
	if _, err := disp.Open(Config{Line: 0, Trigger: Trigger(9)}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("invalid trigger err=%v", err)
	}
	if _, err := disp.Open(Config{Line: 0, Trigger: Level}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("level without Ack err=%v", err)
	}

	db, err := disp.Open(Config{Line: 3, Trigger: Edge})
	if err != nil {
		t.Fatalf("Open line 3: %v", err)
	}
	if db.Line() != 3 || db.IRQ() != BaseSPI+3 || disp.ActiveCount() != 1 {
		t.Fatalf("line=%d irq=%d active=%d", db.Line(), db.IRQ(), disp.ActiveCount())
	}
	if _, err := disp.Open(Config{Line: 3, Trigger: Edge}); !errors.Is(err, ErrLineInUse) {
		t.Fatalf("duplicate Open err=%v, want ErrLineInUse", err)
	}

	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if disp.ActiveCount() != 0 {
		t.Fatalf("ActiveCount=%d after Close", disp.ActiveCount())
	}

	// Re-opening a released line succeeds; closing dispatcher cleans it up.
	db2, err := disp.Open(Config{Line: 3, Trigger: Edge})
	if err != nil {
		t.Fatalf("re-Open line 3: %v", err)
	}
	if err := disp.Close(); err != nil {
		t.Fatalf("Dispatcher.Close: %v", err)
	}
	if err := disp.Close(); err != nil {
		t.Fatalf("second Dispatcher.Close: %v", err)
	}
	if _, err := disp.Open(Config{Line: 0, Trigger: Edge}); !errors.Is(err, ErrClosed) {
		t.Fatalf("Open after Dispatcher.Close err=%v, want ErrClosed", err)
	}
	if err := db2.Wait(context.Background()); !errors.Is(err, ErrClosed) {
		t.Fatalf("Wait on dispatcher-closed doorbell err=%v, want ErrClosed", err)
	}
}

func TestDoorbellEdgeLevelWaitAndYield(t *testing.T) {
	ctrl, acc := newFakeGIC(t, 7)
	disp, err := New(ctrl)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer disp.Close()

	var acks atomic.Uint64
	var events atomic.Uint64
	db, err := disp.Open(Config{
		Line:    0,
		Trigger: Level,
		Ack:     func() { acks.Add(1) },
		OnEvent: func(n uint64) { events.Store(n) },
	})
	if err != nil {
		t.Fatalf("Open level line 0: %v", err)
	}

	// TriggerPending + Dispatch wakes Ready() and increments Count().
	db.TriggerPending()
	ctrl.Dispatch()
	if db.Count() != 1 || acks.Load() != 1 || events.Load() != 1 {
		t.Fatalf("count=%d acks=%d events=%d", db.Count(), acks.Load(), events.Load())
	}
	select {
	case <-db.Ready():
	default:
		t.Fatal("Ready channel not pulsed")
	}

	// Multiple rapid IRQs coalesce into capacity-1 Ready without blocking.
	acc.raise(BaseSPI)
	acc.raise(BaseSPI)
	acc.raise(BaseSPI)
	ctrl.Dispatch()
	if db.Count() != 4 || acks.Load() != 4 || events.Load() != 4 {
		t.Fatalf("coalesced count=%d acks=%d events=%d", db.Count(), acks.Load(), events.Load())
	}
	if !db.WaitTimeout(0) {
		t.Fatal("WaitTimeout(0) missed coalesced wakeup")
	}
	if db.WaitTimeout(0) {
		t.Fatal("WaitTimeout(0) returned true with empty channel")
	}
	if db.WaitTimeout(2 * time.Millisecond) {
		t.Fatal("WaitTimeout(2ms) returned true without IRQ")
	}

	// Blocking Wait wakes on async IRQ.
	done := make(chan error, 1)
	go func() {
		done <- db.Wait(context.Background())
	}()
	time.Sleep(2 * time.Millisecond)
	db.TriggerPending()
	ctrl.Dispatch()
	if err := <-done; err != nil {
		t.Fatalf("Wait err=%v", err)
	}

	// Wait respects context cancellation (both pre-canceled and in-flight).
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := db.Wait(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-canceled Wait err=%v", err)
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 3*time.Millisecond)
	defer cancel2()
	if err := db.Wait(ctx2); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timed-out Wait err=%v", err)
	}

	// YieldFunc wakes immediately on IRQ and bounds wait on silence.
	yieldZero := db.YieldFunc(0)
	yieldZero() // empty non-blocking path
	db.TriggerPending()
	ctrl.Dispatch()
	yieldZero() // drains ready

	yield := db.YieldFunc(50 * time.Millisecond)
	start := time.Now()
	go func() {
		time.Sleep(3 * time.Millisecond)
		db.TriggerPending()
		ctrl.Dispatch()
	}()
	yield()
	if elapsed := time.Since(start); elapsed >= 40*time.Millisecond {
		t.Fatalf("YieldFunc did not wake early on IRQ: elapsed=%v", elapsed)
	}

	// YieldFunc bounds silence by maxWait.
	yieldShort := db.YieldFunc(2 * time.Millisecond)
	yieldShort()

	// Closing unblocks a parked Wait and prevents late handler/TriggerPending actions.
	waitClosed := make(chan error, 1)
	go func() {
		waitClosed <- db.Wait(context.Background())
	}()
	yieldClosed := make(chan struct{})
	go func() {
		db.YieldFunc(time.Second)()
		close(yieldClosed)
	}()
	wtClosed := make(chan bool, 1)
	go func() {
		wtClosed <- db.WaitTimeout(time.Second)
	}()
	time.Sleep(2 * time.Millisecond)
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := <-waitClosed; !errors.Is(err, ErrClosed) {
		t.Fatalf("unblocked Wait err=%v, want ErrClosed", err)
	}
	<-yieldClosed
	if <-wtClosed {
		t.Fatal("unblocked WaitTimeout returned true on Close")
	}

	before := db.Count()
	db.TriggerPending()
	db.handleIRQ()
	if db.Count() != before || acks.Load() != before {
		t.Fatal("closed doorbell executed handler or Ack")
	}
	if db.WaitTimeout(time.Millisecond) {
		t.Fatal("closed WaitTimeout returned true")
	}
	db.YieldFunc(time.Millisecond)()
}

func TestCloseDrainsInFlightHandlerAndCallback(t *testing.T) {
	ctrl, _ := newFakeGIC(t, 7)
	disp, err := New(ctrl)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer disp.Close()

	entered := make(chan struct{})
	release := make(chan struct{})
	var finished atomic.Bool

	db, err := disp.Open(Config{
		Line:    2,
		Trigger: Edge,
		OnEvent: func(uint64) {
			close(entered)
			<-release
			finished.Store(true)
		},
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	db.TriggerPending()
	go ctrl.Dispatch()

	<-entered
	closeDone := make(chan error, 1)
	go func() {
		closeDone <- db.Close()
	}()

	select {
	case err := <-closeDone:
		t.Fatalf("Close returned early while OnEvent was still running (err=%v)", err)
	case <-time.After(15 * time.Millisecond):
	}

	// While Close is draining, the line remains claimed and cannot be re-opened.
	if _, err := disp.Open(Config{Line: 2, Trigger: Edge}); !errors.Is(err, ErrLineInUse) {
		t.Fatalf("Open during Close drain err=%v, want ErrLineInUse", err)
	}

	close(release)
	if err := <-closeDone; err != nil {
		t.Fatalf("Close err=%v", err)
	}
	if !finished.Load() {
		t.Fatal("Close returned before OnEvent completed")
	}
}

func TestCrossDispatcherLineOwnership(t *testing.T) {
	ctrl, _ := newFakeGIC(t, 7)
	disp1, err := New(ctrl)
	if err != nil {
		t.Fatalf("New disp1: %v", err)
	}
	defer disp1.Close()

	disp2, err := New(ctrl)
	if err != nil {
		t.Fatalf("New disp2: %v", err)
	}
	defer disp2.Close()

	db1, err := disp1.Open(Config{Line: 5, Trigger: Edge})
	if err != nil {
		t.Fatalf("disp1.Open line 5: %v", err)
	}
	if _, err := disp2.Open(Config{Line: 5, Trigger: Edge}); !errors.Is(err, ErrLineInUse) {
		t.Fatalf("disp2.Open duplicate line 5 err=%v, want ErrLineInUse", err)
	}

	// Disjoint line on disp2 succeeds cleanly.
	db6, err := disp2.Open(Config{Line: 6, Trigger: Edge})
	if err != nil {
		t.Fatalf("disp2.Open line 6: %v", err)
	}
	defer db6.Close()

	// Closing db1 releases controller-wide ownership of line 5 for disp2.
	if err := db1.Close(); err != nil {
		t.Fatalf("db1.Close: %v", err)
	}
	db2, err := disp2.Open(Config{Line: 5, Trigger: Edge})
	if err != nil {
		t.Fatalf("disp2.Open line 5 after db1.Close: %v", err)
	}
	if err := db2.Close(); err != nil {
		t.Fatalf("db2.Close: %v", err)
	}
}

func TestYieldOrFallback(t *testing.T) {
	// Nil doorbell with custom fallback calls fallback.
	var fallbackCalls atomic.Int32
	YieldOrFallback(nil, time.Millisecond, func() { fallbackCalls.Add(1) })()
	if fallbackCalls.Load() != 1 {
		t.Fatalf("fallbackCalls=%d, want 1", fallbackCalls.Load())
	}

	// Nil doorbell with nil fallback defaults to runtime.Gosched cleanly.
	YieldOrFallback(nil, time.Millisecond, nil)()

	// Active doorbell wakes early on IRQ without calling fallback.
	ctrl, _ := newFakeGIC(t, 7)
	disp, err := New(ctrl)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer disp.Close()

	db, err := disp.Open(Config{Line: 0, Trigger: Edge})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	yield := YieldOrFallback(db, 50*time.Millisecond, func() { fallbackCalls.Add(1) })
	start := time.Now()
	go func() {
		time.Sleep(2 * time.Millisecond)
		db.TriggerPending()
		ctrl.Dispatch()
	}()
	yield()
	if time.Since(start) >= 40*time.Millisecond {
		t.Fatal("YieldOrFallback did not wake early on doorbell IRQ")
	}
	if fallbackCalls.Load() != 1 {
		t.Fatalf("fallback unexpectedly called with active doorbell: %d", fallbackCalls.Load())
	}

	// Non-positive maxWait invokes fallback even with an active doorbell.
	YieldOrFallback(db, 0, func() { fallbackCalls.Add(1) })()
	if fallbackCalls.Load() != 2 {
		t.Fatalf("maxWait<=0 fallbackCalls=%d, want 2", fallbackCalls.Load())
	}

	// Silence bounds wait to maxWait without invoking fallback.
	YieldOrFallback(db, 2*time.Millisecond, func() { fallbackCalls.Add(1) })()
	if fallbackCalls.Load() != 2 {
		t.Fatalf("timeout unexpectedly called fallback: %d", fallbackCalls.Load())
	}

	// Closing while parked in YieldOrFallback invokes fallback.
	parkedDone := make(chan struct{})
	go func() {
		YieldOrFallback(db, time.Second, func() { fallbackCalls.Add(1) })()
		close(parkedDone)
	}()
	time.Sleep(2 * time.Millisecond)
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case <-parkedDone:
	case <-time.After(time.Second):
		t.Fatal("parked YieldOrFallback did not unblock on Close")
	}
	if fallbackCalls.Load() != 3 {
		t.Fatalf("close-while-parked fallbackCalls=%d, want 3", fallbackCalls.Load())
	}

	// Already-closed doorbell invokes fallback on subsequent calls.
	yield()
	if fallbackCalls.Load() != 4 {
		t.Fatalf("already-closed fallbackCalls=%d, want 4", fallbackCalls.Load())
	}
}

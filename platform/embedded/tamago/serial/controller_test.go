// SPDX-License-Identifier: GPL-3.0-or-later

package serial

import (
	"sync"
	"sync/atomic"
	"testing"
)

type mockUARTRx struct {
	b    byte
	ok   bool
	errs LineError
}

type mockUART struct {
	mu        sync.Mutex
	rxQueue   []mockUARTRx
	txFIFO    []byte
	txLimit   int // 0 means unlimited
	rxIRQ     bool
	txIRQ     bool
	clears    int
	txIRQHook func(enable bool)
}

func (m *mockUART) ReadRX() (byte, bool, LineError) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.rxQueue) == 0 {
		return 0, false, 0
	}
	item := m.rxQueue[0]
	m.rxQueue = m.rxQueue[1:]
	return item.b, item.ok, item.errs
}

func (m *mockUART) WriteTX(b byte) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.txLimit > 0 && len(m.txFIFO) >= m.txLimit {
		return false
	}
	m.txFIFO = append(m.txFIFO, b)
	return true
}

func (m *mockUART) EnableRXIRQ(enable bool) {
	m.mu.Lock()
	m.rxIRQ = enable
	m.mu.Unlock()
}

func (m *mockUART) EnableTXIRQ(enable bool) {
	m.mu.Lock()
	m.txIRQ = enable
	hook := m.txIRQHook
	m.mu.Unlock()
	if hook != nil {
		hook(enable)
	}
}

func (m *mockUART) ClearIRQ() {
	m.mu.Lock()
	m.clears++
	m.mu.Unlock()
}

func TestLineErrorDecodersAndCounters(t *testing.T) {
	if got := DecodeDWAPBLSR(0x61); got != 0 {
		t.Fatalf("clean LSR decoded %#x", got)
	}
	if got := DecodeDWAPBLSR(0x1f); got != (ErrOverrun | ErrParity | ErrFraming | ErrBreak) {
		t.Fatalf("all LSR errors = %#x", got)
	}
	if got := DecodePL011DR(0x00ff); got != 0 {
		t.Fatalf("clean PL011 DR decoded %#x", got)
	}
	if got := DecodePL011DR(0x0f00); got != (ErrOverrun | ErrParity | ErrFraming | ErrBreak) {
		t.Fatalf("all PL011 errors = %#x", got)
	}

	var ec ErrorCounters
	ec.Record(ErrOverrun | ErrFraming)
	ec.Record(ErrParity)
	ec.Record(ErrBreak | ErrOverrun)
	ec.Record(0)
	oe, pe, fe, bi := ec.Snapshot()
	if oe != 2 || pe != 1 || fe != 1 || bi != 1 {
		t.Fatalf("ErrorCounters = (%d,%d,%d,%d), want (2,1,1,1)", oe, pe, fe, bi)
	}
	ec.Reset()
	oe, pe, fe, bi = ec.Snapshot()
	if oe != 0 || pe != 0 || fe != 0 || bi != 0 {
		t.Fatalf("Reset did not zero counters: (%d,%d,%d,%d)", oe, pe, fe, bi)
	}
}

func TestControllerErrorsPollingAndIRQ(t *testing.T) {
	if _, err := NewController(nil); err == nil {
		t.Fatal("NewController(nil) succeeded")
	}

	u := &mockUART{
		rxQueue: []mockUARTRx{
			{b: 'X', ok: true, errs: ErrFraming},
			{b: 0, ok: false, errs: ErrOverrun},
			{b: 'A', ok: true, errs: 0},
			{b: 'O', ok: true, errs: ErrOverrun}, // overrun-only warns while preserving the valid byte
			{b: 'Y', ok: true, errs: ErrParity | ErrBreak},
			{b: 'B', ok: true, errs: 0},
		},
	}
	c, err := NewController(u)
	if err != nil {
		t.Fatal(err)
	}

	// Polling mode skips corrupt bytes, preserves overrun-tagged valid bytes, and counts line errors.
	for _, want := range []byte{'A', 'O', 'B'} {
		b, ok := c.Read()
		if !ok || b != want {
			t.Fatalf("poll Read = (%q, %v), want (%q, true)", b, ok, want)
		}
	}
	if b, ok := c.Read(); ok || b != 0 {
		t.Fatalf("poll Read empty = (%d, %v)", b, ok)
	}
	st := c.Stats()
	if st.RXBytes != 3 || st.Overrun != 2 || st.Framing != 1 || st.Parity != 1 || st.Break != 1 {
		t.Fatalf("poll Stats = %+v", st)
	}

	// EnableIRQ resets counters, drains queued FIFO, and enables RX IRQ only.
	u.mu.Lock()
	u.rxQueue = []mockUARTRx{{b: 'C', ok: true}}
	u.mu.Unlock()
	c.EnableIRQ()
	if !u.rxIRQ || u.txIRQ {
		t.Fatalf("unexpected IRQ enable state: rx=%v tx=%v", u.rxIRQ, u.txIRQ)
	}
	select {
	case <-c.Ready():
	default:
		t.Fatal("Ready not signaled after EnableIRQ initial drain")
	}
	if b, ok := c.Read(); !ok || b != 'C' {
		t.Fatalf("IRQ initial Read = (%q, %v)", b, ok)
	}

	// Simulate overflow (> ringSize bytes queued before consumer pops) plus errors.
	u.mu.Lock()
	for i := 0; i < ringSize+10; i++ {
		u.rxQueue = append(u.rxQueue, mockUARTRx{b: byte(i), ok: true})
	}
	u.rxQueue = append(u.rxQueue, mockUARTRx{b: 'Z', ok: true, errs: ErrOverrun})
	u.mu.Unlock()

	c.HandleIRQ()
	c.HandleIRQ() // drains remaining 11 bytes (including overrun-tagged 'Z') after first 256-byte cap
	st = c.Stats()
	if st.IRQCount != 2 || st.RXBytes != 1+ringSize+10+1 || st.RXDropped != 11 || st.Overrun != 1 || u.clears < 3 {
		t.Fatalf("IRQ overflow Stats = %+v clears=%d", st, u.clears)
	}

	c.DisableIRQ()
	if u.rxIRQ || u.txIRQ {
		t.Fatalf("DisableIRQ left IRQs enabled: rx=%v tx=%v", u.rxIRQ, u.txIRQ)
	}
}

func TestControllerIRQModeTransitionsSerializeWithTXWriters(t *testing.T) {
	u := &mockUART{}
	c, err := NewController(u)
	if err != nil {
		t.Fatal(err)
	}

	type txIRQObservation struct {
		enabled  bool
		active   bool
		lockHeld bool
	}
	var observationsMu sync.Mutex
	var observations []txIRQObservation
	u.txIRQHook = func(enabled bool) {
		lockHeld := !c.txMu.TryLock()
		if !lockHeld {
			c.txMu.Unlock()
		}
		observationsMu.Lock()
		observations = append(observations, txIRQObservation{
			enabled:  enabled,
			active:   c.irqActive.Load(),
			lockHeld: lockHeld,
		})
		observationsMu.Unlock()
	}

	c.EnableIRQ()
	observationsMu.Lock()
	observations = nil
	observationsMu.Unlock()

	// Disable must clear the mode before masking TX, while still holding the
	// writer lock, so a waiting writer cannot turn TX IRQ back on afterward.
	c.DisableIRQ()
	observationsMu.Lock()
	if len(observations) != 1 {
		t.Fatalf("DisableIRQ TX mask observations = %+v, want one mask", observations)
	}
	disabled := observations[0]
	observations = nil
	observationsMu.Unlock()
	if disabled.enabled || disabled.active || !disabled.lockHeld {
		t.Fatalf("DisableIRQ transition at TX mask = %+v, want disabled, inactive, lock held", disabled)
	}

	// Model a writer that queued while IRQ mode was inactive during EnableIRQ.
	// The activating transition must notice the queued byte and enable TX IRQ
	// under the same lock before later writers can proceed.
	c.txMu.Lock()
	if !c.tx.Push('q') {
		c.txMu.Unlock()
		t.Fatal("failed to seed queued TX byte")
	}
	c.setIRQActiveLocked(true)
	c.txMu.Unlock()

	observationsMu.Lock()
	defer observationsMu.Unlock()
	if len(observations) != 1 {
		t.Fatalf("EnableIRQ TX mask observations = %+v, want one enable", observations)
	}
	enabled := observations[0]
	if !enabled.enabled || !enabled.active || !enabled.lockHeld {
		t.Fatalf("EnableIRQ transition at TX mask = %+v, want enabled, active, lock held", enabled)
	}
}

func TestControllerTXRingAndConcurrency(t *testing.T) {
	u := &mockUART{txLimit: 4}
	c, err := NewController(u)
	if err != nil {
		t.Fatal(err)
	}
	c.EnableIRQ()

	// Write 8 bytes when hardware FIFO accepts 4: first 4 go directly, next 4 buffer + enable TX IRQ.
	n, dropped := c.Write([]byte("12345678"))
	if n != 8 || dropped != 0 {
		t.Fatalf("Write = (%d, %d), want (8, 0)", n, dropped)
	}
	u.mu.Lock()
	gotFIFO := string(u.txFIFO)
	txOn := u.txIRQ
	u.txFIFO = nil
	u.mu.Unlock()
	if gotFIFO != "1234" || !txOn {
		t.Fatalf("FIFO=%q txIRQ=%v, want \"1234\" true", gotFIFO, txOn)
	}

	// Overflow TX ring beyond 256 bytes cleanly.
	extra := make([]byte, ringSize)
	for i := range extra {
		extra[i] = byte('a' + i%26)
	}
	n2, drop2 := c.Write(extra)
	if n2 != ringSize-4 || drop2 != 4 {
		t.Fatalf("TX overflow Write = (%d, %d), want (%d, 4)", n2, drop2, ringSize-4)
	}
	if c.SendByte('!') {
		t.Fatal("SendByte succeeded on full TX ring")
	}
	if st := c.Stats(); st.TXDropped != 5 || st.TXBytes != 4 {
		t.Fatalf("TX Stats before drain = %+v", st)
	}

	// Drain TX while FIFO accepts 4 bytes per DrainTX call until ring empty and TX IRQ masks.
	for i := 0; i < 64; i++ {
		c.DrainTX()
		u.mu.Lock()
		u.txFIFO = nil
		u.mu.Unlock()
	}
	c.DrainTX()
	if u.txIRQ {
		t.Fatal("DrainTX did not disable TX IRQ when ring emptied")
	}
	if st := c.Stats(); st.TXBytes != 4+uint64(ringSize) {
		t.Fatalf("TXBytes = %d, want %d", st.TXBytes, 4+ringSize)
	}

	// Concurrent producer/consumer/IRQ stress test under -race.
	u2 := &mockUART{txLimit: 8}
	c2, err := NewController(u2)
	if err != nil {
		t.Fatal(err)
	}
	c2.EnableIRQ()

	var stop atomic.Bool
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			c2.SendByte(byte(i))
		}
		stop.Store(true)
	}()
	go func() {
		defer wg.Done()
		for !stop.Load() {
			u2.mu.Lock()
			if len(u2.rxQueue) < 32 {
				u2.rxQueue = append(u2.rxQueue, mockUARTRx{b: 'k', ok: true})
			}
			u2.txFIFO = u2.txFIFO[:0]
			u2.mu.Unlock()
			c2.HandleIRQ()
		}
		c2.HandleIRQ()
	}()
	go func() {
		defer wg.Done()
		for !stop.Load() {
			c2.Read()
		}
		for {
			if _, ok := c2.Read(); !ok {
				break
			}
		}
	}()
	wg.Wait()
	_ = c2.Stats()
}

func TestControllerReadIntoAndPendingCounts(t *testing.T) {
	var nilCtl *Controller
	if nilCtl.PendingRX() != 0 || nilCtl.PendingTX() != 0 || nilCtl.ReadInto(make([]byte, 4)) != 0 {
		t.Fatal("nil Controller reported non-zero pending/read")
	}

	// Polling ReadInto filters corrupt bytes and stops at empty FIFO without allocating.
	uPoll := &mockUART{
		rxQueue: []mockUARTRx{
			{b: 'A', ok: true},
			{b: 0xff, ok: true, errs: ErrFraming},
			{b: 'B', ok: true, errs: ErrOverrun},
			{b: 'C', ok: true},
		},
	}
	cPoll, err := NewController(uPoll)
	if err != nil {
		t.Fatal(err)
	}
	pollDst := make([]byte, 8)
	n := cPoll.ReadInto(pollDst)
	if n != 3 || string(pollDst[:n]) != "ABC" {
		t.Fatalf("polling ReadInto = (%d, %q), want (3, \"ABC\")", n, pollDst[:n])
	}

	// IRQ ReadInto drains RX ring and PendingRX/PendingTX track buffered counts.
	uIRQ := &mockUART{
		txLimit: 2,
		rxQueue: []mockUARTRx{
			{b: 'x', ok: true},
			{b: 'y', ok: true},
			{b: 'z', ok: true},
		},
	}
	cIRQ, err := NewController(uIRQ)
	if err != nil {
		t.Fatal(err)
	}
	cIRQ.EnableIRQ()
	if got := cIRQ.PendingRX(); got != 3 {
		t.Fatalf("PendingRX = %d, want 3", got)
	}
	irqDst := make([]byte, 2)
	if got := cIRQ.ReadInto(irqDst); got != 2 || string(irqDst) != "xy" || cIRQ.PendingRX() != 1 {
		t.Fatalf("first IRQ ReadInto = %q pending=%d", irqDst, cIRQ.PendingRX())
	}
	if got := cIRQ.ReadInto(irqDst); got != 1 || irqDst[0] != 'z' || cIRQ.PendingRX() != 0 {
		t.Fatalf("second IRQ ReadInto = (%d, %q) pending=%d", got, irqDst[:got], cIRQ.PendingRX())
	}

	w, dropped := cIRQ.Write([]byte("hello!"))
	if w != 6 || dropped != 0 || cIRQ.PendingTX() != 4 {
		t.Fatalf("PendingTX after Write = %d (written=%d dropped=%d), want 4", cIRQ.PendingTX(), w, dropped)
	}
	for i := 0; i < 2; i++ {
		uIRQ.mu.Lock()
		uIRQ.txFIFO = nil
		uIRQ.mu.Unlock()
		cIRQ.DrainTX()
	}
	if cIRQ.PendingTX() != 0 {
		t.Fatalf("PendingTX after DrainTX = %d, want 0", cIRQ.PendingTX())
	}
}

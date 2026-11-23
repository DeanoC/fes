//go:build tamago

// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"runtime"
	"time"
	"unsafe"

	"github.com/DeanoC/fes/platform/embedded/tamago/board"
	"github.com/DeanoC/fes/platform/embedded/tamago/dmaops"
	"github.com/DeanoC/fes/platform/embedded/tamago/fpgairq"
	"github.com/DeanoC/fes/platform/embedded/tamago/gic"
	"github.com/DeanoC/fes/platform/embedded/tamago/psci"
	"github.com/DeanoC/fes/platform/embedded/tamago/rtc"
)

func main() {
	fmt.Printf("FES native Go probe os=%s arch=%s EL=%d counter=%dHz\n",
		runtime.GOOS, runtime.GOARCH, board.ExceptionLevel(), board.CounterFrequency())
	if runtime.GOOS != "tamago" || runtime.GOARCH != "arm64" ||
		board.ExceptionLevel() != 1 || board.CounterFrequency() == 0 {
		panic("unexpected boot contract")
	}
	before := time.Now()
	work := make(chan []byte)
	go func() {
		buffer := make([]byte, 1<<20)
		for i := range buffer {
			buffer[i] = byte(i * 31)
		}
		time.Sleep(2 * time.Millisecond)
		work <- buffer
	}()
	buffer := <-work
	if time.Since(before) < 2*time.Millisecond || buffer[12345] != byte((12345*31)%256) {
		panic("timer/goroutine/allocation probe failed")
	}
	runtime.GC()
	fmt.Println("PASS timer goroutine allocation GC")
	// This is TamaGo's volatile memory filesystem, not FAT32 or an SD card.
	payload := []byte("FES RAM filesystem probe\n")
	if err := os.WriteFile("/tmp/fes-probe.txt", payload, 0600); err != nil {
		panic(err)
	}
	got, err := os.ReadFile("/tmp/fes-probe.txt")
	if err != nil || !bytes.Equal(got, payload) {
		panic("volatile filesystem probe failed")
	}
	fmt.Println("PASS volatile memory filesystem (not SD/FAT32)")
	probeGIC()
	probeDMA()
	probePSCI()
	probeRTC()
	fmt.Printf("FES_NATIVE_GO_PROBE PASS os=tamago board=%s\n", board.Name)
}

func probeRTC() {
	dev, err := board.RTC()
	if err != nil {
		// QEMU virt always maps a PL031, so a missing or misidentified RTC
		// there is a failure; only boards without an RTC may skip.
		if errors.Is(err, rtc.ErrNotFound) && board.Name != "qemu" {
			fmt.Println("SKIP rtc (no hardware RTC on board)")
			return
		}
		panic(err)
	}
	if dev.PartNumber() != rtc.PL031PartNumber || dev.Designer() != rtc.ARMDesignerID {
		panic(fmt.Sprintf("rtc: unexpected IDs part=%#x designer=%#x", dev.PartNumber(), dev.Designer()))
	}
	first, ok := board.WallTime()
	if !ok || first.Year() < 2020 {
		panic(fmt.Sprintf("rtc: invalid wall time ok=%t utc=%s", ok, first.Format(time.RFC3339)))
	}
	time.Sleep(2 * time.Millisecond)
	second, ok := board.WallTime()
	if !ok || second.Sub(first) < 2*time.Millisecond {
		panic(fmt.Sprintf("rtc: wall clock failed to advance (%s -> %s)", first.Format(time.RFC3339Nano), second.Format(time.RFC3339Nano)))
	}
	fmt.Printf("PASS rtc pl031 rev=%d utc=%s\n", dev.Revision(), second.Format(time.RFC3339))
}

func probePSCI() {
	var a, b [32]byte
	if _, err := rand.Read(a[:]); err != nil {
		panic(err)
	}
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	var zero [32]byte
	if bytes.Equal(a[:], zero[:]) || bytes.Equal(b[:], zero[:]) || bytes.Equal(a[:], b[:]) {
		panic("psci: crypto/rand produced degenerate output")
	}
	src := board.EntropySource()
	client := board.PSCI()
	major, minor, err := client.Version()
	if err != nil {
		if errors.Is(err, psci.ErrUnsupported) {
			fmt.Printf("SKIP psci (conduit disabled) entropy=%s hw_rng=%t\n", src, src.IsHardware())
			return
		}
		panic(err)
	}
	if major == 0 && minor < 2 {
		panic(fmt.Sprintf("psci: unsupported version %d.%d", major, minor))
	}
	canOff := client.Supports(psci.FuncSystemOff)
	canReset := client.Supports(psci.FuncSystemReset)
	trng := "none"
	if tmaj, tmin, terr := client.TRNGVersion(); terr == nil {
		trng = fmt.Sprintf("%d.%d", tmaj, tmin)
	}
	fmt.Printf("PASS psci version=%d.%d reset=%t off=%t trng=%s entropy=%s hw_rng=%t\n",
		major, minor, canReset, canOff, trng, src, src.IsHardware())
}

func probeDMA() {
	hw := dmaops.Native()
	line := hw.CacheLineSize()
	if line < 16 || line > 4096 || line&(line-1) != 0 {
		panic("dma: invalid CTR_EL0 cache line size")
	}

	// CPU-only EL1 cache-maintenance and pool/ring/poison self-test; no
	// hardware DMA master accesses Go RAM.
	const arenaBytes = 4096
	raw := make([]byte, arenaBytes+line)
	rawAddr := uint64(uintptr(unsafe.Pointer(&raw[0])))
	alignedAddr := (rawAddr + line - 1) &^ (line - 1)
	shift := alignedAddr - rawAddr
	arena := raw[shift : shift+arenaBytes]

	pool, err := dmaops.NewPool(alignedAddr, arena, dmaops.Cached, hw)
	if err != nil {
		panic(err)
	}

	buf, err := pool.Alloc(256, line)
	if err != nil {
		panic(err)
	}
	data := buf.Bytes()
	for i := range data {
		data[i] = byte(i*29 + 7)
	}
	if err := buf.SyncForDevice(dmaops.ToDevice); err != nil {
		panic(err)
	}
	if err := buf.SyncForCPU(dmaops.FromDevice); err != nil {
		panic(err)
	}
	for i, got := range data {
		if got != byte(i*29+7) {
			panic("dma: DC CVAC -> DC IVAC round-trip mismatch")
		}
	}
	if err := buf.SyncForDevice(dmaops.Bidirectional); err != nil {
		panic(err)
	}
	if !errors.Is(buf.SyncRangeForCPU(3, 19, dmaops.FromDevice), dmaops.ErrInvalidRange) {
		panic("dma: unaligned cached receive range must reject")
	}
	if err := buf.SyncRangeForDevice(3, 19, dmaops.ToDevice); err != nil {
		panic(err)
	}
	if err := buf.SyncRangeForCPU(line, line, dmaops.FromDevice); err != nil {
		panic(err)
	}
	for i, got := range data {
		if got != byte(i*29+7) {
			panic("dma: aligned cached sub-range invalidate corrupted bytes")
		}
	}
	if err := buf.Free(); err != nil {
		panic(err)
	}

	ring, err := dmaops.NewRing(pool, dmaops.RingConfig{
		Count:     4,
		DescSize:  line,
		DescAlign: line,
		BufSize:   128,
		BufAlign:  line,
	})
	if err != nil {
		panic(err)
	}
	for i := 0; i < 6; i++ {
		slot, err := ring.Acquire()
		if err != nil {
			panic(err)
		}
		slot.Buf.Bytes()[0] = byte(i + 1)
		if err := ring.Publish(slot, dmaops.ToDevice); err != nil {
			panic(err)
		}
		peek, err := ring.PeekComplete()
		if err != nil || peek.Index != slot.Index {
			panic("dma: ring peek mismatch")
		}
		if err := ring.SyncDescriptorForCPU(peek); err != nil {
			panic(err)
		}
		if err := ring.Retire(peek, dmaops.ToDevice); err != nil {
			panic(err)
		}
	}
	if err := ring.Close(); err != nil {
		panic(err)
	}
	if st := pool.Stats(); st.FreeBytes != arenaBytes || st.FreeSpans != 1 || st.LiveBuffers != 0 {
		panic("dma: pool failed to coalesce after ring close")
	}

	pbuf, err := pool.Alloc(128, line)
	if err != nil {
		panic(err)
	}
	pbuf.Poison(errors.New("probe-timeout"))
	if !errors.Is(pbuf.Free(), dmaops.ErrPoisoned) {
		panic("dma: poisoned buffer allowed Free")
	}
	if pool.Stats().PinnedBytes != pbuf.Cap() {
		panic("dma: poisoned buffer not tracked as pinned")
	}
	runtime.KeepAlive(raw)
	fmt.Printf("PASS dma cacheline=%d cached-sync ring poison\n", line)
}

func probeGIC() {
	g := board.GIC()
	if err := g.Init(); err != nil {
		if errors.Is(err, gic.ErrUnsupported) {
			fmt.Println("SKIP gic (no GICv3 system registers)")
			return
		}
		panic(err)
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		g.Serve(stop)
	}()
	defer func() {
		close(stop)
		<-done
	}()

	sgiHit := make(chan struct{}, 1)
	g.Register(0, func() {
		select {
		case sgiHit <- struct{}{}:
		default:
		}
	})
	g.Enable(0)
	g.SendSGI(0)
	select {
	case <-sgiHit:
	case <-time.After(time.Second):
		panic("gic: SGI 0 did not fire")
	}
	g.Disable(0)
	g.Register(0, nil)

	// Exercise FPGA-to-HPS doorbell dispatcher over SPI 48 (Agilex 5
	// IRQFPGA2HPS0; unused VirtIO slot 0 during QEMU probe), including
	// Doorbell.Wait and Doorbell.YieldFunc wakeups.
	disp, err := fpgairq.New(g)
	if err != nil {
		panic(err)
	}
	db, err := disp.Open(fpgairq.Config{Line: 0, Trigger: fpgairq.Edge})
	if err != nil {
		panic(err)
	}
	db.TriggerPending()
	waitCtx, waitCancel := context.WithTimeout(context.Background(), time.Second)
	if err := db.Wait(waitCtx); err != nil || db.Count() != 1 {
		waitCancel()
		panic(fmt.Sprintf("fpgairq: SPI 48 Wait failed err=%v count=%d", err, db.Count()))
	}
	waitCancel()
	db.TriggerPending()
	db.YieldFunc(time.Second)()
	if db.Count() != 2 {
		panic(fmt.Sprintf("fpgairq: SPI 48 YieldFunc failed count=%d", db.Count()))
	}
	if err := db.Close(); err != nil {
		panic(err)
	}
	if err := disp.Close(); err != nil {
		panic(err)
	}
	g.SetEdge(fpgairq.BaseSPI, false)

	timerHit := make(chan struct{}, 1)
	g.Register(board.IRQPhysTimer, func() {
		gic.DisarmPhysicalTimer()
		select {
		case timerHit <- struct{}{}:
		default:
		}
	})
	g.Enable(board.IRQPhysTimer)
	ticks := uint32(board.CounterFrequency() / 1000)
	if ticks == 0 {
		ticks = 1
	}
	gic.ArmPhysicalTimer(ticks)
	select {
	case <-timerHit:
	case <-time.After(time.Second):
		gic.DisarmPhysicalTimer()
		panic("gic: EL1 physical timer PPI 30 did not fire")
	}
	g.Disable(board.IRQPhysTimer)
	g.Register(board.IRQPhysTimer, nil)

	if err := g.EnableIdle(); err != nil {
		panic(err)
	}
	defer g.DisableIdle()

	idleBefore := g.IdleWakeups()
	sleepStart := time.Now()
	time.Sleep(10 * time.Millisecond)
	elapsed := time.Since(sleepStart)
	wakeups := g.IdleWakeups() - idleBefore
	if elapsed < 10*time.Millisecond || wakeups == 0 {
		panic("gic: WFI idle timer sleep failed")
	}

	g.Register(0, func() {
		select {
		case sgiHit <- struct{}{}:
		default:
		}
	})
	g.Enable(0)
	g.SendSGI(0)
	select {
	case <-sgiHit:
	case <-time.After(time.Second):
		panic("gic: SGI 0 did not fire while WFI idle active")
	}
	g.Disable(0)
	g.Register(0, nil)

	fmt.Printf("PASS gic sgi spi timer wfi-idle redistributor=%#x lines=%d wakeups=%d\n", g.Redistributor(), g.Lines(), wakeups)
}

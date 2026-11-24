// SPDX-License-Identifier: GPL-3.0-or-later
package dwc2

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/gic"
	"github.com/DeanoC/fes/platform/embedded/tamago/usb"
)

const (
	haint          = 0x414
	gintPRTINT     = uint32(1 << 24)
	multiIRQMask   = gintRXFLVL | gintHCH | gintPRTINT | gintDisconn
	multiIRQMaskSF = multiIRQMask | gintSOF

	maxMultiChannels = 16
	maxMultiMPS      = 512
	maxMultiDevices  = 31
	maxEventQueue    = 64
)

func chReg(ch int, base uint32) uint32 { return base + uint32(ch)*0x20 }
func chFIFO(ch int) uint32             { return fifo + uint32(ch)*0x1000 }

// frameBefore reports whether 14-bit USB microframe counter a precedes b
// modulo 0x4000.
func frameBefore(a, b uint16) bool {
	return ((a - b) & 0x3fff) >= 0x2000
}

// partitionFIFOs scales DWC2 dynamic packet RAM from HWCFG3[31:16] so Agilex 5
// (>= 1024 words) accommodates 512-byte HS Bulk ACL packets alongside concurrent
// periodic HID channels while preserving the 256-word minimum profile.
func partitionFIFOs(totalWords uint32) (rxWords, nptxWords, ptxWords uint32) {
	switch {
	case totalWords >= 1024:
		return 512, 256, 256
	case totalWords >= 512:
		return 256, 128, 128
	default:
		return 128, 64, 64
	}
}

type multiChannel struct {
	allocated        bool
	ownerAddr        uint8
	gen              uint64
	speed            usb.Speed
	route            splitRoute
	ep               usb.EndpointDescriptor
	toggle           uint32 // 0 = DATA0, 2 = DATA1
	interval         time.Duration
	overrideInterval time.Duration

	armed         bool
	halting       bool
	waitingSplit  bool
	completeSplit bool
	startFrame    uint16
	nextFrame     uint16

	received bool
	done     bool
	nak      bool
	ack      bool
	nyet     bool

	inBuf      [maxMultiMPS]byte
	inLen      int
	inReady    bool
	inReadyLen int

	outBuf     [maxMultiMPS]byte
	outLen     int
	outPending bool
	outDone    bool
	outErr     error

	deadline time.Time
	nextDue  time.Time
	fault    error
}

func (c *multiChannel) effectiveInterval() time.Duration {
	if c.overrideInterval > 0 {
		return c.overrideInterval
	}
	return c.interval
}

func endpointInterval(speed usb.Speed, ep usb.EndpointDescriptor) time.Duration {
	if ep.TransferType == usb.TransferBulk {
		return 0
	}
	iv := ep.Interval
	if iv == 0 {
		iv = 1
	}
	if speed == usb.SpeedHigh {
		if iv > 16 {
			iv = 16
		}
		return (125 * time.Microsecond) << uint(iv-1)
	}
	return time.Duration(iv) * time.Millisecond
}

type multiDevice struct {
	info  usb.BusDevice
	route splitRoute
	isHub bool
}

const (
	maxHubEnumRetries = 3
	hubEnumRetryDelay = 100 * time.Millisecond
)

type multiHub struct {
	addr             uint8
	speed            usb.Speed
	route            splitRoute
	depth            int
	numPorts         uint8
	singleTT         bool
	powerSwitched    bool
	pwrGoodDelay     time.Duration
	statusEP         usb.EndpointDescriptor
	statusNeedsReset bool
	statusPipe       *multiPipe
	statusRetryAt    time.Time
	repowerRetries   uint8
	repowerRetryAt   time.Time
	retryCount       [maxHubPorts + 1]uint8
	retryAt          [maxHubPorts + 1]time.Time
}

// MultiHost is a 16-channel PIO-only DWC2 USB 2.0 host supporting concurrent
// HID/XInput/Bluetooth HCI pipes, Multi-TT hubs, and dynamic hotplug without
// DMA buffers or global retirement on single-device errors.
type MultiHost struct {
	io        Registers
	owned     bool
	channels  int
	fifoWords uint32
	rxWords   uint32
	npTxWords uint32
	pTxWords  uint32
	rootSpeed      usb.Speed
	rootConn       bool
	rootRetryCount uint8
	rootRetryAt    time.Time
	rootExhausted  bool
	paused         bool
	draining  bool
	fault     error // controller-level fatal fault only (e.g. foreign DMA)

	slots       [maxMultiChannels]multiChannel
	devices     [maxMultiDevices + 1]*multiDevice // indexed by USB address 1..31
	hubs        []*multiHub
	nextID      uint8
	nextPipeGen uint64

	events    [maxEventQueue]usb.HostEvent
	evHead    int
	evCount   int

	irqMu          sync.Mutex
	irqGIC         *gic.Controller
	irqID          uint32
	irqMode        atomic.Bool
	irqActive      atomic.Bool
	irqSuspended   bool
	irqGuard       func() bool
	irqReady       chan struct{}
	irqCount       atomic.Uint64
	irqTimestampNS atomic.Int64
	latency        latencyTracker
}

var _ usb.BusHost = (*MultiHost)(nil)

// OpenMulti initializes the DWC2 host controller in multi-channel PIO mode,
// dynamically partitions packet RAM, enables root VBUS, and enumerates any
// initially attached devices. Booting with zero USB devices attached succeeds
// and hotplug attach events are emitted when devices are plugged in later.
func OpenMulti(io Registers, config Config) (*MultiHost, error) {
	if io == nil || config.PHYClockHz != 60000000 {
		return nil, fmt.Errorf("dwc2: requires firmware-configured 60 MHz ULPI PHY")
	}
	h := &MultiHost{io: io, nextID: 1}
	if err := h.initController(); err != nil {
		return h, h.failController(err)
	}
	if h.io.Read32(hprt)&1 != 0 {
		h.enumerateRoot()
	}
	if h.fault != nil {
		return h, h.fault
	}
	return h, nil
}

func (h *MultiHost) waitReg(reg, mask, want uint32, budget time.Duration) error {
	start := h.io.Now()
	end := start.Add(budget)
	var last uint32
	for n := 0; n < 1000000; n++ {
		last = h.io.Read32(reg)
		if last&mask == want {
			return nil
		}
		if !h.io.Now().Before(end) {
			break
		}
		if n&255 == 255 {
			runtime.Gosched()
		}
	}
	return fmt.Errorf("dwc2: register wait exhausted address=%#x value=%#x mask=%#x want=%#x", reg, last, mask, want)
}

func (h *MultiHost) delay(d time.Duration) error {
	end := h.io.Now().Add(d)
	for n := 0; n < 1000000; n++ {
		if !h.io.Now().Before(end) {
			return nil
		}
		if n&255 == 255 {
			runtime.Gosched()
		}
	}
	return fmt.Errorf("dwc2: delay poll budget exhausted duration=%s", d)
}

func (h *MultiHost) portWrite(set, clear uint32) {
	v := h.io.Read32(hprt) &^ (uint32(1<<1|1<<2|1<<3|1<<5) | clear)
	h.io.Write32(hprt, v|set)
}

func (h *MultiHost) initController() error {
	id := h.io.Read32(snpsid)
	if id < 0x4f542000 || id >= 0x4f544000 {
		return fmt.Errorf("dwc2: unsupported core ID %#x", id)
	}
	cfg := h.io.Read32(hwcfg2)
	mode := cfg & 7
	totalWords := h.io.Read32(hwcfg3) >> 16
	if mode == 3 || mode == 4 || mode == 7 || (cfg>>3)&3 == 1 || (cfg>>3)&3 == 3 || cfg&(1<<19) == 0 || totalWords < 256 {
		return fmt.Errorf("dwc2: unsupported host/PIO/FIFO capabilities")
	}
	if h.io.Read32(gusb)&(1<<4|1<<6|1<<3) != 1<<4 {
		return fmt.Errorf("dwc2: firmware ULPI configuration missing")
	}
	if h.io.Read32(gahb)&(1<<5) != 0 {
		return fmt.Errorf("dwc2: firmware DMA still enabled")
	}
	chCount := int(((cfg >> 14) & 15) + 1)
	if chCount > maxMultiChannels {
		chCount = maxMultiChannels
	}
	if chCount < 2 {
		return fmt.Errorf("dwc2: multi-channel host requires >= 2 channels, got %d", chCount)
	}
	for n := 0; n < chCount; n++ {
		if h.io.Read32(chReg(n, hcchar))&channelEnable != 0 {
			return fmt.Errorf("dwc2: firmware channel %d still active", n)
		}
	}
	h.channels = chCount
	h.fifoWords = totalWords
	h.owned = true

	h.io.Write32(gahb, 0)
	h.io.Write32(gintmask, 0)
	h.io.Write32(haintmsk, 0)
	h.io.Write32(pcgc, 0)
	if err := h.waitReg(reset, 1<<31, 1<<31, 500*time.Millisecond); err != nil {
		return err
	}
	h.io.Write32(reset, 1)
	if err := h.waitReg(reset, 1, 0, 500*time.Millisecond); err != nil {
		return err
	}
	if err := h.waitReg(reset, 1<<31, 1<<31, 500*time.Millisecond); err != nil {
		return err
	}
	if err := h.delay(time.Millisecond); err != nil {
		return err
	}
	h.io.Write32(gusb, (h.io.Read32(gusb)&^uint32(1<<30|1<<15|1<<9|1<<8))|1<<29)
	if err := h.delay(50 * time.Millisecond); err != nil {
		return err
	}
	if err := h.waitReg(gint, 1, 1, 500*time.Millisecond); err != nil {
		return err
	}
	h.io.Write32(hcfg, 0) // HS/FS/LS host, descriptor DMA disabled

	rxW, npW, pW := partitionFIFOs(totalWords)
	h.rxWords = rxW
	h.npTxWords = npW
	h.pTxWords = pW
	h.io.Write32(rxsize, rxW)
	h.io.Write32(nptxsize, npW<<16|rxW)
	h.io.Write32(ptxsize, pW<<16|(rxW+npW))
	h.io.Write32(reset, 0x10<<6|1<<5|1<<4)
	if err := h.waitReg(reset, 1<<5|1<<4, 0, 500*time.Millisecond); err != nil {
		return err
	}
	if h.io.Read32(hprt)&(1<<4) != 0 {
		return fmt.Errorf("dwc2: port overcurrent")
	}
	h.portWrite(1<<12, 0) // Power root port VBUS
	if err := h.delay(100 * time.Millisecond); err != nil {
		return err
	}
	h.io.Write32(gint, 0xffffffff)
	return nil
}

func (h *MultiHost) failController(err error) error {
	if h.fault != nil {
		return h.fault
	}
	h.fault = err
	if !h.owned {
		h.retireIRQ(false)
		return err
	}
	if h.io.Read32(gahb)&(1<<5) != 0 || h.io.Read32(hcfg)&(1<<23) != 0 {
		h.retireIRQ(false)
		return h.fault
	}
	h.retireIRQ(true)
	h.io.Write32(gintmask, 0)
	h.io.Write32(haintmsk, 0)
	for ch := 0; ch < h.channels; ch++ {
		c := h.io.Read32(chReg(ch, hcchar))
		if c&channelEnable != 0 {
			h.io.Write32(chReg(ch, hcchar), c|channelEnable|channelDisable)
			_ = h.waitReg(chReg(ch, hcchar), channelEnable, 0, 50*time.Millisecond)
		}
	}
	return h.fault
}

func (h *MultiHost) pioCheck() error {
	if h.io.Read32(gahb)&(1<<5) != 0 || h.io.Read32(hcfg)&(1<<23) != 0 {
		return fmt.Errorf("dwc2: foreign DMA enabled")
	}
	if !h.irqMode.Load() {
		if h.io.Read32(gahb)&1 != 0 || h.io.Read32(gintmask) != 0 {
			return fmt.Errorf("dwc2: PIO/interrupt ownership changed")
		}
		return nil
	}
	h.irqMu.Lock()
	defer h.irqMu.Unlock()
	ahb := h.io.Read32(gahb)
	mask := h.io.Read32(gintmask)
	if h.irqActive.Load() {
		if ahb&gahbGlobalIRQ == 0 || (mask != 0 && mask != multiIRQMask && mask != multiIRQMaskSF) {
			return fmt.Errorf("dwc2: PIO/interrupt ownership changed")
		}
		return nil
	}
	if ahb&gahbGlobalIRQ != 0 || mask != 0 || h.io.Read32(haintmsk) != 0 {
		return fmt.Errorf("dwc2: PIO/interrupt ownership changed")
	}
	return nil
}

func (h *MultiHost) pushEvent(ev usb.HostEvent) {
	if h.evCount == maxEventQueue {
		// Drop oldest unread event to keep queue bounded without allocating.
		h.evHead = (h.evHead + 1) % maxEventQueue
		h.evCount--
	}
	idx := (h.evHead + h.evCount) % maxEventQueue
	h.events[idx] = ev
	h.evCount++
}

// NextEvent pops the next queued dynamic attach/detach event.
func (h *MultiHost) NextEvent() (usb.HostEvent, bool) {
	if h.evCount == 0 {
		return usb.HostEvent{}, false
	}
	ev := h.events[h.evHead]
	h.evHead = (h.evHead + 1) % maxEventQueue
	h.evCount--
	return ev, true
}

// Devices returns all currently attached enumerated non-hub devices.
func (h *MultiHost) Devices() []usb.BusDevice {
	var out []usb.BusDevice
	for _, dev := range h.devices {
		if dev != nil && !dev.isHub {
			out = append(out, dev.info)
		}
	}
	return out
}

// Channels returns the hardware host channel count discovered from HWCFG2.
func (h *MultiHost) Channels() int { return h.channels }

// FIFOWords returns the dynamic packet RAM depth discovered from HWCFG3.
func (h *MultiHost) FIFOWords() uint32 { return h.fifoWords }

func (h *MultiHost) activeChannelMask() uint32 {
	var mask uint32
	for ch := 1; ch < h.channels; ch++ {
		if h.slots[ch].allocated {
			mask |= 1 << uint(ch)
		}
	}
	return mask
}

// haltChannel cleanly disables channel ch and clears its pending interrupts.
// Returns an error if hardware leaves CHENA set past the 50 ms budget so callers
// never clear, reuse, or hand a live channel to firmware Stop.
func (h *MultiHost) haltChannel(ch int) error {
	_, err := h.haltChannelCapture(ch)
	return err
}

// haltChannelCapture halts channel ch and returns its HCINT bits observed once
// CHENA has cleared (before clearing HCINT), so PausePolling can account for
// transfers that completed concurrently with the halt.
func (h *MultiHost) haltChannelCapture(ch int) (uint32, error) {
	c := h.io.Read32(chReg(ch, hcchar))
	if c&channelEnable != 0 {
		h.io.Write32(chReg(ch, hcchar), c|channelEnable|channelDisable)
		if err := h.waitReg(chReg(ch, hcchar), channelEnable, 0, 50*time.Millisecond); err != nil {
			return 0, fmt.Errorf("dwc2: ch %d failed to halt: %w", ch, err)
		}
	}
	ints := h.io.Read32(chReg(ch, hcint))
	h.io.Write32(chReg(ch, hcmask), 0)
	h.io.Write32(chReg(ch, hcint), 0x7ff)
	if h.io.Read32(chReg(ch, hcchar))&channelEnable != 0 {
		return 0, fmt.Errorf("dwc2: ch %d remained enabled after halt", ch)
	}
	return ints, nil
}

func (h *MultiHost) isSingleTTHubBusy(hubAddr uint32, excludeCh int) bool {
	if hubAddr == 0 {
		return false
	}
	var singleTT bool
	for _, hub := range h.hubs {
		if uint32(hub.addr) == hubAddr {
			singleTT = hub.singleTT
			break
		}
	}
	if !singleTT {
		return false
	}
	for ch := 1; ch < h.channels; ch++ {
		if ch == excludeCh {
			continue
		}
		s := &h.slots[ch]
		if s.allocated && s.fault == nil && s.route.hub == hubAddr && (s.armed || (s.waitingSplit && s.completeSplit)) {
			return true
		}
	}
	return false
}

func (h *MultiHost) armSlot(ch int, completeSplit bool) error {
	if h.fault != nil {
		return h.fault
	}
	s := &h.slots[ch]
	in := s.ep.In()
	epType := uint32(s.ep.TransferType) & 3
	mps := s.ep.MaxPacketSize
	if mps == 0 || mps > maxMultiMPS {
		return fmt.Errorf("dwc2: invalid MPS %d on ch %d", mps, ch)
	}
	// Check TX request queue & FIFO space.
	qReg := uint32(nptxstatus)
	if epType == uint32(usb.TransferInterrupt) {
		qReg = ptxstatus
	}
	q := h.io.Read32(qReg)
	outBytes := 0
	if !in && !completeSplit {
		outBytes = s.outLen
	}
	if (q>>16)&255 == 0 || (!in && !completeSplit && int(q&65535) < (outBytes+3)/4) {
		return nil // Queue busy; retry next Service().
	}

	split := s.route.hub != 0
	selected := h.io.Read32(hfnum)
	frame := uint16(selected)
	if split {
		wire := frame & 0x3fff
		if epType == uint32(usb.TransferInterrupt) {
			wire = (wire + 1) & 0x3fff
		}
		if !completeSplit {
			// Single-TT high-speed hubs share one transaction translator across all
			// downstream ports; serialize new SSPLITs until any active split on this
			// hub finishes its CSPLIT phase.
			if h.isSingleTTHubBusy(s.route.hub, ch) {
				s.waitingSplit = true
				s.completeSplit = false
				return nil
			}
			// Allow SSPLIT on wire microframes 0..5 so CSPLIT (+2) fits inside the
			// same 8-microframe translator window.
			if wire&7 > 5 {
				s.waitingSplit = true
				s.completeSplit = false
				return nil
			}
			s.startFrame = wire
		} else {
			if (wire>>3)&0x7ff != (s.startFrame>>3)&0x7ff {
				// Missed translator window: restart SSPLIT on next frame rather
				// than fatally retiring the device.
				s.completeSplit = false
				s.waitingSplit = true
				return nil
			}
			if frameBefore(wire, s.nextFrame) {
				// Wait until the scheduled CSPLIT microframe (startFrame+2 after
				// SSPLIT ACK, or cur+1 after CSPLIT NYET).
				s.completeSplit = true
				s.waitingSplit = true
				return nil
			}
		}
	}

	h.io.Write32(chReg(ch, hcint), 0x7ff)
	h.io.Write32(chReg(ch, hcmask), 0x7ff)

	var splt uint32
	if split {
		splt = 1<<31 | 3<<14 | s.route.hub<<7 | s.route.port
		if completeSplit {
			splt |= 1 << 16
		}
	}
	h.io.Write32(chReg(ch, hcsplit), splt)

	size := uint32(outBytes)
	if in {
		size = uint32(mps)
	}
	h.io.Write32(chReg(ch, hctsiz), size|1<<19|s.toggle<<29)

	flags := uint32(mps) | uint32(s.ep.Number())<<11 | epType<<18 | 1<<20 | uint32(s.ownerAddr)<<22
	if in {
		flags |= 1 << 15
	}
	if s.speed == usb.SpeedLow {
		flags |= 1 << 17
	}
	if split && epType == uint32(usb.TransferInterrupt) {
		flags = (flags &^ uint32(3<<20)) | 3<<20
	}
	if epType == uint32(usb.TransferInterrupt) && frame&1 == 0 {
		flags |= 1 << 29 // ODD_FRM: target next frame/microframe
	}

	s.armed = true
	s.halting = false
	s.waitingSplit = false
	s.received = false
	s.done = false
	s.nak = false
	s.ack = false
	s.nyet = false
	s.inLen = 0
	s.deadline = h.io.Now().Add(500 * time.Millisecond)

	h.io.Write32(chReg(ch, hcchar), flags|channelEnable)
	if !in && !completeSplit && outBytes > 0 {
		fifoAddr := chFIFO(ch)
		for i := 0; i < outBytes; i += 4 {
			var word [4]byte
			copy(word[:], s.outBuf[i:min(i+4, outBytes)])
			h.io.Write32(fifoAddr, binary.LittleEndian.Uint32(word[:]))
		}
	}
	return nil
}

// consumeSlotRX processes one RX FIFO entry targeting a non-CH0 channel slot
// with full ownership, armed-state, IN direction, buffer size, and DATA PID checks.
func (h *MultiHost) consumeSlotRX(ch, count int, dpid, kind uint32) error {
	if ch <= 0 || ch >= h.channels {
		// Unexpected channel in RX FIFO: drain words to keep FIFO healthy.
		if kind == 2 {
			for i := 0; i < count; i += 4 {
				_ = h.io.Read32(fifo)
			}
		}
		return nil
	}
	s := &h.slots[ch]
	switch kind {
	case 2: // IN data packet
		if !s.allocated || !s.armed || !s.ep.In() || count > len(s.inBuf) || dpid != s.toggle {
			for i := 0; i < count; i += 4 {
				_ = h.io.Read32(fifo)
			}
			if s.allocated {
				s.fault = fmt.Errorf("dwc2: ch %d invalid RX count=%d dpid=%d want=%d", ch, count, dpid, s.toggle)
				if err := h.haltChannel(ch); err != nil {
					return h.failController(err)
				}
				s.armed = false
			}
			return nil
		}
		s.received = true
		s.inLen = count
		for i := 0; i < count; i += 4 {
			var word [4]byte
			binary.LittleEndian.PutUint32(word[:], h.io.Read32(fifo))
			copy(s.inBuf[i:count], word[:min(4, count-i)])
		}
	case 3, 5, 7:
		// Transfer-complete, data-toggle error, channel-halted markers carry no payload.
	}
	return nil
}

// drainRXIntoSlots drains up to maxEntries RX FIFO status/payload records into
// the corresponding active channel slots without discarding valid IN data.
func (h *MultiHost) drainRXIntoSlots(maxEntries int) error {
	for n := 0; n < maxEntries; n++ {
		if h.io.Read32(gint)&gintRXFLVL == 0 {
			break
		}
		v := h.io.Read32(rxpop)
		ch := int(v & 0x0f)
		count := int((v >> 4) & 0x7ff)
		dpid := (v >> 15) & 3
		kind := (v >> 17) & 0x0f
		if err := h.consumeSlotRX(ch, count, dpid, kind); err != nil {
			return err
		}
	}
	return nil
}

// Service drains RX FIFO entries, advances all active hardware channels,
// schedules due periodic/bulk transfers, and processes root & hub hotplug.
func (h *MultiHost) Service() error {
	if h.fault != nil {
		return h.fault
	}
	if h.paused {
		return nil
	}
	if h.irqSuspended {
		if err := h.pioCheck(); err != nil {
			return h.failController(err)
		}
		h.resumeIRQ()
	}
	if err := h.pioCheck(); err != nil {
		return h.failController(err)
	}

	// 1. Drain RX FIFO entries directly into per-channel fixed buffers.
	// Defer channel completion processing if gintRXFLVL remains asserted after
	// the per-call bound so no channel retires HCINT.XFERCOMPL before its
	// payload has been popped from the RX FIFO.
	if err := h.drainRXIntoSlots(256); err != nil {
		return err
	}

	now := h.io.Now()
	if h.io.Read32(gint)&gintRXFLVL != 0 {
		if !h.draining {
			h.armMultiIRQ(now)
		}
		return nil
	}

	// 2. Advance every allocated channel 1..N-1.
	for ch := 1; ch < h.channels; ch++ {
		s := &h.slots[ch]
		if !s.allocated || s.fault != nil {
			continue
		}
		if s.armed {
			h.stepChannel(ch, now)
			if h.fault != nil {
				return h.fault
			}
		}
		if s.fault != nil || s.armed {
			continue
		}
		// Advance waiting split channel or re-arm due IN/OUT transfers.
		if s.waitingSplit {
			if err := h.armSlot(ch, s.completeSplit); err != nil {
				s.fault = err
			}
			continue
		}
		if s.ep.In() {
			if !s.inReady && !now.Before(s.nextDue) {
				if err := h.armSlot(ch, false); err != nil {
					s.fault = err
				}
			}
		} else if s.outPending && !now.Before(s.nextDue) {
			if err := h.armSlot(ch, false); err != nil {
				s.fault = err
			}
		}
	}

	// 3. Hotplug monitoring: root port + hub Interrupt-IN status bitmaps.
	if h.fault != nil {
		return h.fault
	}
	if !h.draining {
		h.pollRootHotplug()
		h.pollHubHotplug()
		if h.fault != nil {
			return h.fault
		}
		h.armMultiIRQ(now)
	}
	return nil
}

func (h *MultiHost) stepChannel(ch int, now time.Time) {
	s := &h.slots[ch]
	split := s.route.hub != 0
	ints := h.io.Read32(chReg(ch, hcint))
	fatal := fatalChannel
	if s.ep.In() && !split {
		fatal &^= dataToggle
	}
	if split {
		fatal &^= 1 << 6 // NYET is part of split handshake
	}
	if ints&fatal != 0 {
		if err := h.haltChannel(ch); err != nil {
			_ = h.failController(err)
		}
		s.armed = false
		s.fault = fmt.Errorf("dwc2: ch %d addr=%d ep=%#x fatal hcint=%#x", ch, s.ownerAddr, s.ep.Address, ints)
		if !s.ep.In() && s.outPending {
			s.outPending = false
			s.outErr = s.fault
		}
		return
	}
	if ints&dataToggle != 0 {
		h.io.Write32(chReg(ch, hcint), dataToggle)
		ints &^= dataToggle
	}
	if s.ep.In() && !split && ints&(1<<5) != 0 {
		h.io.Write32(chReg(ch, hcint), 1<<5)
		ints &^= 1 << 5
	}
	if ints&1 != 0 {
		s.done = true
	}
	if ints&(1<<4) != 0 {
		s.nak = true
	}
	if split {
		if ints&(1<<5) != 0 {
			s.ack = true
		}
		if ints&(1<<6) != 0 {
			s.nyet = true
		}
	}
	if !s.done && !s.nak && !s.ack && !s.nyet {
		if !now.Before(s.deadline) {
			if err := h.haltChannel(ch); err != nil {
				_ = h.failController(err)
				return
			}
			s.armed = false
			if s.ep.In() {
				// Periodic/Bulk IN deadline expired (e.g. silent device); re-arm cleanly
				// instead of fatally retiring the channel.
				s.nextDue = now.Add(s.effectiveInterval())
			} else {
				s.outPending = false
				s.outErr = fmt.Errorf("dwc2: ch %d OUT timeout", ch)
			}
		}
		return
	}

	// Ensure channel is retired before finishing the packet.
	if h.io.Read32(chReg(ch, hcchar))&channelEnable != 0 {
		if !s.halting {
			c := h.io.Read32(chReg(ch, hcchar))
			h.io.Write32(chReg(ch, hcchar), c|channelEnable|channelDisable)
			h.io.Write32(chReg(ch, hcmask), hcintChHltd)
			s.halting = true
		}
		return
	}
	h.io.Write32(chReg(ch, hcint), 0x7ff)
	s.armed = false
	s.halting = false

	if split && s.ack && !s.completeSplit {
		s.completeSplit = true
		s.nextFrame = (s.startFrame + 2) & 0x3fff
		s.waitingSplit = true
		return
	}
	if split && s.nyet && s.completeSplit {
		cur := uint16(h.io.Read32(hfnum)) & 0x3fff
		if s.ep.TransferType == usb.TransferInterrupt {
			cur = (cur + 1) & 0x3fff
		}
		s.nextFrame = (cur + 1) & 0x3fff
		s.waitingSplit = true
		return
	}
	s.completeSplit = false

	if s.nak {
		if s.ep.TransferType == usb.TransferBulk {
			// Yield briefly on Bulk NAK so periodic HID channels keep priority.
			s.nextDue = now.Add(125 * time.Microsecond)
		} else {
			s.nextDue = now.Add(s.effectiveInterval())
		}
		return
	}

	if s.done {
		s.toggle ^= 2
		if s.ep.In() {
			if s.received {
				s.inReady = true
				s.inReadyLen = s.inLen
			}
			s.nextDue = now.Add(s.effectiveInterval())
			if irqTS := h.irqTimestampNS.Swap(0); irqTS > 0 {
				h.latency.record(now.UnixNano() - irqTS)
			}
		} else {
			s.outPending = false
			s.outDone = true
		}
	}
}

// ControlTransfer executes a synchronous bounded EP0 control transfer on CH0,
// supporting both IN (reqType&0x80 != 0) and OUT (reqType&0x80 == 0) data stages.
func (h *MultiHost) ControlTransfer(addr uint8, reqType, req byte, value, index uint16, data []byte) (int, error) {
	if h.fault != nil {
		return 0, h.fault
	}
	if h.paused {
		return 0, fmt.Errorf("dwc2: host paused")
	}
	if err := h.pioCheck(); err != nil {
		return 0, h.failController(err)
	}
	if len(data) > usb.MaxConfigurationBytes {
		return 0, fmt.Errorf("dwc2: control transfer length %d exceeds limit", len(data))
	}
	speed := h.rootSpeed
	ep0 := uint16(8)
	var route splitRoute
	if addr == 0 {
		if speed == usb.SpeedHigh {
			ep0 = 64
		}
	} else {
		if int(addr) >= len(h.devices) || h.devices[addr] == nil {
			return 0, fmt.Errorf("dwc2: unknown device address %d", addr)
		}
		dev := h.devices[addr]
		speed = dev.info.Speed
		ep0 = dev.info.Descriptor.MaxPacketSize0
		route = dev.route
	}
	return h.controlTransferTarget(addr, speed, ep0, route, reqType, req, value, index, data)
}

func (h *MultiHost) controlTransferTarget(addr uint8, speed usb.Speed, ep0 uint16, route splitRoute, reqType, req byte, value, index uint16, data []byte) (int, error) {
	inDir := reqType&0x80 != 0
	length := len(data)
	var setup [8]byte
	setup[0] = reqType
	setup[1] = req
	binary.LittleEndian.PutUint16(setup[2:4], value)
	binary.LittleEndian.PutUint16(setup[4:6], index)
	binary.LittleEndian.PutUint16(setup[6:8], uint16(length))

	deadline := h.io.Now().Add(2 * time.Second)
	// SETUP stage (PID = SETUP = 3)
	if _, err := h.ch0Transfer(addr, speed, ep0, route, false, 3, setup[:], 0, deadline); err != nil {
		return 0, err
	}
	// DATA stage (starts DATA1 = 2, alternates DATA0/DATA1)
	pid := uint32(2)
	doneBytes := 0
	if length > 0 {
		if inDir {
			for doneBytes < length {
				want := min(int(ep0), length-doneBytes)
				n, err := h.ch0Transfer(addr, speed, ep0, route, true, pid, nil, want, deadline)
				if err != nil {
					return doneBytes, err
				}
				copy(data[doneBytes:doneBytes+n], h.slots[0].inBuf[:n])
				doneBytes += n
				pid ^= 2
				if n < int(ep0) {
					break
				}
			}
		} else {
			for doneBytes < length {
				chunk := min(int(ep0), length-doneBytes)
				if _, err := h.ch0Transfer(addr, speed, ep0, route, false, pid, data[doneBytes:doneBytes+chunk], 0, deadline); err != nil {
					return doneBytes, err
				}
				doneBytes += chunk
				pid ^= 2
			}
		}
	}
	// STATUS stage (opposite direction of DATA, or IN when length==0; always DATA1=2)
	statusIn := !inDir || length == 0
	if _, err := h.ch0Transfer(addr, speed, ep0, route, statusIn, 2, nil, 0, deadline); err != nil {
		return doneBytes, err
	}
	return doneBytes, nil
}

func (h *MultiHost) ch0Transfer(addr uint8, speed usb.Speed, ep0 uint16, route splitRoute, in bool, pid uint32, out []byte, limit int, deadline time.Time) (int, error) {
	split := route.hub != 0
	if split && h.isSingleTTHubBusy(route.hub, 0) {
		ttDeadline := h.io.Now().Add(4 * time.Millisecond)
		for h.isSingleTTHubBusy(route.hub, 0) && h.io.Now().Before(ttDeadline) {
			if err := h.drainRXIntoSlots(64); err != nil {
				return 0, err
			}
			now := h.io.Now()
			for ch := 1; ch < h.channels; ch++ {
				s := &h.slots[ch]
				if !s.allocated || s.fault != nil || s.route.hub != route.hub {
					continue
				}
				if s.armed {
					h.stepChannel(ch, now)
					if h.fault != nil {
						return 0, h.fault
					}
				}
				if s.fault == nil && !s.armed && s.waitingSplit && s.completeSplit {
					if err := h.armSlot(ch, true); err != nil {
						s.fault = err
					}
				}
			}
			if h.fault != nil {
				return 0, h.fault
			}
		}
		if h.isSingleTTHubBusy(route.hub, 0) {
			return 0, fmt.Errorf("dwc2: single-TT hub %d busy; refusing CH0 split addr=%d", route.hub, addr)
		}
	}
	for retries := 0; retries < 1000; retries++ {
		if !h.io.Now().Before(deadline) {
			return 0, fmt.Errorf("dwc2: CH0 control timeout addr=%d", addr)
		}
		if err := h.pioCheck(); err != nil {
			return 0, h.failController(err)
		}
		if h.io.Read32(chReg(0, hcchar))&channelEnable != 0 {
			if err := h.haltChannel(0); err != nil {
				return 0, h.failController(err)
			}
		}
		completeSplit := false
		var startFrame, nextFrame uint16
	splitLoop:
		for {
			if !h.io.Now().Before(deadline) {
				return 0, fmt.Errorf("dwc2: CH0 split timeout addr=%d", addr)
			}
			// Wait for both a non-periodic request queue entry and enough TX FIFO
			// words before programming CH0 so concurrent Bulk-OUT traffic cannot
			// overrun or stall the shared non-periodic FIFO.
			outWords := 0
			if !in && !completeSplit {
				outWords = (len(out) + 3) / 4
			}
			if q := h.io.Read32(nptxstatus); (q>>16)&255 == 0 || int(q&65535) < outWords {
				runtime.Gosched()
				continue
			}
			if split {
				frame := uint16(h.io.Read32(hfnum)) & 0x3fff
				if !completeSplit {
					if frame&7 > 5 {
						runtime.Gosched()
						continue
					}
					startFrame = frame
				} else {
					if (frame>>3)&0x7ff != (startFrame>>3)&0x7ff {
						completeSplit = false
						continue
					}
					if frameBefore(frame, nextFrame) {
						runtime.Gosched()
						continue
					}
				}
			}
			h.io.Write32(chReg(0, hcint), 0x7ff)
			h.io.Write32(chReg(0, hcmask), 0x7ff)
			var splt uint32
			if split {
				splt = 1<<31 | 3<<14 | route.hub<<7 | route.port
				if completeSplit {
					splt |= 1 << 16
				}
			}
			h.io.Write32(chReg(0, hcsplit), splt)
			size := uint32(len(out))
			if in {
				size = uint32(ep0)
			} else if completeSplit {
				size = 0
			}
			h.io.Write32(chReg(0, hctsiz), size|1<<19|pid<<29)
			flags := uint32(ep0) | 1<<20 | uint32(addr)<<22
			if in {
				flags |= 1 << 15
			}
			if speed == usb.SpeedLow {
				flags |= 1 << 17
			}
			h.io.Write32(chReg(0, hcchar), flags|channelEnable)
			if !in && !completeSplit && len(out) > 0 {
				for i := 0; i < len(out); i += 4 {
					var word [4]byte
					copy(word[:], out[i:min(i+4, len(out))])
					h.io.Write32(chFIFO(0), binary.LittleEndian.Uint32(word[:]))
				}
			}

			received := false
			gotLen := 0
			for polls := 0; polls < 1000000; polls++ {
				if !h.io.Now().Before(deadline) {
					if err := h.haltChannel(0); err != nil {
						return 0, h.failController(err)
					}
					return 0, fmt.Errorf("dwc2: CH0 transfer timeout addr=%d", addr)
				}
				// Drain RX FIFO entries; route non-CH0 entries through the same
				// ownership/armed/direction/size/PID validator used by Service().
				for h.io.Read32(gint)&gintRXFLVL != 0 {
					v := h.io.Read32(rxpop)
					rch := int(v & 0x0f)
					cnt := int((v >> 4) & 0x7ff)
					dpid := (v >> 15) & 3
					kind := (v >> 17) & 0x0f
					if rch == 0 {
						if kind != 2 {
							continue
						}
						if cnt > limit || cnt > maxMultiMPS {
							for i := 0; i < cnt; i += 4 {
								_ = h.io.Read32(fifo)
							}
							if err := h.haltChannel(0); err != nil {
								return 0, h.failController(err)
							}
							return 0, fmt.Errorf("dwc2: CH0 RX overflow %d > %d", cnt, limit)
						}
						received = true
						gotLen = cnt
						for i := 0; i < cnt; i += 4 {
							var word [4]byte
							binary.LittleEndian.PutUint32(word[:], h.io.Read32(fifo))
							copy(h.slots[0].inBuf[i:cnt], word[:min(4, cnt-i)])
						}
					} else {
						if err := h.consumeSlotRX(rch, cnt, dpid, kind); err != nil {
							return 0, err
						}
					}
				}
				ints := h.io.Read32(chReg(0, hcint))
				fatal := fatalChannel
				if split {
					fatal &^= 1 << 6
				}
				if ints&fatal != 0 {
					if err := h.haltChannel(0); err != nil {
						return 0, h.failController(err)
					}
					return 0, fmt.Errorf("dwc2: CH0 fatal hcint=%#x addr=%d", ints, addr)
				}
				if in && !split && ints&(1<<4) != 0 {
					h.io.Write32(chReg(0, hcint), 1<<4)
					ints &^= 1 << 4
				}
				if in && !split && ints&(1<<5) != 0 {
					h.io.Write32(chReg(0, hcint), 1<<5)
					ints &^= 1 << 5
				}
				if ints&1 != 0 {
					if err := h.haltChannel(0); err != nil {
						return 0, h.failController(err)
					}
					if in && limit > 0 && !received {
						return 0, fmt.Errorf("dwc2: CH0 completed without RX")
					}
					return gotLen, nil
				}
				if split && ints&(1<<5) != 0 && !completeSplit {
					if err := h.haltChannel(0); err != nil {
						return 0, h.failController(err)
					}
					completeSplit = true
					nextFrame = (startFrame + 2) & 0x3fff
					continue splitLoop
				}
				if split && ints&(1<<6) != 0 {
					if err := h.haltChannel(0); err != nil {
						return 0, h.failController(err)
					}
					if completeSplit {
						nextFrame = (uint16(h.io.Read32(hfnum)) + 1) & 0x3fff
						continue splitLoop
					}
					break splitLoop // Retry SSPLIT on transient TT NYET
				}
				if ints&(1<<4) != 0 {
					if err := h.haltChannel(0); err != nil {
						return 0, h.failController(err)
					}
					break splitLoop // Retry on NAK
				}
				if polls&255 == 255 {
					runtime.Gosched()
				}
			}
			if err := h.haltChannel(0); err != nil {
				return 0, h.failController(err)
			}
			return 0, fmt.Errorf("dwc2: CH0 poll budget exhausted addr=%d", addr)
		}
	}
	return 0, fmt.Errorf("dwc2: CH0 NAK budget exhausted addr=%d", addr)
}

// OpenPipe allocates a dedicated hardware host channel (1..channels-1) for ep
// on the enumerated device addr.
func (h *MultiHost) OpenPipe(addr uint8, ep usb.EndpointDescriptor) (usb.Pipe, error) {
	if h.fault != nil {
		return nil, h.fault
	}
	if int(addr) == 0 || int(addr) >= len(h.devices) || h.devices[addr] == nil {
		return nil, fmt.Errorf("dwc2: unknown device address %d", addr)
	}
	if ep.TransferType != usb.TransferInterrupt && ep.TransferType != usb.TransferBulk {
		return nil, fmt.Errorf("dwc2: unsupported endpoint transfer type %d", ep.TransferType)
	}
	if ep.MaxPacketSize == 0 || ep.MaxPacketSize > maxMultiMPS {
		return nil, fmt.Errorf("dwc2: unsupported endpoint MPS %d", ep.MaxPacketSize)
	}
	reqWords := uint32((int(ep.MaxPacketSize) + 3) / 4)
	if ep.In() {
		if reqWords+2 > h.rxWords {
			return nil, fmt.Errorf("dwc2: IN endpoint MPS %d exceeds RX FIFO capacity %d words", ep.MaxPacketSize, h.rxWords)
		}
	} else if ep.TransferType == usb.TransferInterrupt {
		if reqWords > h.pTxWords {
			return nil, fmt.Errorf("dwc2: Interrupt-OUT endpoint MPS %d exceeds periodic TX FIFO capacity %d words", ep.MaxPacketSize, h.pTxWords)
		}
	} else {
		if reqWords > h.npTxWords {
			return nil, fmt.Errorf("dwc2: Bulk-OUT endpoint MPS %d exceeds non-periodic TX FIFO capacity %d words", ep.MaxPacketSize, h.npTxWords)
		}
	}
	ch := -1
	for i := 1; i < h.channels; i++ {
		s := &h.slots[i]
		if s.allocated {
			if s.ownerAddr == addr && s.ep.Address == ep.Address {
				return nil, fmt.Errorf("dwc2: endpoint %#x on device %d already open", ep.Address, addr)
			}
			continue
		}
		if ch < 0 {
			ch = i
		}
	}
	if ch < 0 {
		return nil, fmt.Errorf("dwc2: all %d hardware channels in use", h.channels-1)
	}
	dev := h.devices[addr]
	h.nextPipeGen++
	gen := h.nextPipeGen
	h.slots[ch] = multiChannel{
		allocated: true,
		ownerAddr: addr,
		gen:       gen,
		speed:     dev.info.Speed,
		route:     dev.route,
		ep:        ep,
		toggle:    0, // DATA0
		interval:  endpointInterval(dev.info.Speed, ep),
		nextDue:   h.io.Now(),
	}
	p := &multiPipe{host: h, ch: ch, addr: addr, gen: gen, ep: ep}
	if h.irqMode.Load() && h.irqActive.Load() {
		h.io.Write32(haintmsk, h.activeChannelMask())
	}
	return p, nil
}

type multiPipe struct {
	host   *MultiHost
	ch     int
	addr   uint8
	gen    uint64
	ep     usb.EndpointDescriptor
	closed bool
}

func (p *multiPipe) DeviceAddress() uint8          { return p.addr }
func (p *multiPipe) Endpoint() usb.EndpointDescriptor { return p.ep }

func (p *multiPipe) PollInto(dst []byte) (int, error) {
	if p.closed || p.host == nil {
		return 0, errors.New("dwc2: pipe closed")
	}
	s := &p.host.slots[p.ch]
	if !s.allocated || s.ownerAddr != p.addr || s.gen != p.gen {
		return 0, errors.New("dwc2: device detached")
	}
	if s.fault != nil {
		return 0, s.fault
	}
	if !s.inReady {
		return 0, nil
	}
	n := s.inReadyLen
	if n > len(dst) {
		return 0, fmt.Errorf("dwc2: buffer too small (%d < %d)", len(dst), n)
	}
	copy(dst[:n], s.inBuf[:n])
	s.inReady = false
	s.inReadyLen = 0
	return n, nil
}

func (p *multiPipe) Write(data []byte) error {
	if p.closed || p.host == nil {
		return errors.New("dwc2: pipe closed")
	}
	if p.ep.In() {
		return errors.New("dwc2: cannot write to IN pipe")
	}
	if len(data) > int(p.ep.MaxPacketSize) || len(data) > maxMultiMPS {
		return fmt.Errorf("dwc2: OUT write %d exceeds MPS %d", len(data), p.ep.MaxPacketSize)
	}
	s := &p.host.slots[p.ch]
	if !s.allocated || s.ownerAddr != p.addr || s.gen != p.gen {
		return errors.New("dwc2: device detached")
	}
	if s.fault != nil {
		return s.fault
	}
	if s.outPending {
		return errors.New("dwc2: OUT pipe busy")
	}
	copy(s.outBuf[:], data)
	s.outLen = len(data)
	s.outPending = true
	s.outDone = false
	s.outErr = nil
	s.nextDue = p.host.io.Now()
	return nil
}

func (p *multiPipe) WriteDone() (bool, error) {
	if p.closed || p.host == nil {
		return false, errors.New("dwc2: pipe closed")
	}
	s := &p.host.slots[p.ch]
	if !s.allocated || s.ownerAddr != p.addr || s.gen != p.gen {
		return false, errors.New("dwc2: device detached")
	}
	if s.outErr != nil {
		return false, s.outErr
	}
	if s.fault != nil {
		return false, s.fault
	}
	return s.outDone, nil
}

func (p *multiPipe) SetIntervalOverride(interval time.Duration) {
	if p.closed || p.host == nil {
		return
	}
	s := &p.host.slots[p.ch]
	if s.allocated && s.ownerAddr == p.addr && s.gen == p.gen {
		s.overrideInterval = interval
	}
}

func (p *multiPipe) Close() error {
	if p.closed || p.host == nil {
		return nil
	}
	p.closed = true
	if p.host.fault != nil {
		return p.host.fault
	}
	s := &p.host.slots[p.ch]
	if s.allocated && s.ownerAddr == p.addr && s.gen == p.gen {
		if err := p.host.pioCheck(); err != nil {
			s.fault = err
			return p.host.failController(err)
		}
		if err := p.host.haltChannel(p.ch); err != nil {
			// Retain slot as allocated/faulted so an active hardware channel is never reused.
			s.fault = err
			return p.host.failController(err)
		}
		p.host.slots[p.ch] = multiChannel{}
		if p.host.irqMode.Load() && p.host.irqActive.Load() {
			p.host.io.Write32(haintmsk, p.host.activeChannelMask())
		}
	}
	return nil
}

// PausePolling drains any completed IN/OUT transfers (preserving data toggles
// and received payloads across ResumePolling), halts every active hardware
// channel, and suspends GICv3 IRQs so zero MMIO occurs while paused.
func (h *MultiHost) PausePolling(ctx context.Context) error {
	if h == nil || ctx == nil || h.io == nil || !h.owned {
		return fmt.Errorf("dwc2: polling context required")
	}
	if h.fault != nil {
		return h.fault
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if h.paused {
		return fmt.Errorf("dwc2: polling already paused")
	}
	if err := h.pioCheck(); err != nil {
		return h.failController(err)
	}
	h.draining = true
	defer func() { h.draining = false }()

	// 1. Account for any already-completed transfers in RX FIFO / HCINT first.
	if err := h.drainRXIntoSlots(64); err != nil {
		return err
	}
	now := h.io.Now()
	for ch := 1; ch < h.channels; ch++ {
		if h.slots[ch].allocated && h.slots[ch].armed {
			h.stepChannel(ch, now)
			if h.fault != nil {
				return h.fault
			}
		}
	}
	if h.fault != nil {
		return h.fault
	}

	// 2. Halt every hardware channel while capturing any HCINT completion bits
	// observed as CHENA clears, keeping s.armed set until the post-halt RX drain
	// runs so DATA0/DATA1 toggles, received IN payloads, and ACKed start-splits
	// stay synchronized.
	var haltedInts [maxMultiChannels]uint32
	for ch := 0; ch < h.channels; ch++ {
		if err := ctx.Err(); err != nil {
			return h.failController(err)
		}
		ints, err := h.haltChannelCapture(ch)
		if err != nil {
			return h.failController(err)
		}
		haltedInts[ch] = ints
	}
	if err := h.drainRXIntoSlots(256); err != nil {
		return err
	}
	hasPendingCSplit := false
	for ch := 1; ch < h.channels; ch++ {
		s := &h.slots[ch]
		if !s.allocated {
			continue
		}
		if s.armed {
			split := s.route.hub != 0
			if haltedInts[ch]&1 != 0 || s.done || (s.ep.In() && s.received) {
				s.toggle ^= 2
				if s.ep.In() {
					if s.received {
						s.inReady = true
						s.inReadyLen = s.inLen
					}
					s.nextDue = now.Add(s.effectiveInterval())
				} else {
					s.outPending = false
					s.outDone = true
				}
				s.waitingSplit = false
				s.completeSplit = false
			} else if split && !s.completeSplit && (haltedInts[ch]&(1<<5) != 0 || s.ack) {
				// SSPLIT was accepted by the hub transaction translator concurrently
				// with halt: promote to CSPLIT and finish the split phase before pause.
				s.ack = false
				s.completeSplit = true
				s.waitingSplit = true
				s.nextFrame = (s.startFrame + 2) & 0x3fff
			} else {
				s.waitingSplit = false
				s.completeSplit = false
			}
			s.armed = false
			s.halting = false
		}
		if s.fault == nil && s.waitingSplit && s.completeSplit {
			hasPendingCSplit = true
		}
	}

	// 3. Finish any ACKed split transactions (CSPLIT) within a bounded 4 ms window
	// so hub transaction translators are not left holding an uncompleted split
	// across an arbitrary pause duration.
	if hasPendingCSplit {
		csplitDeadline := h.io.Now().Add(4 * time.Millisecond)
		for {
			if err := ctx.Err(); err != nil {
				return h.failController(err)
			}
			if err := h.drainRXIntoSlots(64); err != nil {
				return err
			}
			t := h.io.Now()
			pending := false
			for ch := 1; ch < h.channels; ch++ {
				s := &h.slots[ch]
				if !s.allocated || s.fault != nil {
					continue
				}
				if s.armed {
					h.stepChannel(ch, t)
					if h.fault != nil {
						return h.fault
					}
				}
				if s.fault != nil {
					continue
				}
				if !s.armed && s.waitingSplit && s.completeSplit {
					if err := h.armSlot(ch, true); err != nil {
						s.fault = err
					}
				}
				if s.fault == nil && (s.armed || (s.waitingSplit && s.completeSplit)) {
					pending = true
				}
			}
			if h.fault != nil {
				return h.fault
			}
			if !pending || !t.Before(csplitDeadline) {
				break
			}
		}
		// Halt any CSPLIT channel still armed at the deadline and account for any
		// completion observed while halting.
		var postInts [maxMultiChannels]uint32
		for ch := 1; ch < h.channels; ch++ {
			if h.slots[ch].allocated && h.slots[ch].armed {
				ints, err := h.haltChannelCapture(ch)
				if err != nil {
					return h.failController(err)
				}
				postInts[ch] = ints
			}
		}
		if err := h.drainRXIntoSlots(256); err != nil {
			return err
		}
		t := h.io.Now()
		for ch := 1; ch < h.channels; ch++ {
			s := &h.slots[ch]
			if !s.allocated {
				continue
			}
			if s.armed {
				if postInts[ch]&1 != 0 || s.done || (s.ep.In() && s.received) {
					s.toggle ^= 2
					if s.ep.In() {
						if s.received {
							s.inReady = true
							s.inReadyLen = s.inLen
						}
						s.nextDue = t.Add(s.effectiveInterval())
					} else {
						s.outPending = false
						s.outDone = true
					}
					s.waitingSplit = false
					s.completeSplit = false
				} else if s.completeSplit {
					s.waitingSplit = true
				}
				s.armed = false
				s.halting = false
			} else if s.waitingSplit && !s.completeSplit {
				// armSlot saw wire>>3 != startFrame>>3 (window closed); clear waitingSplit.
				s.waitingSplit = false
			}
		}
	}
	for ch := 0; ch < h.channels; ch++ {
		if h.io.Read32(chReg(ch, hcchar))&channelEnable != 0 {
			return h.failController(fmt.Errorf("dwc2: ch %d active after pause drain", ch))
		}
	}
	if err := h.pioCheck(); err != nil {
		return h.failController(err)
	}
	h.suspendIRQ()
	h.paused = true
	return nil
}

// ResumePolling exits the paused state without re-enumerating attached devices.
func (h *MultiHost) ResumePolling(ctx context.Context) error {
	if h == nil || ctx == nil || h.io == nil || !h.owned {
		return fmt.Errorf("dwc2: polling context required")
	}
	if h.fault != nil {
		return h.fault
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !h.paused {
		return fmt.Errorf("dwc2: polling is not paused")
	}
	h.paused = false
	return nil
}

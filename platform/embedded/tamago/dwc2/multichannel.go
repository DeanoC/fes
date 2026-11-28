// SPDX-License-Identifier: GPL-3.0-or-later

// SPDX-License-Identifier: GPL-3.0-or-later
package dwc2

import (
	"context"
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
	hctsizDoPng    = uint32(1 << 31)
	gintPRTINT     = uint32(1 << 24)
	multiIRQMask   = gintRXFLVL | gintHCH | gintPRTINT | gintDisconn
	multiIRQMaskSF = multiIRQMask | gintSOF

	maxMultiChannels = 16
	maxMultiMPS      = 512
	maxMultiDevices  = 31
	maxEventQueue    = 64
)

func chReg(ch int, base uint32) uint32 { return base + uint32(ch)*0x20 }

func chFIFO(ch int) uint32 { return fifo + uint32(ch)*0x1000 }

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
	doPing     bool
	pinging    bool

	deadline     time.Time
	haltDeadline time.Time
	nextDue      time.Time
	fault        error
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
	io             Registers
	owned          bool
	channels       int
	fifoWords      uint32
	rxWords        uint32
	npTxWords      uint32
	pTxWords       uint32
	rootSpeed      usb.Speed
	rootConn       bool
	rootRetryCount uint8
	rootRetryAt    time.Time
	rootExhausted  bool
	paused         bool
	draining       bool
	fault          error // controller-level fatal fault only (e.g. foreign DMA)

	slots       [maxMultiChannels]multiChannel
	devices     [maxMultiDevices + 1]*multiDevice // indexed by USB address 1..31
	hubs        []*multiHub
	nextID      uint8
	nextPipeGen uint64

	events  [maxEventQueue]usb.HostEvent
	evHead  int
	evCount int

	irqMu          sync.Mutex
	irqGIC         *gic.Controller
	irqID          uint32
	irqMode        atomic.Bool
	irqActive      atomic.Bool
	irqSuspended   atomic.Bool
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
	if h.io.Read32(gahb)&(1<<5) != 0 || h.io.Read32(hcfg)&(1<<23) != 0 {
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
	h.irqMu.Lock()
	guard := h.irqGuard
	h.irqMu.Unlock()
	if (guard != nil && !guard()) || h.io.Read32(gahb)&(1<<5) != 0 || h.io.Read32(hcfg)&(1<<23) != 0 {
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
	// Defer stepChannel while gintRXFLVL remains asserted so no IN channel
	// retires HCINT.XFERCOMPL before its payload is popped from the RX FIFO.
	if err := h.drainRXIntoSlots(64); err != nil {
		return err
	}
	now := h.io.Now()
	if h.io.Read32(gint)&gintRXFLVL == 0 {
		for ch := 1; ch < h.channels; ch++ {
			if h.slots[ch].allocated && h.slots[ch].armed {
				h.stepChannel(ch, now)
				if h.fault != nil {
					return h.fault
				}
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
	for i := 0; i < 16 && h.io.Read32(gint)&gintRXFLVL != 0; i++ {
		if err := ctx.Err(); err != nil {
			return h.failController(err)
		}
		if err := h.drainRXIntoSlots(256); err != nil {
			return err
		}
	}
	hasPendingCSplit := false
	for ch := 1; ch < h.channels; ch++ {
		s := &h.slots[ch]
		if !s.allocated {
			continue
		}
		if s.armed {
			split := s.route.hub != 0
			hsOut := !s.ep.In() && !split && s.speed == usb.SpeedHigh && s.ep.TransferType == usb.TransferBulk
			if s.pinging {
				s.pinging = false
				if haltedInts[ch]&(1<<5) != 0 || s.ack {
					s.doPing = false
				}
				s.waitingSplit = false
				s.completeSplit = false
			} else if hsOut && (haltedInts[ch]&(1<<6) != 0 || s.nyet) {
				s.doPing = true
				s.toggle ^= 2
				s.outPending = false
				s.outDone = true
				s.waitingSplit = false
				s.completeSplit = false
			} else if (s.ep.In() && s.received) || (!s.ep.In() && (haltedInts[ch]&1 != 0 || s.done)) {
				s.toggle ^= 2
				if s.ep.In() {
					s.inReady = true
					s.inReadyLen = s.inLen
					s.nextDue = now.Add(s.effectiveInterval())
				} else {
					if hsOut {
						s.doPing = false
					}
					s.outPending = false
					s.outDone = true
					if s.ep.TransferType == usb.TransferInterrupt {
						s.nextDue = now.Add(s.effectiveInterval())
					}
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
			} else if split && s.completeSplit && (haltedInts[ch]&(1<<6) != 0 || s.nyet) {
				// CSPLIT returned NYET concurrently with halt: defer CSPLIT to the
				// next microframe so Step 3 finishes it before pause completes.
				s.nyet = false
				cur := uint16(h.io.Read32(hfnum)) & 0x3fff
				if s.ep.TransferType == usb.TransferInterrupt {
					cur = (cur + 1) & 0x3fff
				}
				s.nextFrame = (cur + 1) & 0x3fff
				s.waitingSplit = true
			} else {
				s.waitingSplit = false
				s.completeSplit = false
			}
			s.armed = false
			s.halting = false
			s.haltDeadline = time.Time{}
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
			if h.io.Read32(gint)&gintRXFLVL != 0 {
				if !t.Before(csplitDeadline) {
					break
				}
				continue
			}
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
		for i := 0; i < 16 && h.io.Read32(gint)&gintRXFLVL != 0; i++ {
			if err := ctx.Err(); err != nil {
				return h.failController(err)
			}
			if err := h.drainRXIntoSlots(256); err != nil {
				return err
			}
		}
		t := h.io.Now()
		for ch := 1; ch < h.channels; ch++ {
			s := &h.slots[ch]
			if !s.allocated {
				continue
			}
			if s.armed {
				if (s.ep.In() && s.received) || (!s.ep.In() && (postInts[ch]&1 != 0 || s.done)) {
					s.toggle ^= 2
					if s.ep.In() {
						s.inReady = true
						s.inReadyLen = s.inLen
						s.nextDue = t.Add(s.effectiveInterval())
					} else {
						s.outPending = false
						s.outDone = true
						if s.ep.TransferType == usb.TransferInterrupt {
							s.nextDue = t.Add(s.effectiveInterval())
						}
					}
					s.waitingSplit = false
					s.completeSplit = false
				} else if s.completeSplit {
					s.waitingSplit = true
				}
				s.armed = false
				s.halting = false
				s.haltDeadline = time.Time{}
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
	if err := h.suspendIRQ(); err != nil {
		if h.fault == nil {
			h.fault = err
		}
		return h.fault
	}
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
	if h.irqMode.Load() {
		h.signalReady()
	}
	return nil
}

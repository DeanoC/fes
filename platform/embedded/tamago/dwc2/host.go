// SPDX-License-Identifier: GPL-3.0-or-later

// Package dwc2 is an exploratory polling PIO keyboard/gamepad host. Firmware
// owns SoC clocks, ULPI PHY, pinmux and VBUS routing. DMA is never enabled.
package dwc2

import (
	"encoding/binary"
	"fmt"
	"runtime"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/usb"
)

// Register identities were checked against Linux v6.12 drivers/usb/dwc2/hw.h.
const (
	gahb           = 0x008
	gusb           = 0x00c
	reset          = 0x010
	gint           = 0x014
	gintmask       = 0x018
	rxpeek         = 0x01c
	rxpop          = 0x020
	rxsize         = 0x024
	nptxsize       = 0x028
	nptxstatus     = 0x02c
	snpsid         = 0x040
	hwcfg2         = 0x048
	hwcfg3         = 0x04c
	ptxsize        = 0x100
	hcfg           = 0x400
	hfir           = 0x404
	hfnum          = 0x408
	ptxstatus      = 0x410
	hprt           = 0x440
	hcchar         = 0x500
	hcsplit        = 0x504
	hcint          = 0x508
	hcmask         = 0x50c
	hctsiz         = 0x510
	pcgc           = 0xe00
	fifo           = 0x1000
	channelEnable  = uint32(1 << 31)
	channelDisable = uint32(1 << 30)
	dataToggle     = uint32(1 << 10)
	fatalChannel   = uint32(1<<2 | 1<<3 | 1<<6 | 1<<7 | 1<<8 | 1<<9 | 1<<10)
)

type Registers interface {
	Read32(uint32) uint32
	Write32(uint32, uint32)
	Now() time.Time
}

// Config is an actual firmware clock declaration. This prototype requires a
// 60 MHz ULPI PHY already configured, reset released and VBUS supply available.
type Config struct{ PHYClockHz uint32 }

type packet struct {
	split, completeSplit, waiting bool
	ack, nyet                     bool
	char, transferSize            uint32
	out                           []byte
	startFrame, nextFrame         uint16

	active, in, done, nak, received, halting bool
	pid                                      uint32
	limit                                    int
	data                                     []byte
	deadline                                 time.Time
	polls                                    int
	lastStep                                 time.Time
	stepGapNS, maxStepGapNS                  int64
	toggleRX, toggleIRQs                     uint32
	ackIRQs                                  uint32
	nakIRQs                                  uint32
	continuations                            uint32
	requestAfter                             time.Time
	lastContinue                             continuationState
	firstToggle                              error
	toggleRetiring, toggleRetry              bool
	toggleCancelled                          bool
	handledToggleRX, handledToggleIRQs       uint32
	toggleRetirements                        uint32
}
type Host struct {
	packetSequence        uint64
	channelCommands       channelCommands
	pollingPaused         bool
	draining              bool
	channels              uint32
	gamepadMode           bool
	gamepad               *usb.Gamepad
	reportDescriptor      []byte
	reportDescriptorBytes uint16
	initializing          bool
	bootDeadline          time.Time

	keyboard                 target
	keyboardFound            bool
	path                     []hubPort
	nextAddress              uint32
	portBudget               int
	interval                 time.Duration
	healthNext               time.Time
	healthStage, healthIndex int
	healthPolls              int
	healthDeadline           time.Time
	healthStatus             uint32
	devices                  []string

	rootSpeed uint32
	route     splitRoute

	io      Registers
	owned   bool
	speed   uint32
	address uint32
	ep0     uint16
	config  usb.Configuration
	pending packet
	fault   error
	next    time.Time
	toggle  uint32
}

func Open(io Registers, config Config) (*Host, error) {
	return openInput(io, config, false)
}
func openInput(io Registers, config Config, gamepad bool) (*Host, error) {
	if io == nil || config.PHYClockHz != 60000000 {
		return nil, fmt.Errorf("dwc2: requires firmware-configured 60 MHz ULPI PHY")
	}
	h := &Host{io: io, ep0: 8, gamepadMode: gamepad, initializing: true, bootDeadline: io.Now().Add(4 * time.Second)}
	if err := h.initialize(); err != nil {
		return h, h.fail(err)
	}
	if err := h.enumerate(); err != nil {
		return h, h.fail(err)
	}
	h.initializing = false
	return h, nil
}
func (h *Host) controlDeadline(end time.Time) time.Time {
	if h.initializing && h.bootDeadline.Before(end) {
		return h.bootDeadline
	}
	return end
}

func (h *Host) wait(reg, mask, want uint32) error {
	start := h.io.Now()
	end := h.controlDeadline(start.Add(500 * time.Millisecond))
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
	return fmt.Errorf("dwc2: register wait exhausted address=%#x value=%#x mask=%#x want=%#x elapsed=%s", reg, last, mask, want, h.io.Now().Sub(start))
}
func (h *Host) pause(duration time.Duration) error {
	start := h.io.Now()
	end := start.Add(duration)
	if h.initializing && end.After(h.bootDeadline) {
		return fmt.Errorf("dwc2: initialization delay exceeds startup budget")
	}
	for n := 0; n < 1000000; n++ {
		if !h.io.Now().Before(end) {
			return nil
		}
		if n&255 == 255 {
			runtime.Gosched()
		}
	}
	return fmt.Errorf("dwc2: delay poll budget exhausted duration=%s elapsed=%s", duration, h.io.Now().Sub(start))
}

// fail permanently retires this host. No report or channel is reused after
// ambiguity; PIO holds no Go-heap DMA addresses. A fresh boot is required.
func (h *Host) fail(err error) error {
	if h.fault != nil {
		return h.fault
	}
	h.fault = err
	if !h.owned {
		return err
	}
	if h.io.Read32(gahb)&(1<<5) != 0 {
		// DMA state no longer belongs to this PIO driver. Do not halt or
		// rewrite a channel whose address/ownership is now uncertain.
		return h.fault
	}
	h.io.Write32(gintmask, 0)
	// Do not touch a firmware-owned active DMA/channel on admission failure.
	if h.pending.active && h.io.Read32(hcchar)&channelEnable != 0 {
		h.writeChannel("containment", h.io.Read32(hcchar)|channelEnable|channelDisable)
		if halt := h.wait(hcchar, channelEnable, 0); halt != nil {
			h.fault = fmt.Errorf("%w; containment: %v; channel_commands={%s}", err, halt, h.channelCommands)
		}
	}
	return h.fault
}
func (h *Host) portWrite(set, clear uint32) {
	// ENA is write-one-to-disable; connection/enable/overcurrent changes are W1C.
	v := h.io.Read32(hprt) &^ (uint32(1<<1|1<<2|1<<3|1<<5) | clear)
	h.io.Write32(hprt, v|set)
}
func (h *Host) initialize() error {
	id := h.io.Read32(snpsid)
	if id < 0x4f542000 || id >= 0x4f544000 {
		return fmt.Errorf("dwc2: unsupported core ID %#x", id)
	}
	cfg := h.io.Read32(hwcfg2)
	mode := cfg & 7
	if mode == 3 || mode == 4 || mode == 7 || (cfg>>3)&3 == 1 || (cfg>>3)&3 == 3 || cfg&(1<<19) == 0 || h.io.Read32(hwcfg3)>>16 < 256 {
		return fmt.Errorf("dwc2: unsupported host/PIO/FIFO capabilities")
	}
	if h.io.Read32(gusb)&(1<<4|1<<6|1<<3) != 1<<4 {
		return fmt.Errorf("dwc2: firmware ULPI configuration missing")
	}
	if h.io.Read32(gahb)&(1<<5) != 0 {
		return fmt.Errorf("dwc2: firmware DMA still enabled")
	}
	channels := ((cfg >> 14) & 15) + 1
	h.channels = channels
	for n := uint32(0); n < channels; n++ {
		if h.io.Read32(hcchar+n*0x20)&channelEnable != 0 {
			return fmt.Errorf("dwc2: firmware channel still active")
		}
	}
	h.owned = true
	h.io.Write32(gahb, 0) // polling, slave PIO, no global IRQ or DMA
	h.io.Write32(gintmask, 0)
	h.io.Write32(pcgc, 0)
	if err := h.wait(reset, 1<<31, 1<<31); err != nil {
		return err
	}
	h.io.Write32(reset, 1)
	if err := h.wait(reset, 1, 0); err != nil {
		return err
	}
	if err := h.wait(reset, 1<<31, 1<<31); err != nil {
		return err
	}
	if err := h.pause(time.Millisecond); err != nil {
		return err
	}
	h.io.Write32(gusb, (h.io.Read32(gusb)&^uint32(1<<30|1<<15|1<<9|1<<8))|1<<29)
	if err := h.pause(50 * time.Millisecond); err != nil {
		return err
	}
	if err := h.wait(gint, 1, 1); err != nil {
		return err
	}
	h.io.Write32(hcfg, 0) // 30/60 MHz HS PHY; FS/LS operation, no descriptor DMA
	h.io.Write32(rxsize, 128)
	h.io.Write32(nptxsize, 64<<16|128)
	h.io.Write32(ptxsize, 64<<16|192)
	h.io.Write32(reset, 0x10<<6|1<<5|1<<4)
	if err := h.wait(reset, 1<<5|1<<4, 0); err != nil {
		return err
	}
	if h.io.Read32(hprt)&(1<<4) != 0 {
		return fmt.Errorf("dwc2: port overcurrent")
	}
	h.portWrite(1<<12, 0)
	if err := h.pause(100 * time.Millisecond); err != nil {
		return err
	}
	if err := h.wait(hprt, 1, 1); err != nil {
		return err
	}
	h.portWrite(1<<8, 0)
	if err := h.pause(50 * time.Millisecond); err != nil {
		return err
	}
	h.portWrite(0, 1<<8)
	if err := h.wait(hprt, 1|1<<2, 1|1<<2); err != nil {
		return err
	}
	if err := h.pause(10 * time.Millisecond); err != nil {
		return err
	}
	h.speed = (h.io.Read32(hprt) >> 17) & 3
	if h.speed > 2 {
		return fmt.Errorf("dwc2: invalid root speed")
	}
	h.rootSpeed = h.speed
	if h.speed == 0 {
		h.ep0 = 64
	}
	h.io.Write32(gint, 0xffffffff) // retire stale disconnect/port status before enumeration
	interval := uint32(59999)
	if h.rootSpeed == 0 {
		interval = 7499
	}
	h.io.Write32(hfir, interval) // 60 MHz ULPI: HS microframes or FS/LS frames, after enable
	return nil
}

func (h *Host) link() error {
	port := h.io.Read32(hprt)
	if port&(5|1<<12) != 5|1<<12 || port&(1<<4|1<<7|1<<8) != 0 || (port>>17)&3 != h.rootSpeed || h.io.Read32(gint)&(1<<29) != 0 {
		return fmt.Errorf("dwc2: root link disconnected/reset/changed: %#x", port)
	}
	return nil
}

func (h *Host) begin(ep uint8, mps uint16, kind uint32, in bool, pid uint32, out []byte, limit int, deadline time.Time) error {
	if err := h.link(); err != nil {
		return err
	}
	if err := h.pio(); err != nil {
		return err
	}
	if h.pending.active || h.io.Read32(hcchar)&channelEnable != 0 {
		return fmt.Errorf("dwc2: channel not retired")
	}
	if mps == 0 || mps > 64 || limit < 0 || limit > int(mps) || len(out) > int(mps) {
		return fmt.Errorf("dwc2: invalid packet bounds")
	}
	queue := h.io.Read32(nptxstatus)
	if kind == 3 {
		queue = h.io.Read32(ptxstatus)
	}
	if (queue>>16)&255 == 0 || (!in && int(queue&65535) < (len(out)+3)/4) {
		return fmt.Errorf("dwc2: FIFO/request queue unavailable")
	}
	size := uint32(len(out))
	if in {
		size = uint32(mps)
	}
	flags := uint32(mps) | uint32(ep&15)<<11 | kind<<18 | 1<<20 | h.address<<22
	if in {
		flags |= 1 << 15
	}
	if h.speed == 2 {
		flags |= 1 << 17
	}
	split := h.route.hub != 0
	if split && (h.rootSpeed != 0 || h.speed == 0 || h.route.port == 0 || h.route.hub > 127 || h.route.port > 127) {
		return fmt.Errorf("dwc2: invalid transaction translator route")
	}
	if split && kind == 3 {
		flags = (flags &^ uint32(3<<20)) | 3<<20
	}
	h.packetSequence++
	h.pending = packet{active: true, in: in, pid: pid, limit: limit, deadline: deadline, split: split, char: flags, transferSize: size, out: append([]byte(nil), out...)}
	if split {
		h.pending.waiting = true
		return nil
	}
	return h.armPacket(false)
}

func (h *Host) pio() error {
	if h.io.Read32(gahb)&(1<<5|1) != 0 || h.io.Read32(gintmask) != 0 || h.io.Read32(hcfg)&(1<<23) != 0 {
		return fmt.Errorf("dwc2: PIO/interrupt ownership changed")
	}
	return nil
}

// step consumes a fixed number of RX entries and register reads. Completion
// includes channel retirement; it never hands a still-owned buffer to callers.
func (h *Host) step() (done bool, data []byte, nak bool, err error) {
	defer func() {
		if err != nil {
			p := &h.pending
			err = fmt.Errorf("dwc2: packet address=%d endpoint=%d in=%t pid=%d split=%t complete_split=%t received=%t done=%t nak=%t halting=%t polls=%d toggle_rx=%d toggle_irqs=%d ack_irqs=%d nak_irqs=%d continuations=%d interval=%s last_continue={%s} service={last_gap_ns=%d max_gap_ns=%d}: %w",
				h.address, (p.char>>11)&15, p.in, p.pid, p.split, p.completeSplit, p.received, p.done, p.nak, p.halting, p.polls, p.toggleRX, p.toggleIRQs, p.ackIRQs, p.nakIRQs, p.continuations, h.interval, p.lastContinue, p.stepGapNS, p.maxStepGapNS, err)
			if p.firstToggle != nil {
				err = fmt.Errorf("%w; first toggle observation: %v", err, p.firstToggle)
			}
			err = fmt.Errorf("%w; channel_commands={%s}", err, h.channelCommands)
		}
	}()
	if err := h.pio(); err != nil {
		return false, nil, false, err
	}
	p := &h.pending
	if !p.active {
		return false, nil, false, fmt.Errorf("dwc2: no active packet")
	}
	p.polls++
	if p.polls > 1000000 {
		return false, nil, false, fmt.Errorf("dwc2: packet timeout")
	}
	now := h.io.Now()
	if !p.lastStep.IsZero() {
		p.stepGapNS = now.Sub(p.lastStep).Nanoseconds()
		if p.stepGapNS > p.maxStepGapNS {
			p.maxStepGapNS = p.stepGapNS
		}
	}
	p.lastStep = now
	if !now.Before(p.deadline) {
		return false, nil, false, fmt.Errorf("dwc2: packet timeout")
	}
	if err := h.link(); err != nil {
		return false, nil, false, err
	}
	if p.toggleRetry {
		return false, nil, false, h.rearmPeriodicToggle()
	}

	if p.waiting {
		frame := uint16(h.io.Read32(hfnum))
		if !p.completeSplit {
			// Start early in a USB frame, leaving time for TT work and completion.
			if frame&7 > 1 {
				return false, nil, false, nil
			}
			p.startFrame = frame
			if (p.char>>18)&3 == 3 {
				p.startFrame++
			}
		} else {
			age := uint16(frame - p.startFrame)
			if age >= 0x8000 {
				return false, nil, false, nil
			}
			if age > 7 || frame>>3 != p.startFrame>>3 || (p.char>>18)&3 == 3 && frame&7 == 7 {
				return false, nil, false, fmt.Errorf("dwc2: split completion missed its frame")
			}
			if uint16(frame-p.nextFrame) >= 0x8000 {
				return false, nil, false, nil
			}
		}
		p.waiting = false
		err := h.armPacket(p.completeSplit)
		return false, nil, false, err
	}
	for n := 0; n < 8; n++ {
		irq := h.io.Read32(gint)
		if irq&(1<<4) == 0 {
			break
		}
		// GRXSTSR observes the queue head without consuming it. Retain both
		// this sample and the destructive read if hardware returns a status
		// outside the PIO contract. Neither observation authorizes a retry.
		peek := h.io.Read32(rxpeek)
		v := h.io.Read32(rxpop)
		count := int((v >> 4) & 2047)
		kind := (v >> 17) & 15
		if v&15 != 0 {
			return false, nil, false, h.rxFault("unexpected RX channel", irq, peek, v)
		}
		switch kind {
		case 2:
			if !p.in || p.received || count > p.limit || (v>>15)&3 != p.pid || p.split && !p.completeSplit {
				return false, nil, false, h.rxFault("invalid RX size/PID/ownership", irq, peek, v)
			}
			p.received = true
			for i := 0; i < count; i += 4 {
				var word [4]byte
				binary.LittleEndian.PutUint32(word[:], h.io.Read32(fifo))
				p.data = append(p.data, word[:min(4, count-i)]...)
			}
		case 3, 7:
			// Transfer-complete and channel-halted entries carry no FIFO
			// payload. BCNT/DPID are meaningful for IN-data status2 only;
			// hardware may retain the preceding packet's values here.
			// HCINT and channel retirement still govern completion below.
		case 5:
			// DWC2 discards an IN packet with an unexpected data toggle. This
			// record has no payload; the armed transfer can still receive the
			// expected packet. Do not rearm, advance PID or extend its deadline.
			// A translator may need TT clearing, outside this direct-IN path.
			if !p.in || p.split || p.received || p.done || p.halting {
				return false, nil, false, h.rxFault("unexpected data-toggle notification", irq, peek, v)
			}
			p.toggleRX++
			if p.firstToggle == nil {
				p.firstToggle = h.rxFault("IN data-toggle notification", irq, peek, v)
			}
		default:
			return false, nil, false, h.rxFault(fmt.Sprintf("unsupported RX status %d", kind), irq, peek, v)
		}
	}
	if h.io.Read32(gint)&(1<<4) != 0 {
		return false, nil, false, nil
	}
	ints := h.io.Read32(hcint)
	fatal := fatalChannel
	if p.in && !p.split {
		fatal &^= dataToggle
	}
	if p.split {
		fatal &^= 1 << 6
	}
	if ints&fatal != 0 {
		return false, nil, false, h.channelFault(ints)
	}
	if ints&dataToggle != 0 {
		p.toggleIRQs++
		h.io.Write32(hcint, dataToggle) // W1C only this notification.
		ints &^= dataToggle
	}
	// Non-split IN ACK-after-error notifications are not completion. Clear
	// only that notification so it cannot indefinitely block PIO continuation.
	// Split ACK keeps its translator state machine below; OUT/PING is unchanged.
	if p.in && !p.split && ints&(1<<5) != 0 {
		p.ackIRQs++
		h.io.Write32(hcint, 1<<5)
		ints &^= 1 << 5
	}
	// Slave-mode direct control IN queues tokens within the same transfer.
	// A NAK is a notification from one token, not that transfer's outcome:
	// another queued token can supply data/completion before we poll again.
	// Linux's slave control-IN mask likewise excludes NAK. Acknowledge only
	// this bit; real RX, completion and channel retirement remain mandatory.
	// Periodic, split and OUT NAKs keep their existing retirement semantics.
	if p.in && !p.split && (p.char>>18)&3 == 0 && ints&(1<<4) != 0 {
		p.nakIRQs++
		h.io.Write32(hcint, 1<<4)
		ints &^= 1 << 4
	}
	if ints&1 != 0 {
		p.done = true
	}
	if ints&(1<<4) != 0 {
		p.nak = true
	}
	if p.split {
		p.ack = p.ack || ints&(1<<5) != 0
		p.nyet = p.nyet || ints&(1<<6) != 0
	}
	if (p.done || p.received) && p.nak || p.ack && (p.done || p.received || p.nak || p.nyet) || p.nyet && (p.done || p.received || p.nak) {
		return false, nil, false, fmt.Errorf("dwc2: contradictory completion")
	}
	if p.done || p.nak || p.ack || p.nyet {
		if h.io.Read32(hcchar)&channelEnable != 0 {
			if !p.halting {
				h.writeChannel("retire", h.io.Read32(hcchar)|channelEnable|channelDisable)
				p.halting = true
			}
			return false, nil, false, nil
		}
		h.io.Write32(hcint, 0x7ff)
		if p.split && p.ack {
			if p.completeSplit {
				return false, nil, false, fmt.Errorf("dwc2: ACK during complete split")
			}
			p.completeSplit = true
			p.nextFrame = p.startFrame + 2
			p.waiting = true
			p.ack = false
			p.halting = false
			return false, nil, false, nil
		}
		if p.split && p.nyet {
			if !p.completeSplit {
				return false, nil, false, fmt.Errorf("dwc2: NYET during start split")
			}
			p.nextFrame = uint16(h.io.Read32(hfnum)) + 1
			p.waiting = true
			p.nyet = false
			p.halting = false
			return false, nil, false, nil
		}
		if p.split && p.done && !p.completeSplit {
			return false, nil, false, fmt.Errorf("dwc2: start split completed without ACK")
		}
		if p.done && p.in && !p.received {
			return false, nil, false, fmt.Errorf("dwc2: completion without RX packet")
		}
		p.active = false
		p.toggleRetiring, p.toggleRetry = false, false
		return true, p.data, p.nak, nil
	}
	if ints&(1<<1) != 0 && !p.periodicTogglePending() {
		return false, nil, false, fmt.Errorf("dwc2: unexplained channel halt")
	}

	return false, nil, false, h.continueIN()
}

func (h *Host) transfer(in bool, pid uint32, out []byte, limit int, deadline time.Time) ([]byte, error) {
	for retries := 0; retries < 1000; retries++ {
		if err := h.begin(0, h.ep0, 0, in, pid, out, limit, deadline); err != nil {
			return nil, err
		}
		for polls := 0; polls < 1000000; polls++ {
			done, data, nak, err := h.step()
			if err != nil {
				return nil, err
			}
			if done {
				if !nak {
					return data, nil
				}
				break
			}
			if polls&255 == 255 {
				runtime.Gosched()
			}
		}
		if h.pending.active {
			return nil, fmt.Errorf("dwc2: control poll budget exhausted")
		}
	}
	return nil, fmt.Errorf("dwc2: control NAK budget exhausted")
}
func (h *Host) request(requestType, request byte, value, index uint16, length int) (result []byte, err error) {
	phase, offset := "admit", 0
	defer func() {
		if err != nil {
			err = fmt.Errorf("dwc2: control address=%d type=%#02x request=%#02x value=%#04x index=%#04x length=%d phase=%s offset=%d ep0=%d: %w",
				h.address, requestType, request, value, index, length, phase, offset, h.ep0, err)
		}
	}()
	if length < 0 || length > usb.MaxConfigurationBytes || (length != 0 && requestType&0x80 == 0) {
		return nil, fmt.Errorf("dwc2: unsupported control request")
	}
	setup := []byte{requestType, request, byte(value), byte(value >> 8), byte(index), byte(index >> 8), byte(length), byte(length >> 8)}
	deadline := h.controlDeadline(h.io.Now().Add(2 * time.Second))
	phase = "setup"
	if _, err := h.transfer(false, 3, setup, 0, deadline); err != nil {
		return nil, err
	}
	var data []byte
	pid := uint32(2)
	for len(data) < length {
		phase, offset = "data", len(data)
		packet, err := h.transfer(true, pid, nil, min(int(h.ep0), length-len(data)), deadline)
		if err != nil {
			return nil, err
		}
		data = append(data, packet...)
		pid ^= 2
		if len(packet) < int(h.ep0) {
			break
		}
	}
	// No-data requests use IN status; control reads use OUT status. DATA1.
	phase, offset = "status", len(data)
	if _, err := h.transfer(length == 0, 2, nil, 0, deadline); err != nil {
		return nil, err
	}
	return data, nil
}
func (h *Host) enumerate() error {
	return h.discover()
}

// PollKeyboard never spins waiting for device input. NAK retires the current
// attempt and preserves the interrupt data toggle. Call regularly to progress.
func (h *Host) PollKeyboard() ([]byte, error) {
	if h.gamepadMode {
		return nil, fmt.Errorf("dwc2: gamepad cannot supply keyboard reports")
	}
	return h.pollInput()
}
func (h *Host) pollInput() ([]byte, error) {
	if h.fault != nil {
		return nil, h.fault
	}
	if h.pollingPaused {
		return nil, nil
	}
	if h.healthStage != 0 || !h.pending.active && len(h.path) > 0 && !h.io.Now().Before(h.healthNext) {
		until := h.io.Now().Add(2 * time.Millisecond)
		for n := 0; n < 8; n++ {
			_, err := h.pollHubHealth()
			if err != nil {
				return nil, h.fail(err)
			}
			if h.healthStage == 0 || !h.io.Now().Before(until) {
				break
			}
		}
		return nil, nil
	}
	if !h.pending.active {
		if h.io.Now().Before(h.next) {
			return nil, nil
		}
		limit := 8
		if h.gamepadMode {
			limit = h.gamepad.MaxReportBytes()
		}
		if err := h.begin(h.config.Endpoint, h.config.MaxPacketSize, 3, true, h.toggle, nil, limit, h.io.Now().Add(500*time.Millisecond)); err != nil {
			return nil, h.fail(err)
		}
		return nil, nil
	}
	done, data, nak, err := h.pollStep()
	if err != nil {
		return nil, h.fail(err)
	}
	if !done {
		return nil, nil
	}
	h.next = h.io.Now().Add(h.interval)
	if nak {
		return nil, nil
	}
	if h.gamepadMode {
		if _, _, err := h.gamepad.Feed(data); err != nil {
			return nil, h.fail(err)
		}
	} else if len(data) != 8 {
		return nil, h.fail(fmt.Errorf("dwc2: short boot keyboard report"))
	}
	h.toggle ^= 2
	return data, nil
}

// SPDX-License-Identifier: GPL-3.0-or-later

package dwc2

import (
	"encoding/binary"
	"fmt"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/usb"
)

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
	split := s.route.hub != 0
	ping := !in && !split && s.speed == usb.SpeedHigh && s.ep.TransferType == usb.TransferBulk && s.doPing

	// Check TX request queue & FIFO space.
	qReg := uint32(nptxstatus)
	if epType == uint32(usb.TransferInterrupt) {
		qReg = ptxstatus
	}
	q := h.io.Read32(qReg)
	outBytes := 0
	if !in && !completeSplit && !ping {
		outBytes = s.outLen
	}
	if (q>>16)&255 == 0 || (!in && !completeSplit && !ping && int(q&65535) < (outBytes+3)/4) {
		return nil // Queue busy; retry next Service().
	}

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
	tsiz := size | 1<<19 | s.toggle<<29
	if ping {
		tsiz = hctsizDoPng | 1<<19 | s.toggle<<29
	}
	h.io.Write32(chReg(ch, hctsiz), tsiz)

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
	s.haltDeadline = time.Time{}
	s.pinging = ping
	s.completeSplit = split && completeSplit
	s.waitingSplit = false
	s.received = false
	s.done = false
	s.nak = false
	s.ack = false
	s.nyet = false
	s.inLen = 0
	s.deadline = h.io.Now().Add(500 * time.Millisecond)

	h.io.Write32(chReg(ch, hcchar), flags|channelEnable)
	if !in && !completeSplit && !ping && outBytes > 0 {
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
				s.halting = false
				s.haltDeadline = time.Time{}
				s.completeSplit = false
				s.waitingSplit = false
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
	if h.irqSuspended.Load() {
		if err := h.pioCheck(); err != nil {
			return h.failController(err)
		}
		if err := h.resumeIRQ(); err != nil {
			if h.fault == nil {
				h.fault = err
			}
			return h.fault
		}
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
	hsOut := !s.ep.In() && !split && s.speed == usb.SpeedHigh && s.ep.TransferType == usb.TransferBulk
	ints := h.io.Read32(chReg(ch, hcint))
	fatal := fatalChannel
	if s.ep.In() && !split {
		fatal &^= dataToggle
	}
	if split || hsOut {
		fatal &^= 1 << 6 // NYET is part of split handshake and HS Bulk-OUT PING flow control
	}
	if ints&fatal != 0 {
		if err := h.haltChannel(ch); err != nil {
			_ = h.failController(err)
		}
		s.armed = false
		s.halting = false
		s.haltDeadline = time.Time{}
		s.pinging = false
		s.completeSplit = false
		s.waitingSplit = false
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
	wantAck := (split && !s.completeSplit) || s.pinging
	if !wantAck && ints&(1<<5) != 0 {
		h.io.Write32(chReg(ch, hcint), 1<<5)
		ints &^= 1 << 5
	}
	if ints&1 != 0 {
		if s.ep.In() && !s.received {
			if h.io.Read32(gint)&gintRXFLVL != 0 {
				if err := h.drainRXIntoSlots(256); err != nil {
					if h.fault == nil {
						_ = h.failController(err)
					}
					return
				}
				if h.fault != nil || s.fault != nil || !s.armed {
					return
				}
			}
			if !s.received {
				// Leave s.armed=true and HCINT intact until the IN payload is popped
				// from the RX FIFO; enforce s.deadline if payload never arrives.
				if !now.Before(s.deadline) {
					if err := h.haltChannel(ch); err != nil {
						_ = h.failController(err)
						return
					}
					s.armed = false
					s.halting = false
					s.haltDeadline = time.Time{}
					s.pinging = false
					s.completeSplit = false
					s.waitingSplit = false
					s.nextDue = now.Add(s.effectiveInterval())
				}
				return
			}
		}
		s.done = true
	}
	if ints&(1<<4) != 0 {
		s.nak = true
	}
	if wantAck && ints&(1<<5) != 0 {
		s.ack = true
	}
	if (split || hsOut) && ints&(1<<6) != 0 {
		s.nyet = true
	}
	if !s.done && !s.nak && !s.ack && !s.nyet {
		if !now.Before(s.deadline) {
			if err := h.haltChannel(ch); err != nil {
				_ = h.failController(err)
				return
			}
			s.armed = false
			s.halting = false
			s.haltDeadline = time.Time{}
			s.pinging = false
			s.completeSplit = false
			s.waitingSplit = false
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
			s.haltDeadline = now.Add(50 * time.Millisecond)
		} else if !now.Before(s.haltDeadline) {
			s.armed = false
			s.halting = false
			s.haltDeadline = time.Time{}
			s.pinging = false
			s.completeSplit = false
			s.waitingSplit = false
			err := fmt.Errorf("dwc2: ch %d failed to halt", ch)
			s.fault = err
			if !s.ep.In() && s.outPending {
				s.outPending = false
				s.outErr = err
			}
			_ = h.failController(err)
		}
		return
	}
	h.io.Write32(chReg(ch, hcint), 0x7ff)
	s.armed = false
	s.halting = false
	s.haltDeadline = time.Time{}

	if s.pinging {
		s.pinging = false
		if s.ack {
			// Device buffer is ready; clear PING state so Service immediately arms DATA OUT.
			s.doPing = false
			s.nextDue = now
		} else {
			// Device still busy (NAK); keep PING state and yield one microframe.
			s.doPing = true
			s.nextDue = now.Add(125 * time.Microsecond)
		}
		return
	}

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

	if hsOut && s.nyet {
		// USB 2.0 §8.5.1.1: HS Bulk-OUT NYET accepts the current DATA packet
		// and requires a PING before sending the next DATA packet.
		s.doPing = true
		s.toggle ^= 2
		s.outPending = false
		s.outDone = true
		return
	}

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
		if s.ep.In() {
			if !s.received {
				return
			}
			s.toggle ^= 2
			s.inReady = true
			s.inReadyLen = s.inLen
			s.nextDue = now.Add(s.effectiveInterval())
			if irqTS := h.irqTimestampNS.Swap(0); irqTS > 0 {
				h.latency.record(now.UnixNano() - irqTS)
			}
		} else {
			s.toggle ^= 2
			if hsOut {
				s.doPing = false
			}
			s.outPending = false
			s.outDone = true
			if s.ep.TransferType == usb.TransferInterrupt {
				s.nextDue = now.Add(s.effectiveInterval())
			}
		}
	}
}

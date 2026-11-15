// SPDX-License-Identifier: GPL-3.0-or-later
package dwc2

import (
	"encoding/binary"
	"fmt"
	"time"
)

type splitRoute struct{ hub, port uint32 }

// armPacket sends OUT bytes only with SSPLIT. CSPLIT OUT always has zero size
// and no FIFO write. ACK/NYET handling must retire the old channel first.
func (h *Host) armPacket(complete bool) error {
	p := &h.pending
	h.io.Write32(hcint, 0x7ff)
	h.io.Write32(hcmask, 0x7ff)
	split := uint32(0)
	if p.split {
		split = 1<<31 | 3<<14 | h.route.hub<<7 | h.route.port
		if complete {
			split |= 1 << 16
		}
	}
	h.io.Write32(hcsplit, split)
	size := p.transferSize
	if complete && !p.in {
		size = 0
	}
	h.io.Write32(hctsiz, size|1<<19|p.pid<<29)
	periodic := (p.char>>18)&3 == 3
	var selectionStart time.Time
	if periodic && !p.split {
		selectionStart = h.io.Now()
	}
	selected := h.io.Read32(hfnum)
	frame := uint16(selected)
	if p.split {
		if !complete {
			if frame&7 > 1 {
				p.waiting = true
				return nil
			}
			p.startFrame = frame
			if periodic {
				p.startFrame++
			}
		} else {
			wire := frame
			if periodic {
				wire++
			}
			if uint16(wire-p.startFrame) < 2 || wire>>3 != p.startFrame>>3 {
				return fmt.Errorf("dwc2: complete split arm missed translator window")
			}
		}
	}
	flags := p.char
	if periodic && !p.split {
		p.requestAfter = h.io.Now().Add(h.interval)
	}
	if (flags>>18)&3 == 3 && frame&1 == 0 {
		flags |= 1 << 29
	}
	if periodic && !p.split {
		reason := "arm"
		if p.toggleRetirements != 0 {
			reason = "toggle-rearm"
		}
		h.writePeriodicChannel(reason, flags|channelEnable, selected, selectionStart)
	} else {
		h.writeChannel("arm", flags|channelEnable)
	}
	if !complete {
		for i := 0; i < len(p.out); i += 4 {
			var word [4]byte
			copy(word[:], p.out[i:min(i+4, len(p.out))])
			h.io.Write32(fifo, binary.LittleEndian.Uint32(word[:]))
		}
	}
	return nil
}

// Split handshakes need microframe service even when the caller polls at a
// millisecond interval. Bound one call to 2ms/4096 steps; never wait for input.
func (h *Host) pollStep() (bool, []byte, bool, error) {
	if !h.pending.split {
		return h.step()
	}
	until := h.io.Now().Add(2 * time.Millisecond)
	for n := 0; n < 4096; n++ {
		done, data, nak, err := h.step()
		if done || err != nil {
			return done, data, nak, err
		}
		if !h.io.Now().Before(until) {
			break
		}
	}
	return false, nil, false, nil
}
